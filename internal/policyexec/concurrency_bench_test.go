package policyexec

import (
	"io"
	"os"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// benchmarkSeparateParallelPolicy uses two reviewed, sleep-bound command gates with no
// shared outputs. The helper sleeps 3s, so serial execution is ~6s and bounded
// overlap is ~3s. Run with -benchtime=1x; do not use these numbers as a general
// speedup promise for CPU- or disk-bound project gates.
func benchmarkSeparateParallelPolicy(b *testing.B) spec.Policy {
	b.Helper()
	reason := "not applicable in benchmark fixture"
	threshold := 80.0
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "pass"}, Cwd: ".", TimeoutSeconds: 10}})
		case "coverage":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "coverage-pass"}, Cwd: ".", TimeoutSeconds: 10},
				Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan,
				ThresholdPercent: &threshold})
		case "lint", "dependency":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, ParallelSafe: true,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", "sleep"}, Cwd: ".", TimeoutSeconds: 10}})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	return spec.Policy{SchemaVersion: spec.LegacyPolicySchemaVersion, Gates: gates}
}

func runSeparateConcurrencyBenchmark(b *testing.B, jobs int) {
	b.Helper()
	policy := benchmarkSeparateParallelPolicy(b)
	if _, err := policyplan.Compile(policy); err != nil {
		b.Fatalf("benchmark policy invalid: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := ExecuteWithOptions(policy, b.TempDir(), io.Discard, Options{Jobs: jobs})
		if result.Overall != spec.StatusPass {
			b.Fatalf("jobs=%d overall=%s", jobs, result.Overall)
		}
		b.ReportMetric(float64(result.Concurrency.Observation.MaxConcurrent), "max-concurrent")
	}
}

func BenchmarkSafeParallelJobsOneSeparate(b *testing.B) { runSeparateConcurrencyBenchmark(b, 1) }

func BenchmarkSafeParallelJobsTwoSeparate(b *testing.B) { runSeparateConcurrencyBenchmark(b, 2) }
