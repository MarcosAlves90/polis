package changestatus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Repo     string
	Contract string
}

type Result struct {
	Status             string           `json:"status"`
	State              string           `json:"state"`
	RetentionMode      string           `json:"retention_mode"`
	Consistent         bool             `json:"consistent"`
	Contract           *ContractSummary `json:"contract,omitempty"`
	Baseline           *BaselineSummary `json:"baseline,omitempty"`
	ImplementationPlan StageSummary     `json:"implementation_plan"`
	RedProof           StageSummary     `json:"red_proof"`
	Package            PackageSummary   `json:"package"`
	Evidence           StageSummary     `json:"evidence"`
	Gates              []GateSummary    `json:"gates"`
	NextAction         *NextAction      `json:"next_action,omitempty"`
	CandidateContracts []string         `json:"candidate_contracts,omitempty"`
	Problems           []string         `json:"problems,omitempty"`
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
	Status string   `json:"status"`
	Paths  []string `json:"paths,omitempty"`
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
	Command string `json:"command"`
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
	}
	if !retention.RepositoryEnabled() {
		result.Problems = []string{"repository artifact retention is disabled; no persisted workflow state is available"}
		return result, nil
	}

	contracts, contractProblems := loadContracts(retention, root)
	result.Problems = append(result.Problems, contractProblems...)
	for _, candidate := range contracts {
		result.CandidateContracts = append(result.CandidateContracts, candidate.path)
	}
	selected, selectionProblem := selectContract(root, opts.Contract, contracts)
	if selectionProblem != "" {
		result.Problems = append(result.Problems, selectionProblem)
		result.State = StateAmbiguous
		result.Consistent = false
		result.Status = "FAIL"
		return result, nil
	}
	if selected == nil {
		if len(result.Problems) > 0 {
			return inconsistent(result), nil
		}
		result.State = StateEmpty
		result.NextAction = &NextAction{Command: "polis start", Reason: "no locked Change Contract is persisted"}
		return result, nil
	}
	result.Contract = &ContractSummary{
		Path: selected.path, SHA256: selected.digest, SchemaVersion: selected.contract.SchemaVersion,
		Kind: selected.contract.Kind, RegressionMode: selected.contract.Regression.Mode,
	}
	result.Baseline = deriveBaseline(ctx, root, selected.contract)

	plans, planProblems := loadArtifacts(retention, root, "plans", int64(spec.MaxImplementationPlanBytes), "implementation plan exceeds maximum size")
	result.Problems = append(result.Problems, planProblems...)
	linkedPlans := linkedPlanPaths(plans, *selected)
	if len(linkedPlans) == 1 {
		result.ImplementationPlan = StageSummary{Status: StageComplete, Paths: linkedPlans}
	} else if len(linkedPlans) > 1 {
		result.ImplementationPlan = StageSummary{Status: StageAmbiguous, Paths: linkedPlans}
		result.Problems = append(result.Problems, "multiple retained implementation plans validate against the selected contract")
	}

	packages, packageProblems := loadPackages(retention, root)
	result.Problems = append(result.Problems, packageProblems...)
	linkedPackages := make([]packageCandidate, 0)
	for _, candidate := range packages {
		if bytes.Equal(candidate.pkg.ChangeRaw, selected.raw) {
			linkedPackages = append(linkedPackages, candidate)
		}
	}
	if len(linkedPackages) > 1 {
		for _, candidate := range linkedPackages {
			result.Package.Paths = append(result.Package.Paths, candidate.path)
		}
		result.Package.Status = StageAmbiguous
		result.Problems = append(result.Problems, "multiple verified retained packages are linked to the selected contract")
		return inconsistent(result), nil
	}

	proofs, proofProblems := loadArtifacts(retention, root, "proofs", int64(spec.MaxPatchMemberBytes), "input exceeds maximum size")
	result.Problems = append(result.Problems, proofProblems...)
	validProofs := validProofPaths(ctx, root, proofs, selected.contract)
	if selected.contract.RequiresRedGreen() {
		if len(validProofs) > 0 {
			result.RedProof = StageSummary{Status: StageComplete, Paths: validProofs}
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
			result.NextAction = &NextAction{Command: "polis preflight", Reason: "the verified package contains deferred producer gates that require consumer validation"}
		} else {
			result.State = StateComplete
		}
		return result, nil
	}

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
			result.NextAction = &NextAction{Command: "polis capture-red", Reason: "a validated persisted Red proof is required before implementation can be packaged"}
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
	result.State = StateReadyToBuild
	result.NextAction = &NextAction{Command: "polis build", Reason: "required persisted prerequisites are complete; build after the implementation changes are present"}
	return result, nil
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

func linkedPlanPaths(plans []retainedArtifact, selected contractCandidate) []string {
	paths := make([]string, 0)
	for _, artifact := range plans {
		plan, err := spec.DecodeImplementationPlan(artifact.raw)
		if err != nil {
			continue
		}
		if err := plan.ValidateAgainst(selected.contract, selected.raw); err == nil {
			paths = append(paths, artifact.path)
		}
	}
	sort.Strings(paths)
	return paths
}

func validProofPaths(ctx context.Context, repo string, proofs []retainedArtifact, contract spec.ChangeContract) []string {
	if !contract.RequiresRedGreen() {
		return nil
	}
	paths := make([]string, 0)
	for _, artifact := range proofs {
		if err := redcapture.ValidateProof(ctx, repo, artifact.raw, contract); err == nil {
			paths = append(paths, artifact.path)
		}
	}
	sort.Strings(paths)
	return paths
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
