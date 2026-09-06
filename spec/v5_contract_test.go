package spec

import (
	"math"
	"strings"
	"testing"
)

func cleanCommand(argv ...string) CommandSpec {
	return CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 60, Environment: &EnvironmentSpec{Mode: EnvironmentModeClean, Pass: []string{"PATH"}}}
}

func TestChangeContractV2RequiresAndEnforcesScope(t *testing.T) {
	c := ChangeContract{
		SchemaVersion: ChangeContractSchemaVersion,
		Kind:          ChangeKindFeature,
		Behavior:      cleanCommand("go", "test", "./..."),
		Affected:      cleanCommand("go", "test", "./..."),
		Regression:    RegressionContract{Mode: RegressionModeNotApplicable, ReasonCode: RegressionReasonNotDefect},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected missing scope to fail")
	}
	c.Scope = &ChangeScope{AllowedPaths: []string{"spec/", "cmd/polis/main.go"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid v2 contract: %v", err)
	}
	if !c.AllowsPath("spec/change.go") || !c.AllowsPath("cmd/polis/main.go") || c.AllowsPath("README.md") {
		t.Fatal("scope matching is incorrect")
	}
}

func TestPolicyV3RequiresExplicitCommandEnvironment(t *testing.T) {
	threshold := 80.0
	reason := "not applicable"
	gates := make([]GatePolicy, 0, len(ProjectGateOrder))
	for _, id := range ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, GatePolicy{ID: id, Mode: GateModeCommand, Command: &CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}})
		case "coverage":
			gates = append(gates, GatePolicy{ID: id, Mode: GateModeCoverage, Command: &CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60, Environment: &EnvironmentSpec{Mode: EnvironmentModeClean, Pass: []string{"PATH"}}}, Adapter: CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		default:
			gates = append(gates, GatePolicy{ID: id, Mode: GateModeNotApplicable, Reason: &reason})
		}
	}
	p := Policy{SchemaVersion: PolicySchemaVersion, Gates: gates}
	if err := p.Validate(); err == nil {
		t.Fatal("expected v3 command without environment to fail")
	}
	gates[0].Command.Environment = &EnvironmentSpec{Mode: EnvironmentModeClean, Pass: []string{"PATH"}}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid v3 policy: %v", err)
	}
}

func TestLCOVAndCoberturaAdapters(t *testing.T) {
	lcov := []byte("TN:\nSF:a.go\nDA:1,1\nDA:2,0\nend_of_record\nSF:b.go\nDA:1,3\nend_of_record\n")
	got, err := ParseCoverage(CoverageAdapterLCOVV1, lcov)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoveredLines != 2 || got.TotalLines != 3 || math.Abs(got.Percent-66.66666666666667) > 1e-9 {
		t.Fatalf("unexpected lcov metric: %+v", got)
	}

	cobertura := []byte(`<?xml version="1.0"?><coverage><packages><package><classes><class filename="a.py"><lines><line number="1" hits="1"/><line number="2" hits="0"/></lines></class><class filename="b.py"><lines><line number="1" hits="2"/></lines></class></classes></package></packages></coverage>`)
	got, err = ParseCoverage(CoverageAdapterCoberturaV1, cobertura)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoveredLines != 2 || got.TotalLines != 3 {
		t.Fatalf("unexpected cobertura metric: %+v", got)
	}
}

func strictSpecificationFixture() *DevelopmentSpecification {
	clause := func(id, statement string) SpecificationClause {
		return SpecificationClause{ID: id, Statement: statement}
	}
	return &DevelopmentSpecification{
		Objective:          "machine-enforce strict SDD/TDD",
		Requirements:       []SpecificationClause{clause("REQ-001", "strict features prove Red then Green")},
		AcceptanceCriteria: []AcceptanceCriterion{{ID: "AC-001", Statement: "strict features prove Red then Green", Requirements: []string{"REQ-001"}, Proof: ProofGateRegression}},
		Invariants:         []SpecificationClause{clause("INV-001", "legacy contracts keep their semantics")},
		ForbiddenStates:    []SpecificationClause{clause("FORBID-001", "production paths are absent from the Red probe")},
		Inputs:             []SpecificationClause{clause("IN-001", "external strict Change Contract")},
		Outputs:            []SpecificationClause{clause("OUT-001", "validated strict delivery")},
		FailureSemantics:   []SpecificationClause{clause("FAIL-001", "invalid proof fails closed")},
	}
}

func strictFeatureContractFixture() ChangeContract {
	exit := 1
	regression := cleanCommand("go", "test", "./...", "-run", "TestStrictFeature")
	return ChangeContract{
		SchemaVersion:     StrictChangeContractSchemaVersion,
		Kind:              ChangeKindFeature,
		Scope:             &ChangeScope{AllowedPaths: []string{"spec/", "internal/"}},
		TestScope:         &ChangeScope{AllowedPaths: []string{"spec/"}},
		DevelopmentMethod: DevelopmentMethodStrictSDDTDDV1,
		Specification:     strictSpecificationFixture(),
		Behavior:          cleanCommand("go", "test", "./..."),
		Affected:          cleanCommand("go", "test", "./..."),
		Regression: RegressionContract{
			Mode:                   RegressionModeRedGreen,
			Command:                &regression,
			BaselineExitCode:       &exit,
			BaselineOutputContains: []string{"STRICT-FEATURE-RED"},
		},
	}
}

func TestChangeContractV3AcceptsStrictFeature(t *testing.T) {
	c := strictFeatureContractFixture()
	if err := c.Validate(); err != nil {
		t.Fatalf("valid strict feature: %v", err)
	}
	if !c.RequiresRedGreen() {
		t.Fatal("strict feature must require Red -> Green")
	}
	if err := c.ValidateTestPaths([]string{"spec/change_test.go"}); err != nil {
		t.Fatalf("valid strict test path: %v", err)
	}
}

func TestChangeContractV3RejectsMissingSpecificationSections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DevelopmentSpecification)
	}{
		{"objective", func(s *DevelopmentSpecification) { s.Objective = "" }},
		{"acceptance criteria", func(s *DevelopmentSpecification) { s.AcceptanceCriteria = nil }},
		{"invariants", func(s *DevelopmentSpecification) { s.Invariants = nil }},
		{"forbidden states", func(s *DevelopmentSpecification) { s.ForbiddenStates = nil }},
		{"inputs", func(s *DevelopmentSpecification) { s.Inputs = nil }},
		{"outputs", func(s *DevelopmentSpecification) { s.Outputs = nil }},
		{"failure semantics", func(s *DevelopmentSpecification) { s.FailureSemantics = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := strictFeatureContractFixture()
			tc.mutate(c.Specification)
			if err := c.Validate(); err == nil {
				t.Fatal("expected strict specification validation error")
			}
		})
	}
}

func TestChangeContractV3RejectsWeakFeatureRegression(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Regression = RegressionContract{Mode: RegressionModeNotApplicable, ReasonCode: RegressionReasonNotDefect}
	if err := c.Validate(); err == nil {
		t.Fatal("strict feature without Red -> Green accepted")
	}
}

func TestChangeContractV3RejectsInvalidSpecificationClauses(t *testing.T) {
	for _, mutate := range []func(*DevelopmentSpecification){
		func(s *DevelopmentSpecification) { s.AcceptanceCriteria[0].ID = "" },
		func(s *DevelopmentSpecification) { s.AcceptanceCriteria[0].Statement = "  " },
		func(s *DevelopmentSpecification) { s.Invariants[0].ID = s.AcceptanceCriteria[0].ID },
	} {
		c := strictFeatureContractFixture()
		mutate(c.Specification)
		if err := c.Validate(); err == nil {
			t.Fatal("invalid strict specification clause accepted")
		}
	}
}

func TestChangeContractV3RequiresTestScopeInsideChangeScope(t *testing.T) {
	c := strictFeatureContractFixture()
	c.TestScope = &ChangeScope{AllowedPaths: []string{"docs/"}}
	if err := c.Validate(); err == nil {
		t.Fatal("test scope outside change scope accepted")
	}

	c = strictFeatureContractFixture()
	if err := c.ValidateTestPaths([]string{"internal/packagebuild/build_test.go"}); err == nil {
		t.Fatal("Red path outside test scope accepted")
	}
}

func TestChangeContractV3ReportsSchemaV3EnvironmentRequirement(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Regression.Command.Environment = nil
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "schema v3 requires explicit command environment") {
		t.Fatalf("expected schema-v3 environment error, got %v", err)
	}
}

func TestChangeContractV3BehaviorPreservingRequiresGreenGreen(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Kind = ChangeKindBehaviorPreserving
	regression := cleanCommand("go", "test", "./...", "-run", "TestCharacterization")
	c.Regression = RegressionContract{Mode: RegressionModeGreenGreen, Command: &regression}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid strict behavior-preserving contract: %v", err)
	}
	if !c.RequiresGreenGreen() || !c.RequiresBaselineProof() || c.RequiresRedGreen() || c.RequiresRegressionPatch() {
		t.Fatalf("unexpected proof predicates: red=%v green=%v baseline=%v patch=%v", c.RequiresRedGreen(), c.RequiresGreenGreen(), c.RequiresBaselineProof(), c.RequiresRegressionPatch())
	}
}

func TestChangeContractV3BehaviorPreservingRejectsNotApplicable(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Kind = ChangeKindBehaviorPreserving
	c.Regression = RegressionContract{Mode: RegressionModeNotApplicable, ReasonCode: RegressionReasonNotDefect}
	if err := c.Validate(); err == nil {
		t.Fatal("strict behavior-preserving accepted not_applicable regression")
	}
}

func TestChangeContractV3GreenGreenRejectsRedOracleFields(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Kind = ChangeKindBehaviorPreserving
	regression := cleanCommand("go", "test", "./...", "-run", "TestCharacterization")
	exit := 1
	c.Regression = RegressionContract{Mode: RegressionModeGreenGreen, Command: &regression, BaselineExitCode: &exit}
	if err := c.Validate(); err == nil {
		t.Fatal("green_green accepted red-only baseline oracle")
	}
}

func TestStrictSpecificationRequiresTraceability(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Specification.Requirements = []SpecificationClause{{ID: "REQ-001", Statement: "strict feature is test-first"}}
	c.Specification.AcceptanceCriteria = []AcceptanceCriterion{{ID: "AC-001", Statement: "feature proves Red then Green", Requirements: []string{"REQ-001"}, Proof: ProofGateRegression}}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid traceability rejected: %v", err)
	}
	links := c.Specification.TraceabilityLinks()
	if len(links) != 1 || links[0].RequirementID != "REQ-001" || links[0].AcceptanceCriterionID != "AC-001" || links[0].Proof != ProofGateRegression {
		t.Fatalf("unexpected links: %+v", links)
	}
}

func TestStrictSpecificationRejectsOrphanRequirement(t *testing.T) {
	c := strictFeatureContractFixture()
	c.Specification.Requirements = []SpecificationClause{{ID: "REQ-001", Statement: "covered"}, {ID: "REQ-002", Statement: "orphan"}}
	c.Specification.AcceptanceCriteria = []AcceptanceCriterion{{ID: "AC-001", Statement: "covers one", Requirements: []string{"REQ-001"}, Proof: ProofGateRegression}}
	if err := c.Validate(); err == nil {
		t.Fatal("orphan requirement accepted")
	}
}

func TestStrictSpecificationRejectsUnknownDuplicateAndUnsupportedProof(t *testing.T) {
	for _, mutate := range []func(*DevelopmentSpecification){
		func(s *DevelopmentSpecification) { s.AcceptanceCriteria[0].Requirements = []string{"REQ-404"} },
		func(s *DevelopmentSpecification) {
			s.AcceptanceCriteria[0].Requirements = []string{"REQ-001", "REQ-001"}
		},
		func(s *DevelopmentSpecification) { s.AcceptanceCriteria[0].Proof = "coverage" },
	} {
		c := strictFeatureContractFixture()
		c.Specification.Requirements = []SpecificationClause{{ID: "REQ-001", Statement: "strict proof"}}
		c.Specification.AcceptanceCriteria = []AcceptanceCriterion{{ID: "AC-001", Statement: "proof", Requirements: []string{"REQ-001"}, Proof: ProofGateRegression}}
		mutate(c.Specification)
		if err := c.Validate(); err == nil {
			t.Fatal("invalid traceability accepted")
		}
	}
}

func TestChangeContractV4RequiresValidBaselineLock(t *testing.T) {
	c := strictFeatureContractFixture()
	c.SchemaVersion = LockedChangeContractSchemaVersion
	c.DevelopmentMethod = DevelopmentMethodStrictSDDTDDV2
	digest, err := c.Specification.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	c.BaselineLock = &BaselineLock{
		GitObjectFormat:     "sha1",
		BaseCommit:          strings.Repeat("1", 40),
		BaseTree:            strings.Repeat("2", 40),
		PolicySHA256:        strings.Repeat("a", 64),
		SpecificationSHA256: digest,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid locked contract: %v", err)
	}
	if !c.IsStrictDevelopment() || !c.RequiresRedGreen() || !c.RequiresRegressionPatch() {
		t.Fatalf("locked strict predicates incorrect")
	}
}

func TestChangeContractV4RejectsSpecificationDigestMismatch(t *testing.T) {
	c := strictFeatureContractFixture()
	c.SchemaVersion = LockedChangeContractSchemaVersion
	c.DevelopmentMethod = DevelopmentMethodStrictSDDTDDV2
	c.BaselineLock = &BaselineLock{GitObjectFormat: "sha1", BaseCommit: strings.Repeat("1", 40), BaseTree: strings.Repeat("2", 40), PolicySHA256: strings.Repeat("a", 64), SpecificationSHA256: strings.Repeat("b", 64)}
	if err := c.Validate(); err == nil {
		t.Fatal("specification digest mismatch accepted")
	}
}

func TestChangeContractV3RejectsBaselineLock(t *testing.T) {
	c := strictFeatureContractFixture()
	c.BaselineLock = &BaselineLock{}
	if err := c.Validate(); err == nil {
		t.Fatal("schema v3 accepted baseline_lock")
	}
}
