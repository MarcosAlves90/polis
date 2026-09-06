package devstart

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func cleanCmd(argv ...string) spec.CommandSpec {
	return spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 60, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}}
}

func policyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable"
	threshold := 80.0
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			cmd := cleanCmd("true")
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &cmd})
		case "coverage":
			cmd := cleanCmd("true")
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

func draftContractBytes(t *testing.T) []byte {
	t.Helper()
	exit := 1
	reg := cleanCmd("false")
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	contract := spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"feature_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "lock before implementation",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "feature is test-first")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "red then green", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "baseline is immutable")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "implementation predates lock")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "draft contract")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "locked contract")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "drift fails")},
		},
		Behavior:   cleanCmd("true"),
		Affected:   cleanCmd("true"),
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &reg, BaselineExitCode: &exit, BaselineOutputContains: []string{"RED"}},
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func makeRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), policyBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	return repo
}

func TestStartProducesLockedV4WithoutMutatingRepo(t *testing.T) {
	repo := makeRepo(t)
	ext := t.TempDir()
	draft := filepath.Join(ext, "draft.json")
	out := filepath.Join(ext, "locked.json")
	if err := os.WriteFile(draft, draftContractBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Start(context.Background(), Options{Repo: repo, Contract: draft, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != out || result.SHA256 == "" {
		t.Fatalf("result=%+v", result)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := spec.DecodeChangeContract(raw)
	if err != nil {
		t.Fatal(err)
	}
	if locked.SchemaVersion != spec.LockedChangeContractSchemaVersion || locked.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 || locked.BaselineLock == nil {
		t.Fatalf("locked=%+v", locked)
	}
	cmd := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "--untracked-files=all")
	if b, err := cmd.CombinedOutput(); err != nil || len(b) != 0 {
		t.Fatalf("repo mutated: err=%v status=%q", err, b)
	}
}

func TestStartRejectsDirtyRepoAndInvalidPolicy(t *testing.T) {
	repo := makeRepo(t)
	ext := t.TempDir()
	draft := filepath.Join(ext, "draft.json")
	out := filepath.Join(ext, "locked.json")
	if err := os.WriteFile(draft, draftContractBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), Options{Repo: repo, Contract: draft, Out: out}); err == nil {
		t.Fatal("dirty repo accepted")
	}
	if err := os.Remove(filepath.Join(repo, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), Options{Repo: repo, Contract: draft, Out: out}); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
