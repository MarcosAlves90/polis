package packageapply

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func fixturePassCommand() []string {
	return []string{"git", "rev-parse", "--is-inside-work-tree"}
}

func policyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in package apply fixture"
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func lockedApplyFixtureContract(t *testing.T, repo string) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindBehaviorPreserving,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "produce apply fixture under V6 locked workflow",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "existing Add behavior remains Green")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Add passes on baseline and target", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "consumer HEAD and index are preserved")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "apply bypasses validation")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "clean baseline")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "validated target")},
			FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "baseline or validation mismatch blocks apply")},
		},
		Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "apply-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "apply-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("polis start apply fixture: %v", err)
	}
	return locked
}

func repoWithArtifact(t *testing.T) (repo, artifact, target string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), policyBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "coverage.out"), []byte("mode: set\nexample.com/polisfixture/calc.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/polisfixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc.go"), []byte("package polisfixture\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc_test.go"), []byte("package polisfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	contractPath := lockedApplyFixtureContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "gitrex", Change: "apply-test", Out: t.TempDir(), Contract: contractPath})
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	artifact, target = built.Path, built.TargetTree
	git(t, repo, "restore", "--", "app.txt")
	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture not clean: %q", status)
	}
	return repo, artifact, target
}

func TestApplyExactBaselinePreservesIndexAndUsesEphemeralEvidence(t *testing.T) {
	repo, artifact, target := repoWithArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	result, err := Apply(context.Background(), artifact, repo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s -> %s", beforeIndex, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "new.txt")); string(b) != "new\n" {
		t.Fatalf("new.txt=%q", b)
	}
	if result.EvidencePath != "" {
		t.Fatalf("persistent evidence path=%q", result.EvidencePath)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("git evidence residue: %v", err)
	}
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if !strings.Contains(status, "M app.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("unexpected post-apply status: %q", status)
	}
	if strings.Contains(status, "polis-results") {
		t.Fatalf("evidence polluted worktree: %q", status)
	}
}

func TestApplyRejectsDirtyWorktreeBeforeMutation(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("user work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected dirty worktree rejection")
	}
	after := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if after != before {
		t.Fatalf("dirty state changed: before=%q after=%q", before, after)
	}
}

func TestApplyRejectsWrongHead(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "other.txt")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "other")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected baseline mismatch")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "base\n" {
		t.Fatalf("app mutated on wrong head: %q", b)
	}
}

func TestApplyRejectsMalformedPackage(t *testing.T) {
	repo, _, _ := repoWithArtifact(t)
	bad := filepath.Join(t.TempDir(), "bad.polis")
	if err := os.WriteFile(bad, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), bad, repo); err == nil {
		t.Fatal("expected malformed package rejection")
	}
}

func TestApplySecondAttemptFailsClosedBecauseWorktreeIsNoLongerClean(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if _, err := Apply(context.Background(), artifact, repo); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected second apply rejection")
	}
	after := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if after != before {
		t.Fatalf("second apply changed state: before=%q after=%q", before, after)
	}
}

func TestApplyRejectsNonRepositoryTarget(t *testing.T) {
	_, artifact, _ := repoWithArtifact(t)
	if _, err := Apply(context.Background(), artifact, t.TempDir()); err == nil {
		t.Fatal("expected non-repository target rejection")
	}
}

func TestApplyUsesCurrentDirectoryWhenRepoEmpty(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if _, err := Apply(context.Background(), artifact, ""); err != nil {
		t.Fatalf("Apply() with default repo error = %v", err)
	}
}

func TestPreflightValidatesWithoutMutatingConsumerFiles(t *testing.T) {
	repo, artifact, target := repoWithArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeStatus := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	result, err := Preflight(context.Background(), artifact, repo)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s -> %s", beforeIndex, got)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("status changed: before=%q after=%q", beforeStatus, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "base\n" {
		t.Fatalf("preflight mutated app.txt: %q", b)
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("preflight created new.txt: %v", err)
	}
}

func externalApplyPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in zero-residue apply fixture"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			cmd := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &cmd})
		case "coverage":
			cmd := spec.CommandSpec{Argv: []string{"cp", "coverage.fixture", "coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
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

func lockedExternalApplyContract(t *testing.T, repo, policyPath string) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindBehaviorPreserving,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"app.txt", "new.txt", "calc_test.go"}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}}, DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective: "apply without target residue", Requirements: []spec.SpecificationClause{clause("REQ-001", "consumer applies package-contained policy without repository policy state")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "apply changes only payload paths", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "HEAD and index are preserved")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "tool metadata remains in target Git directory")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "external-policy artifact")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "payload-only working tree")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "baseline or validation mismatch blocks mutation")},
		},
		Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "apply-external-draft.json")
	lockedPath := filepath.Join(t.TempDir(), "apply-external-locked.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Policy: policyPath, Contract: draftPath, Out: lockedPath}); err != nil {
		t.Fatalf("start external apply fixture: %v", err)
	}
	return lockedPath
}

func repoWithExternalArtifact(t *testing.T) (repo, artifact, target string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "external-consumer")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	files := map[string]string{
		"app.txt":          "base\n",
		"go.mod":           "module example.com/applyexternal\n\ngo 1.23\n",
		"calc.go":          "package applyexternal\n\nfunc Add(a, b int) int { return a + b }\n",
		"calc_test.go":     "package applyexternal\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n",
		"coverage.out":     "mode: set\nexample.com/applyexternal/calc.go:3.24,3.38 1 1\n",
		"coverage.fixture": "mode: set\nexample.com/applyexternal/calc.go:3.24,3.38 1 1\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policyPath, externalApplyPolicyBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	contract := lockedExternalApplyContract(t, repo, policyPath)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Policy: policyPath, Project: "external", Change: "apply-zero-residue", Out: t.TempDir(), Contract: contract})
	if err != nil {
		t.Fatalf("build external fixture: %v", err)
	}
	artifact, target = built.Path, built.TargetTree
	git(t, repo, "restore", "--", "app.txt")
	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture not clean: %q", status)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("unexpected .polis baseline state: %v", err)
	}
	return repo, artifact, target
}

func TestPreflightExternalPolicyNeedsNoRepositoryPolicyState(t *testing.T) {
	repo, artifact, target := repoWithExternalArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(context.Background(), artifact, repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed")
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed")
	}
	afterConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatal("git config changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("preflight git residue: %v", err)
	}
}

func TestApplyExternalPolicyLeavesNoToolGitMetadata(t *testing.T) {
	repo, artifact, target := repoWithExternalArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeRefs := git(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	beforeConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), artifact, repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if result.EvidencePath != "" {
		t.Fatalf("persistent evidence path=%q", result.EvidencePath)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed")
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed")
	}
	if got := git(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"); got != beforeRefs {
		t.Fatalf("refs changed\nbefore=%s\nafter=%s", beforeRefs, got)
	}
	afterConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatal("git config changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("git metadata residue: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("worktree residue: %v", err)
	}
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if !strings.Contains(status, "M app.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("unexpected payload status: %q", status)
	}
}
