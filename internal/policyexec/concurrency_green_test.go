package policyexec

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func concurrencyFixture(t *testing.T, modes map[string]string, safe []string, deps map[string][]string) spec.Policy {
	t.Helper()
	policy := testPolicy(t, "pass")
	policy.SchemaVersion = spec.PolicySchemaVersion
	safeSet := map[string]bool{}
	for _, id := range safe {
		if id == "coverage" {
			t.Fatal("coverage must remain exclusive")
		}
		safeSet[id] = true
	}
	for i := range policy.Gates {
		gate := &policy.Gates[i]
		if gate.Command != nil {
			gate.Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
		}
		if mode, ok := modes[gate.ID]; ok {
			gate.Mode = spec.GateModeCommand
			gate.Reason = nil
			gate.Command = helperCommand(t, mode, 10)
			gate.Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
		}
		if dependency, ok := deps[gate.ID]; ok {
			gate.DependsOn = append([]string(nil), dependency...)
		}
		gate.ParallelSafe = safeSet[gate.ID]
	}
	return policy
}

func hasReason(findings []spec.ConcurrencyFinding, reason string) bool {
	for _, finding := range findings {
		if finding.Reason == reason {
			return true
		}
	}
	return false
}

func TestConcurrencyGreenInvalidJobsReportsBlocker(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{"lint": "pass"}, []string{"lint"}, nil)
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 0})
	if result.Overall != spec.StatusBlocked || !hasReason(result.Concurrency.Diagnosis.Blockers, spec.ConcurrencyInvalidJobs) {
		t.Fatalf("invalid jobs result=%+v", result.Concurrency)
	}
}

func TestConcurrencyGreenJobsOneReportsWorkerCapAndJobsOne(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{"lint": "pass", "dependency": "pass"}, []string{"lint", "dependency"}, nil)
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 1})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s", result.Overall)
	}
	if result.Concurrency.Observation.OverlapObserved || result.Concurrency.Observation.MaxConcurrent != 1 {
		t.Fatalf("jobs=1 observation=%+v", result.Concurrency.Observation)
	}
	blockers := result.Concurrency.Diagnosis.Blockers
	if !hasReason(blockers, spec.ConcurrencyJobsOne) || !hasReason(blockers, spec.ConcurrencyWorkerCap) {
		t.Fatalf("jobs=1 blockers=%+v", blockers)
	}
}

func TestConcurrencyGreenPolicyRestrictionsExplainSerialRun(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{"lint": "pass"}, []string{"lint"}, map[string][]string{"lint": {"test.complete"}})
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 4})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s", result.Overall)
	}
	if result.Concurrency.Observation.OverlapObserved {
		t.Fatalf("policy-restricted gates overlapped: %+v", result.Concurrency)
	}
	for _, reason := range []string{spec.ConcurrencyPrerequisite, spec.ConcurrencyMissingOptIn, spec.ConcurrencyCoverageExclusive} {
		if !hasReason(result.Concurrency.Diagnosis.Blockers, reason) {
			t.Fatalf("missing %s in blockers=%+v", reason, result.Concurrency.Diagnosis.Blockers)
		}
	}
}

func TestConcurrencyGreenSelectionAndReuseExplainMissingOverlap(t *testing.T) {
	modes := map[string]string{"lint": "pass", "dependency": "pass"}
	selected := ExecuteWithOptions(concurrencyFixture(t, modes, []string{"lint", "dependency"}, nil), t.TempDir(), io.Discard,
		Options{Jobs: 4, Selected: map[string]bool{"lint": true}})
	if !hasReason(selected.Concurrency.Diagnosis.Blockers, spec.ConcurrencySelection) {
		t.Fatalf("selection blockers=%+v", selected.Concurrency.Diagnosis.Blockers)
	}

	reused := ExecuteWithOptions(concurrencyFixture(t, modes, []string{"lint", "dependency"}, nil), t.TempDir(), io.Discard,
		Options{Jobs: 4, Reuse: func(gate spec.GatePolicy, current Result) (Outcome, bool) {
			if gate.ID == "lint" || gate.ID == "dependency" {
				return Outcome{Status: spec.StatusPass, Action: "reused", Reason: "green fixture"}, true
			}
			return Outcome{}, false
		}})
	if reused.Overall != spec.StatusPass || reused.Concurrency.Observation.OverlapObserved {
		t.Fatalf("reused result=%+v", reused.Concurrency)
	}
	if !hasReason(reused.Concurrency.Diagnosis.Blockers, spec.ConcurrencyAllReused) {
		t.Fatalf("reuse blockers=%+v", reused.Concurrency.Diagnosis.Blockers)
	}
	for _, id := range []string{"lint", "dependency"} {
		found := false
		for _, launched := range reused.Concurrency.Observation.LaunchedGates {
			if launched == id {
				found = true
			}
		}
		if found {
			t.Fatalf("reused gate %s was reported as launched: %v", id, reused.Concurrency.Observation.LaunchedGates)
		}
	}
}

func TestConcurrencyGreenFailedPrerequisiteExplainsMissingOverlap(t *testing.T) {
	policy := concurrencyFixture(t,
		map[string]string{"test.complete": "fail", "lint": "pass", "dependency": "pass"},
		[]string{"lint", "dependency"},
		map[string][]string{"lint": {"test.complete"}})
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 2})
	if result.Overall != spec.StatusFail || result.Gates["lint"] != spec.StatusBlocked || result.Gates["dependency"] != spec.StatusPass {
		t.Fatalf("overall=%s gates=%v", result.Overall, result.Gates)
	}
	if result.Concurrency.Observation.OverlapObserved || result.Concurrency.Observation.MaxConcurrent != 1 {
		t.Fatalf("unexpected overlap observation=%+v", result.Concurrency.Observation)
	}
	if !hasReason(result.Concurrency.Diagnosis.Blockers, spec.ConcurrencyPrerequisite) {
		t.Fatalf("blocked prerequisite not explained in blockers=%+v", result.Concurrency.Diagnosis.Blockers)
	}
}

func TestConcurrencyGreenMixedFailurePreservesActualOutcomes(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{"lint": "pass", "dependency": "fail"}, []string{"lint", "dependency"}, nil)
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 2})
	if result.Overall != spec.StatusFail {
		t.Fatalf("overall=%s", result.Overall)
	}
	if result.Gates["lint"] != spec.StatusPass || result.Gates["dependency"] != spec.StatusFail {
		t.Fatalf("gate statuses=%+v", result.Gates)
	}
	if !result.Concurrency.Diagnosis.CanOverlap {
		t.Fatalf("diagnosis=%+v", result.Concurrency.Diagnosis)
	}
	launched := map[string]bool{}
	for _, id := range result.Concurrency.Observation.LaunchedGates {
		launched[id] = true
	}
	if !launched["lint"] || !launched["dependency"] {
		t.Fatalf("launched gates=%v", result.Concurrency.Observation.LaunchedGates)
	}
}

func TestConcurrencyGreenJobsOneAndJobsTwoShareGateSemantics(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{"lint": "pass", "dependency": "pass"}, []string{"lint", "dependency"}, nil)
	serial := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 1})
	parallel := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 2})
	if serial.Overall != spec.StatusPass || parallel.Overall != spec.StatusPass {
		t.Fatalf("overall serial=%s parallel=%s", serial.Overall, parallel.Overall)
	}
	if !reflect.DeepEqual(serial.Gates, parallel.Gates) {
		t.Fatalf("gate statuses differ: serial=%+v parallel=%+v", serial.Gates, parallel.Gates)
	}
	if strings.Join(serial.Concurrency.Diagnosis.EligibleGates, ",") != strings.Join(parallel.Concurrency.Diagnosis.EligibleGates, ",") {
		t.Fatalf("eligible gates differ: serial=%v parallel=%v", serial.Concurrency.Diagnosis.EligibleGates, parallel.Concurrency.Diagnosis.EligibleGates)
	}
}

// benchmarkParallelPolicy uses two reviewed, sleep-bound command gates with no
// shared outputs. The helper sleeps 3s, so serial execution is about 6s and
// bounded overlap about 3s. These numbers are fixture evidence, not a general
// speedup promise for CPU- or disk-bound project gates.
func benchmarkParallelPolicy(b *testing.B) spec.Policy {
	b.Helper()
	reason := "not applicable in benchmark fixture"
	threshold := 80.0
	environment := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "pass"}, Cwd: ".", TimeoutSeconds: 10, Environment: environment}})
		case "coverage":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "coverage-pass"}, Cwd: ".", TimeoutSeconds: 10, Environment: environment},
				Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan,
				ThresholdPercent: &threshold})
		case "lint", "dependency":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, ParallelSafe: true,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "sleep"}, Cwd: ".", TimeoutSeconds: 10, Environment: environment}})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	return spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates}
}

func runConcurrencyBenchmark(b *testing.B, jobs int) {
	b.Helper()
	policy := benchmarkParallelPolicy(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := ExecuteWithOptions(policy, b.TempDir(), io.Discard, Options{Jobs: jobs})
		if result.Overall != spec.StatusPass {
			b.Fatalf("jobs=%d overall=%s", jobs, result.Overall)
		}
		b.ReportMetric(float64(result.Concurrency.Observation.MaxConcurrent), "max-concurrent")
	}
}

func BenchmarkSafeParallelJobsOne(b *testing.B) { runConcurrencyBenchmark(b, 1) }

func BenchmarkSafeParallelJobsTwo(b *testing.B) { runConcurrencyBenchmark(b, 2) }
