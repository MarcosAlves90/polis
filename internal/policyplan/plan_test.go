package policyplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if len(plan.DependencyEdges) != 1 || plan.DependencyEdges[0].Gate != "coverage" || plan.DependencyEdges[0].DependsOn != "test.complete" {
		t.Fatalf("dependency edges=%v", plan.DependencyEdges)
	}
	if len(plan.ExecutionOrder) != len(spec.ProjectGateOrder) || plan.ExecutionOrder[0] != "test.complete" {
		t.Fatalf("execution order=%v", plan.ExecutionOrder)
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
	if len(plan.Gates[1].DependsOn) != 1 || plan.Gates[1].DependsOn[0] != "test.complete" {
		t.Fatalf("coverage dependencies=%v", plan.Gates[1].DependsOn)
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
	policies[1].DependsOn[0] = "mutated"
	if got := plan.GatePolicies()[0].Command.Argv[0]; got == "mutated" {
		t.Fatal("plan exposed mutable gate policy state")
	}
	if got := plan.GatePolicies()[1].DependsOn[0]; got == "mutated" {
		t.Fatal("plan exposed mutable dependency state")
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
	if plan.ValidationLevelProvenance != (Decision{Source: DecisionSourcePolicyDefault, Reason: "omitted_validation_level_defaults_to_strict"}) {
		t.Fatalf("default level provenance=%+v", plan.ValidationLevelProvenance)
	}
	if len(plan.EnabledGates) != 2 || len(plan.DisabledGates) != len(spec.ProjectGateOrder)-2 {
		t.Fatalf("enabled=%v disabled=%v", plan.EnabledGates, plan.DisabledGates)
	}
}

func TestCompileProvenanceFollowsEffectiveDecisions(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	policy.ValidationLevel = spec.ValidationLevelStrict
	policy.Gates[1].DependsOn = []string{"test.complete"} // Declaring the required edge again must not change its origin.
	policy.Gates[4] = spec.GatePolicy{ID: "build", Mode: spec.GateModeCommand, Command: cloneCommand(policy.Gates[0].Command), DependsOn: []string{"test.complete"}}
	plan, err := CompileWithOptions(policy, CompileOptions{DeferredGates: []string{"coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ValidationLevelProvenance != (Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_validation_level"}) {
		t.Fatalf("explicit level provenance=%+v", plan.ValidationLevelProvenance)
	}
	if want := []DependencyEdge{
		{Gate: "coverage", DependsOn: "test.complete", Provenance: Decision{Source: DecisionSourceMandatoryDependency, Reason: "built_in_prerequisite"}},
		{Gate: "build", DependsOn: "test.complete", Provenance: Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_prerequisite"}},
	}; !reflect.DeepEqual(plan.DependencyEdges, want) {
		t.Fatalf("dependency provenance=%+v want=%+v", plan.DependencyEdges, want)
	}
	testGate, deferred, disabled := plan.Gates[0], plan.Gates[1], plan.Gates[2]
	if testGate.Provenance != (GateProvenance{
		Configuration:       Decision{Source: DecisionSourcePolicyExplicit, Reason: "policy_gate_declaration"},
		State:               Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_gate_mode"},
		ProducerAction:      Decision{Source: DecisionSourcePolicyExplicit, Reason: "enabled_gate_scheduled"},
		ConsumerRequirement: Decision{Source: DecisionSourceGateState, Reason: "enabled_gate_required"},
	}) {
		t.Fatalf("scheduled gate provenance=%+v", testGate.Provenance)
	}
	if deferred.State != GateStateEnabled || deferred.Provenance.State != testGate.Provenance.State || deferred.Provenance.ProducerAction != (Decision{Source: DecisionSourceExecutorDeferral, Reason: "requested_by_executor"}) || deferred.Provenance.ConsumerRequirement != (Decision{Source: DecisionSourceExecutorDeferral, Reason: "deferred_gate_requires_consumer_validation"}) {
		t.Fatalf("deferred gate provenance=%+v", deferred)
	}
	if disabled.Provenance != (GateProvenance{
		Configuration:       Decision{Source: DecisionSourcePolicyExplicit, Reason: "policy_gate_declaration"},
		State:               Decision{Source: DecisionSourceNotApplicable, Reason: "declared_not_applicable"},
		ProducerAction:      Decision{Source: DecisionSourceNotApplicable, Reason: "gate_not_applicable"},
		ConsumerRequirement: Decision{Source: DecisionSourceNotApplicable, Reason: "gate_not_applicable"},
	}) || disabled.Reason == nil || *disabled.Reason != "disabled for this planning fixture" {
		t.Fatalf("disabled gate provenance=%+v", disabled)
	}
	if plan.Guarantees[0].Provenance != (Decision{Source: DecisionSourceGateState, Reason: "enabled_gate_required"}) || plan.Guarantees[1].Provenance != deferred.Provenance.ConsumerRequirement || plan.Guarantees[2].Provenance != disabled.Provenance.ConsumerRequirement {
		t.Fatalf("guarantee provenance=%+v", plan.Guarantees[:3])
	}
	if !strings.Contains(plan.Guarantees[1].Description, "consumer validation must execute") {
		t.Fatalf("deferred guarantee incorrectly describes producer execution: %+v", plan.Guarantees[1])
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Plan
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.DependencyEdges, plan.DependencyEdges) || !reflect.DeepEqual(decoded.Gates[1].Provenance, deferred.Provenance) || decoded.ValidationLevelProvenance != plan.ValidationLevelProvenance {
		t.Fatalf("JSON round trip lost provenance: %+v", decoded)
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

func TestCompileRejectsNonportableShellCommand(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	policy.Gates[0].Command.Argv = []string{"bash", "-lc", "cd backend && pytest"}
	if _, err := Compile(policy); err == nil || !strings.Contains(err.Error(), "test.complete") {
		t.Fatalf("PORTABILITY-RED: shell-based Project Policy command was accepted: %v", err)
	}
}

func TestCompileWithOptionsDefersAndCanonicalizesEnabledGates(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	command := policy.Gates[0].Command
	policy.Gates[5] = spec.GatePolicy{ID: "smoke", Mode: spec.GateModeCommand, Command: command}

	plan, err := CompileWithOptions(policy, CompileOptions{DeferredGates: []string{"smoke", "coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"coverage", "smoke"}; !reflect.DeepEqual(plan.DeferredGates, want) {
		t.Fatalf("deferred gates=%v want=%v", plan.DeferredGates, want)
	}
	if !plan.ConsumerValidationRequired {
		t.Fatal("plan did not require consumer validation")
	}
	if plan.Gates[0].ProducerAction != ProducerActionExecute || plan.Gates[0].ConsumerRequirement != ConsumerRequirementRequired {
		t.Fatalf("producer gate responsibility=%+v", plan.Gates[0])
	}
	if plan.Gates[1].ProducerAction != ProducerActionDeferred || plan.Gates[1].ConsumerRequirement != ConsumerRequirementRequired {
		t.Fatalf("deferred coverage responsibility=%+v", plan.Gates[1])
	}
	if plan.Gates[2].ProducerAction != ProducerActionNotApplicable || plan.Gates[2].ConsumerRequirement != ConsumerRequirementNotRequired {
		t.Fatalf("disabled gate responsibility=%+v", plan.Gates[2])
	}
}

func TestCompileWithOptionsRejectsInvalidDeferredGates(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	cases := []struct {
		name   string
		gates  []string
		policy spec.Policy
	}{
		{name: "unknown", gates: []string{"deploy"}, policy: policy},
		{name: "non project invariant", gates: []string{"integrity"}, policy: policy},
		{name: "duplicate", gates: []string{"coverage", "coverage"}, policy: policy},
		{name: "not applicable", gates: []string{"lint"}, policy: policy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompileWithOptions(tc.policy, CompileOptions{DeferredGates: tc.gates}); err == nil {
				t.Fatalf("invalid deferred gates accepted: %v", tc.gates)
			}
		})
	}
}

func TestCompileWithOptionsRejectsDirectAndTransitiveDeferredPrerequisites(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	policy.Gates[2] = spec.GatePolicy{ID: "lint", Mode: spec.GateModeCommand, Command: policy.Gates[0].Command, DependsOn: []string{"test.complete"}}
	if _, err := CompileWithOptions(policy, CompileOptions{DeferredGates: []string{"test.complete"}}); err == nil {
		t.Fatal("producer gate with direct deferred prerequisite was accepted")
	}
	policy = planPolicy(spec.ValidationLevelStrict)
	if _, err := CompileWithOptions(policy, CompileOptions{DeferredGates: []string{"test.complete", "coverage"}}); err != nil {
		t.Fatalf("dependent gates deferred together rejected: %v", err)
	}
	policy = planPolicy(spec.ValidationLevelStandard)
	policy.Gates[2] = spec.GatePolicy{ID: "lint", Mode: spec.GateModeCommand, Command: policy.Gates[0].Command}
	policy.Gates[3] = spec.GatePolicy{ID: "typecheck", Mode: spec.GateModeCommand, Command: policy.Gates[0].Command}
	dependencies := map[string][]string{"lint": {"typecheck"}, "typecheck": {"coverage"}}
	if err := validateDeferredDependencies(policy, []string{"lint"}, dependencies, map[string]struct{}{"coverage": {}}); err == nil {
		t.Fatal("transitive deferred prerequisite was accepted")
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
