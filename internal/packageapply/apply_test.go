package packageapply

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polis-dev/polis-v4/internal/packagebuild"
	"github.com/polis-dev/polis-v4/spec"
)

func policyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in package apply fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./...", "-coverprofile=.polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
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
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	contract := spec.ChangeContract{SchemaVersion: spec.ChangeContractSchemaVersion, Kind: spec.ChangeKindFeature, Behavior: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	contractRaw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(contractPath, contractRaw, 0o600); err != nil {
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

func TestApplyExactBaselinePreservesIndexAndWritesEvidenceOutsideWorktree(t *testing.T) {
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
	if _, err := os.Stat(result.EvidencePath); err != nil {
		t.Fatalf("evidence missing: %v", err)
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
