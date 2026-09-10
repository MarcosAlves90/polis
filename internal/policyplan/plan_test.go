package policyplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestCompileBuildsExplicitPlan(t *testing.T) {
	plan := mustCompile(t, planPolicy(spec.ValidationLevelStandard))
	assertPlanMetadata(t, plan)
	assertPlanInventory(t, plan)
	assertPlanGateDetails(t, plan)
	assertPlanGuarantees(t, plan)
	assertPlanInvariants(t, plan)
	assertPlanIsDefensivelyCopied(t, plan)
}

func mustCompile(t *testing.T, policy spec.Policy) Plan {
	t.Helper()
	plan, err := Compile(policy)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertPlanMetadata(t *testing.T, plan Plan) {
	t.Helper()
	if plan.PlanVersion != PlanVersion || plan.PolicySchemaVersion != spec.PolicySchemaVersion {
		t.Fatalf("plan version=%d policy schema=%d", plan.PlanVersion, plan.PolicySchemaVersion)
	}
	if plan.ValidationLevel != spec.ValidationLevelStandard {
		t.Fatalf("validation level=%q", plan.ValidationLevel)
	}
}

func assertPlanInventory(t *testing.T, plan Plan) {
	t.Helper()
	if len(plan.EnabledGates) != 1 || plan.EnabledGates[0] != "test.complete" {
		t.Fatalf("enabled gates=%v", plan.EnabledGates)
	}
	if len(plan.DisabledGates) != len(spec.ProjectGateOrder)-1 {
		t.Fatalf("disabled gates=%v", plan.DisabledGates)
	}
	if len(plan.Gates) != len(spec.ProjectGateOrder) || len(plan.Guarantees) != len(spec.ProjectGateOrder) {
		t.Fatalf("gates=%d guarantees=%d", len(plan.Gates), len(plan.Guarantees))
	}
}

func assertPlanGateDetails(t *testing.T, plan Plan) {
	t.Helper()
	if plan.Gates[0].State != GateStateEnabled || plan.Gates[0].Command == nil {
		t.Fatalf("test gate=%+v", plan.Gates[0])
	}
	if plan.Gates[1].State != GateStateDisabled || plan.Gates[1].Command != nil || plan.Gates[1].Reason == nil {
		t.Fatalf("coverage gate=%+v", plan.Gates[1])
	}
}

func assertPlanGuarantees(t *testing.T, plan Plan) {
	t.Helper()
	if plan.Guarantees[0].Status != GuaranteeProvided || plan.Guarantees[1].Status != GuaranteeNotProvided {
		t.Fatalf("guarantees=%+v", plan.Guarantees[:2])
	}
}

func assertPlanInvariants(t *testing.T, plan Plan) {
	t.Helper()
	if len(plan.MandatoryInvariants) == 0 {
		t.Fatal("mandatory invariants are missing")
	}
}

func assertPlanIsDefensivelyCopied(t *testing.T, plan Plan) {
	t.Helper()
	policies := plan.GatePolicies()
	policies[0].Command.Argv[0] = "mutated"
	if got := plan.GatePolicies()[0].Command.Argv[0]; got == "mutated" {
		t.Fatal("plan exposed mutable gate policy state")
	}
}

func TestCompileDefaultsAbsentLevelToStrict(t *testing.T) {
	plan, err := Compile(planPolicy(spec.ValidationLevelStrict))
	if err != nil {
		t.Fatal(err)
	}
	if plan.ValidationLevel != spec.ValidationLevelStrict {
		t.Fatalf("validation level=%q", plan.ValidationLevel)
	}
	if len(plan.EnabledGates) != 2 || len(plan.DisabledGates) != len(spec.ProjectGateOrder)-2 {
		t.Fatalf("enabled=%v disabled=%v", plan.EnabledGates, plan.DisabledGates)
	}
}

func TestCompileMinimalDisablesAllProjectGates(t *testing.T) {
	plan, err := Compile(planPolicy(spec.ValidationLevelMinimal))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.EnabledGates) != 0 || len(plan.DisabledGates) != len(spec.ProjectGateOrder) {
		t.Fatalf("enabled=%v disabled=%v", plan.EnabledGates, plan.DisabledGates)
	}
	for i, gate := range plan.Gates {
		if gate.State != GateStateDisabled || gate.Command != nil || plan.Guarantees[i].Status != GuaranteeNotProvided {
			t.Fatalf("gate=%+v guarantee=%+v", gate, plan.Guarantees[i])
		}
	}
}

func TestCompileRejectsInvalidPolicy(t *testing.T) {
	if _, err := Compile(spec.Policy{SchemaVersion: spec.PolicySchemaVersion}); err == nil || !strings.Contains(err.Error(), "compile execution plan") {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadCommittedPolicyIncludesSourceAndDigest(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(planPolicy(spec.ValidationLevelMinimal))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	policyPath := filepath.Join(repo, ".polis", "policy.json")
	if err := os.WriteFile(policyPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	plan, err := Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if plan.PolicySource != PolicySourceCommitted || plan.PolicySHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("source=%q digest=%q", plan.PolicySource, plan.PolicySHA256)
	}
	if plan.Runtime.OS == "" || plan.Runtime.Architecture == "" || plan.Runtime.GoVersion == "" {
		t.Fatalf("runtime=%+v", plan.Runtime)
	}
}

func planPolicy(level string) spec.Policy {
	reason := "disabled for this planning fixture"
	command := func(argv ...string) *spec.CommandSpec {
		return &spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 30, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}}
	}
	threshold := 80.0
	testGate := spec.GatePolicy{ID: "test.complete", Mode: spec.GateModeCommand, Command: command("go", "test", "./...")}
	coverageGate := spec.GatePolicy{ID: "coverage", Mode: spec.GateModeCoverage, Command: command("go", "test", "-coverprofile=coverage.out", "./..."), Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold}
	if level == spec.ValidationLevelMinimal {
		testGate = spec.GatePolicy{ID: "test.complete", Mode: spec.GateModeNotApplicable, Reason: &reason}
	}
	if level != spec.ValidationLevelStrict {
		coverageGate = spec.GatePolicy{ID: "coverage", Mode: spec.GateModeNotApplicable, Reason: &reason}
	}
	gates := []spec.GatePolicy{testGate, coverageGate}
	for _, id := range spec.ProjectGateOrder[2:] {
		gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
	}
	policy := spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates}
	if level != spec.ValidationLevelStrict {
		policy.ValidationLevel = level
	}
	return policy
}
