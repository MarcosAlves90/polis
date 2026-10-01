package gaterun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestGateRunHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--gate-helper" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	mode := os.Args[index+1]
	switch mode {
	case "pass":
		fmt.Print("safe output")
	case "secret":
		fmt.Print(os.Getenv("POLIS_TEST_SECRET"))
	case "secret-command":
		fmt.Fprintf(os.Stderr, "sh: line 1: %s: command not found\n", os.Getenv("POLIS_TEST_SECRET"))
		os.Exit(127)
	case "secret-module":
		fmt.Fprintf(os.Stderr, "ModuleNotFoundError: No module named '%s'\n", os.Getenv("POLIS_TEST_SECRET"))
		os.Exit(1)
	case "secret-environment":
		fmt.Fprintf(os.Stderr, "sh: line 1: %s: unbound variable\n", os.Getenv("POLIS_TEST_SECRET"))
		os.Exit(1)
	case "mutate":
		_ = os.WriteFile("source.txt", []byte("mutated"), 0o600)
	case "fail":
		os.Exit(7)
	}
	os.Exit(0)
}

func command(mode string) *spec.CommandSpec {
	return &spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=TestGateRunHelper", "--", "--gate-helper", mode}, Cwd: ".", TimeoutSeconds: 30, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}}
}

func fixture(t *testing.T) (string, spec.Policy, policyplan.Plan) {
	t.Helper()
	repo := t.TempDir()
	policy := spec.Policy{SchemaVersion: 3, ValidationLevel: spec.ValidationLevelStandard}
	for _, id := range spec.ProjectGateOrder {
		reason := "fixture not applicable"
		g := spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason}
		if id == "test.complete" || id == "lint" || id == "build" {
			g = spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: command("pass")}
		}
		policy.Gates = append(policy.Gates, g)
	}
	mustWrite(t, filepath.Join(repo, "source.txt"), "original")
	if err := os.Mkdir(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(policy)
	mustWrite(t, filepath.Join(repo, ".polis", "policy.json"), string(raw)+"\n")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"config", "core.autocrlf", "false"}, {"add", "."}, {"commit", "-qm", "baseline"}} {
		git(t, repo, args...)
	}
	plan, err := policyplan.Load(context.Background(), policyplan.Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	return repo, policy, plan
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	s, err := gitutil.Output(context.Background(), repo, nil, nil, args...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func compiled(t *testing.T, policy spec.Policy) policyplan.Plan {
	t.Helper()
	p, err := policyplan.Compile(policy)
	if err != nil {
		t.Fatal(err)
	}
	p.PolicySHA256 = digest(policy)
	return p
}

func runOK(t *testing.T, repo string, plan policyplan.Plan, opts Options) Manifest {
	t.Helper()
	m, _, err := Run(context.Background(), repo, plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestIdentityCoversCategoriesWithoutSecretValues(t *testing.T) {
	in := Inputs{Repository: "/repo", Head: "head", SourceSHA256: "source", PolicySHA256: "policy", POLISVersion: "6.9.0", Runtime: "runtime", EnvironmentID: "env-v1"}
	g := spec.GatePolicy{ID: "lint", Mode: "command", Command: command("pass")}
	before := identity(in, g, map[string]string{"test.complete": "result"})
	for _, category := range []string{"source", "contract", "baseline", "policy", "command", "environment", "dependencies", "polis_version"} {
		t.Run(category, func(t *testing.T) {
			after := Identity{Categories: map[string]string{}}
			for k, v := range before.Categories {
				after.Categories[k] = v
			}
			after.Categories[category] = "different"
			after.SHA256 = digest(after.Categories)
			if got := differences(before, after); !reflect.DeepEqual(got, []string{category}) {
				t.Fatal(got)
			}
		})
	}
	if got := differences(Identity{}, before); !reflect.DeepEqual(got, []string{"identity_missing_or_invalid"}) {
		t.Fatal(got)
	}
	repo, policy, _ := fixture(t)
	t.Setenv("POLIS_TEST_SECRET", "secret-value-never-record-this")
	policy.Gates[0].Command = command("secret")
	policy.Gates[0].Command.Environment.Pass = []string{"POLIS_TEST_SECRET"}
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"test.complete"}, Out: filepath.Join(t.TempDir(), "run.json")}
	m := runOK(t, repo, compiled(t, policy), opts)
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), os.Getenv("POLIS_TEST_SECRET")) || strings.Contains(string(raw), "\"stdout\"") {
		t.Fatalf("unsafe manifest: %s", raw)
	}
	loaded, err := Load(opts.Out)
	if err != nil || loaded.RunID != m.RunID {
		t.Fatalf("load=%+v err=%v", loaded, err)
	}
}

func TestSelectionIncludesDependenciesAndConservativeFallback(t *testing.T) {
	_, policy, _ := fixture(t)
	policy.Gates[4].DependsOn = []string{"lint"}
	policy.Gates[2].DependsOn = []string{"test.complete"}
	plan := compiled(t, policy)
	set, _, err := selectGates(plan, []string{"build"}, false, nil)
	if err != nil || len(set) != 3 || !set["lint"] || !set["test.complete"] {
		t.Fatalf("set=%v err=%v", set, err)
	}
	for _, request := range [][]string{{"unknown"}, {"coverage"}, {"lint", "lint"}} {
		if _, _, err := selectGates(plan, request, false, nil); err == nil {
			t.Fatal("accepted invalid selection", request)
		}
	}
	policy.Gates[0].InputPaths = []string{"tests"}
	policy.Gates[2].InputPaths = []string{"src"}
	policy.Gates[4].InputPaths = []string{"build"}
	policy.Gates[2].DependsOn, policy.Gates[4].DependsOn = nil, nil
	plan = compiled(t, policy)
	set, _, _ = selectGates(plan, nil, true, []string{"src/a.go"})
	if len(set) != 1 || !set["lint"] {
		t.Fatal(set)
	}
	set, _, _ = selectGates(plan, nil, true, []string{"unknown.txt"})
	if len(set) != 3 {
		t.Fatal("unmapped file skipped checks", set)
	}
	set, _, _ = selectGates(plan, nil, true, nil)
	if len(set) != 0 {
		t.Fatal(set)
	}
	policy.Gates[0].InputPaths = nil
	set, _, _ = selectGates(compiled(t, policy), nil, true, nil)
	if !set["test.complete"] {
		t.Fatal("unmapped gate omitted", set)
	}
}

func TestRunReusesOnlyValidEvidenceAndNeverOnReplay(t *testing.T) {
	repo, policy, _ := fixture(t)
	policy.Gates[2].DependsOn = []string{"test.complete"}
	plan := compiled(t, policy)
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: filepath.Join(t.TempDir(), "run.json")}
	first := runOK(t, repo, plan, opts)
	opts.Reuse, opts.Out = opts.Out, ""
	second := runOK(t, repo, plan, opts)
	if priorGate(&second, "lint").Action != "reused" || priorGate(&second, "test.complete").Action != "reused" {
		t.Fatal(second)
	}
	for _, tc := range []struct {
		name   string
		modify func(*Manifest)
		opts   func(*Options)
	}{
		{"identity", func(m *Manifest) { priorGate(m, "lint").Identity = Identity{} }, nil},
		{"failed", func(m *Manifest) { priorGate(m, "lint").Status = spec.StatusFail }, nil},
		{"output", func(m *Manifest) { priorGate(m, "lint").Observation = nil }, nil},
		{"dependency", func(m *Manifest) { priorGate(m, "test.complete").Observation.StdoutSHA256 = digest("different-output") }, nil},
		{"environment", nil, func(o *Options) { o.EnvironmentID = "env-v2" }},
		{"unversioned", nil, func(o *Options) { o.EnvironmentID = "" }},
		{"version", nil, func(o *Options) { o.Version = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(first)
			var old Manifest
			_ = json.Unmarshal(raw, &old)
			if tc.modify != nil {
				tc.modify(&old)
			}
			_ = old.seal()
			path := filepath.Join(t.TempDir(), "old.json")
			if err := Write(repo, path, old); err != nil {
				t.Fatal(err)
			}
			o := opts
			o.Reuse = path
			if tc.opts != nil {
				tc.opts(&o)
			}
			m := runOK(t, repo, plan, o)
			if priorGate(&m, "lint").Action != "executed" {
				t.Fatalf("reused invalid evidence: %v", m)
			}
		})
	}
	opts.Replay, opts.Reuse, opts.Selected = opts.Reuse, "", nil
	replay := runOK(t, repo, plan, opts)
	if replay.RunID == first.RunID || replay.ReplayOf != first.RunID || priorGate(&replay, "lint").Action != "executed" {
		t.Fatal(replay)
	}
	mustWrite(t, filepath.Join(repo, "source.txt"), "changed")
	if _, _, err := Run(context.Background(), repo, plan, opts); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatal(err)
	}
}

func TestRunSourceMutationsAreNotCurrentAndInputsStayUnchanged(t *testing.T) {
	repo, policy, plan := fixture(t)
	head, before, err := snapshot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	first := runOK(t, repo, plan, Options{Version: "test"})
	_, after, _ := snapshot(context.Background(), repo)
	if before != after || first.Status != spec.StatusPass {
		t.Fatal(first)
	}
	policy.Gates[0].Command = command("mutate")
	m := runOK(t, repo, compiled(t, policy), Options{Version: "test"})
	if m.Current || m.Status != spec.StatusBlocked || m.Inputs.SourceSHA256 != before || m.Inputs.Head != head {
		t.Fatal(m)
	}
	if priorGate(&m, "lint").Status != spec.StatusBlocked || priorGate(&m, "test.complete").Status != spec.StatusBlocked {
		t.Fatal(m)
	}
}

func TestSnapshotsCoverIndexUntrackedDeletionAndBoundaries(t *testing.T) {
	repo, _, _ := fixture(t)
	_, before, _ := snapshot(context.Background(), repo)
	mustWrite(t, filepath.Join(repo, "source.txt"), "staged")
	git(t, repo, "add", "source.txt")
	mustWrite(t, filepath.Join(repo, "source.txt"), "original")
	_, after, _ := snapshot(context.Background(), repo)
	if before == after {
		t.Fatal("index not covered")
	}
	git(t, repo, "reset", "-q", "HEAD", "--", "source.txt")
	if err := os.Remove(filepath.Join(repo, "source.txt")); err != nil {
		t.Fatal(err)
	}
	_, deleted, err := snapshot(context.Background(), repo)
	if err != nil || deleted == before {
		t.Fatal(deleted, err)
	}
	mustWrite(t, filepath.Join(repo, "new.txt"), "new")
	paths, err := changedPaths(context.Background(), repo, "HEAD")
	if err != nil || !reflect.DeepEqual(paths, []string{"new.txt", "source.txt"}) {
		t.Fatal(paths, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err == nil {
		if _, _, err := snapshot(context.Background(), repo); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestManifestTamperingAndInvalidOptionsFailClosed(t *testing.T) {
	repo, _, plan := fixture(t)
	m := runOK(t, repo, plan, Options{Version: "test"})
	path := filepath.Join(t.TempDir(), "run.json")
	if err := Write(repo, path, m); err != nil {
		t.Fatal(err)
	}
	if err := Write(repo, path, m); err == nil {
		t.Fatal("overwrote record")
	}
	if err := Write(repo, filepath.Join(repo, "run.json"), m); err == nil {
		t.Fatal("wrote inside worktree")
	}
	m.Status = spec.StatusFail
	raw, _ := json.Marshal(m)
	mustWrite(t, path, string(raw))
	if _, err := Load(path); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	mustWrite(t, path, "{} {}")
	if _, err := Load(path); err == nil {
		t.Fatal("trailing data accepted")
	}
	mustWrite(t, path, strings.Repeat("x", MaxManifestBytes+1))
	if _, err := Load(path); err == nil {
		t.Fatal("oversized manifest accepted")
	}
	for _, opts := range []Options{{Jobs: 17, Version: "test"}, {}, {Version: "test", EnvironmentID: "secret=value"}, {Version: "test", EnvironmentID: strings.Repeat("a", 129)}, {Version: "test", Replay: path, Reuse: path}, {Version: "test", Out: filepath.Join(repo, "record.json")}, {Version: "test", Out: filepath.Join(t.TempDir(), "missing", "record.json")}, {Version: "test", Reuse: "missing-record"}, {Version: "test", Contract: "missing-contract"}} {
		if _, _, err := Run(context.Background(), repo, plan, opts); err == nil {
			t.Fatal("invalid options accepted", opts)
		}
	}
}

func TestContractBaselineBindingAndReplayEmptySelection(t *testing.T) {
	repo, policy, plan := fixture(t)
	cmd := *command("pass")
	clauses := func(id string) []spec.SpecificationClause {
		return []spec.SpecificationClause{{ID: id, Statement: "test"}}
	}
	development := &spec.DevelopmentSpecification{Objective: "test", Requirements: []spec.SpecificationClause{{ID: "REQ-1", Statement: "test"}}, AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-1", Statement: "test", Requirements: []string{"REQ-1"}, Proof: "regression"}}, Invariants: clauses("INV-1"), ForbiddenStates: clauses("FORBID-1"), Inputs: clauses("INPUT-1"), Outputs: clauses("OUTPUT-1"), FailureSemantics: clauses("FAIL-1")}
	lock, err := devlock.Snapshot(context.Background(), repo, development)
	if err != nil {
		t.Fatal(err)
	}
	contract := spec.ChangeContract{SchemaVersion: 4, Kind: spec.ChangeKindBehaviorPreserving, Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"."}}, DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV2, Specification: development, BaselineLock: &lock, Behavior: cmd, Affected: cmd, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &cmd}}
	raw, _ := json.Marshal(contract)
	path := filepath.Join(t.TempDir(), "contract.json")
	mustWrite(t, path, string(raw))
	m := runOK(t, repo, plan, Options{Version: "test", Contract: path})
	if m.Inputs.ContractSHA256 == "" || m.Inputs.Baseline == nil || m.Inputs.Baseline.BaseCommit != lock.BaseCommit {
		t.Fatal(m)
	}
	plan.PolicySHA256 = digest("wrong")
	if _, _, err := Run(context.Background(), repo, plan, Options{Version: "test", Contract: path}); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatal(err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].Command != nil {
			policy.Gates[i].InputPaths = []string{"source.txt"}
		}
	}
	plan = compiled(t, policy)
	opts := Options{Version: "test", EnvironmentID: "env-v1", Affected: true, Out: filepath.Join(t.TempDir(), "empty.json")}
	first := runOK(t, repo, plan, opts)
	if len(first.SelectedGates) != 0 {
		t.Fatal(first)
	}
	opts.Replay, opts.Out, opts.Affected = opts.Out, "", false
	replayed := runOK(t, repo, plan, opts)
	if len(replayed.SelectedGates) != 0 {
		t.Fatal("empty replay ran full plan", replayed)
	}
}
