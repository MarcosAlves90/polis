package policyplan

import (
	"reflect"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestPlanConcurrencySummaryReportsEligibilityAndBlockers(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	for i := range policy.Gates {
		switch policy.Gates[i].ID {
		case "lint", "dependency":
			policy.Gates[i].Mode = spec.GateModeCommand
			policy.Gates[i].Reason = nil
			policy.Gates[i].Command = &spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30,
				Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}}
			policy.Gates[i].ParallelSafe = true
		}
	}
	plan := mustCompile(t, policy)

	summary := plan.ConcurrencySummary()
	eligible := map[string]GateConcurrency{}
	for _, gate := range summary.EligibleGates {
		eligible[gate.Gate] = gate
	}
	if len(eligible) != 2 || eligible["lint"].Reason != ConcurrencyDeclaredParallelSafe || eligible["dependency"].Reason != ConcurrencyDeclaredParallelSafe {
		t.Fatalf("eligible gates=%+v", summary.EligibleGates)
	}
	exclusive := map[string]GateConcurrency{}
	for _, gate := range summary.ExclusiveGates {
		exclusive[gate.Gate] = gate
	}
	if exclusive["coverage"].Reason != ConcurrencyCoverageExclusive {
		t.Fatalf("coverage exclusion=%+v", exclusive["coverage"])
	}
	if exclusive["test.complete"].Reason != ConcurrencyMissingOptIn {
		t.Fatalf("missing opt-in exclusion=%+v", exclusive["test.complete"])
	}
	foundGroup := false
	for _, group := range summary.OverlapGroups {
		if reflect.DeepEqual(group.Gates, []string{"dependency", "lint"}) {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Fatalf("overlap groups=%+v", summary.OverlapGroups)
	}

	serial := plan.ConcurrencyDiagnosis(ConcurrencyOptions{Jobs: 1})
	if serial.CanOverlap || !hasConcurrencyReason(serial.Blockers, ConcurrencyJobsOne) {
		t.Fatalf("serial diagnosis=%+v", serial)
	}
	parallel := plan.ConcurrencyDiagnosis(ConcurrencyOptions{Jobs: 2})
	if !parallel.CanOverlap || hasConcurrencyReason(parallel.Blockers, ConcurrencyWorkerCap) {
		t.Fatalf("parallel diagnosis=%+v", parallel)
	}
	selected := plan.ConcurrencyDiagnosis(ConcurrencyOptions{Jobs: 4, Selected: map[string]bool{"lint": true}})
	if selected.CanOverlap || !hasConcurrencyReason(selected.Blockers, ConcurrencySelection) {
		t.Fatalf("selected diagnosis=%+v", selected)
	}
}

func hasConcurrencyReason(findings []ConcurrencyFinding, reason string) bool {
	for _, finding := range findings {
		if finding.Reason == reason {
			return true
		}
	}
	return false
}
