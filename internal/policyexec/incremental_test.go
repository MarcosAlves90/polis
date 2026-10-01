package policyexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestScheduleHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "schedule-helper" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	dir, id := os.Args[index+1], os.Args[index+2]
	if err := os.WriteFile(filepath.Join(dir, "started-"+id), []byte(id), 0o600); err != nil {
		os.Exit(9)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "release-"+id)); err == nil {
			fmt.Print(id)
			os.Exit(0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(8)
}

func waitForStart(t *testing.T, dir, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "started-"+id)); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("gate did not start", id)
}

func release(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "release-"+id), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func scheduledPolicy(t *testing.T, dir string, parallel bool) spec.Policy {
	p := testPolicy(t, "pass")
	p.SchemaVersion = 3
	p.ValidationLevel = spec.ValidationLevelStandard
	reason := "no coverage in scheduler fixture"
	p.Gates[1] = spec.GatePolicy{ID: "coverage", Mode: spec.GateModeNotApplicable, Reason: &reason}
	for i := range p.Gates {
		g := &p.Gates[i]
		if g.ID == "test.complete" || g.ID == "lint" || g.ID == "build" {
			*g = spec.GatePolicy{ID: g.ID, Mode: spec.GateModeCommand, ParallelSafe: parallel,
				Command: &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestScheduleHelper", "--", "schedule-helper", dir, g.ID}, Cwd: ".", TimeoutSeconds: 20, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}}}
		}
	}
	return p
}

func TestBoundedConcurrencyAndStableEvidenceOrder(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprint(parallel), func(t *testing.T) {
			dir := t.TempDir()
			p := scheduledPolicy(t, dir, parallel)
			plan, err := policyplan.Compile(p)
			if err != nil {
				t.Fatal(err)
			}
			var evidence bytes.Buffer
			results := make(chan Result, 1)
			go func() { results <- ExecutePlanWithOptions(plan, t.TempDir(), &evidence, Options{Jobs: 2}) }()
			waitForStart(t, dir, "test.complete")
			if parallel {
				waitForStart(t, dir, "lint")
				if _, err := os.Stat(filepath.Join(dir, "started-build")); !os.IsNotExist(err) {
					t.Fatal("job bound exceeded")
				}
				// Complete the later gate first. Reporting must remain plan-ordered.
				release(t, dir, "lint")
				waitForStart(t, dir, "build")
				release(t, dir, "build")
				release(t, dir, "test.complete")
			} else {
				if _, err := os.Stat(filepath.Join(dir, "started-lint")); !os.IsNotExist(err) {
					t.Fatal("unsafe gates overlapped")
				}
				release(t, dir, "test.complete")
				waitForStart(t, dir, "lint")
				if _, err := os.Stat(filepath.Join(dir, "started-build")); !os.IsNotExist(err) {
					t.Fatal("unsafe gate overlap")
				}
				release(t, dir, "lint")
				waitForStart(t, dir, "build")
				release(t, dir, "build")
			}
			result := <-results
			if result.Overall != spec.StatusPass {
				t.Fatal(result)
			}
			var ids []string
			dec := json.NewDecoder(&evidence)
			for {
				var e spec.EvidenceEvent
				if err := dec.Decode(&e); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if e.Event == "gate_finished" {
					ids = append(ids, e.Gate)
				}
			}
			if !reflect.DeepEqual(ids, plan.ExecutionOrder) {
				t.Fatal(ids, plan.ExecutionOrder)
			}
			for _, id := range []string{"test.complete", "lint", "build"} {
				if result.Outcomes[id].Command.Observation.Stdout != id {
					t.Fatal("outcome associated with wrong gate", result)
				}
			}
		})
	}
}

func TestPrerequisiteCompletionAndFailuresDoNotSuppressUnrelatedGates(t *testing.T) {
	dir := t.TempDir()
	p := scheduledPolicy(t, dir, true)
	p.Gates[2].DependsOn = []string{"test.complete"}
	plan, err := policyplan.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan Result, 1)
	root := t.TempDir()
	go func() { results <- ExecutePlanWithOptions(plan, root, io.Discard, Options{Jobs: 3}) }()
	waitForStart(t, dir, "test.complete")
	waitForStart(t, dir, "build")
	if _, err := os.Stat(filepath.Join(dir, "started-lint")); !os.IsNotExist(err) {
		t.Fatal("dependent started before prerequisite")
	}
	release(t, dir, "test.complete")
	waitForStart(t, dir, "lint")
	release(t, dir, "lint")
	release(t, dir, "build")
	if result := <-results; result.Overall != spec.StatusPass {
		t.Fatal(result)
	}
	p.Gates[0].Command = helperCommand(t, "fail", 10)
	p.Gates[0].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}
	plan, err = policyplan.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	result := ExecutePlanWithOptions(plan, root, io.Discard, Options{Jobs: 3})
	if result.Overall != spec.StatusFail || result.Gates["lint"] != spec.StatusBlocked || result.Outcomes["lint"].Command != nil || result.Gates["build"] != spec.StatusPass {
		t.Fatal(result)
	}
	for _, jobs := range []int{0, -1, MaxJobs + 1} {
		if ValidateJobs(jobs) == nil {
			t.Fatal("invalid job bound accepted")
		}
		if r := ExecutePlanWithOptions(plan, root, io.Discard, Options{Jobs: jobs}); r.Overall != spec.StatusBlocked {
			t.Fatal(r)
		}
	}
}
