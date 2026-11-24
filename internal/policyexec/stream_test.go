package policyexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue44PolicyHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "fast-fail":
		_, _ = os.Stdout.WriteString("ISSUE44_FAST_FAIL\n")
		os.Exit(1)
	case "slow-pass":
		_, _ = os.Stdout.WriteString("ISSUE44_SLOW_START\n")
		time.Sleep(700 * time.Millisecond)
		_, _ = os.Stderr.WriteString("ISSUE44_SLOW_END\n")
		os.Exit(0)
	case "slow-first":
		time.Sleep(250 * time.Millisecond)
		_, _ = os.Stdout.WriteString("ISSUE44_GATE_A\n")
		os.Exit(0)
	case "emit-b":
		_, _ = os.Stdout.WriteString("ISSUE44_GATE_B\n")
		os.Exit(0)
	default:
		os.Exit(9)
	}
}

func issue44HelperCommand(mode string) *spec.CommandSpec {
	return &spec.CommandSpec{
		Argv:           []string{os.Args[0], "-test.run=^TestIssue44PolicyHelper$", "--", mode},
		Cwd:            ".",
		TimeoutSeconds: 20,
		Environment:    &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit},
	}
}

func issue44ParallelPolicy(t *testing.T, firstMode, secondMode string) spec.Policy {
	t.Helper()
	policy := testPolicy(t, "")
	policy.SchemaVersion = spec.PolicySchemaVersion
	first, second := 0, 2
	policy.Gates[first].Command = issue44HelperCommand(firstMode)
	policy.Gates[first].ParallelSafe = true
	policy.Gates[second].Mode = spec.GateModeCommand
	policy.Gates[second].Reason = nil
	policy.Gates[second].Command = issue44HelperCommand(secondMode)
	policy.Gates[second].ParallelSafe = true
	return policy
}

func TestIssue44IncrementalResultVisibleWhileOtherGateRuns(t *testing.T) {
	policy := issue44ParallelPolicy(t, "fast-fail", "slow-pass")
	slowDone := make(chan struct{})
	fastEarly := false
	var mu sync.Mutex
	var results []Outcome
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{
		Jobs: 2,
		OnGateComplete: func(gate spec.GatePolicy, outcome Outcome, _ int64) {
			if gate.ID == "lint" {
				close(slowDone)
			}
		},
		OnGateResult: func(gate spec.GatePolicy, outcome Outcome, _ int64) {
			mu.Lock()
			results = append(results, outcome)
			if gate.ID == "test.complete" {
				select {
				case <-slowDone:
					fastEarly = false
				default:
					fastEarly = true
				}
			}
			mu.Unlock()
		},
	})
	if result.Overall != spec.StatusFail {
		t.Fatalf("overall=%s", result.Overall)
	}
	mu.Lock()
	defer mu.Unlock()
	if !fastEarly {
		t.Fatalf("fast gate result was not observable while slow gate ran: results=%+v", results)
	}
	var seen bool
	for _, outcome := range results {
		if outcome.Status == spec.StatusFail {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("incremental FAIL result missing: %+v", results)
	}
}

func TestIssue44ParallelOutputCorrelatedAndEvidenceDeterministic(t *testing.T) {
	policy := issue44ParallelPolicy(t, "slow-first", "emit-b")
	var mu sync.Mutex
	var chunks []commandexec.OutputChunk
	var evidence bytes.Buffer
	result := ExecuteWithOptions(policy, t.TempDir(), &evidence, Options{
		Jobs: 2,
		OnOutput: func(chunk commandexec.OutputChunk) error {
			mu.Lock()
			chunks = append(chunks, chunk)
			mu.Unlock()
			return nil
		},
	})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s evidence=%s", result.Overall, evidence.String())
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	sequences := map[string]uint64{}
	for _, chunk := range chunks {
		if !strings.HasPrefix(string(chunk.Data), "ISSUE44_GATE_") {
			continue
		}
		want := map[string]string{"ISSUE44_GATE_A": "test.complete", "ISSUE44_GATE_B": "lint"}[strings.TrimSpace(string(chunk.Data))]
		if want == "" || chunk.Gate != want {
			t.Fatalf("misattributed chunk: gate=%s data=%q", chunk.Gate, chunk.Data)
		}
		seen[chunk.Gate] = true
		key := chunk.Gate + "/" + chunk.Stream
		sequences[key]++
		if chunk.Sequence != sequences[key] {
			t.Fatalf("non-monotonic sequence for %s: %d want %d", key, chunk.Sequence, sequences[key])
		}
	}
	if !seen["test.complete"] || !seen["lint"] {
		t.Fatalf("missing parallel gate output: %+v", chunks)
	}
	var finished []string
	for _, line := range strings.Split(strings.TrimSpace(evidence.String()), "\n") {
		var event struct {
			Event string `json:"event"`
			Gate  string `json:"gate"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid evidence line %q: %v", line, err)
		}
		if event.Event == "gate_finished" && (event.Gate == "test.complete" || event.Gate == "lint") {
			finished = append(finished, event.Gate)
		}
	}
	if !slices.Equal(finished, []string{"test.complete", "lint"}) {
		t.Fatalf("final evidence order changed: %v", finished)
	}
}

func TestIssue44ReusedAndOmittedGatesEmitResults(t *testing.T) {
	policy := testPolicy(t, "pass")
	policy.SchemaVersion = spec.PolicySchemaVersion
	var mu sync.Mutex
	var actions []string
	result := ExecuteWithOptions(policy, t.TempDir(), io.Discard, Options{
		Jobs: 1,
		Reuse: func(gate spec.GatePolicy, _ Result) (Outcome, bool) {
			return Outcome{Status: spec.StatusPass, Action: "reused", Reason: "matching identity"}, true
		},
		OnGateResult: func(gate spec.GatePolicy, outcome Outcome, _ int64) {
			mu.Lock()
			actions = append(actions, fmt.Sprintf("%s:%s", gate.ID, outcome.Action))
			mu.Unlock()
		},
	})
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s", result.Overall)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(actions, "test.complete:reused") {
		t.Fatalf("reused gate result missing: %v", actions)
	}
	if !slices.Contains(actions, "lint:omitted") {
		t.Fatalf("omitted gate result missing: %v", actions)
	}
}

func TestIssue44ObserverFailureDoesNotChangeGateResult(t *testing.T) {
	policy := testPolicy(t, "pass")
	policy.SchemaVersion = spec.PolicySchemaVersion
	var evidence bytes.Buffer
	result := ExecuteWithOptions(policy, t.TempDir(), &evidence, Options{
		Jobs: 1,
		OnOutput: func(chunk commandexec.OutputChunk) error {
			return fmt.Errorf("sink closed")
		},
	})
	if result.Overall != spec.StatusPass || result.Gates["test.complete"] != spec.StatusPass {
		t.Fatalf("observer error altered result: %+v", result)
	}
	if !strings.Contains(evidence.String(), `"event":"command_finished"`) {
		t.Fatalf("evidence lost after observer error: %s", evidence.String())
	}
}
