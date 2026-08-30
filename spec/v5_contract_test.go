package spec

import (
	"math"
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
