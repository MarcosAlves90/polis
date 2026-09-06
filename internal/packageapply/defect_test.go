package packageapply

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/redcapture"
	"github.com/MarcosAlves90/polis/v6/spec"
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
	contractPath := writeDefectContract(t, repo)
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
	writeFixtureFile(t, filepath.Join(repo, ".polis", "coverage.out"), []byte("mode: set\nexample.com/polisfixture/calc.go:1.1,1.2 1 1\n"))
	writeFixtureFile(t, filepath.Join(repo, "go.mod"), []byte("module example.com/defect\n\ngo 1.23\n"))
	writeFixtureFile(t, filepath.Join(repo, "calc.go"), []byte("package defect\nfunc Double(n int) int { return n }\n"))
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func writeDefectContract(t *testing.T, repo string) string {
	t.Helper()
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestDouble"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindDefect,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "fix Double through locked V6 Red-to-Green",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "Double returns twice its input")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Double(2) equals 4", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "captured test remains immutable")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "defect bypasses Red")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "integer input")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "doubled output")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "wrong result fails regression")},
		},
		Behavior: pass, Affected: pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"DOUBLE-REGRESSION"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "defect-draft-v3.json")
	writeFixtureFile(t, draftPath, raw)
	locked := filepath.Join(t.TempDir(), "defect-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("polis start defect fixture: %v", err)
	}
	return locked
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
