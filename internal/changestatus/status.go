package changestatus

import (
	"bytes"
	"context"
	"fmt"
	"reflect"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type EvidenceState string

const (
	EvidenceComplete    EvidenceState = "complete"
	EvidenceMissing     EvidenceState = "missing"
	EvidenceIncomplete  EvidenceState = "incomplete"
	EvidenceNotRequired EvidenceState = "not_required"
)

type Options struct {
	Repo            string
	Policy          string
	Contract        string
	RegressionPatch string
	Artifact        string
}

type ContractSummary struct {
	Source            string `json:"source"`
	SchemaVersion     int    `json:"schema_version"`
	Kind              string `json:"kind"`
	DevelopmentMethod string `json:"development_method"`
	RegressionMode    string `json:"regression_mode"`
}

type ArtifactSummary struct {
	Path       string `json:"path"`
	Project    string `json:"project"`
	Change     string `json:"change"`
	TargetTree string `json:"target_tree"`
}

type EvidenceSummary struct {
	ID     string        `json:"id"`
	State  EvidenceState `json:"state"`
	Detail string        `json:"detail,omitempty"`
}

type GateSummary struct {
	ID     string        `json:"id"`
	State  EvidenceState `json:"state"`
	Status spec.Status   `json:"status,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

type NextAction struct {
	Action  string `json:"action"`
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason"`
}

type Report struct {
	Status        string             `json:"status"`
	State         string             `json:"state"`
	Consistent    bool               `json:"consistent"`
	Contract      *ContractSummary   `json:"contract,omitempty"`
	Baseline      *spec.BaselineLock `json:"baseline,omitempty"`
	Artifact      *ArtifactSummary   `json:"artifact,omitempty"`
	Evidence      []EvidenceSummary  `json:"evidence"`
	EnabledGates  []string           `json:"enabled_gates"`
	DisabledGates []string           `json:"disabled_gates"`
	DeferredGates []string           `json:"deferred_gates"`
	Gates         []GateSummary      `json:"gates"`
	NextAction    NextAction         `json:"next_action"`
	Problems      []string           `json:"problems,omitempty"`
}

func Summarize(ctx context.Context, opts Options) (Report, error) {
	report := emptyReport()
	repo, err := gitutil.ResolveRoot(ctx, opts.Repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return report, err
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		return inconsistent(report, "artifact retention: "+err.Error()), nil
	}

	var contract spec.ChangeContract
	haveContract := false
	contractSource := ""
	if opts.Contract != "" {
		loaded, err := loadContract(repo, opts.Contract, retention)
		if err != nil {
			return inconsistent(report, "locked contract: "+err.Error()), nil
		}
		contract = loaded
		haveContract = true
		contractSource = opts.Contract
	}

	var pkg *packageverify.Package
	if opts.Artifact != "" {
		if _, err := retention.ReadInput(repo, opts.Artifact, "packages", packageverify.MaxArchiveBytes, "input exceeds maximum size %d"); err != nil {
			report.Evidence = baseEvidence(haveContract, false, false, false)
			return inconsistent(report, "delivery package: "+err.Error()), nil
		}
		loaded, err := packageverify.Load(opts.Artifact)
		if err != nil {
			report.Evidence = baseEvidence(haveContract, false, false, false)
			if len(report.Evidence) == 4 {
				report.Evidence[3] = EvidenceSummary{ID: "delivery_package", State: EvidenceIncomplete, Detail: err.Error()}
			}
			return inconsistent(report, "delivery package: "+err.Error()), nil
		}
		pkg = &loaded
		if haveContract {
			if !reflect.DeepEqual(contract, loaded.Change) {
				return inconsistent(report, "explicit locked contract does not match the contract embedded in the delivery package"), nil
			}
		} else {
			contract = loaded.Change
			haveContract = true
			contractSource = "embedded in " + opts.Artifact
		}
	}

	if !haveContract {
		if opts.RegressionPatch != "" {
			return inconsistent(report, "regression proof was supplied without a locked contract or delivery package"), nil
		}
		report.Evidence = baseEvidence(false, false, false, false)
		report.NextAction = NextAction{Action: "start", Command: "polis start", Reason: "no persisted locked Change Contract was supplied"}
		return report, nil
	}

	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 || contract.BaselineLock == nil {
		return inconsistent(report, "Change Contract is not a locked schema-v4 or schema-v6 contract produced by polis start"), nil
	}
	lock := *contract.BaselineLock
	report.Contract = &ContractSummary{
		Source: contractSource, SchemaVersion: contract.SchemaVersion, Kind: contract.Kind,
		DevelopmentMethod: contract.DevelopmentMethod, RegressionMode: contract.Regression.Mode,
	}
	report.Baseline = &lock

	var plan policyplan.Plan
	if pkg != nil {
		plan, err = policyplan.CompileWithOptions(pkg.Policy, policyplan.CompileOptions{DeferredGates: pkg.Result.DeferredGates})
		if err != nil {
			return inconsistent(report, "packaged Project Policy cannot produce the persisted execution plan: "+err.Error()), nil
		}
		if opts.Policy != "" {
			policyRaw, policy, err := policyload.LoadExternal(repo, opts.Policy)
			if err != nil {
				return inconsistent(report, "Project Policy: "+err.Error()), nil
			}
			if err := devlock.ValidatePolicy(contract, policyRaw); err != nil {
				return inconsistent(report, "Project Policy does not match the locked baseline: "+err.Error()), nil
			}
			if !reflect.DeepEqual(policy, pkg.Policy) {
				return inconsistent(report, "explicit Project Policy does not match the policy embedded in the delivery package"), nil
			}
		}
	} else {
		policyRaw, policy, err := loadPolicy(ctx, repo, opts.Policy)
		if err != nil {
			return inconsistent(report, "Project Policy: "+err.Error()), nil
		}
		if err := devlock.ValidatePolicy(contract, policyRaw); err != nil {
			return inconsistent(report, "Project Policy does not match the locked baseline: "+err.Error()), nil
		}
		plan, err = policyplan.Compile(policy)
		if err != nil {
			return inconsistent(report, "Project Policy cannot produce an execution plan: "+err.Error()), nil
		}
	}
	report.EnabledGates = append([]string{}, plan.EnabledGates...)
	report.DisabledGates = append([]string{}, plan.DisabledGates...)
	report.DeferredGates = append([]string{}, plan.DeferredGates...)

	var proof []byte
	if opts.RegressionPatch != "" {
		if !contract.RequiresRedGreen() {
			return inconsistent(report, "regression proof was supplied for a Change Contract that does not require Red-to-Green proof"), nil
		}
		proof, err = retention.ReadInput(repo, opts.RegressionPatch, "proofs", int64(spec.MaxPatchMemberBytes), "input exceeds maximum size %d")
		if err != nil {
			return inconsistent(report, "regression proof: "+err.Error()), nil
		}
		if len(proof) == 0 {
			return inconsistent(report, "regression proof is empty"), nil
		}
	}
	if pkg != nil && len(proof) > 0 && !bytes.Equal(proof, pkg.RegressionPatch) {
		return inconsistent(report, "explicit regression proof does not match the proof embedded in the delivery package"), nil
	}

	var events []spec.EvidenceEvent
	if pkg != nil {
		evidenceVersion, err := spec.EvidenceVersionForFormat(pkg.Manifest.FormatVersion)
		if err != nil {
			return inconsistent(report, "delivery evidence version: "+err.Error()), nil
		}
		events, err = spec.DecodeEvidenceVersion(pkg.Evidence, evidenceVersion)
		if err != nil {
			return inconsistent(report, "delivery evidence: "+err.Error()), nil
		}
	}
	report.Evidence = summarizeEvidence(contract, len(proof) > 0, pkg != nil)
	report.Gates = summarizeGates(plan, events, pkg != nil)

	if pkg != nil {
		report.State = "built"
		report.Artifact = &ArtifactSummary{Path: opts.Artifact, Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, TargetTree: pkg.Manifest.TargetTree}
		reason := "the persisted delivery package and its evidence validate; consumer preflight is the next canonical check"
		if len(plan.DeferredGates) > 0 {
			reason = "the persisted delivery package is valid and deferred gates still require consumer validation"
		}
		report.NextAction = NextAction{Action: "preflight", Command: "polis preflight", Reason: reason}
		return report, nil
	}

	if contract.RequiresRedGreen() && len(proof) == 0 {
		if err := devlock.ValidateRepository(ctx, repo, contract); err != nil {
			return inconsistent(report, "locked baseline is not valid for capture-red: "+err.Error()), nil
		}
		report.State = "locked"
		report.NextAction = NextAction{Action: "capture_red", Command: "polis capture-red", Reason: "the locked Red-to-Green contract has no persisted regression proof"}
		return report, nil
	}

	if err := devlock.ValidateBuildRepository(ctx, repo, contract); err != nil {
		return inconsistent(report, "locked baseline is not valid for build: "+err.Error()), nil
	}
	report.State = "ready_to_build"
	reason := "the locked contract does not require a Red proof"
	if contract.RequiresRedGreen() {
		reason = "a regression proof is persisted; build must validate and bind it to the locked contract"
	}
	report.NextAction = NextAction{Action: "build", Command: "polis build", Reason: reason}
	return report, nil
}

func emptyReport() Report {
	return Report{
		Status: "PASS", State: "unlocked", Consistent: true,
		Evidence: []EvidenceSummary{}, EnabledGates: []string{}, DisabledGates: []string{}, DeferredGates: []string{}, Gates: []GateSummary{}, Problems: []string{},
	}
}

func inconsistent(report Report, problem string) Report {
	report.Status = "FAIL"
	report.State = "inconsistent"
	report.Consistent = false
	report.Problems = append(report.Problems, problem)
	report.NextAction = NextAction{Action: "repair", Reason: "resolve the reported persisted-state inconsistency before continuing the workflow"}
	return report
}

func loadContract(repo, filename string, retention artifactretention.State) (spec.ChangeContract, error) {
	raw, err := retention.ReadInput(repo, filename, "contracts", int64(spec.MaxContractMemberBytes), "input exceeds maximum size %d")
	if err != nil {
		return spec.ChangeContract{}, err
	}
	contract, err := spec.DecodeChangeContract(raw)
	if err != nil {
		return spec.ChangeContract{}, fmt.Errorf("invalid Change Contract: %w", err)
	}
	return contract, nil
}

func loadPolicy(ctx context.Context, repo, filename string) ([]byte, spec.Policy, error) {
	if filename != "" {
		return policyload.LoadExternal(repo, filename)
	}
	return policyload.LoadCommitted(ctx, repo)
}

func baseEvidence(contract, regression, producer, pkg bool) []EvidenceSummary {
	contractState := EvidenceMissing
	if contract {
		contractState = EvidenceComplete
	}
	regressionState := EvidenceMissing
	if regression {
		regressionState = EvidenceIncomplete
	}
	producerState := EvidenceMissing
	if producer {
		producerState = EvidenceComplete
	}
	packageState := EvidenceMissing
	if pkg {
		packageState = EvidenceComplete
	}
	return []EvidenceSummary{
		{ID: "locked_contract", State: contractState},
		{ID: "regression_proof", State: regressionState},
		{ID: "producer_evidence", State: producerState},
		{ID: "delivery_package", State: packageState},
	}
}

func summarizeEvidence(contract spec.ChangeContract, proofPresent, packagePresent bool) []EvidenceSummary {
	items := baseEvidence(true, proofPresent, packagePresent, packagePresent)
	if !contract.RequiresRedGreen() {
		items[1] = EvidenceSummary{ID: "regression_proof", State: EvidenceNotRequired}
	} else if packagePresent {
		items[1] = EvidenceSummary{ID: "regression_proof", State: EvidenceComplete, Detail: "validated and bound by the delivery package"}
	} else if proofPresent {
		items[1] = EvidenceSummary{ID: "regression_proof", State: EvidenceIncomplete, Detail: "persisted proof is present but is not bound to the locked contract until a successful build"}
	}
	return items
}

func summarizeGates(plan policyplan.Plan, events []spec.EvidenceEvent, packagePresent bool) []GateSummary {
	finished := make(map[string]spec.EvidenceEvent, len(events))
	for _, event := range events {
		if event.Event == "gate_finished" {
			finished[event.Gate] = event
		}
	}
	gates := make([]GateSummary, 0, len(plan.Gates))
	for _, gate := range plan.Gates {
		entry := GateSummary{ID: gate.ID}
		if gate.State == policyplan.GateStateDisabled {
			entry.State = EvidenceNotRequired
			if event, ok := finished[gate.ID]; ok {
				entry.Status = event.Status
				entry.Reason = evidenceReason(event)
			}
			gates = append(gates, entry)
			continue
		}
		if !packagePresent {
			entry.State = EvidenceMissing
			gates = append(gates, entry)
			continue
		}
		event, ok := finished[gate.ID]
		if !ok {
			entry.State = EvidenceMissing
			gates = append(gates, entry)
			continue
		}
		entry.Status = event.Status
		entry.Reason = evidenceReason(event)
		switch event.Status {
		case spec.StatusPass:
			entry.State = EvidenceComplete
		case spec.StatusDeferred, spec.StatusFail, spec.StatusBlocked:
			entry.State = EvidenceIncomplete
		default:
			entry.State = EvidenceIncomplete
		}
		gates = append(gates, entry)
	}
	return gates
}

func evidenceReason(event spec.EvidenceEvent) string {
	if event.Reason == nil {
		return ""
	}
	return *event.Reason
}
