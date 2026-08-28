package packagebuild

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v4/internal/packageverify"
	"github.com/MarcosAlves90/polis/v4/internal/redcapture"
	"github.com/MarcosAlves90/polis/v4/spec"
)

func testPolicyBytes(t *testing.T, failing bool) []byte {
	t.Helper()
	reason := "not applicable in package build fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			argv := []string{"go", "test", "./..."}
			if failing {
				argv = []string{"git", "diff", "--exit-code", "HEAD", "--", "app.txt"}
			}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 60}})
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

func newRepo(t *testing.T, failingPolicy bool) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), testPolicyBytes(t, failingPolicy), 0o644); err != nil {
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
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func withFeatureContract(t *testing.T, opts Options) Options {
	t.Helper()
	contract := spec.ChangeContract{
		SchemaVersion: spec.ChangeContractSchemaVersion,
		Kind:          spec.ChangeKindFeature,
		Behavior:      spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60},
		Affected:      spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60},
		Regression:    spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect},
	}
	b, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	opts.Contract = path
	return opts
}

func TestBuildCreatesVerifiedPackageWithoutMutatingSourceState(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHEAD := runGit(t, repo, "rev-parse", "HEAD")
	beforeIndex := runGit(t, repo, "write-tree")
	beforeStatus := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	out := filepath.Join(t.TempDir(), "out")

	result, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "add-new-file", Out: out}))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.HasSuffix(result.Path, ".polis") || !strings.Contains(filepath.Base(result.Path), "polis-gitrex-add-new-file-") {
		t.Fatalf("path=%q", result.Path)
	}
	if _, err := packageverify.Verify(result.Path); err != nil {
		t.Fatalf("built package verify failed: %v", err)
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != beforeHEAD {
		t.Fatalf("HEAD changed: %s -> %s", beforeHEAD, got)
	}
	if got := runGit(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("real index changed: %s -> %s", beforeIndex, got)
	}
	if got := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("source status changed:\nBEFORE %q\nAFTER  %q", beforeStatus, got)
	}
}

func TestBuildRejectsStagedChanges(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "app.txt")
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "staged-change", Out: t.TempDir()})); err == nil {
		t.Fatal("expected staged-change rejection")
	}
}

func TestBuildRejectsCleanWorktree(t *testing.T) {
	repo := newRepo(t, false)
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "nothing", Out: t.TempDir()})); err == nil {
		t.Fatal("expected clean worktree rejection")
	}
}

func TestBuildRejectsModifiedPolicy(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(repo, ".polis", "policy.json"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n")
	_ = f.Close()
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "policy-change", Out: t.TempDir()})); err == nil {
		t.Fatal("expected modified policy rejection")
	}
}

func TestBuildDoesNotPackageValidationFailure(t *testing.T) {
	repo := newRepo(t, true)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "failing-validation", Out: out})); err == nil {
		t.Fatal("expected validation failure")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected output after failed validation: %v", entries)
	}
}

func TestBuildRejectsMissingOrMalformedCommittedPolicy(t *testing.T) {
	t.Run("missing working policy", func(t *testing.T) {
		repo := newRepo(t, false)
		if err := os.Remove(filepath.Join(repo, ".polis", "policy.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "missing-policy", Out: t.TempDir()})); err == nil {
			t.Fatal("expected missing policy rejection")
		}
	})

	t.Run("malformed committed policy", func(t *testing.T) {
		repo := newRepo(t, false)
		if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), []byte(`{"schema_version":1,"gates":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, repo, "add", ".polis/policy.json")
		runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "bad policy")
		if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "bad-policy", Out: t.TempDir()})); err == nil {
			t.Fatal("expected malformed policy rejection")
		}
	})
}

func TestBuildRejectsInvalidIdentityAndMissingInputs(t *testing.T) {
	if _, err := Build(context.Background(), Options{}); err == nil {
		t.Fatal("expected required-input rejection")
	}
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "Bad Project", Change: "change", Out: t.TempDir()})); err == nil {
		t.Fatal("expected invalid project rejection")
	}
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "Bad Change", Out: t.TempDir()})); err == nil {
		t.Fatal("expected invalid change rejection")
	}
}

func TestCopyExclusiveRefusesExistingTarget(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source")
	dst := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(src, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyExclusive(src, dst); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected collision error, got %v", err)
	}
	b, _ := os.ReadFile(dst)
	if string(b) != "existing" {
		t.Fatalf("target overwritten: %q", b)
	}
}

func TestBuildRejectsPolicyNotCommittedInHead(t *testing.T) {
	repo := newRepo(t, false)
	runGit(t, repo, "rm", "-q", ".polis/policy.json")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "remove policy")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), testPolicyBytes(t, false), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), withFeatureContract(t, Options{Repo: repo, Project: "gitrex", Change: "uncommitted-policy", Out: t.TempDir()})); err == nil || !strings.Contains(err.Error(), "must exist in HEAD") {
		t.Fatalf("expected committed-policy error, got %v", err)
	}
}

func TestBuildDefectRequiresAndReproducesRedGreen(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "double.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "buggy double")
	exit := 1
	contract := spec.ChangeContract{SchemaVersion: spec.ChangeContractSchemaVersion, Kind: spec.ChangeKindDefect, Behavior: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./...", "-run", "TestDouble"}, Cwd: ".", TimeoutSeconds: 60}, BaselineExitCode: &exit, BaselineOutputContains: []string{"DOUBLE-RED"}}}
	raw, _ := json.Marshal(contract)
	contractPath := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(contractPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestDouble(t *testing.T){if Double(2)!=4{t.Fatal(\"DOUBLE-RED\")}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	redPath := filepath.Join(t.TempDir(), "red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: contractPath, Out: redPath}); err != nil {
		t.Fatalf("capture-red: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "fix-double", Out: t.TempDir(), Contract: contractPath, RegressionPatch: redPath})
	if err != nil {
		t.Fatalf("Build defect: %v", err)
	}
	pkg, err := packageverify.Load(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Change.Kind != spec.ChangeKindDefect || len(pkg.RegressionPatch) == 0 {
		t.Fatalf("pkg=%+v", pkg.Change)
	}
}

func TestBuildRejectsContractPlacementAndRegressionModeMisuse(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "change.json")
	feature := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	raw, _ := json.Marshal(feature)
	os.WriteFile(inside, raw, 0o600)
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "inside-contract", Out: t.TempDir(), Contract: inside}); err == nil {
		t.Fatal("expected inside contract rejection")
	}
	_ = os.Remove(inside)
	outside := filepath.Join(t.TempDir(), "change.json")
	os.WriteFile(outside, raw, 0o600)
	red := filepath.Join(t.TempDir(), "red.patch")
	os.WriteFile(red, []byte("not used"), 0o600)
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "feature-with-red", Out: t.TempDir(), Contract: outside, RegressionPatch: red}); err == nil {
		t.Fatal("expected non-defect regression patch rejection")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{}`), 0o600)
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "bad-contract", Out: t.TempDir(), Contract: bad}); err == nil {
		t.Fatal("expected malformed contract rejection")
	}
}

func TestBuildDefectRequiresRegressionPatchAndRejectsProbePathOutsideTarget(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int{return n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "double.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "buggy")
	exit := 1
	contract := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./...", "-run", "TestDouble"}, Cwd: ".", TimeoutSeconds: 60}, BaselineExitCode: &exit, BaselineOutputContains: []string{"PATH-RED"}}}
	raw, _ := json.Marshal(contract)
	cp := filepath.Join(t.TempDir(), "change.json")
	os.WriteFile(cp, raw, 0o600)
	if err := os.WriteFile(filepath.Join(repo, "probe_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestDouble(t *testing.T){if Double(2)!=4{t.Fatal(\"PATH-RED\")}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "missing-red", Out: t.TempDir(), Contract: cp}); err == nil {
		t.Fatal("expected missing regression patch rejection")
	}
	red := filepath.Join(t.TempDir(), "red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: cp, Out: red}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "probe_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int{return n*2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "probe-path-missing", Out: t.TempDir(), Contract: cp, RegressionPatch: red}); err == nil || !strings.Contains(err.Error(), "absent from final payload") {
		t.Fatalf("expected probe path rejection, got %v", err)
	}
}

func TestReadExternalInputRejectsDirectoryAndOversize(t *testing.T) {
	repo := t.TempDir()
	dir := t.TempDir()
	if _, err := readExternalInput(repo, dir, 10); err == nil {
		t.Fatal("expected directory rejection")
	}
	p := filepath.Join(t.TempDir(), "big")
	os.WriteFile(p, []byte("123456"), 0o600)
	if _, err := readExternalInput(repo, p, 3); err == nil {
		t.Fatal("expected size rejection")
	}
	if _, err := readExternalInput(repo, filepath.Join(t.TempDir(), "missing"), 3); err == nil {
		t.Fatal("expected missing rejection")
	}
}

func TestReadExternalInputRejectsPhysicalAliasIntoRepo(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "real"), alias); err != nil {
		t.Fatal(err)
	}
	insideReal := filepath.Join(repo, "change.json")
	if err := os.WriteFile(insideReal, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	insideAlias := filepath.Join(alias, "repo", "change.json")
	if _, err := readExternalInput(repo, insideAlias, 1024); err == nil {
		t.Fatal("expected physical alias into repo to be rejected")
	}
}
