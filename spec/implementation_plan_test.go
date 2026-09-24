package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeImplementationPlanRejectsAmbiguousJSON(t *testing.T) {
	valid := `{"schema_version":1,"change_contract_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","git_object_format":"sha1","base_commit":"1111111111111111111111111111111111111111","base_tree":"2222222222222222222222222222222222222222","strategy":"red_green","steps":[{"id":"PLAN-001","kind":"test","objective":"Establish a failing test","requirements":["REQ-001"],"acceptance_criteria":["AC-001"]},{"id":"PLAN-002","kind":"implementation","objective":"Implement the requirement","requirements":["REQ-001"],"acceptance_criteria":["AC-001"],"depends_on":["PLAN-001"]}]}`
	cases := []struct {
		name string
		raw  string
	}{
		{name: "duplicate top-level key", raw: strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)},
		{name: "duplicate nested key", raw: strings.Replace(valid, `"id":"PLAN-001"`, `"id":"PLAN-001","id":"PLAN-002"`, 1)},
		{name: "unknown field", raw: valid[:len(valid)-1] + `,"unexpected":true}`},
		{name: "case-insensitive top-level alias", raw: strings.Replace(valid, `"schema_version":1`, `"SchemaVersion":1`, 1)},
		{name: "case-insensitive step alias", raw: strings.Replace(valid, `"id":"PLAN-001"`, `"ID":"PLAN-001"`, 1)},
		{name: "trailing JSON value", raw: valid + ` {}`},
		{name: "null steps array", raw: valid[:strings.Index(valid, `,"steps":`)] + `,"steps":null}`},
		{name: "null requirements array", raw: strings.Replace(valid, `"requirements":["REQ-001"]`, `"requirements":null`, 1)},
		{name: "null acceptance criteria array", raw: strings.Replace(valid, `"acceptance_criteria":["AC-001"]`, `"acceptance_criteria":null`, 1)},
		{name: "null allowed paths array", raw: strings.Replace(valid, `"objective":"Establish a failing test"`, `"objective":"Establish a failing test","allowed_paths":null`, 1)},
		{name: "null dependency array", raw: strings.Replace(valid, `"depends_on":["PLAN-001"]`, `"depends_on":null`, 1)},
		{name: "null contract proofs array", raw: strings.Replace(valid, `"kind":"test"`, `"kind":"test","contract_proofs":null`, 1)},
		{name: "null project gates array", raw: strings.Replace(valid, `"kind":"implementation"`, `"kind":"implementation","project_gates":null`, 1)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeImplementationPlan([]byte(tt.raw)); err == nil {
				t.Fatal("expected strict decoder error")
			}
		})
	}
}

func TestDecodeImplementationPlanRejectsExcessiveJSONNesting(t *testing.T) {
	depth := 65
	raw := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	if _, err := DecodeImplementationPlan([]byte(raw)); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("excessively nested JSON error=%v, want nesting-depth rejection", err)
	}
}

func TestImplementationPlanTopologicalOrderRejectsCycles(t *testing.T) {
	plan := ImplementationPlan{Steps: []ImplementationPlanStep{
		{ID: "PLAN-001", DependsOn: []string{"PLAN-002"}},
		{ID: "PLAN-002", DependsOn: []string{"PLAN-001"}},
	}}
	if _, err := plan.TopologicalOrder(); err == nil {
		t.Fatal("expected dependency-cycle error")
	}
}

func TestImplementationPlanBindsExactLockedContractBytes(t *testing.T) {
	contract := validImplementationPlanContract(t, LockedChangeContractSchemaVersion, ChangeKindFeature)
	raw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	plan := validRedGreenImplementationPlan(hex.EncodeToString(sum[:]))
	if err := plan.ValidateAgainst(contract, raw); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	semanticallyEqualBytes, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == string(semanticallyEqualBytes) {
		t.Fatal("fixture must distinguish exact bytes from re-serialized contract bytes")
	}
	if err := plan.ValidateAgainst(contract, semanticallyEqualBytes); err == nil || !strings.Contains(err.Error(), "different Change Contract") {
		t.Fatalf("plan bound to re-serialized bytes accepted: %v", err)
	}
}

func TestImplementationPlanValidatesAgainstContractSemantics(t *testing.T) {
	cases := []struct {
		name      string
		contract  func(ChangeContract) ChangeContract
		plan      func(ImplementationPlan) ImplementationPlan
		wantError string
	}{
		{name: "locked schema v4 accepted"},
		{name: "locked schema v6 accepted", contract: func(c ChangeContract) ChangeContract {
			c.SchemaVersion = CommitIntentLockedChangeContractSchemaVersion
			return c
		}},
		{name: "unlocked schema v3 rejected", contract: func(c ChangeContract) ChangeContract {
			c.SchemaVersion = StrictChangeContractSchemaVersion
			c.DevelopmentMethod = DevelopmentMethodStrictSDDTDDV1
			c.BaselineLock = nil
			return c
		}, wantError: "locked strict Change Contract"},
		{name: "base commit mismatch rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.BaseCommit = strings.Repeat("3", 40); return p }, wantError: "baseline does not match"},
		{name: "base tree mismatch rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.BaseTree = strings.Repeat("3", 40); return p }, wantError: "baseline does not match"},
		{name: "strategy mismatch rejected", plan: func(p ImplementationPlan) ImplementationPlan {
			p.Strategy = ImplementationPlanStrategyGreenGreen
			return p
		}, wantError: "strategy"},
		{name: "unknown requirement rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[2].Requirements = []string{"REQ-999"}; return p }, wantError: "unknown requirement"},
		{name: "requirement without implementation rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[2].Requirements = nil; return p }, wantError: "does not cover requirement"},
		{name: "test scope escape rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].AllowedPaths = []string{"pkg/"}; return p }, wantError: "outside test_scope"},
		{name: "change scope escape rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[2].AllowedPaths = []string{"pkg/"}; return p }, wantError: "outside Change Contract scope"},
		{name: "acceptance criterion without linked proof rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].Requirements = nil; return p }, wantError: "no linked requirement proof path"},
		{name: "unknown acceptance criterion rejected", plan: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[0].AcceptanceCriteria = []string{"AC-999"}
			return p
		}, wantError: "unknown acceptance criterion"},
		{name: "acceptance criterion without validation rejected", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].AcceptanceCriteria = nil; return p }, wantError: "does not cover acceptance criterion"},
		{name: "final validation missing", plan: func(p ImplementationPlan) ImplementationPlan { p.Steps = p.Steps[:len(p.Steps)-1]; return p }, wantError: "final validation"},
		{name: "final validation omits contract proofs", plan: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[3].ContractProofs = []string{"behavior", "affected"}
			return p
		}, wantError: "must reference regression, behavior, and affected"},
		{name: "implementation before Red proof rejected", plan: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[2].DependsOn = []string{"PLAN-001"}
			p.Steps[1].DependsOn = []string{"PLAN-003"}
			p.Steps[3].DependsOn = []string{"PLAN-002"}
			p.Steps = []ImplementationPlanStep{p.Steps[0], p.Steps[2], p.Steps[1], p.Steps[3]}
			return p
		}, wantError: "order test, Red proof, and implementation"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			contract := validImplementationPlanContract(t, LockedChangeContractSchemaVersion, ChangeKindFeature)
			if tt.contract != nil {
				contract = tt.contract(contract)
			}
			raw, err := json.MarshalIndent(contract, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			plan := validRedGreenImplementationPlan(hex.EncodeToString(sum[:]))
			if tt.plan != nil {
				plan = tt.plan(plan)
			}
			err = plan.ValidateAgainst(contract, raw)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("valid contract-bound plan rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ValidateAgainst error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestImplementationPlanRequiresGreenGreenCharacterizationAroundImplementation(t *testing.T) {
	contract := validImplementationPlanContract(t, LockedChangeContractSchemaVersion, ChangeKindBehaviorPreserving)
	raw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	valid := validGreenGreenImplementationPlan(hex.EncodeToString(sum[:]))
	if err := valid.ValidateAgainst(contract, raw); err != nil {
		t.Fatalf("valid Green/Green plan rejected: %v", err)
	}
	cases := []struct {
		name      string
		mutate    func(ImplementationPlan) ImplementationPlan
		wantError string
	}{
		{name: "missing baseline characterization", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].ContractProofs = nil; return p }, wantError: "baseline characterization"},
		{name: "missing target characterization", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[2].ContractProofs = nil; return p }, wantError: "target characterization"},
		{name: "Red proof forbidden", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps = append(p.Steps, ImplementationPlanStep{ID: "PLAN-005", Kind: ImplementationPlanStepProof, Objective: "Run a Red proof", ContractProofs: []string{"regression"}, DependsOn: []string{"PLAN-004"}})
			return p
		}, wantError: "must not introduce a Red proof"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.mutate(cloneImplementationPlan(valid))
			if err := plan.ValidateAgainst(contract, raw); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ValidateAgainst error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestImplementationPlanGraphRejectsMalformedEdgesAndSortsDeterministically(t *testing.T) {
	base := validRedGreenImplementationPlan(strings.Repeat("a", 64))
	cases := []struct {
		name      string
		mutate    func(ImplementationPlan) ImplementationPlan
		wantError string
	}{
		{name: "duplicate step IDs", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[1].ID = p.Steps[0].ID; return p }, wantError: "duplicate implementation plan step ID"},
		{name: "unknown dependency", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[1].DependsOn = []string{"PLAN-999"}; return p }, wantError: "unknown step"},
		{name: "self dependency", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[1].DependsOn = []string{"PLAN-002"}; return p }, wantError: "self-dependency"},
		{name: "duplicate dependency", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[1].DependsOn = []string{"PLAN-001", "PLAN-001"}
			return p
		}, wantError: "duplicate reference"},
		{name: "forward dependency", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].DependsOn = []string{"PLAN-002"}; return p }, wantError: "must precede"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.mutate(cloneImplementationPlan(base)).Validate(); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate error=%v want substring %q", err, tt.wantError)
			}
		})
	}
	ordered, err := (ImplementationPlan{Steps: []ImplementationPlanStep{
		{ID: "PLAN-003", DependsOn: []string{"PLAN-001"}}, {ID: "PLAN-002"}, {ID: "PLAN-001"},
	}}).TopologicalOrder()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PLAN-001", "PLAN-002", "PLAN-003"}
	if strings.Join(ordered, ",") != strings.Join(want, ",") {
		t.Fatalf("topological order=%v want=%v", ordered, want)
	}
}

func TestImplementationPlanValidateRejectsMalformedStructure(t *testing.T) {
	base := validRedGreenImplementationPlan(strings.Repeat("a", 64))
	cases := []struct {
		name      string
		mutate    func(ImplementationPlan) ImplementationPlan
		wantError string
	}{
		{name: "unsupported schema", mutate: func(p ImplementationPlan) ImplementationPlan { p.SchemaVersion++; return p }, wantError: "schema_version"},
		{name: "invalid contract digest", mutate: func(p ImplementationPlan) ImplementationPlan { p.ChangeContractSHA256 = "BAD"; return p }, wantError: "change_contract_sha256"},
		{name: "unsupported object format", mutate: func(p ImplementationPlan) ImplementationPlan { p.GitObjectFormat = "sha512"; return p }, wantError: "git_object_format"},
		{name: "invalid base commit", mutate: func(p ImplementationPlan) ImplementationPlan { p.BaseCommit = strings.Repeat("A", 40); return p }, wantError: "base_commit"},
		{name: "invalid base tree", mutate: func(p ImplementationPlan) ImplementationPlan { p.BaseTree = "bad"; return p }, wantError: "base_tree"},
		{name: "unsupported strategy", mutate: func(p ImplementationPlan) ImplementationPlan { p.Strategy = "invented"; return p }, wantError: "strategy"},
		{name: "empty steps", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps = nil; return p }, wantError: "steps must not be empty"},
		{name: "too many steps", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps = make([]ImplementationPlanStep, MaxImplementationPlanSteps+1)
			return p
		}, wantError: "more than"},
		{name: "malformed step ID", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].ID = "STEP-001"; return p }, wantError: "PLAN-"},
		{name: "non-canonical step ID", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].ID = "PLAN-0001"; return p }, wantError: "not a canonical"},
		{name: "overflow step ID", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[0].ID = "PLAN-999999999999999999999999"
			return p
		}, wantError: "not a canonical"},
		{name: "empty objective", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].Objective = "  "; return p }, wantError: "objective"},
		{name: "unsupported step kind", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].Kind = "command"; return p }, wantError: "unsupported"},
		{name: "empty requirement reference", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[0].Requirements = []string{""}; return p }, wantError: "empty reference"},
		{name: "duplicate acceptance reference", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[0].AcceptanceCriteria = []string{"AC-001", "AC-001"}
			return p
		}, wantError: "duplicate reference"},
		{name: "invalid allowed path", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[0].AllowedPaths = []string{"../outside"}
			return p
		}, wantError: "allowed_paths"},
		{name: "empty proof reference", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[1].ContractProofs = []string{""}; return p }, wantError: "empty reference"},
		{name: "unsupported proof", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[1].ContractProofs = []string{"command"}
			return p
		}, wantError: "unsupported proof"},
		{name: "empty project gate", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[3].ProjectGates = []string{""}; return p }, wantError: "empty reference"},
		{name: "gate on non-validation step", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[0].ProjectGates = []string{"test.complete"}
			return p
		}, wantError: "only valid on validation"},
		{name: "unknown gate", mutate: func(p ImplementationPlan) ImplementationPlan {
			p.Steps[3].ProjectGates = []string{"unknown.gate"}
			return p
		}, wantError: "unknown project gate"},
		{name: "no implementation step", mutate: func(p ImplementationPlan) ImplementationPlan { p.Steps[2].Kind = ImplementationPlanStepTest; return p }, wantError: "implementation step"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.mutate(cloneImplementationPlan(base)).Validate(); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestImplementationPlanRequiresSerializedOrderToMatchTopologicalOrder(t *testing.T) {
	plan := validRedGreenImplementationPlan(strings.Repeat("a", 64))
	for i := range plan.Steps {
		plan.Steps[i].DependsOn = nil
	}
	plan.Steps[0], plan.Steps[1] = plan.Steps[1], plan.Steps[0]
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "topological order") {
		t.Fatalf("non-canonical serialized order accepted: %v", err)
	}
}

func TestImplementationPlanProjectGatesFollowEffectiveExecutionOrder(t *testing.T) {
	plan := validRedGreenImplementationPlan(strings.Repeat("a", 64))
	plan.Steps[3].ProjectGates = []string{ProjectGateOrder[0], ProjectGateOrder[2]}
	if err := plan.ValidateProjectGates(ProjectGateOrder); err != nil {
		t.Fatalf("canonical project gate order rejected: %v", err)
	}
	plan.Steps[3].ProjectGates = []string{ProjectGateOrder[2], ProjectGateOrder[0]}
	if err := plan.ValidateProjectGates(ProjectGateOrder); err == nil || !strings.Contains(err.Error(), "execution order") {
		t.Fatalf("out-of-order project gates accepted: %v", err)
	}
	plan.Steps[3].ProjectGates = []string{"unknown.gate"}
	if err := plan.ValidateProjectGates(ProjectGateOrder); err == nil || !strings.Contains(err.Error(), "outside the effective policy") {
		t.Fatalf("unknown project gate accepted: %v", err)
	}
}

func cloneImplementationPlan(plan ImplementationPlan) ImplementationPlan {
	plan.Steps = append([]ImplementationPlanStep(nil), plan.Steps...)
	return plan
}

func validImplementationPlanContract(t *testing.T, schemaVersion int, kind string) ChangeContract {
	t.Helper()
	command := validCommand()
	command.Environment = &EnvironmentSpec{Mode: EnvironmentModeClean, Pass: []string{"PATH"}}
	development := DevelopmentSpecification{
		Objective:        "Implement the locked requirement",
		Requirements:     []SpecificationClause{{ID: "REQ-001", Statement: "The behavior is correct"}},
		Invariants:       []SpecificationClause{{ID: "INV-001", Statement: "The locked baseline remains authoritative"}},
		ForbiddenStates:  []SpecificationClause{{ID: "FORB-001", Statement: "Unscoped files are not changed"}},
		Inputs:           []SpecificationClause{{ID: "IN-001", Statement: "The locked contract is the input"}},
		Outputs:          []SpecificationClause{{ID: "OUT-001", Statement: "The planned change is delivered"}},
		FailureSemantics: []SpecificationClause{{ID: "FAIL-001", Statement: "Invalid plans fail closed"}},
		AcceptanceCriteria: []AcceptanceCriterion{{
			ID: "AC-001", Statement: "The behavior passes", Requirements: []string{"REQ-001"}, Proof: ProofGateRegression,
		}},
	}
	specificationSHA256, err := development.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	exitCode := 1
	regression := RegressionContract{Mode: RegressionModeRedGreen, Command: &command, BaselineExitCode: &exitCode, BaselineOutputContains: []string{"FAIL"}}
	if kind == ChangeKindBehaviorPreserving {
		regression = RegressionContract{Mode: RegressionModeGreenGreen, Command: &command}
	}
	var baselineLock *BaselineLock
	method := DevelopmentMethodStrictSDDTDDV2
	if schemaVersion == StrictChangeContractSchemaVersion {
		method = DevelopmentMethodStrictSDDTDDV1
	} else {
		baselineLock = &BaselineLock{
			GitObjectFormat: "sha1", BaseCommit: strings.Repeat("1", 40), BaseTree: strings.Repeat("2", 40),
			PolicySHA256: strings.Repeat("a", 64), SpecificationSHA256: specificationSHA256,
		}
	}
	contract := ChangeContract{
		SchemaVersion:     schemaVersion,
		Kind:              kind,
		Scope:             &ChangeScope{AllowedPaths: []string{"internal/"}},
		TestScope:         &ChangeScope{AllowedPaths: []string{"internal/"}},
		DevelopmentMethod: method,
		Specification:     &development,
		BaselineLock:      baselineLock,
		Behavior:          command, Affected: command, Regression: regression,
	}
	if err := contract.Validate(); err != nil {
		t.Fatalf("invalid fixture Change Contract: %v", err)
	}
	return contract
}

func validRedGreenImplementationPlan(contractSHA256 string) ImplementationPlan {
	return ImplementationPlan{
		SchemaVersion: ImplementationPlanSchemaVersion, ChangeContractSHA256: contractSHA256,
		GitObjectFormat: "sha1", BaseCommit: strings.Repeat("1", 40), BaseTree: strings.Repeat("2", 40),
		Strategy: ImplementationPlanStrategyRedGreen,
		Steps: []ImplementationPlanStep{
			{ID: "PLAN-001", Kind: ImplementationPlanStepTest, Objective: "Establish the failing test", Requirements: []string{"REQ-001"}, AcceptanceCriteria: []string{"AC-001"}, AllowedPaths: []string{"internal/"}},
			{ID: "PLAN-002", Kind: ImplementationPlanStepProof, Objective: "Capture the required Red proof", ContractProofs: []string{"regression"}, DependsOn: []string{"PLAN-001"}},
			{ID: "PLAN-003", Kind: ImplementationPlanStepImplementation, Objective: "Implement REQ-001", Requirements: []string{"REQ-001"}, AcceptanceCriteria: []string{"AC-001"}, AllowedPaths: []string{"internal/"}, DependsOn: []string{"PLAN-002"}},
			{ID: "PLAN-004", Kind: ImplementationPlanStepValidation, Objective: "Run final contract proofs", ContractProofs: []string{"regression", "behavior", "affected"}, DependsOn: []string{"PLAN-003"}},
		},
	}
}

func validGreenGreenImplementationPlan(contractSHA256 string) ImplementationPlan {
	return ImplementationPlan{
		SchemaVersion: ImplementationPlanSchemaVersion, ChangeContractSHA256: contractSHA256,
		GitObjectFormat: "sha1", BaseCommit: strings.Repeat("1", 40), BaseTree: strings.Repeat("2", 40),
		Strategy: ImplementationPlanStrategyGreenGreen,
		Steps: []ImplementationPlanStep{
			{ID: "PLAN-001", Kind: ImplementationPlanStepTest, Objective: "Characterize the baseline", Requirements: []string{"REQ-001"}, AcceptanceCriteria: []string{"AC-001"}, AllowedPaths: []string{"internal/"}, ContractProofs: []string{"regression"}},
			{ID: "PLAN-002", Kind: ImplementationPlanStepImplementation, Objective: "Implement REQ-001", Requirements: []string{"REQ-001"}, AcceptanceCriteria: []string{"AC-001"}, AllowedPaths: []string{"internal/"}, DependsOn: []string{"PLAN-001"}},
			{ID: "PLAN-003", Kind: ImplementationPlanStepTest, Objective: "Characterize the target", Requirements: []string{"REQ-001"}, AcceptanceCriteria: []string{"AC-001"}, AllowedPaths: []string{"internal/"}, ContractProofs: []string{"regression"}, DependsOn: []string{"PLAN-002"}},
			{ID: "PLAN-004", Kind: ImplementationPlanStepValidation, Objective: "Run final validation", ContractProofs: []string{"behavior", "affected"}, DependsOn: []string{"PLAN-003"}},
		},
	}
}
