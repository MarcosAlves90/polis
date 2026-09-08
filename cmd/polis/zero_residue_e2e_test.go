package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func zeroResiduePolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in zero-residue E2E"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			cmd := spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &cmd})
		case "coverage":
			cmd := spec.CommandSpec{Argv: []string{"go", "test", "-coverprofile=coverage.out", "./..."}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &cmd, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	raw, err := json.MarshalIndent(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func zeroResidueDraft(t *testing.T) []byte {
	t.Helper()
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"feature.go", "feature_test.go"}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"feature_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "add a neutral feature without producer residue",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "the target receives only intended feature changes")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Feature returns the expected value", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "target Git metadata remains free of producer residue")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "tool-owned files or Git objects remain after apply")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "clean Git baseline and external policy")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "feature.go and feature_test.go payload")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "validation mismatch blocks mutation")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"undefined: Feature"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gitRunE2E(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func objectSet(t *testing.T, repo string) []string {
	t.Helper()
	root := filepath.Join(repo, ".git", "objects")
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func relativeFileSet(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func assertWorktreeHasNoToolReference(t *testing.T, repo string) {
	t.Helper()
	err := filepath.WalkDir(repo, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path == filepath.Join(repo, ".git") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(raw)), "polis") {
			rel, _ := filepath.Rel(repo, path)
			t.Fatalf("worktree file %s contains tool reference", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestZeroResidueExternalPolicyWorkflow(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/zeroresidue\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package zeroresidue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunE2E(t, repo, "init", "-q")
	gitRunE2E(t, repo, "add", ".")
	gitRunE2E(t, repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "base")

	baselineHead := gitRunE2E(t, repo, "rev-parse", "HEAD")
	baselineIndex := gitRunE2E(t, repo, "write-tree")
	baselineRefs := gitRunE2E(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	baselineConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	baselineObjects := objectSet(t, repo)
	baselineGitFiles := relativeFileSet(t, filepath.Join(repo, ".git"))

	external := t.TempDir()
	policyPath := filepath.Join(external, "policy.json")
	draftPath := filepath.Join(external, "draft.json")
	lockedPath := filepath.Join(external, "locked.json")
	redPath := filepath.Join(external, "red.patch")
	outDir := filepath.Join(external, "out")
	if err := os.WriteFile(policyPath, zeroResiduePolicyBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, zeroResidueDraft(t), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--policy", policyPath, "--contract", draftPath, "--out", lockedPath}, &out, &errOut); code != 0 {
		t.Fatalf("start code=%d stderr=%s", code, errOut.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "feature_test.go"), []byte("package zeroresidue\n\nimport \"testing\"\n\nfunc TestFeature(t *testing.T) { if Feature() != 42 { t.Fatal(\"unexpected\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"capture-red", "--repo", repo, "--contract", lockedPath, "--out", redPath}, &out, &errOut); code != 0 {
		t.Fatalf("capture-red code=%d stderr=%s", code, errOut.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte("package zeroresidue\n\nfunc Feature() int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"build", "--repo", repo, "--policy", policyPath, "--project", "fixture", "--change", "zero-residue", "--contract", lockedPath, "--regression-patch", redPath, "--out", outDir}, &out, &errOut); code != 0 {
		t.Fatalf("build code=%d stderr=%s", code, errOut.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifact entries=%v err=%v", entries, err)
	}
	artifact := filepath.Join(outDir, entries[0].Name())
	for _, command := range [][]string{{"verify", artifact}, {"inspect", "--format", "json", artifact}} {
		out.Reset()
		errOut.Reset()
		if code := run(command, &out, &errOut); code != 0 {
			t.Fatalf("%v code=%d stderr=%s", command, code, errOut.String())
		}
	}

	gitRunE2E(t, repo, "restore", "--", "feature.go")
	if err := os.Remove(filepath.Join(repo, "feature_test.go")); err != nil {
		t.Fatal(err)
	}
	if status := gitRunE2E(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("consumer baseline dirty: %q", status)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"preflight", "--repo", repo, artifact}, &out, &errOut); code != 0 {
		t.Fatalf("preflight code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"apply", "--repo", repo, artifact}, &out, &errOut); code != 0 {
		t.Fatalf("apply code=%d stderr=%s", code, errOut.String())
	}

	if got := gitRunE2E(t, repo, "rev-parse", "HEAD"); got != baselineHead {
		t.Fatalf("HEAD changed: %s -> %s", baselineHead, got)
	}
	if got := gitRunE2E(t, repo, "write-tree"); got != baselineIndex {
		t.Fatalf("index changed: %s -> %s", baselineIndex, got)
	}
	if got := gitRunE2E(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"); got != baselineRefs {
		t.Fatalf("refs changed\nbefore=%s\nafter=%s", baselineRefs, got)
	}
	afterConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterConfig, baselineConfig) {
		t.Fatal("git config changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("worktree tool residue: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("git tool residue: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "worktrees")); !os.IsNotExist(err) {
		t.Fatalf("temporary worktree metadata remains: %v", err)
	}
	if gotObjects := objectSet(t, repo); !reflect.DeepEqual(gotObjects, baselineObjects) {
		t.Fatalf("Git object residue detected\nbefore=%v\nafter=%v", baselineObjects, gotObjects)
	}
	if gotGitFiles := relativeFileSet(t, filepath.Join(repo, ".git")); !reflect.DeepEqual(gotGitFiles, baselineGitFiles) {
		t.Fatalf("Git metadata file residue detected\nbefore=%v\nafter=%v", baselineGitFiles, gotGitFiles)
	}
	assertWorktreeHasNoToolReference(t, repo)
	status := gitRunE2E(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if !strings.Contains(status, "M feature.go") || !strings.Contains(status, "?? feature_test.go") {
		t.Fatalf("unexpected final payload status: %q", status)
	}
	if strings.Contains(strings.ToLower(status), "polis") {
		t.Fatalf("tool reference in final status: %q", status)
	}
}
