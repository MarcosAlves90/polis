package gaterun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIntentToAddChangesInvalidateReuseAndReplay(t *testing.T) {
	repo, policy, _ := fixture(t)
	mustWrite(t, filepath.Join(repo, "empty.txt"), "")
	git(t, repo, "add", "-N", "empty.txt")
	policy.Gates[0].Command = &spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code"}, Cwd: ".", TimeoutSeconds: 30, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}}
	plan := compiled(t, policy)
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"test.complete"}, Out: filepath.Join(t.TempDir(), "run.json")}
	first := runOK(t, repo, plan, opts)
	if first.Status != spec.StatusPass {
		t.Fatal("intent-to-add should not be staged", first.Status)
	}
	git(t, repo, "add", "empty.txt")
	opts.Reuse, opts.Out = opts.Out, ""
	second := runOK(t, repo, plan, opts)
	g := priorGate(&second, "test.complete")
	if first.Inputs.SourceSHA256 == second.Inputs.SourceSHA256 || g.Action != "executed" || g.Status != spec.StatusFail || !reflect.DeepEqual(g.StaleCategories, []string{"source"}) {
		t.Fatalf("staged empty-file change reused stale evidence: action=%s status=%s stale=%v", g.Action, g.Status, g.StaleCategories)
	}
	opts.Replay, opts.Reuse, opts.Selected = opts.Reuse, "", nil
	if _, _, err := Run(context.Background(), repo, plan, opts); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("replay accepted changed staging state: %v", err)
	}
}

func TestAffectedIncludesStagedChangesCanceledInWorktree(t *testing.T) {
	repo, policy, _ := fixture(t)
	mustWrite(t, filepath.Join(repo, "source.txt"), "staged")
	git(t, repo, "add", "source.txt")
	mustWrite(t, filepath.Join(repo, "source.txt"), "original")
	paths, err := changedPaths(context.Background(), repo, "HEAD")
	if err != nil || !reflect.DeepEqual(paths, []string{"source.txt"}) {
		t.Fatalf("staged change disappeared: paths=%v err=%v", paths, err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].Command != nil {
			policy.Gates[i].InputPaths = []string{"source.txt"}
		}
	}
	m := runOK(t, repo, compiled(t, policy), Options{Version: "test", Affected: true})
	if priorGate(&m, "lint").Action != "executed" {
		t.Fatal("affected gate omitted", m.SelectedGates)
	}
}

func TestBlockedOutputDoesNotPersistEnvironmentValues(t *testing.T) {
	repo, policy, _ := fixture(t)
	t.Setenv("POLIS_TEST_SECRET", "sentinel-output-secret-never-persist")
	// Avoid instrumentation warnings changing the single-line diagnostic.
	t.Setenv("GOCOVERDIR", t.TempDir())
	for _, mode := range []string{"secret-command", "secret-module", "secret-environment"} {
		t.Run(mode, func(t *testing.T) {
			policy.Gates[0].Command = command(mode)
			policy.Gates[0].Command.Environment.Pass = []string{"POLIS_TEST_SECRET", "GOCOVERDIR"}
			opts := Options{Version: "test", Selected: []string{"test.complete"}, Out: filepath.Join(t.TempDir(), "run.json")}
			m := runOK(t, repo, compiled(t, policy), opts)
			g := priorGate(&m, "test.complete")
			if g.Status != spec.StatusBlocked || !strings.Contains(g.Reason, "intended checks did not run") {
				t.Fatalf("missing prerequisite status/reason: %s %s", g.Status, g.Reason)
			}
			raw, err := json.Marshal(m)
			if err != nil || strings.Contains(string(raw), os.Getenv("POLIS_TEST_SECRET")) {
				t.Fatal("manifest reason leaked an output-derived environment value")
			}
			loaded, err := Load(opts.Out)
			if err != nil || loaded.RunID != m.RunID {
				t.Fatalf("saved record load: %v", err)
			}
		})
	}
}
