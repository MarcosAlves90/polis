package devlock

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func testSpecification() *spec.DevelopmentSpecification {
	return &spec.DevelopmentSpecification{
		Objective:          "lock baseline",
		Requirements:       []spec.SpecificationClause{{ID: "REQ-001", Statement: "baseline is exact"}},
		AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "lock validates", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
		Invariants:         []spec.SpecificationClause{{ID: "INV-001", Statement: "policy bytes are committed"}},
		ForbiddenStates:    []spec.SpecificationClause{{ID: "FORBID-001", Statement: "drift is accepted"}},
		Inputs:             []spec.SpecificationClause{{ID: "IN-001", Statement: "git repo"}},
		Outputs:            []spec.SpecificationClause{{ID: "OUT-001", Statement: "baseline lock"}},
		FailureSemantics:   []spec.SpecificationClause{{ID: "FAIL-001", Statement: "drift fails"}},
	}
}

func policyV3Bytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		case "coverage":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
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

func newLockRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), policyV3Bytes(t), 0o644); err != nil {
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

func TestSnapshotAndValidateLockedBaseline(t *testing.T) {
	repo := newLockRepo(t)
	lock, err := Snapshot(context.Background(), repo, testSpecification())
	if err != nil {
		t.Fatal(err)
	}
	c := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Specification: testSpecification(), BaselineLock: &lock}
	if err := Validate(context.Background(), repo, c); err != nil {
		t.Fatalf("validate lock: %v", err)
	}
}

func TestValidateRejectsHeadAndPolicyDrift(t *testing.T) {
	repo := newLockRepo(t)
	specification := testSpecification()
	lock, err := Snapshot(context.Background(), repo, specification)
	if err != nil {
		t.Fatal(err)
	}
	c := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Specification: specification, BaselineLock: &lock}
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "add", "other.txt")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, b)
	}
	cmd = exec.Command("git", "-C", repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "drift")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, b)
	}
	if err := Validate(context.Background(), repo, c); err == nil {
		t.Fatal("HEAD drift accepted")
	}

	repo = newLockRepo(t)
	lock, err = Snapshot(context.Background(), repo, specification)
	if err != nil {
		t.Fatal(err)
	}
	c.BaselineLock = &lock
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), append(policyV3Bytes(t), ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), repo, c); err == nil {
		t.Fatal("working policy drift accepted")
	}
}

func TestValidateBuildRepositoryAcceptsDescendantHead(t *testing.T) {
	repo := newLockRepo(t)
	specification := testSpecification()
	lock, err := Snapshot(context.Background(), repo, specification)
	if err != nil {
		t.Fatal(err)
	}
	contract := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Specification: specification, BaselineLock: &lock}

	if err := os.WriteFile(filepath.Join(repo, "target.txt"), []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "add", "target.txt")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, b)
	}
	cmd = exec.Command("git", "-C", repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "target")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, b)
	}

	if err := ValidateBuildRepository(context.Background(), repo, contract); err != nil {
		t.Fatalf("validate descendant build repository: %v", err)
	}
	if err := ValidateRepository(context.Background(), repo, contract); err == nil {
		t.Fatal("exact baseline validation unexpectedly accepted descendant HEAD")
	}
}

func TestValidateBuildRepositoryRejectsNonDescendantHead(t *testing.T) {
	repo := newLockRepo(t)
	specification := testSpecification()
	lock, err := Snapshot(context.Background(), repo, specification)
	if err != nil {
		t.Fatal(err)
	}
	contract := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Specification: specification, BaselineLock: &lock}

	tree := strings.TrimSpace(runGitLockTest(t, repo, "write-tree"))
	unrelated := strings.TrimSpace(runGitLockTest(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit-tree", tree, "-m", "unrelated"))
	runGitLockTest(t, repo, "reset", "--hard", unrelated)

	err = ValidateBuildRepository(context.Background(), repo, contract)
	if err == nil || !strings.Contains(err.Error(), "not an ancestor") {
		t.Fatalf("expected non-descendant rejection, got %v", err)
	}
}

func TestValidateBuildRepositoryRejectsLockedBaseTreeMismatch(t *testing.T) {
	repo := newLockRepo(t)
	specification := testSpecification()
	lock, err := Snapshot(context.Background(), repo, specification)
	if err != nil {
		t.Fatal(err)
	}
	if lock.GitObjectFormat == "sha256" {
		lock.BaseTree = strings.Repeat("f", 64)
	} else {
		lock.BaseTree = strings.Repeat("f", 40)
	}
	contract := spec.ChangeContract{SchemaVersion: spec.LockedChangeContractSchemaVersion, Specification: specification, BaselineLock: &lock}

	err = ValidateBuildRepository(context.Background(), repo, contract)
	if err == nil || !strings.Contains(err.Error(), "base_tree mismatch") {
		t.Fatalf("expected locked base tree rejection, got %v", err)
	}
}

func runGitLockTest(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return string(b)
}
