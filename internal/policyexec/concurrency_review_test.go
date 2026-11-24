package policyexec

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

// TestConcurrencyReviewNoOverlapWhenFirstFinishesFirst forces the first safe
// gate to finish before the second gate's goroutine starts. The completion
// marker is written by the existing coverage-pass helper, so the fixture is
// portable and uses no shell.
func TestConcurrencyReviewNoOverlapWhenFirstFinishesFirst(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "coverage.out")
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	policy := concurrencyFixture(t, map[string]string{
		"lint":       "coverage-pass",
		"dependency": "pass",
	}, []string{"lint", "dependency"}, nil)

	markerObserved := false
	result := ExecuteWithOptions(policy, root, io.Discard, Options{Jobs: 2,
		OnGateStart: func(gate spec.GatePolicy) {
			if gate.ID != "dependency" {
				return
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					markerObserved = true
					break
				}
				if time.Now().After(deadline) {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			// Let the first command exit and its completion timestamp settle.
			time.Sleep(50 * time.Millisecond)
		},
	})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s results=%+v", result.Overall, result.Gates)
	}
	if !markerObserved {
		t.Fatal("first gate completion marker was not observed")
	}
	if result.Concurrency.Observation.OverlapObserved || result.Concurrency.Observation.MaxConcurrent != 1 {
		t.Fatalf("false-positive overlap: observation=%+v", result.Concurrency.Observation)
	}
}

// TestConcurrencyReviewWorkerCapWaitIsDiagnosed places three independent safe
// gates behind a two-worker bound. The third gate is ready but must wait for a
// worker slot, so the run must explain that wait with a worker_cap finding.
func TestConcurrencyReviewWorkerCapWaitIsDiagnosed(t *testing.T) {
	policy := concurrencyFixture(t, map[string]string{
		"lint":       "pass",
		"build":      "pass",
		"dependency": "pass",
	}, []string{"lint", "build", "dependency"}, nil)
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{Jobs: 2})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s results=%+v", result.Overall, result.Gates)
	}
	found := false
	for _, finding := range result.Concurrency.Diagnosis.Blockers {
		if finding.Reason != spec.ConcurrencyWorkerCap || !strings.Contains(finding.Detail, "jobs=2") {
			continue
		}
		for _, gate := range finding.Gates {
			if gate == "dependency" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("worker-cap wait not diagnosed: blockers=%+v", result.Concurrency.Diagnosis.Blockers)
	}
}
