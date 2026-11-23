package policyexec

import (
	"io"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestSafeParallelExecutionReportsOverlapAndBlockers(t *testing.T) {
	policy := testPolicy(t, "pass")
	for i := range policy.Gates {
		switch policy.Gates[i].ID {
		case "lint", "dependency":
			policy.Gates[i].Mode = spec.GateModeCommand
			policy.Gates[i].Command = helperCommand(t, "sleep", 10)
			policy.Gates[i].Reason = nil
			policy.Gates[i].ParallelSafe = true
		}
	}

	parallel := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 2})
	if parallel.Overall != spec.StatusPass {
		t.Fatalf("parallel overall=%s", parallel.Overall)
	}
	if !parallel.Concurrency.Observation.OverlapObserved || parallel.Concurrency.Observation.MaxConcurrent < 2 {
		t.Fatalf("expected observed overlap, got %+v", parallel.Concurrency)
	}
	if parallel.Concurrency.Diagnosis.Jobs != 2 || !parallel.Concurrency.Diagnosis.CanOverlap {
		t.Fatalf("parallel diagnosis=%+v", parallel.Concurrency.Diagnosis)
	}

	serialPolicy := testPolicy(t, "pass")
	serial := ExecuteWithOptions(serialPolicy, t.TempDir(), io.Discard, Options{Jobs: 4})
	if serial.Overall != spec.StatusPass {
		t.Fatalf("serial overall=%s", serial.Overall)
	}
	if serial.Concurrency.Observation.OverlapObserved {
		t.Fatalf("unsafe gates falsely reported overlap: %+v", serial.Concurrency)
	}
	if !hasConcurrencyReasonValue(serial.Concurrency.Diagnosis.Blockers, spec.ConcurrencyMissingOptIn) {
		t.Fatalf("missing opt-in reason absent: %+v", serial.Concurrency.Diagnosis.Blockers)
	}
	if !hasConcurrencyReasonValue(serial.Concurrency.Diagnosis.Blockers, spec.ConcurrencyCoverageExclusive) {
		t.Fatalf("coverage exclusivity reason absent: %+v", serial.Concurrency.Diagnosis.Blockers)
	}
}

func hasConcurrencyReasonValue(findings []spec.ConcurrencyFinding, reason string) bool {
	for _, finding := range findings {
		if finding.Reason == reason {
			return true
		}
	}
	return false
}
