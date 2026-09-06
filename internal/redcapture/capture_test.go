package redcapture

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v\n%s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), capturePolicyV3(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base")

	code := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "HEAD"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindDefect,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"regression.txt"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "capture defect Red without source mutation",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "defect is reproduced before fix")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "regression probe fails on baseline", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "HEAD and index remain unchanged")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "capture mutates source")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "test-only Red delta")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "validated Red patch")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "wrong oracle blocks capture")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &code, BaselineOutputContains: []string{"BUG-RED"}},
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
		t.Fatalf("polis start defect fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("BUG-RED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, lockedPath
}

func TestCaptureProducesValidatedPatchWithoutMutatingSource(t *testing.T) {
	repo, cp := fixture(t)
	out := filepath.Join(t.TempDir(), "red.patch")
	head := git(t, repo, "rev-parse", "HEAD")
	idx := git(t, repo, "write-tree")
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	r, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: out})
	if e != nil {
		t.Fatal(e)
	}
	if r.SHA256 == "" {
		t.Fatal("missing hash")
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "regression.txt") {
		t.Fatalf("patch=%s", b)
	}
	if git(t, repo, "rev-parse", "HEAD") != head || git(t, repo, "write-tree") != idx || git(t, repo, "status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatal("source mutated")
	}
}
func TestCaptureRejectsStagedAndExistingOutput(t *testing.T) {
	repo, cp := fixture(t)
	git(t, repo, "add", "regression.txt")
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(t.TempDir(), "x")}); e == nil {
		t.Fatal("expected staged rejection")
	}
	git(t, repo, "restore", "--staged", "regression.txt")
	out := filepath.Join(t.TempDir(), "x")
	os.WriteFile(out, []byte("keep"), 0o600)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: out}); e == nil {
		t.Fatal("expected collision")
	}
}
func TestCaptureRejectsContractOrOutputInsideRepo(t *testing.T) {
	repo, cp := fixture(t)
	raw, _ := os.ReadFile(cp)
	inside := filepath.Join(repo, "change.json")
	os.WriteFile(inside, raw, 0o600)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: inside, Out: filepath.Join(t.TempDir(), "x")}); e == nil {
		t.Fatal("expected contract path rejection")
	}
	os.Remove(inside)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(repo, "red.patch")}); e == nil {
		t.Fatal("expected output path rejection")
	}
}

func TestCaptureRejectsInvalidInputsAndNonDefect(t *testing.T) {
	if _, err := Capture(context.Background(), Options{}); err == nil {
		t.Fatal("expected required input error")
	}
	if _, err := Capture(context.Background(), Options{Repo: t.TempDir(), Contract: "x", Out: "y"}); err == nil {
		t.Fatal("expected non-repo error")
	}
	repo, cp := fixture(t)
	if err := os.Remove(filepath.Join(repo, "regression.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(t.TempDir(), "x")}); err == nil {
		t.Fatal("expected clean worktree error")
	}

	cmd := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "HEAD"}, Cwd: ".", TimeoutSeconds: 30}
	feature := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: cmd, Affected: cmd, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	raw, _ := json.Marshal(feature)
	featurePath := filepath.Join(t.TempDir(), "feature.json")
	os.WriteFile(featurePath, raw, 0o600)
	if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: featurePath, Out: filepath.Join(t.TempDir(), "y")}); err == nil {
		t.Fatal("expected non-defect rejection")
	}

	badPath := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(badPath, []byte(`{}`), 0o600)
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: badPath, Out: filepath.Join(t.TempDir(), "z")}); err == nil {
		t.Fatal("expected malformed contract rejection")
	}
}

func TestCaptureRejectsWrongRedOracle(t *testing.T) {
	repo, cp := fixture(t)
	raw, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	var c spec.ChangeContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	wrong := 9
	c.Regression.BaselineExitCode = &wrong
	raw, _ = json.Marshal(c)
	wrongPath := filepath.Join(t.TempDir(), "wrong.json")
	os.WriteFile(wrongPath, raw, 0o600)
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: wrongPath, Out: filepath.Join(t.TempDir(), "red.patch")}); err == nil {
		t.Fatal("expected oracle rejection")
	}
}

func TestReadExternalRejectsDirectoryAndOversize(t *testing.T) {
	repo := t.TempDir()
	dir := t.TempDir()
	if _, err := readExternal(repo, dir, 10); err == nil {
		t.Fatal("expected directory rejection")
	}
	p := filepath.Join(t.TempDir(), "big")
	os.WriteFile(p, []byte("123456"), 0o600)
	if _, err := readExternal(repo, p, 3); err == nil {
		t.Fatal("expected size rejection")
	}
}

func TestReadExternalRejectsPhysicalAliasIntoRepo(t *testing.T) {
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
	if _, err := readExternal(repo, insideAlias, 1024); err == nil {
		t.Fatal("expected physical alias into repo to be rejected")
	}
}

func strictFeatureFixture(t *testing.T, changeProduction bool) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), capturePolicyV3(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base")

	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "HEAD"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"regression.txt"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "capture feature Red before production",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "feature begins with failing test")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "feature begins with failing test", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "source worktree is not mutated")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "production path changes before Red")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "test-only working tree delta")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "validated Red patch")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "out-of-scope Red path is rejected")},
		},
		Behavior:   pass,
		Affected:   pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"STRICT-FEATURE-RED"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "strict-feature-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(t.TempDir(), "strict-feature-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: contractPath}); err != nil {
		t.Fatalf("polis start strict feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("STRICT-FEATURE-RED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changeProduction {
		if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("production changed before Red\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo, contractPath
}

func TestCaptureStrictFeatureAcceptsTestOnlyRed(t *testing.T) {
	repo, contract := strictFeatureFixture(t, false)
	out := filepath.Join(t.TempDir(), "strict-feature-red.patch")
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: contract, Out: out}); err != nil {
		t.Fatalf("strict feature capture: %v", err)
	}
}

func TestCaptureStrictFeatureRejectsProductionPathBeforeRed(t *testing.T) {
	repo, contract := strictFeatureFixture(t, true)
	out := filepath.Join(t.TempDir(), "strict-feature-red.patch")
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: contract, Out: out}); err == nil || !strings.Contains(err.Error(), "outside test scope") {
		t.Fatalf("expected test-scope rejection, got %v", err)
	}
}

func TestSortedPathKeysReturnsStableLexicographicOrder(t *testing.T) {
	got := sortedPathKeys(map[string]struct{}{"z_test.go": {}, "a_test.go": {}, "m_test.go": {}})
	want := []string{"a_test.go", "m_test.go", "z_test.go"}
	if len(got) != len(want) {
		t.Fatalf("sortedPathKeys length=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedPathKeys[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}

func capturePolicyV3(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			cmd := spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &cmd})
		case "coverage":
			cmd := spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &cmd, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	raw, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestCaptureLockedFeatureRejectsBaselineDrift(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), capturePolicyV3(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base")

	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	reg := spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	development := &spec.DevelopmentSpecification{
		Objective:          "capture only on locked baseline",
		Requirements:       []spec.SpecificationClause{clause("REQ-001", "Red uses locked baseline")},
		AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "drift blocks capture", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
		Invariants:         []spec.SpecificationClause{clause("INV-001", "HEAD remains locked")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "capture after drift")},
		Inputs: []spec.SpecificationClause{clause("IN-001", "test delta")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "Red patch")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "drift fails")},
	}
	lock, err := devlock.Snapshot(context.Background(), repo, development)
	if err != nil {
		t.Fatal(err)
	}
	contract := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Kind: spec.ChangeKindFeature, Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"regression.txt"}}, DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV2, Specification: development, BaselineLock: &lock, Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &reg, BaselineExitCode: &exit, BaselineOutputContains: []string{"STRICT-FEATURE-RED"}}}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(t.TempDir(), "locked.json")
	if err := os.WriteFile(contractPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "drift.txt"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "drift.txt")
	git(t, repo, "-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "drift")
	if err := os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("STRICT-FEATURE-RED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "red.patch")
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: contractPath, Out: out}); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("expected locked baseline rejection, got %v", err)
	}
}

func TestCaptureV6RejectsUnlockedSchemaV3Contract(t *testing.T) {
	repo, lockedPath := strictFeatureFixture(t, false)
	raw, err := os.ReadFile(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	var contract spec.ChangeContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	contract.SchemaVersion = spec.StrictChangeContractSchemaVersion
	contract.DevelopmentMethod = spec.DevelopmentMethodStrictSDDTDDV1
	contract.BaselineLock = nil
	raw, err = json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "unlocked-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "strict-feature-red.patch")
	_, err = Capture(context.Background(), Options{Repo: repo, Contract: draftPath, Out: out})
	if err == nil || !strings.Contains(err.Error(), "schema v4") || !strings.Contains(err.Error(), "polis start") {
		t.Fatalf("expected V6 locked schema-v4 capture rejection, got %v", err)
	}
}
