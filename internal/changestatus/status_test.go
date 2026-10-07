package changestatus

import (
	"slices"
	"strings"
	"testing"
)

func TestProjectExplainsVerifiedIncompleteAndStaleWorkflowState(t *testing.T) {
	result := Project(Result{
		Contract:           &ContractSummary{Path: "contract.json"},
		Baseline:           &BaselineSummary{Resolvable: true, PolicyStatus: "match"},
		ImplementationPlan: StageSummary{Status: StageIncomplete},
		RedProof:           StageSummary{Status: StageIncomplete},
		Package:            PackageSummary{Status: StageMissing},
		Evidence:           StageSummary{Status: StageComplete},
		Gates: []GateSummary{
			{ID: "test.complete", Status: StageComplete},
			{ID: "coverage", Status: StageMissing},
			{ID: "lint", Status: "deferred"},
		},
		Workspace: &WorkspaceSummary{
			CheckpointState: "source_snapshot_differs",
			Differences:     []string{"target_tree", "policy_sha256"},
		},
		Problems: []string{"explicit package is unavailable", "explicit package is unavailable"},
	})

	for _, want := range []string{
		"locked Change Contract validated and selected",
		"locked baseline resolves in the current repository",
		"effective Project Policy matches the locked contract",
		"package validation evidence verified",
		"project gate test.complete complete",
	} {
		if !slices.Contains(result.Proven, want) {
			t.Errorf("Proven missing %q: %v", want, result.Proven)
		}
	}
	for _, fragment := range []string{
		"Implementation Plan is present",
		"Red proof is present",
		"project gate lint is deferred",
		"workspace checkpoint is stale: target_tree, policy_sha256",
		"explicit package is unavailable",
	} {
		if !containsFragment(result.StaleOrUnproven, fragment) {
			t.Errorf("StaleOrUnproven missing %q: %v", fragment, result.StaleOrUnproven)
		}
	}
	for _, want := range []string{"validated Red proof", "verified POLIS package", "project gate coverage"} {
		if !slices.Contains(result.Missing, want) {
			t.Errorf("Missing missing %q: %v", want, result.Missing)
		}
	}
	if got := countString(result.StaleOrUnproven, "explicit package is unavailable"); got != 1 {
		t.Fatalf("duplicate problem was not deduplicated: got %d entries in %v", got, result.StaleOrUnproven)
	}
}

func TestProjectExplainsUnavailablePrerequisitesAndCheckpointLimits(t *testing.T) {
	tests := []struct {
		name       string
		result     Result
		staleWant  string
		missing    []string
		provenWant string
	}{
		{
			name: "matching checkpoint remains historical",
			result: Result{
				Baseline:           &BaselineSummary{Resolvable: false, PolicyStatus: "unavailable"},
				ImplementationPlan: StageSummary{Status: StageComplete},
				RedProof:           StageSummary{Status: StageMissing},
				Workspace:          &WorkspaceSummary{CheckpointState: "source_snapshot_matches"},
			},
			staleWant:  "unsigned historical evidence",
			missing:    []string{"locked Change Contract", "resolvable locked baseline", "effective Project Policy"},
			provenWant: "Implementation Plan validates against the selected contract",
		},
		{
			name: "checkpoint comparison unavailable and policy mismatched",
			result: Result{
				Contract:  &ContractSummary{Path: "contract.json"},
				Baseline:  &BaselineSummary{Resolvable: false, PolicyStatus: "mismatch"},
				RedProof:  StageSummary{Status: StageComplete},
				Package:   PackageSummary{Status: StageComplete},
				Workspace: &WorkspaceSummary{CheckpointState: "checkpoint_unavailable"},
			},
			staleWant:  "cannot be safely compared",
			missing:    []string{"resolvable locked baseline", "matching Project Policy"},
			provenWant: "POLIS package verified and bound to the selected contract",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Project(tt.result)
			if !containsFragment(got.StaleOrUnproven, tt.staleWant) {
				t.Fatalf("StaleOrUnproven missing %q: %v", tt.staleWant, got.StaleOrUnproven)
			}
			for _, want := range tt.missing {
				if !slices.Contains(got.Missing, want) {
					t.Errorf("Missing missing %q: %v", want, got.Missing)
				}
			}
			if !slices.Contains(got.Proven, tt.provenWant) {
				t.Fatalf("Proven missing %q: %v", tt.provenWant, got.Proven)
			}
		})
	}
}

func containsFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
