package changestatus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/internal/redcapture"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	StateUnavailable                = "unavailable"
	StateEmpty                      = "empty"
	StateAmbiguous                  = "ambiguous"
	StateBlocked                    = "blocked"
	StateRedProofMissing            = "red_proof_missing"
	StateImplementationPending      = "implementation_pending"
	StateReadyToBuild               = "ready_to_build"
	StateConsumerValidationRequired = "consumer_validation_required"
	StateComplete                   = "complete"
	StateInconsistent               = "inconsistent"

	StageComplete    = "complete"
	StageMissing     = "missing"
	StageNotRequired = "not_required"
	StageAmbiguous   = "ambiguous"
	StageIncomplete  = "incomplete"
)

type Options struct {
	Repo               string
	Policy             string
	Contract           string
	ImplementationPlan string
	RegressionPatch    string
	Package            string
}

type Result struct {
	Status              string            `json:"status"`
	State               string            `json:"state"`
	RetentionMode       string            `json:"retention_mode"`
	Consistent          bool              `json:"consistent"`
	Contract            *ContractSummary  `json:"contract,omitempty"`
	Baseline            *BaselineSummary  `json:"baseline,omitempty"`
	ImplementationPlan  StageSummary      `json:"implementation_plan"`
	RedProof            StageSummary      `json:"red_proof"`
	Package             PackageSummary    `json:"package"`
	Evidence            StageSummary      `json:"evidence"`
	Gates               []GateSummary     `json:"gates"`
	Workspace           *WorkspaceSummary `json:"workspace,omitempty"`
	Proven              []string          `json:"proven"`
	StaleOrUnproven     []string          `json:"stale_or_unproven"`
	Missing             []string          `json:"missing"`
	NextAction          *NextAction       `json:"next_action,omitempty"`
	CandidateContracts  []string          `json:"candidate_contracts,omitempty"`
	Problems            []string          `json:"problems,omitempty"`
	selectedContractRaw []byte
}

type WorkspaceSummary struct {
	Path                         string   `json:"path"`
	CheckpointState              string   `json:"checkpoint_state"`
	RecordedValidationStatus     string   `json:"recorded_validation_status"`
	ReportAuthenticated          bool     `json:"report_authenticated"`
	CurrentValidationEstablished bool     `json:"current_validation_established"`
	ProofInputDigestsBound       bool     `json:"proof_input_digests_bound"`
	DeliveryArtifactVerified     bool     `json:"delivery_artifact_verified"`
	RecordedTargetTree           string   `json:"recorded_target_tree"`
	CurrentTargetTree            *string  `json:"current_target_tree"`
	Differences                  []string `json:"differences"`
}

type ContractSummary struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	SchemaVersion  int    `json:"schema_version"`
	Kind           string `json:"kind"`
	RegressionMode string `json:"regression_mode"`
}

type BaselineSummary struct {
	GitObjectFormat     string `json:"git_object_format"`
	BaseCommit          string `json:"base_commit"`
	BaseTree            string `json:"base_tree"`
	PolicySHA256        string `json:"policy_sha256"`
	SpecificationSHA256 string `json:"specification_sha256"`
	Resolvable          bool   `json:"resolvable"`
	RepositoryRelation  string `json:"repository_relation"`
	PolicyStatus        string `json:"policy_status"`
}

type StageSummary struct {
	Status          string   `json:"status"`
	Paths           []string `json:"paths,omitempty"`
	IncompletePaths []string `json:"incomplete_paths,omitempty"`
	Detail          string   `json:"detail,omitempty"`
}

type PackageSummary struct {
	Status                     string   `json:"status"`
	Paths                      []string `json:"paths,omitempty"`
	Project                    string   `json:"project,omitempty"`
	Change                     string   `json:"change,omitempty"`
	TargetTree                 string   `json:"target_tree,omitempty"`
	ConsumerValidationRequired bool     `json:"consumer_validation_required,omitempty"`
}

type GateSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type NextAction struct {
	Action  string `json:"action"`
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason"`
}

type retainedArtifact struct {
	path string
	raw  []byte
}

type contractCandidate struct {
	path     string
	raw      []byte
	contract spec.ChangeContract
	digest   string
}

type packageCandidate struct {
	path string
	pkg  packageverify.Package
}

func Derive(ctx context.Context, opts Options) (Result, error) {
	root, err := gitutil.ResolveRoot(ctx, opts.Repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return Result{}, err
	}
	retention, err := artifactretention.Load(ctx, root)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Status:             "PASS",
		State:              StateUnavailable,
		RetentionMode:      string(retention.Mode()),
		Consistent:         true,
		ImplementationPlan: StageSummary{Status: StageMissing},
		RedProof:           StageSummary{Status: StageMissing},
		Package:            PackageSummary{Status: StageMissing},
		Evidence:           StageSummary{Status: StageMissing},
		Gates:              []GateSummary{},
		Proven:             []string{},
		StaleOrUnproven:    []string{},
		Missing:            []string{},
	}

	var explicitPackage *packageCandidate
	if opts.Package != "" {
		candidate, err := loadPackageInput(retention, root, opts.Package)
		if err != nil {
			return Result{}, fmt.Errorf("load explicit package: %w", err)
		}
		explicitPackage = &candidate
	}

	var selected *contractCandidate
	if opts.Contract != "" {
		candidate, err := loadContractInput(retention, root, opts.Contract)
		if err != nil {
			return Result{}, fmt.Errorf("load explicit locked Change Contract: %w", err)
		}
		selected = &candidate
	} else if explicitPackage != nil {
		candidate := contractCandidateFromPackage(*explicitPackage)
		selected = &candidate
	} else if retention.RepositoryEnabled() {
		contracts, contractProblems := loadContracts(retention, root)
		result.Problems = append(result.Problems, contractProblems...)
		for _, candidate := range contracts {
			result.CandidateContracts = append(result.CandidateContracts, candidate.path)
		}
		var selectionProblem string
		selected, selectionProblem = selectContract(root, "", contracts)
		if selectionProblem != "" {
			result.Problems = append(result.Problems, selectionProblem)
			result.State = StateAmbiguous
			result.Consistent = false
			result.Status = "FAIL"
			return result, nil
		}
	} else {
		result.Problems = []string{"external artifact retention has no discoverable workflow state; pass --contract or --package to reconstruct it explicitly"}
		return result, nil
	}
	if selected == nil {
		if len(result.Problems) > 0 {
			return inconsistent(result), nil
		}
		result.State = StateEmpty
		result.NextAction = &NextAction{Action: "start", Command: "polis start", Reason: "no locked Change Contract is persisted"}
		return result, nil
	}
	result.Contract = &ContractSummary{
		Path: selected.path, SHA256: selected.digest, SchemaVersion: selected.contract.SchemaVersion,
		Kind: selected.contract.Kind, RegressionMode: selected.contract.Regression.Mode,
	}
	result.selectedContractRaw = append([]byte(nil), selected.raw...)
	result.Baseline = deriveBaseline(ctx, root, selected.contract)
	if opts.Policy != "" {
		policyRaw, policy, err := policyload.LoadExternal(root, opts.Policy)
		if err != nil {
			return Result{}, fmt.Errorf("load explicit Project Policy: %w", err)
		}
		if err := devlock.ValidatePolicy(selected.contract, policyRaw); err != nil {
			result.Baseline.PolicyStatus = "mismatch"
			result.Problems = append(result.Problems, "explicit Project Policy does not match the locked baseline policy")
		} else {
			result.Baseline.PolicyStatus = "match"
			result.Gates = missingGateSummaries(policy)
		}
	}

	plans, planProblems := workflowArtifacts(retention, root, "plans", opts.ImplementationPlan, int64(spec.MaxImplementationPlanBytes), "implementation plan exceeds maximum size")
	result.Problems = append(result.Problems, planProblems...)
	linkedPlans, incompletePlans := linkedPlanPaths(plans, *selected)
	if len(linkedPlans) == 1 {
		result.ImplementationPlan = StageSummary{Status: StageComplete, Paths: linkedPlans, IncompletePaths: incompletePlans}
		if len(incompletePlans) > 0 {
			result.ImplementationPlan.Detail = "some retained plans bound to this contract do not validate"
		}
	} else if len(linkedPlans) > 1 {
		result.ImplementationPlan = StageSummary{Status: StageAmbiguous, Paths: linkedPlans, IncompletePaths: incompletePlans}
		result.Problems = append(result.Problems, "multiple retained implementation plans validate against the selected contract")
	} else if len(incompletePlans) > 0 {
		result.ImplementationPlan = StageSummary{
			Status: StageIncomplete, IncompletePaths: incompletePlans,
			Detail: "retained plan candidates bound to this contract do not validate",
		}
	} else if opts.ImplementationPlan != "" && len(plans) == 1 {
		result.ImplementationPlan = StageSummary{
			Status: StageIncomplete, IncompletePaths: []string{plans[0].path},
			Detail: "explicit implementation plan is not bound to the selected contract",
		}
	}

	packages, packageProblems := loadWorkflowPackages(retention, root, explicitPackage)
	result.Problems = append(result.Problems, packageProblems...)
	linkedPackages := make([]packageCandidate, 0)
	for _, candidate := range packages {
		if bytes.Equal(candidate.pkg.ChangeRaw, selected.raw) {
			linkedPackages = append(linkedPackages, candidate)
		}
	}
	if explicitPackage != nil && len(linkedPackages) == 0 {
		result.Problems = append(result.Problems, "explicit package is not bound to the selected Change Contract")
	}
	if len(linkedPackages) > 1 {
		for _, candidate := range linkedPackages {
			result.Package.Paths = append(result.Package.Paths, candidate.path)
		}
		result.Package.Status = StageAmbiguous
		result.Problems = append(result.Problems, "multiple verified retained packages are linked to the selected contract")
		return inconsistent(result), nil
	}

	proofs, proofProblems := workflowArtifacts(retention, root, "proofs", opts.RegressionPatch, int64(spec.MaxPatchMemberBytes), "input exceeds maximum size")
	result.Problems = append(result.Problems, proofProblems...)
	validProofs, incompleteProofs := validProofPaths(ctx, root, proofs, selected.contract)
	if selected.contract.RequiresRedGreen() {
		if len(validProofs) > 0 {
			result.RedProof = StageSummary{Status: StageComplete, Paths: validProofs, IncompletePaths: incompleteProofs}
			if len(incompleteProofs) > 0 {
				result.RedProof.Detail = "some retained proofs apply within the locked test scope but fail the Red oracle"
			}
		} else if len(incompleteProofs) > 0 {
			result.RedProof = StageSummary{
				Status: StageIncomplete, IncompletePaths: incompleteProofs,
				Detail: "retained proofs apply within the locked test scope but fail the Red oracle",
			}
		} else if opts.RegressionPatch != "" && len(proofs) == 1 {
			result.RedProof = StageSummary{
				Status: StageIncomplete, IncompletePaths: []string{proofs[0].path},
				Detail: "explicit regression patch is not a valid Red proof for the selected contract",
			}
		}
	} else {
		result.RedProof = StageSummary{Status: StageNotRequired}
	}

	if len(linkedPackages) == 1 {
		applyPackage(&result, linkedPackages[0])
	}
	if len(result.Problems) > 0 {
		return inconsistent(result), nil
	}
	if result.Package.Status == StageComplete {
		if result.Package.ConsumerValidationRequired {
			result.State = StateConsumerValidationRequired
			result.NextAction = &NextAction{Action: "consumer_validation", Command: "polis preflight", Reason: "the verified package contains deferred producer gates that require consumer validation"}
		} else {
			result.State = StateComplete
		}
		return result, nil
	}

	if opts.Policy == "" {
		policyRaw, policy, policyErr := policyload.LoadCommitted(ctx, root)
		if policyErr == nil {
			if err := devlock.ValidatePolicy(selected.contract, policyRaw); err != nil {
				result.Baseline.PolicyStatus = "mismatch"
			} else {
				result.Baseline.PolicyStatus = "match"
				result.Gates = missingGateSummaries(policy)
			}
		} else {
			result.Baseline.PolicyStatus = "unavailable"
		}
	}

	if !result.Baseline.Resolvable {
		result.State = StateBlocked
		result.Problems = append(result.Problems, "the locked baseline cannot be resolved and validated in the current repository")
		return result, nil
	}
	if result.Baseline.PolicyStatus != "match" {
		result.State = StateBlocked
		result.Problems = append(result.Problems, "the current committed Project Policy does not match the locked baseline policy")
		return result, nil
	}
	if selected.contract.RequiresRedGreen() && result.RedProof.Status != StageComplete {
		result.State = StateRedProofMissing
		if result.Baseline.RepositoryRelation == "at_baseline" {
			result.NextAction = &NextAction{Action: "capture_red", Command: "polis capture-red", Reason: "a validated persisted Red proof is required before implementation can be packaged"}
		} else {
			result.State = StateBlocked
			result.Problems = append(result.Problems, "capture-red requires HEAD at the locked baseline before a persisted Red proof exists")
		}
		return result, nil
	}
	if result.Baseline.RepositoryRelation != "at_baseline" && result.Baseline.RepositoryRelation != "descendant" {
		result.State = StateBlocked
		result.Problems = append(result.Problems, "the locked baseline is not an ancestor of current HEAD")
		return result, nil
	}
	result.State = StateImplementationPending
	result.NextAction = &NextAction{Action: "implement", Reason: "no verified package records completion; continue the locked implementation, then run polis build"}
	return result, nil
}

// SelectedContractRaw returns the exact validated locked Change Contract bytes
// used to derive this result. Callers can reuse those bytes for read-only
// projections without reopening a weaker or different artifact path.
func (result Result) SelectedContractRaw() []byte {
	return append([]byte(nil), result.selectedContractRaw...)
}

func loadContracts(retention artifactretention.State, repo string) ([]contractCandidate, []string) {
	artifacts, problems := loadArtifacts(retention, repo, "contracts", int64(spec.MaxContractMemberBytes), "Change Contract exceeds maximum size")
	candidates := make([]contractCandidate, 0, len(artifacts))
	for _, artifact := range artifacts {
		contract, err := spec.DecodeChangeContract(artifact.raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invalid retained contract %s: %v", artifact.path, err))
			continue
		}
		if !contract.IsLockedStrictDevelopment() || contract.BaselineLock == nil || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 {
			problems = append(problems, fmt.Sprintf("retained contract %s is not a locked strict-development contract", artifact.path))
			continue
		}
		sum := sha256.Sum256(artifact.raw)
		candidates = append(candidates, contractCandidate{path: artifact.path, raw: artifact.raw, contract: contract, digest: hex.EncodeToString(sum[:])})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })
	return candidates, problems
}

func loadContractInput(retention artifactretention.State, repo, filename string) (contractCandidate, error) {
	input := resolveWorkflowInputPath(repo, filename)
	raw, err := retention.ReadInput(repo, input, "contracts", int64(spec.MaxContractMemberBytes), "Change Contract exceeds maximum size")
	if err != nil {
		return contractCandidate{}, err
	}
	contract, err := spec.DecodeChangeContract(raw)
	if err != nil {
		return contractCandidate{}, err
	}
	if !contract.IsLockedStrictDevelopment() || contract.BaselineLock == nil || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 {
		return contractCandidate{}, errors.New("Change Contract is not a locked strict-development contract")
	}
	sum := sha256.Sum256(raw)
	return contractCandidate{path: artifactPathLabel(repo, input), raw: raw, contract: contract, digest: hex.EncodeToString(sum[:])}, nil
}

func contractCandidateFromPackage(candidate packageCandidate) contractCandidate {
	raw := candidate.pkg.ChangeRaw
	sum := sha256.Sum256(raw)
	return contractCandidate{
		path:     candidate.path + "#change",
		raw:      raw,
		contract: candidate.pkg.Change,
		digest:   hex.EncodeToString(sum[:]),
	}
}

func artifactPathLabel(repo, filename string) string {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return filename
	}
	root, err := filepath.Abs(repo)
	if err == nil {
		if relative, relErr := filepath.Rel(root, abs); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}
	return abs
}

func resolveWorkflowInputPath(repo, filename string) string {
	if filepath.IsAbs(filename) {
		return filename
	}
	slash := filepath.ToSlash(filename)
	if slash == artifactretention.ManagedRoot || strings.HasPrefix(slash, artifactretention.ManagedRoot+"/") {
		return filepath.Join(repo, filepath.FromSlash(slash))
	}
	return filename
}

func workflowArtifacts(retention artifactretention.State, repo, class, explicit string, maximum int64, oversize string) ([]retainedArtifact, []string) {
	if explicit == "" {
		if !retention.RepositoryEnabled() {
			return nil, nil
		}
		return loadArtifacts(retention, repo, class, maximum, oversize)
	}
	input := resolveWorkflowInputPath(repo, explicit)
	raw, err := retention.ReadInput(repo, input, class, maximum, oversize)
	if err != nil {
		return nil, []string{fmt.Sprintf("invalid explicit %s artifact %s: %v", class, explicit, err)}
	}
	return []retainedArtifact{{path: artifactPathLabel(repo, input), raw: raw}}, nil
}

func selectContract(repo, requested string, candidates []contractCandidate) (*contractCandidate, string) {
	if requested == "" {
		switch len(candidates) {
		case 0:
			return nil, ""
		case 1:
			return &candidates[0], ""
		default:
			return nil, "multiple locked Change Contracts are persisted; use --contract to select one explicitly"
		}
	}
	wanted := requested
	if !filepath.IsAbs(wanted) && (wanted == artifactretention.ManagedRoot || strings.HasPrefix(filepath.ToSlash(wanted), artifactretention.ManagedRoot+"/")) {
		wanted = filepath.Join(repo, filepath.FromSlash(wanted))
	}
	wantedAbs, err := filepath.Abs(wanted)
	if err != nil {
		return nil, fmt.Sprintf("resolve selected contract: %v", err)
	}
	for index := range candidates {
		candidateAbs := filepath.Join(repo, filepath.FromSlash(candidates[index].path))
		if filepath.Clean(candidateAbs) == filepath.Clean(wantedAbs) {
			return &candidates[index], ""
		}
	}
	return nil, "--contract must select one of the retained locked Change Contracts"
}

func loadArtifacts(retention artifactretention.State, repo, class string, maximum int64, oversize string) ([]retainedArtifact, []string) {
	paths, err := retention.ListPaths(repo, class)
	if err != nil {
		return nil, []string{err.Error()}
	}
	artifacts := make([]retainedArtifact, 0, len(paths))
	problems := make([]string, 0)
	for _, path := range paths {
		raw, err := retention.ReadInput(repo, filepath.Join(repo, filepath.FromSlash(path)), class, maximum, oversize)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invalid retained %s artifact %s: %v", class, path, err))
			continue
		}
		artifacts = append(artifacts, retainedArtifact{path: path, raw: raw})
	}
	return artifacts, problems
}

func loadPackages(retention artifactretention.State, repo string) ([]packageCandidate, []string) {
	artifacts, problems := loadArtifacts(retention, repo, "packages", packageverify.MaxArchiveBytes, "POLIS archive exceeds maximum size")
	packages := make([]packageCandidate, 0, len(artifacts))
	for _, artifact := range artifacts {
		pkg, err := packageverify.LoadBytes(artifact.raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invalid retained package %s: %v", artifact.path, err))
			continue
		}
		packages = append(packages, packageCandidate{path: artifact.path, pkg: pkg})
	}
	return packages, problems
}

func loadPackageInput(retention artifactretention.State, repo, filename string) (packageCandidate, error) {
	input := resolveWorkflowInputPath(repo, filename)
	raw, err := retention.ReadInput(repo, input, "packages", packageverify.MaxArchiveBytes, "POLIS archive exceeds maximum size")
	if err != nil {
		return packageCandidate{}, err
	}
	pkg, err := packageverify.LoadBytes(raw)
	if err != nil {
		return packageCandidate{}, err
	}
	return packageCandidate{path: artifactPathLabel(repo, input), pkg: pkg}, nil
}

func loadWorkflowPackages(retention artifactretention.State, repo string, explicit *packageCandidate) ([]packageCandidate, []string) {
	if explicit != nil {
		return []packageCandidate{*explicit}, nil
	}
	if !retention.RepositoryEnabled() {
		return nil, nil
	}
	return loadPackages(retention, repo)
}

func linkedPlanPaths(plans []retainedArtifact, selected contractCandidate) ([]string, []string) {
	paths := make([]string, 0)
	incomplete := make([]string, 0)
	for _, artifact := range plans {
		var binding struct {
			ChangeContractSHA256 string `json:"change_contract_sha256"`
		}
		if err := json.Unmarshal(artifact.raw, &binding); err != nil || binding.ChangeContractSHA256 != selected.digest {
			continue
		}
		plan, err := spec.DecodeImplementationPlan(artifact.raw)
		if err == nil {
			err = plan.ValidateAgainst(selected.contract, selected.raw)
		}
		if err != nil {
			incomplete = append(incomplete, artifact.path)
			continue
		}
		paths = append(paths, artifact.path)
	}
	sort.Strings(paths)
	sort.Strings(incomplete)
	return paths, incomplete
}

func validProofPaths(ctx context.Context, repo string, proofs []retainedArtifact, contract spec.ChangeContract) ([]string, []string) {
	if !contract.RequiresRedGreen() {
		return nil, nil
	}
	paths := make([]string, 0)
	incomplete := make([]string, 0)
	for _, artifact := range proofs {
		associated, err := redcapture.ValidateProofCandidate(ctx, repo, artifact.raw, contract)
		if err == nil {
			paths = append(paths, artifact.path)
		} else if associated {
			incomplete = append(incomplete, artifact.path)
		}
	}
	sort.Strings(paths)
	sort.Strings(incomplete)
	return paths, incomplete
}

func deriveBaseline(ctx context.Context, repo string, contract spec.ChangeContract) *BaselineSummary {
	lock := contract.BaselineLock
	summary := &BaselineSummary{
		GitObjectFormat: lock.GitObjectFormat, BaseCommit: lock.BaseCommit, BaseTree: lock.BaseTree,
		PolicySHA256: lock.PolicySHA256, SpecificationSHA256: lock.SpecificationSHA256,
		RepositoryRelation: "unknown", PolicyStatus: "unknown",
	}
	if err := devlock.ValidateRepositoryBase(ctx, repo, contract); err != nil {
		return summary
	}
	summary.Resolvable = true
	if err := devlock.ValidateRepository(ctx, repo, contract); err == nil {
		summary.RepositoryRelation = "at_baseline"
		return summary
	}
	if err := devlock.ValidateBuildRepository(ctx, repo, contract); err == nil {
		summary.RepositoryRelation = "descendant"
	} else {
		summary.RepositoryRelation = "diverged"
	}
	return summary
}

func applyPackage(result *Result, candidate packageCandidate) {
	pkg := candidate.pkg
	result.Package = PackageSummary{
		Status: StageComplete, Paths: []string{candidate.path}, Project: pkg.Result.Project, Change: pkg.Result.Change,
		TargetTree: pkg.Result.TargetTree, ConsumerValidationRequired: pkg.Result.ConsumerValidationRequired,
	}
	result.Evidence = StageSummary{Status: StageComplete}
	if pkg.ImplementationPlan != nil {
		result.ImplementationPlan = StageSummary{Status: StageComplete}
	}
	if pkg.Change.RequiresRedGreen() {
		result.RedProof = StageSummary{Status: StageComplete}
	}
	result.Gates = packageGateSummaries(pkg)
}

func missingGateSummaries(policy spec.Policy) []GateSummary {
	summary := policy.ValidationSummary()
	statuses := make(map[string]string, len(summary.EnabledGates)+len(summary.DisabledGates))
	for _, id := range summary.EnabledGates {
		statuses[id] = StageMissing
	}
	for _, id := range summary.DisabledGates {
		statuses[id] = "disabled"
	}
	return orderedGateSummaries(statuses)
}

func packageGateSummaries(pkg packageverify.Package) []GateSummary {
	summary := pkg.Policy.ValidationSummary()
	deferred := make(map[string]struct{}, len(pkg.Result.DeferredGates))
	for _, id := range pkg.Result.DeferredGates {
		deferred[id] = struct{}{}
	}
	statuses := make(map[string]string, len(summary.EnabledGates)+len(summary.DisabledGates))
	for _, id := range summary.EnabledGates {
		if _, ok := deferred[id]; ok {
			statuses[id] = "deferred"
		} else {
			statuses[id] = StageComplete
		}
	}
	for _, id := range summary.DisabledGates {
		statuses[id] = "disabled"
	}
	return orderedGateSummaries(statuses)
}

func orderedGateSummaries(statuses map[string]string) []GateSummary {
	result := make([]GateSummary, 0, len(statuses))
	for _, id := range spec.ProjectGateOrder {
		if status, ok := statuses[id]; ok {
			result = append(result, GateSummary{ID: id, Status: status})
		}
	}
	return result
}

func inconsistent(result Result) Result {
	result.Status = "FAIL"
	result.State = StateInconsistent
	result.Consistent = false
	result.NextAction = nil
	return result
}

// Project adds an agent-oriented explanation layer over the authoritative
// workflow fields without changing their status or next-action semantics.
func Project(result Result) Result {
	result.Proven = []string{}
	result.StaleOrUnproven = []string{}
	result.Missing = []string{}

	if result.Contract == nil {
		result.Missing = append(result.Missing, "locked Change Contract")
	} else {
		result.Proven = append(result.Proven, "locked Change Contract validated and selected")
	}
	if result.Baseline != nil {
		if result.Baseline.Resolvable {
			result.Proven = append(result.Proven, "locked baseline resolves in the current repository")
		} else {
			result.StaleOrUnproven = append(result.StaleOrUnproven, "locked baseline is not currently resolvable")
			result.Missing = append(result.Missing, "resolvable locked baseline")
		}
		switch result.Baseline.PolicyStatus {
		case "match":
			result.Proven = append(result.Proven, "effective Project Policy matches the locked contract")
		case "mismatch":
			result.StaleOrUnproven = append(result.StaleOrUnproven, "effective Project Policy does not match the locked contract")
			result.Missing = append(result.Missing, "matching Project Policy")
		case "unavailable":
			result.StaleOrUnproven = append(result.StaleOrUnproven, "effective Project Policy is unavailable")
			result.Missing = append(result.Missing, "effective Project Policy")
		}
	}

	switch result.ImplementationPlan.Status {
	case StageComplete:
		result.Proven = append(result.Proven, "Implementation Plan validates against the selected contract")
	case StageIncomplete, StageAmbiguous:
		result.StaleOrUnproven = append(result.StaleOrUnproven, "Implementation Plan is present but not usable as current contract-bound evidence")
	}
	switch result.RedProof.Status {
	case StageComplete:
		result.Proven = append(result.Proven, "Red proof validates against the locked contract")
	case StageMissing:
		if result.Contract != nil {
			result.Missing = append(result.Missing, "validated Red proof")
		}
	case StageIncomplete, StageAmbiguous:
		result.StaleOrUnproven = append(result.StaleOrUnproven, "Red proof is present but does not validate")
		result.Missing = append(result.Missing, "validated Red proof")
	}

	if result.Package.Status == StageComplete {
		result.Proven = append(result.Proven, "POLIS package verified and bound to the selected contract")
	} else if result.Contract != nil {
		result.Missing = append(result.Missing, "verified POLIS package")
	}
	if result.Evidence.Status == StageComplete {
		result.Proven = append(result.Proven, "package validation evidence verified")
	}

	for _, gate := range result.Gates {
		switch gate.Status {
		case StageComplete:
			result.Proven = append(result.Proven, "project gate "+gate.ID+" complete")
		case StageMissing:
			result.Missing = append(result.Missing, "project gate "+gate.ID)
		case "deferred":
			result.StaleOrUnproven = append(result.StaleOrUnproven, "project gate "+gate.ID+" is deferred to consumer validation")
		}
	}

	if result.Workspace != nil {
		switch result.Workspace.CheckpointState {
		case "source_snapshot_matches":
			result.StaleOrUnproven = append(result.StaleOrUnproven,
				"workspace checkpoint matches current source identities but is unsigned historical evidence and does not establish current validation")
		case "source_snapshot_differs":
			detail := "workspace checkpoint is stale"
			if len(result.Workspace.Differences) != 0 {
				detail += ": " + strings.Join(result.Workspace.Differences, ", ")
			}
			result.StaleOrUnproven = append(result.StaleOrUnproven, detail)
		case "checkpoint_unavailable":
			result.StaleOrUnproven = append(result.StaleOrUnproven, "workspace checkpoint cannot be safely compared with the current source snapshot")
		}
	}

	for _, problem := range result.Problems {
		result.StaleOrUnproven = appendUnique(result.StaleOrUnproven, problem)
	}
	result.Proven = uniqueStrings(result.Proven)
	result.StaleOrUnproven = uniqueStrings(result.StaleOrUnproven)
	result.Missing = uniqueStrings(result.Missing)
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = appendUnique(result, value)
	}
	return result
}
