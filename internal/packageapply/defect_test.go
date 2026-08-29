package packageapply

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v4/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v4/internal/packageverify"
	"github.com/MarcosAlves90/polis/v4/internal/redcapture"
	"github.com/MarcosAlves90/polis/v4/spec"
)

type defectFixture struct {
	repo         string
	contractPath string
	redPath      string
	built        packagebuild.Result
}

func TestDefectRedGreenBuildVerifyApply(t *testing.T) {
	fixture := buildDefectFixture(t)
	assertDefectPackage(t, fixture.built.Path)
	restoreDefectBaseline(t, fixture.repo)
	assertDefectApply(t, fixture.repo, fixture.built)
}

func buildDefectFixture(t *testing.T) defectFixture {
	t.Helper()
	repo := createDefectRepository(t)
	contractPath := writeDefectContract(t)
	writeDefectRegressionTest(t, repo)
	redPath := filepath.Join(t.TempDir(), "red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: contractPath, Out: redPath}); err != nil {
		t.Fatalf("capture red: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc.go"), []byte("package defect\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "defect", Change: "fix-double", Contract: contractPath, RegressionPatch: redPath, Out: t.TempDir()})
	if err != nil {
		t.Fatalf("build defect: %v", err)
	}
	return defectFixture{repo: repo, contractPath: contractPath, redPath: redPath, built: built}
}

func createDefectRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	writeFixtureFile(t, filepath.Join(repo, ".polis", "policy.json"), policyBytes(t))
	writeFixtureFile(t, filepath.Join(repo, "go.mod"), []byte("module example.com/defect\n\ngo 1.23\n"))
	writeFixtureFile(t, filepath.Join(repo, "calc.go"), []byte("package defect\nfunc Double(n int) int { return n }\n"))
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func writeDefectContract(t *testing.T) string {
	t.Helper()
	exit := 1
	contract := spec.ChangeContract{SchemaVersion: spec.ChangeContractSchemaVersion, Kind: spec.ChangeKindDefect,
		Behavior:   spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60},
		Affected:   spec.CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60},
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &spec.CommandSpec{Argv: []string{"go", "test", "./...", "-run", "TestDouble"}, Cwd: ".", TimeoutSeconds: 60}, BaselineExitCode: &exit, BaselineOutputContains: []string{"DOUBLE-REGRESSION"}},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "change.json")
	writeFixtureFile(t, path, raw)
	return path
}

func writeDefectRegressionTest(t *testing.T, repo string) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(repo, "calc_test.go"), []byte("package defect\nimport \"testing\"\nfunc TestDouble(t *testing.T){ if Double(2)!=4 { t.Fatal(\"DOUBLE-REGRESSION\") } }\n"))
}

func writeFixtureFile(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertDefectPackage(t *testing.T, filename string) {
	t.Helper()
	pkg, err := packageverify.Load(filename)
	if err != nil {
		t.Fatalf("verify defect: %v", err)
	}
	if pkg.Change.Kind != spec.ChangeKindDefect || len(pkg.RegressionPatch) == 0 {
		t.Fatalf("package=%+v", pkg.Change)
	}
}

func restoreDefectBaseline(t *testing.T, repo string) {
	t.Helper()
	git(t, repo, "restore", "--", "calc.go")
	if err := os.Remove(filepath.Join(repo, "calc_test.go")); err != nil {
		t.Fatal(err)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("baseline not clean: %q", status)
	}
}

func assertDefectApply(t *testing.T, repo string, built packagebuild.Result) {
	t.Helper()
	result, err := Apply(context.Background(), built.Path, repo)
	if err != nil {
		t.Fatalf("apply defect: %v", err)
	}
	if result.TargetTree != built.TargetTree {
		t.Fatalf("target=%s want=%s", result.TargetTree, built.TargetTree)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "calc.go")); !strings.Contains(string(b), "n * 2") {
		t.Fatalf("calc.go=%s", b)
	}
	if _, err := os.Stat(filepath.Join(repo, "calc_test.go")); err != nil {
		t.Fatalf("regression test missing: %v", err)
	}
}
