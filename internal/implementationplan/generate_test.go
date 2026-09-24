package implementationplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestGenerateRedGreenPlanIsDeterministicAndContractBound(t *testing.T) {
	contract := planContract(spec.LockedChangeContractSchemaVersion, spec.ChangeKindFeature)
	raw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	gateOrder := []string{spec.ProjectGateOrder[0], spec.ProjectGateOrder[2]}
	first, err := Generate(contract, raw, gateOrder)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(contract, raw, gateOrder)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("identical inputs generated different plans:\nfirst=%+v\nsecond=%+v", first, second)
	}
	seenIDs := make(map[string]struct{}, len(first.Steps))
	for i, step := range first.Steps {
		wantID := fmt.Sprintf("PLAN-%03d", i+1)
		if step.ID != wantID {
			t.Fatalf("step %d ID=%q want deterministic ID %q", i, step.ID, wantID)
		}
		if _, duplicate := seenIDs[step.ID]; duplicate {
			t.Fatalf("generated duplicate step ID %q", step.ID)
		}
		seenIDs[step.ID] = struct{}{}
	}
	if err := first.ValidateAgainst(contract, raw); err != nil {
		t.Fatalf("generated plan does not validate against contract: %v", err)
	}
	if err := first.ValidateProjectGates(gateOrder); err != nil {
		t.Fatalf("generated plan does not follow policy gate order: %v", err)
	}
	sum := sha256.Sum256(raw)
	if first.ChangeContractSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("plan contract digest=%s want %x", first.ChangeContractSHA256, sum)
	}
	if len(first.Steps) != 4 || first.Steps[0].Kind != spec.ImplementationPlanStepTest || first.Steps[1].Kind != spec.ImplementationPlanStepProof || first.Steps[2].Kind != spec.ImplementationPlanStepImplementation || first.Steps[3].Kind != spec.ImplementationPlanStepValidation {
		t.Fatalf("unexpected Red/Green sequence: %+v", first.Steps)
	}
	if !reflect.DeepEqual(first.Steps[0].AllowedPaths, contract.TestScope.AllowedPaths) || !reflect.DeepEqual(first.Steps[2].AllowedPaths, contract.Scope.AllowedPaths) {
		t.Fatalf("generated steps invented scope paths: %+v", first.Steps)
	}
	if !reflect.DeepEqual(first.Steps[3].ProjectGates, gateOrder) {
		t.Fatalf("validation project gates=%v want=%v", first.Steps[3].ProjectGates, gateOrder)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"argv"`) || strings.Contains(string(encoded), `"command"`) {
		t.Fatalf("plan copied executable command data: %s", encoded)
	}
	ordered, err := first.TopologicalOrder()
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range first.Steps {
		if ordered[i] != step.ID {
			t.Fatalf("topological order=%v does not match serialized step order", ordered)
		}
	}
}

func TestGenerateGreenGreenPlanCharacterizesBothSides(t *testing.T) {
	contract := planContract(spec.LockedChangeContractSchemaVersion, spec.ChangeKindBehaviorPreserving)
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Generate(contract, raw, []string{spec.ProjectGateOrder[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateAgainst(contract, raw); err != nil {
		t.Fatalf("generated Green/Green plan is invalid: %v", err)
	}
	if plan.Strategy != spec.ImplementationPlanStrategyGreenGreen || len(plan.Steps) != 4 {
		t.Fatalf("unexpected Green/Green plan: %+v", plan)
	}
	if plan.Steps[0].Kind != spec.ImplementationPlanStepTest || plan.Steps[1].Kind != spec.ImplementationPlanStepImplementation || plan.Steps[2].Kind != spec.ImplementationPlanStepTest || plan.Steps[3].Kind != spec.ImplementationPlanStepValidation {
		t.Fatalf("unexpected Green/Green sequence: %+v", plan.Steps)
	}
	if !strings.Contains(strings.ToLower(plan.Steps[0].Objective), "baseline") || !strings.Contains(strings.ToLower(plan.Steps[2].Objective), "target") {
		t.Fatalf("Green/Green steps do not identify both characterization phases: %+v", plan.Steps)
	}
}

func TestGenerateDefectPlanUsesRedGreenOrdering(t *testing.T) {
	contract := planContract(spec.LockedChangeContractSchemaVersion, spec.ChangeKindDefect)
	raw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Generate(contract, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != spec.ImplementationPlanStrategyRedGreen || len(plan.Steps) != 4 {
		t.Fatalf("unexpected defect plan: %+v", plan)
	}
	wantKinds := []string{
		spec.ImplementationPlanStepTest,
		spec.ImplementationPlanStepProof,
		spec.ImplementationPlanStepImplementation,
		spec.ImplementationPlanStepValidation,
	}
	for i, want := range wantKinds {
		if plan.Steps[i].Kind != want {
			t.Fatalf("step %d kind=%q want=%q", i, plan.Steps[i].Kind, want)
		}
	}
}

func TestGenerateAcceptsLockedSchemaV4AndV6ButRejectsDraftAndLegacy(t *testing.T) {
	for _, version := range []int{spec.LockedChangeContractSchemaVersion, spec.CommitIntentLockedChangeContractSchemaVersion} {
		contract := planContract(version, spec.ChangeKindFeature)
		raw, err := json.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Generate(contract, raw, nil); err != nil {
			t.Fatalf("locked schema v%d rejected: %v", version, err)
		}
	}
	draft := planContract(spec.CommitIntentDraftChangeContractSchemaVersion, spec.ChangeKindFeature)
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(draft, raw, nil); err == nil {
		t.Fatal("draft Change Contract accepted")
	}
	legacy := planContract(spec.LegacyChangeContractSchemaVersion, spec.ChangeKindFeature)
	if _, err := Generate(legacy, raw, nil); err == nil {
		t.Fatal("legacy Change Contract accepted")
	}
}

func planContract(schemaVersion int, kind string) spec.ChangeContract {
	command := spec.CommandSpec{
		Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60,
		Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"PATH"}},
	}
	development := &spec.DevelopmentSpecification{
		Objective:          "Implement the locked requirement",
		Requirements:       []spec.SpecificationClause{{ID: "REQ-001", Statement: "The behavior is correct"}},
		AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "The behavior passes", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
		Invariants:         []spec.SpecificationClause{{ID: "INV-001", Statement: "The locked baseline remains authoritative"}},
		ForbiddenStates:    []spec.SpecificationClause{{ID: "FORB-001", Statement: "Unscoped files are not changed"}},
		Inputs:             []spec.SpecificationClause{{ID: "IN-001", Statement: "The locked contract is the input"}},
		Outputs:            []spec.SpecificationClause{{ID: "OUT-001", Statement: "The planned change is delivered"}},
		FailureSemantics:   []spec.SpecificationClause{{ID: "FAIL-001", Statement: "Invalid plans fail closed"}},
	}
	exitCode := 1
	regression := spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &command, BaselineExitCode: &exitCode, BaselineOutputContains: []string{"FAIL"}}
	if kind == spec.ChangeKindBehaviorPreserving {
		regression = spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &command}
	}
	method := spec.DevelopmentMethodStrictSDDTDDV2
	var baselineLock *spec.BaselineLock
	if schemaVersion == spec.StrictChangeContractSchemaVersion || schemaVersion == spec.CommitIntentDraftChangeContractSchemaVersion {
		method = spec.DevelopmentMethodStrictSDDTDDV1
	} else {
		specificationSHA256, _ := development.SHA256()
		baselineLock = &spec.BaselineLock{GitObjectFormat: "sha1", BaseCommit: strings.Repeat("1", 40), BaseTree: strings.Repeat("2", 40), PolicySHA256: strings.Repeat("a", 64), SpecificationSHA256: specificationSHA256}
	}
	return spec.ChangeContract{
		SchemaVersion: schemaVersion, Kind: kind,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"internal/"}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"internal/"}},
		DevelopmentMethod: method, Specification: development, BaselineLock: baselineLock,
		Behavior: command, Affected: command, Regression: regression,
	}
}
