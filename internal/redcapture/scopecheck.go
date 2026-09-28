package redcapture

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	testScopeRule    = "test_scope.allowed_paths"
	relativePathRule = "repository_relative_path"
)

type ScopeOptions struct {
	Repo     string
	Contract string
	Paths    []string
}

type ScopePathResult struct {
	Path   string      `json:"path"`
	Status spec.Status `json:"status"`
	Rule   string      `json:"rule,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

type ScopeResult struct {
	Status             spec.Status       `json:"status"`
	AllowedTestPaths   []string          `json:"allowed_test_paths"`
	AllowedChangePaths []string          `json:"allowed_change_paths"`
	Paths              []ScopePathResult `json:"paths"`
	RejectedPaths      []ScopePathResult `json:"rejected_paths"`
	ProbeExecuted      bool              `json:"probe_executed"`
	ProofCaptured      bool              `json:"proof_captured"`
}

// CheckScope assesses proposed file paths without capturing a patch or running
// the contract's regression command. Capture remains authoritative for the
// paths in the actual Red probe patch.
func CheckScope(ctx context.Context, opts ScopeOptions) (ScopeResult, error) {
	if opts.Repo == "" || opts.Contract == "" || len(opts.Paths) == 0 {
		return ScopeResult{}, errors.New("repo, contract, and at least one path are required")
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return ScopeResult{}, err
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		return ScopeResult{}, err
	}
	contract, _, err := loadRedGreenContract(repo, opts.Contract, retention)
	if err != nil {
		return ScopeResult{}, err
	}
	if err := devlock.ValidateRepository(ctx, repo, contract); err != nil {
		return ScopeResult{}, fmt.Errorf("locked development baseline: %w", err)
	}
	return assessScopePaths(contract, opts.Paths), nil
}

func assessScopePaths(contract spec.ChangeContract, paths []string) ScopeResult {
	unique := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		unique[path] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for path := range unique {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	result := ScopeResult{
		Status:        spec.StatusPass,
		Paths:         make([]ScopePathResult, 0, len(ordered)),
		RejectedPaths: make([]ScopePathResult, 0),
	}
	if contract.TestScope != nil {
		result.AllowedTestPaths = append([]string{}, contract.TestScope.AllowedPaths...)
	}
	if contract.Scope != nil {
		result.AllowedChangePaths = append([]string{}, contract.Scope.AllowedPaths...)
	}
	for _, path := range ordered {
		decision := ScopePathResult{Path: path, Status: spec.StatusPass}
		if err := spec.ValidateRepoRelativePath(path); err != nil {
			decision.Status = spec.StatusFail
			decision.Rule = relativePathRule
			decision.Reason = err.Error()
		} else if path == "." {
			decision.Status = spec.StatusFail
			decision.Rule = relativePathRule
			decision.Reason = "probe path must name a file"
		} else if err := contract.ValidateTestPaths([]string{path}); err != nil {
			decision.Status = spec.StatusFail
			decision.Rule = testScopeRule
			decision.Reason = "no allowed test path matches this file"
		}
		result.Paths = append(result.Paths, decision)
		if decision.Status == spec.StatusFail {
			result.Status = spec.StatusFail
			result.RejectedPaths = append(result.RejectedPaths, decision)
		}
	}
	return result
}
