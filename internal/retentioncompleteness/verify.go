package retentioncompleteness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// VerifyOptions binds a retained completion record to one exact validation run.
type VerifyOptions struct {
	Retention      artifactretention.State
	Repo           string
	RecordPath     string
	ExpectedRecord *spec.RetainedValidationRecord
	ContractRaw    []byte
	Contract       spec.ChangeContract
	PolicyRaw      []byte
	Policy         spec.Policy
	EvidenceRaw    []byte
	PlanRaw        []byte
	ProofRaw       []byte
	TargetTree     string
}

// Verification describes the exact artifacts that passed post-write checks.
type Verification struct {
	Record     spec.RetainedValidationRecord
	RecordPath string
	Paths      []string
}

// Verify reads the exact retained record and every artifact it names through
// the safe content-addressed reader, then checks their identities and links.
func Verify(ctx context.Context, opts VerifyOptions) (Verification, error) {
	_ = ctx
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{opts.Retention.RepositoryEnabled(), errors.New("repository retention is not enabled")},
		{opts.Contract.BaselineLock != nil && len(opts.ContractRaw) > 0 && len(opts.PolicyRaw) > 0 && opts.TargetTree != "", errors.New("retained validation verification requires locked contract, policy, and target tree inputs")},
		{opts.RecordPath != "", errors.New("retained validation verification requires a record path")},
	} {
		if !check.ok {
			return Verification{}, check.err
		}
	}
	decodedContract, err := spec.DecodeChangeContract(opts.ContractRaw)
	if err != nil {
		return Verification{}, fmt.Errorf("decode validated Change Contract: %w", err)
	}
	if !reflect.DeepEqual(decodedContract, opts.Contract) {
		return Verification{}, errors.New("contract struct differs from the exact validated Change Contract bytes")
	}
	decodedPolicy, policyErr := decodeBoundPolicy(opts.PolicyRaw, opts.Policy)
	if policyErr != nil {
		return Verification{}, policyErr
	}
	opts.Policy = decodedPolicy
	recordRaw, err := readRetained(opts.Retention, opts.Repo, opts.RecordPath, classEvidence, "validation record")
	if err != nil {
		return Verification{}, err
	}
	if opts.ExpectedRecord != nil {
		expectedRaw, encodeErr := spec.EncodeRetainedValidationRecord(*opts.ExpectedRecord)
		if encodeErr != nil || !equalBytes(recordRaw, expectedRaw) {
			return Verification{}, errors.New("retained validation record bytes differ from the published record")
		}
	}
	record, err := spec.DecodeRetainedValidationRecord(recordRaw)
	if err != nil {
		return Verification{}, fmt.Errorf("decode retained validation record: %w", err)
	}
	if err := verifyRecordBinding(record, opts); err != nil {
		return Verification{}, err
	}
	paths, err := verifyRecordArtifacts(opts.Retention, opts.Repo, record, opts)
	if err != nil {
		return Verification{}, err
	}
	return Verification{Record: record, RecordPath: opts.RecordPath, Paths: paths}, nil
}

func verifyRecordBinding(record spec.RetainedValidationRecord, opts VerifyOptions) error {
	lock := opts.Contract.BaselineLock
	summary := opts.Policy.ValidationSummary()
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{record.ContractSHA256 == digestHex(opts.ContractRaw), errors.New("retained validation record is bound to a different Change Contract")},
		{record.PolicySHA256 == digestHex(opts.PolicyRaw) && record.PolicySHA256 == lock.PolicySHA256, errors.New("retained validation record is bound to a different Project Policy")},
		{record.GitObjectFormat == lock.GitObjectFormat && record.BaseCommit == lock.BaseCommit && record.BaseTree == lock.BaseTree && record.SpecificationSHA256 == lock.SpecificationSHA256, errors.New("retained validation record is bound to a different locked baseline")},
		{record.TargetTree == opts.TargetTree, errors.New("retained validation record is bound to a different target tree")},
		{record.ValidationLevel == summary.Level && equalStrings(record.EnabledGates, summary.EnabledGates) && equalStrings(record.DisabledGates, summary.DisabledGates), errors.New("retained validation record is bound to a different effective Project Policy")},
		{!opts.Contract.RequiresRedGreen() || record.RegressionPatchSHA256 != "", errors.New("retained validation record omits the required Red proof")},
	} {
		if !check.ok {
			return check.err
		}
	}
	return nil
}

func verifyRecordArtifacts(state artifactretention.State, repo string, record spec.RetainedValidationRecord, opts VerifyOptions) ([]string, error) {
	contractRaw, err := readCheckedRetained(state, repo, record.RetainedContract, classContracts, record.ContractSHA256, "Change Contract")
	if err != nil {
		return nil, err
	}
	if !equalBytes(contractRaw, opts.ContractRaw) {
		return nil, errors.New("retained Change Contract differs from the validated contract bytes")
	}
	decodedContract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil || !decodedContract.IsLockedStrictDevelopment() || decodedContract.BaselineLock == nil {
		return nil, errors.New("retained Change Contract is not a locked strict-development contract")
	}
	evidenceRaw, err := readCheckedRetained(state, repo, record.RetainedEvidence, classEvidence, record.EvidenceSHA256, "validation evidence")
	if err != nil {
		return nil, err
	}
	if len(opts.EvidenceRaw) > 0 && !equalBytes(evidenceRaw, opts.EvidenceRaw) {
		return nil, errors.New("retained validation evidence differs from the validated evidence bytes")
	}
	if err := verifyEvidenceOutcomes(evidenceRaw, record); err != nil {
		return nil, err
	}
	paths := []string{record.RetainedContract, record.RetainedEvidence}
	for _, item := range []struct {
		path, class, digest, label, expected, missingError, mismatchError string
	}{
		{record.RetainedPlan, classPlans, record.ImplementationPlanSHA256, "Implementation Plan", string(opts.PlanRaw), "retained validation record omits the selected Implementation Plan", "retained Implementation Plan differs from the validated plan bytes"},
		{record.RetainedProof, classProofs, record.RegressionPatchSHA256, "Red proof", string(opts.ProofRaw), "retained validation record omits the captured Red proof", "retained Red proof differs from the captured proof bytes"},
	} {
		if item.path == "" {
			if item.expected != "" {
				return nil, errors.New(item.missingError)
			}
			continue
		}
		raw, err := readCheckedRetained(state, repo, item.path, item.class, item.digest, item.label)
		if err != nil {
			return nil, err
		}
		if item.expected != "" && string(raw) != item.expected {
			return nil, errors.New(item.mismatchError)
		}
		if item.class == classPlans {
			plan, decodeErr := spec.DecodeImplementationPlan(raw)
			if decodeErr != nil {
				return nil, fmt.Errorf("decode retained Implementation Plan: %w", decodeErr)
			}
			if validateErr := plan.ValidateAgainst(decodedContract, contractRaw); validateErr != nil {
				return nil, fmt.Errorf("retained Implementation Plan is not bound to the validated contract: %w", validateErr)
			}
		}
		paths = append(paths, item.path)
	}
	sort.Strings(paths)
	return paths, nil
}

func readCheckedRetained(state artifactretention.State, repo, path, class, digest, label string) ([]byte, error) {
	raw, err := readRetained(state, repo, path, class, label)
	if err != nil {
		return nil, err
	}
	if digestHex(raw) != digest {
		return nil, fmt.Errorf("retained %s differs from the validated bytes", label)
	}
	return raw, nil
}

func verifyEvidenceOutcomes(raw []byte, record spec.RetainedValidationRecord) error {
	events, err := spec.DecodeEvidenceVersion(raw, spec.EvidenceVersionV3)
	if err != nil {
		return fmt.Errorf("decode retained validation evidence: %w", err)
	}
	var configured *spec.EvidenceEvent
	observed := make(map[string]spec.Status, len(record.ProducerGateStatuses))
	for index := range events {
		switch events[index].Event {
		case "validation_configured":
			if configured != nil {
				return errors.New("retained validation evidence repeats its configuration event")
			}
			configured = &events[index]
		case "gate_finished":
			if spec.IsProjectGate(events[index].Gate) {
				if _, ok := observed[events[index].Gate]; ok {
					return fmt.Errorf("retained validation evidence repeats gate outcome %q", events[index].Gate)
				}
				observed[events[index].Gate] = events[index].Status
			}
		}
	}
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{configured != nil && configured.Gate == "policy", errors.New("retained validation evidence is missing its configuration event")},
		{configured != nil && configured.ValidationLevel == record.ValidationLevel && equalStrings(configured.EnabledGates, record.EnabledGates) && equalStrings(configured.DisabledGates, record.DisabledGates) && equalStrings(configured.DeferredGates, record.DeferredGates), errors.New("retained validation evidence configuration differs from the completion record")},
		{reflect.DeepEqual(observed, record.ProducerGateStatuses), errors.New("retained validation evidence gate outcomes differ from the completion record")},
	} {
		if !check.ok {
			return check.err
		}
	}
	return nil
}

const (
	StatusComplete    = "complete"
	StatusIncomplete  = "incomplete"
	StatusStale       = "stale"
	StatusAmbiguous   = "ambiguous"
	StatusEmpty       = "empty"
	StatusExternal    = "external"
	StatusUnavailable = "unavailable"
)

// StatusOptions provides the current inputs used to judge retained readiness.
type StatusOptions struct {
	Retention   artifactretention.State
	Repo        string
	ContractRaw []byte
	Contract    spec.ChangeContract
	PolicyRaw   []byte
	Policy      spec.Policy
	TargetTree  string
}

// StatusAssessment is the agent-facing readiness projection for repository
// retention. It never turns missing or unbound evidence into completion.
type StatusAssessment struct {
	State       string
	Detail      string
	Record      *spec.RetainedValidationRecord
	RecordPaths []string
	Corrective  string
}

// AssessStatus classifies retained workflow evidence for status/readiness.
func AssessStatus(ctx context.Context, opts StatusOptions) StatusAssessment {
	if !opts.Retention.RepositoryEnabled() {
		return StatusAssessment{State: StatusExternal, Detail: "external retention does not publish repository completeness evidence", Corrective: "none"}
	}
	for _, check := range []struct {
		ok         bool
		assessment StatusAssessment
	}{
		{len(opts.ContractRaw) > 0 && opts.Contract.BaselineLock != nil && len(opts.PolicyRaw) > 0, StatusAssessment{State: StatusEmpty, Detail: "no locked Change Contract and policy are available for retained readiness", Corrective: "polis start"}},
		{opts.TargetTree != "", StatusAssessment{State: StatusIncomplete, Detail: "current source tree cannot be captured for retained readiness", Corrective: "inspect the Git index"}},
	} {
		if !check.ok {
			return check.assessment
		}
	}
	paths, err := opts.Retention.ListPaths(opts.Repo, classEvidence)
	if err != nil {
		return StatusAssessment{State: StatusIncomplete, Detail: err.Error(), Corrective: "polis workspace validate"}
	}
	candidates, stale, unreadable := collectStatusCandidates(ctx, opts, paths)
	if len(candidates) > 1 && retainedCandidatesConflict(candidates) {
		conflicting := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			conflicting = append(conflicting, candidate.path)
		}
		sort.Strings(conflicting)
		return StatusAssessment{State: StatusAmbiguous, Detail: "multiple retained validation records describe the current run", RecordPaths: conflicting, Corrective: "inspect retained validation evidence"}
	}
	if len(unreadable) > 0 {
		sort.Strings(unreadable)
		return StatusAssessment{
			State: StatusIncomplete, Detail: "retained validation evidence for the current run is unreadable or unbound",
			RecordPaths: unreadable, Corrective: "polis workspace validate",
		}
	}
	if len(candidates) > 0 {
		selected := preferredCurrentCandidate(candidates)
		currentPaths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			currentPaths = append(currentPaths, candidate.path)
		}
		sort.Strings(currentPaths)
		return StatusAssessment{
			State: StatusComplete, Detail: "current retained validation evidence is complete and verified",
			Record: &selected.record, RecordPaths: currentPaths, Corrective: "none",
		}
	}
	if len(stale) > 0 {
		return StatusAssessment{
			State: StatusStale, Detail: "retained validation evidence belongs to a different source tree or baseline",
			RecordPaths: stale, Corrective: "polis workspace validate",
		}
	}
	return StatusAssessment{
		State: StatusIncomplete, Detail: "no retained validation record is bound to the current locked workflow",
		Corrective: "polis workspace validate",
	}
}

type statusCandidate struct {
	path   string
	record spec.RetainedValidationRecord
}

func collectStatusCandidates(ctx context.Context, opts StatusOptions, paths []string) ([]statusCandidate, []string, []string) {
	candidates := make([]statusCandidate, 0)
	stale := make([]string, 0)
	unreadable := make([]string, 0)
	contractDigest := digestHex(opts.ContractRaw)
	policyDigest := digestHex(opts.PolicyRaw)
	for _, path := range paths {
		raw, err := readRetained(opts.Retention, opts.Repo, path, classEvidence, "validation record")
		if err != nil {
			unreadable = append(unreadable, path)
			continue
		}
		record, err := spec.DecodeRetainedValidationRecord(raw)
		if err != nil {
			continue
		}
		if record.ContractSHA256 == contractDigest && record.PolicySHA256 == policyDigest {
			if record.TargetTree != opts.TargetTree || record.BaseCommit != opts.Contract.BaselineLock.BaseCommit {
				stale = append(stale, path)
				continue
			}
			if _, err := Verify(ctx, VerifyOptions{
				Retention: opts.Retention, Repo: opts.Repo, RecordPath: path,
				ContractRaw: opts.ContractRaw, Contract: opts.Contract,
				PolicyRaw: opts.PolicyRaw, Policy: opts.Policy, TargetTree: opts.TargetTree,
			}); err != nil {
				unreadable = append(unreadable, path)
				continue
			}
			candidates = append(candidates, statusCandidate{path: path, record: record})
			continue
		}
		if isSupersededBaselineRecord(record, opts, contractDigest, policyDigest) {
			stale = append(stale, path)
		}
	}
	return candidates, stale, unreadable
}

// isSupersededBaselineRecord identifies evidence from an earlier lock of the
// same specification and policy. Records from other specifications are
// legitimate artifacts of another contract and are not readiness evidence here.
func isSupersededBaselineRecord(record spec.RetainedValidationRecord, opts StatusOptions, contractDigest, policyDigest string) bool {
	lock := opts.Contract.BaselineLock
	return record.ContractSHA256 != contractDigest &&
		record.PolicySHA256 == policyDigest &&
		record.SpecificationSHA256 == lock.SpecificationSHA256 &&
		(record.BaseCommit != lock.BaseCommit || record.BaseTree != lock.BaseTree || record.TargetTree != opts.TargetTree)
}

// preferredCurrentCandidate selects the strongest verified current record. A
// build record supersedes a workspace record because it also binds the emitted
// validation evidence at delivery completion; both remain current proof paths.
func preferredCurrentCandidate(candidates []statusCandidate) statusCandidate {
	for _, candidate := range candidates {
		if candidate.record.Operation == spec.RetainedValidationOperationBuild {
			return candidate
		}
	}
	return candidates[0]
}

func retainedCandidatesConflict(candidates []statusCandidate) bool {
	operations := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		operations[candidate.record.Operation]++
	}
	for _, count := range operations {
		if count > 1 {
			return true
		}
	}
	return false
}

// FormatAssessmentDetails renders bounded readiness details for CLI output.
func FormatAssessmentDetails(assessment StatusAssessment) string {
	parts := []string{assessment.Detail}
	if len(assessment.RecordPaths) > 0 {
		parts = append(parts, "paths: "+describePaths(assessment.RecordPaths))
	}
	if len(assessment.RecordPaths) == 0 && assessment.Record != nil {
		parts = append(parts, "operation: "+assessment.Record.Operation)
	}
	return strings.Join(parts, "; ")
}
