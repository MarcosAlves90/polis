package packagebuild

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/redcapture"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func fixturePassCommand() []string {
	return []string{"git", "rev-parse", "--is-inside-work-tree"}
}

func testPolicyBytes(t *testing.T, failing bool) []byte {
	t.Helper()
	reason := "not applicable in package build fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			argv := fixturePassCommand()
			if failing {
				argv = []string{"git", "diff", "--exit-code", "HEAD", "--", "app.txt"}
			}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 60}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.LegacyPolicySchemaVersion, Gates: gates})
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
		SchemaVersion: spec.LegacyChangeContractSchemaVersion,
		Kind:          spec.ChangeKindFeature,
		Behavior:      spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60},
		Affected:      spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60},
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

func newV6Repo(t *testing.T, failingPolicy bool) string {
	t.Helper()
	repo := newRepo(t, failingPolicy)
	upgradeFixturePolicyToV3(t, repo)
	runGit(t, repo, "add", ".polis/policy.json")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "upgrade policy to v3")
	return repo
}

func lockedCharacterizationContract(t *testing.T, repo string, allowedPaths ...string) string {
	t.Helper()
	if len(allowedPaths) == 0 {
		allowedPaths = []string{"."}
	}
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindBehaviorPreserving,
		Scope:             &spec.ChangeScope{AllowedPaths: allowedPaths},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "preserve package-build fixture behavior",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "existing Add behavior remains Green")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Add characterization passes on baseline and target", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "source HEAD and index remain unchanged")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "target changes characterized Add behavior")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "committed baseline")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "verified POLIS package")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "failed characterization or policy blocks build")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	lockedPath := filepath.Join(t.TempDir(), "locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: lockedPath}); err != nil {
		t.Fatalf("polis start fixture: %v", err)
	}
	return lockedPath
}

func TestBuildCreatesVerifiedPackageWithoutMutatingSourceState(t *testing.T) {
	repo := newV6Repo(t, false)
	contract := lockedCharacterizationContract(t, repo, ".")
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

	result, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "add-new-file", Out: out, Contract: contract})
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
	repo := newV6Repo(t, true)
	contract := lockedCharacterizationContract(t, repo, ".")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "failing-validation", Out: out, Contract: contract}); err == nil {
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
	repo := newV6Repo(t, false)
	contract := lockedCharacterizationContract(t, repo, ".")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "Bad Project", Change: "change", Out: t.TempDir(), Contract: contract}); err == nil {
		t.Fatal("expected invalid project rejection")
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "Bad Change", Out: t.TempDir(), Contract: contract}); err == nil {
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

func lockedDoubleDefectFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repo := newRepo(t, false)
	upgradeFixturePolicyToV3(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".polis/policy.json", "double.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "buggy double baseline")
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestDouble"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindDefect,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"double_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "fix Double through locked Red-to-Green",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "Double returns twice its input")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Double(2) equals 4", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "captured regression test remains immutable")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "defect fix bypasses Red")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "integer input")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "doubled integer")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "incorrect Double fails regression")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"DOUBLE-RED"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "defect-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	lockedPath := filepath.Join(t.TempDir(), "defect-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: lockedPath}); err != nil {
		t.Fatalf("polis start defect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestDouble(t *testing.T){if Double(2)!=4{t.Fatal(\"DOUBLE-RED\")}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	redPath := filepath.Join(t.TempDir(), "defect-red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: lockedPath, Out: redPath}); err != nil {
		t.Fatalf("capture locked defect Red: %v", err)
	}
	return repo, lockedPath, redPath
}

func TestBuildDefectRequiresAndReproducesRedGreen(t *testing.T) {
	repo, contractPath, redPath := lockedDoubleDefectFixture(t)
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
	if pkg.Change.Kind != spec.ChangeKindDefect || pkg.Change.SchemaVersion != spec.LockedChangeContractSchemaVersion || len(pkg.RegressionPatch) == 0 {
		t.Fatalf("pkg=%+v", pkg.Change)
	}
}

func TestBuildRejectsContractPlacementAndRegressionModeMisuse(t *testing.T) {
	repo := newRepo(t, false)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(repo, "change.json")
	feature := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
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
	repo, contractPath, red := lockedDoubleDefectFixture(t)
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "missing-red", Out: t.TempDir(), Contract: contractPath}); err == nil || !strings.Contains(err.Error(), "requires regression-patch") {
		t.Fatalf("expected missing regression patch rejection, got %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "double_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int{return n*2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "probe-path-missing", Out: t.TempDir(), Contract: contractPath, RegressionPatch: red}); err == nil || !strings.Contains(err.Error(), "absent from final payload") {
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

func TestBuildRejectsChangedPathOutsideV6Scope(t *testing.T) {
	repo := newV6Repo(t, false)
	contract := lockedCharacterizationContract(t, repo, "app.txt", "calc_test.go")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "outside.txt"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "gitrex", Change: "scope-reject", Out: t.TempDir(), Contract: contract})
	if err == nil || !strings.Contains(err.Error(), "outside change scope") {
		t.Fatalf("expected scope rejection, got %v", err)
	}
}

func strictFeatureBuildContract(t *testing.T, repo string) string {
	t.Helper()
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "strict_feature_test.go"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	contract := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"strict_feature_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "prove strict feature build requires Red input",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "strict feature requires regression patch")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "strict feature requires regression patch", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "legacy feature builds stay compatible")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "strict feature builds without Red")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "captured Red patch")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "validated strict artifact")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "missing Red patch blocks build")},
		},
		Behavior: pass,
		Affected: pass,
		Regression: spec.RegressionContract{
			Mode:                   spec.RegressionModeRedGreen,
			Command:                &regression,
			BaselineExitCode:       &exit,
			BaselineOutputContains: []string{"STRICT-BUILD-RED"},
		},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "strict-feature.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildLockedFeatureRequiresRegressionPatch(t *testing.T) {
	repo, contract, _ := strictDoubleFeatureFixture(t)
	_, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-feature", Out: t.TempDir(), Contract: contract})
	if err == nil || !strings.Contains(err.Error(), "requires regression-patch") {
		t.Fatalf("expected strict regression-patch requirement, got %v", err)
	}
}

func strictDoubleFeatureContract(t *testing.T) string {
	t.Helper()
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestStrictFeatureDouble"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	contract := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"strict_feature_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "drive Double behavior test-first",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "Double multiplies by two")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Double multiplies by two", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "captured test remains unchanged")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "feature bypasses Red")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "integer input")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "doubled integer")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "incorrect Double fails regression")},
		},
		Behavior: pass,
		Affected: pass,
		Regression: spec.RegressionContract{
			Mode:                   spec.RegressionModeRedGreen,
			Command:                &regression,
			BaselineExitCode:       &exit,
			BaselineOutputContains: []string{"STRICT-FEATURE-RED"},
		},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "strict-double-feature.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func strictDoubleFeatureFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repo := newRepo(t, false)
	upgradeFixturePolicyToV3(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".polis/policy.json", "double.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "baseline without double behavior")
	draft := strictDoubleFeatureContract(t)
	contract := filepath.Join(t.TempDir(), "strict-double-feature-locked.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draft, Out: contract}); err != nil {
		t.Fatalf("polis start strict feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "strict_feature_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestStrictFeatureDouble(t *testing.T){if Double(2)!=4{t.Fatal(\"STRICT-FEATURE-RED\")}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	redPatch := filepath.Join(t.TempDir(), "strict-feature-red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: contract, Out: redPatch}); err != nil {
		t.Fatalf("capture strict feature Red: %v", err)
	}
	return repo, contract, redPatch
}

func TestBuildStrictFeatureReproducesRedGreen(t *testing.T) {
	repo, contract, redPatch := strictDoubleFeatureFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-double", Out: t.TempDir(), Contract: contract, RegressionPatch: redPatch}); err != nil {
		t.Fatalf("strict feature build: %v", err)
	}
}

func TestBuildStrictFeatureRejectsRedTestLaundering(t *testing.T) {
	repo, contract, redPatch := strictDoubleFeatureFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "strict_feature_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestStrictFeatureDouble(t *testing.T){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-double-laundered-test", Out: t.TempDir(), Contract: contract, RegressionPatch: redPatch})
	if err == nil || !strings.Contains(err.Error(), "differs from captured Red proof") {
		t.Fatalf("expected captured Red proof immutability rejection, got %v", err)
	}
}

func upgradeFixturePolicyToV3(t *testing.T, repo string) {
	t.Helper()
	rawPolicy, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(rawPolicy)
	if err != nil {
		t.Fatal(err)
	}
	policy.SchemaVersion = spec.PolicySchemaVersion
	for i := range policy.Gates {
		if policy.Gates[i].Command != nil {
			policy.Gates[i].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
		}
	}
	raw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPolicyV3RejectsUnlockedStrictChangeContractV3(t *testing.T) {
	repo := newV6Repo(t, false)
	draft := strictBehaviorPreservingContract(t)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-policy-v3", Out: t.TempDir(), Contract: draft})
	if err == nil || !strings.Contains(err.Error(), "schema v4") {
		t.Fatalf("expected unlocked strict schema-v3 rejection, got %v", err)
	}
}

func TestBuildStrictFeatureRejectsCapturedRedPathMissingFromTarget(t *testing.T) {
	repo, contractPath, redPatch := strictDoubleFeatureFixture(t)
	if err := os.Remove(filepath.Join(repo, "strict_feature_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "proj", Change: "strict-missing-red-test", Contract: contractPath, RegressionPatch: redPatch, Out: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "is absent from final payload") {
		t.Fatalf("expected captured Red path-presence rejection, got %v", err)
	}
}

func strictBehaviorPreservingContract(t *testing.T) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestCharacterization"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	contract := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindBehaviorPreserving,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"characterization_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "preserve characterized behavior during refactor",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "characterization is Green on baseline and target")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "characterization is Green on baseline and target", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "observable value remains 42")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "refactor changes observable value")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "committed characterization")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "behavior-preserving target")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "target characterization failure blocks build")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "strict-behavior-preserving.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func strictBehaviorPreservingFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := newRepo(t, false)
	upgradeFixturePolicyToV3(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "value.go"), []byte("package polisfixture\nfunc Value() int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "characterization_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestCharacterization(t *testing.T){if Value()!=42{t.Fatalf(\"got %d\", Value())}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".polis/policy.json", "value.go", "characterization_test.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "characterized baseline")
	draft := strictBehaviorPreservingContract(t)
	locked := filepath.Join(t.TempDir(), "strict-behavior-preserving-locked.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draft, Out: locked}); err != nil {
		t.Fatalf("polis start behavior-preserving: %v", err)
	}
	return repo, locked
}

func TestBuildStrictBehaviorPreservingReproducesGreenGreen(t *testing.T) {
	repo, contract := strictBehaviorPreservingFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "value.go"), []byte("package polisfixture\nconst preservedValue = 42\nfunc Value() int { return preservedValue }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-green-green", Out: t.TempDir(), Contract: contract}); err != nil {
		t.Fatalf("strict behavior-preserving build: %v", err)
	}
}

func TestBuildStrictBehaviorPreservingRejectsTargetRegression(t *testing.T) {
	repo, contract := strictBehaviorPreservingFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "value.go"), []byte("package polisfixture\nfunc Value() int { return 41 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "strict-green-green-broken", Out: t.TempDir(), Contract: contract}); err == nil {
		t.Fatal("strict behavior-preserving target regression accepted")
	}
}

func lockedDoubleFeatureFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repo := newRepo(t, false)
	upgradeFixturePolicyToV3(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".polis/policy.json", "double.go")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "locked strict baseline")
	draft := strictDoubleFeatureContract(t)
	locked := filepath.Join(t.TempDir(), "locked.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draft, Out: locked}); err != nil {
		t.Fatalf("start locked contract: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "strict_feature_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestStrictFeatureDouble(t *testing.T){if Double(2)!=4{t.Fatal(\"STRICT-FEATURE-RED\")}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	redPatch := filepath.Join(t.TempDir(), "locked-red.patch")
	if _, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: repo, Contract: locked, Out: redPatch}); err != nil {
		t.Fatalf("capture locked Red: %v", err)
	}
	return repo, locked, redPatch
}

func TestBuildLockedFeatureReproducesRedGreen(t *testing.T) {
	repo, contract, redPatch := lockedDoubleFeatureFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "locked-double", Out: t.TempDir(), Contract: contract, RegressionPatch: redPatch}); err != nil {
		t.Fatalf("locked feature build: %v", err)
	}
}

func TestBuildLockedFeatureRejectsRedTestLaundering(t *testing.T) {
	repo, contract, redPatch := lockedDoubleFeatureFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "strict_feature_test.go"), []byte("package polisfixture\nimport \"testing\"\nfunc TestStrictFeatureDouble(t *testing.T){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "double.go"), []byte("package polisfixture\nfunc Double(n int) int { return n * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "locked-laundered", Out: t.TempDir(), Contract: contract, RegressionPatch: redPatch})
	if err == nil || !strings.Contains(err.Error(), "differs from captured Red proof") {
		t.Fatalf("expected locked test immutability rejection, got %v", err)
	}
}

func TestBuildV6RejectsSchemaV2ProducerInput(t *testing.T) {
	repo := newRepo(t, false)
	upgradeFixturePolicyToV3(t, repo)
	runGit(t, repo, "add", ".polis/policy.json")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "policy v3 baseline")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	contract := spec.ChangeContract{
		SchemaVersion: spec.ChangeContractSchemaVersion,
		Kind:          spec.ChangeKindFeature,
		Scope:         &spec.ChangeScope{AllowedPaths: []string{"app.txt"}},
		Behavior:      pass,
		Affected:      pass,
		Regression:    spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(t.TempDir(), "schema-v2.json")
	if err := os.WriteFile(contractPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "legacy-v2", Out: t.TempDir(), Contract: contractPath})
	if err == nil || !strings.Contains(err.Error(), "schema v4") || !strings.Contains(err.Error(), "polis start") {
		t.Fatalf("expected V6 locked schema-v4 producer rejection, got %v", err)
	}
}

func TestBuildV6RejectsUnlockedSchemaV3ProducerInput(t *testing.T) {
	repo := newV6Repo(t, false)
	contract := strictBehaviorPreservingContract(t)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "unlocked-v3", Out: t.TempDir(), Contract: contract})
	if err == nil || !strings.Contains(err.Error(), "schema v4") || !strings.Contains(err.Error(), "polis start") {
		t.Fatalf("expected V6 locked schema-v4 producer rejection, got %v", err)
	}
}
