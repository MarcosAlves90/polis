package changeexec

import (
	"bytes"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func cmd(argv ...string) spec.CommandSpec {
	return spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: 5}
}

func TestExecuteBaselineAcceptsDeclaredRed(t *testing.T) {
	code := 7
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: ptrCmd(cmd("sh", "-c", "printf BUG >&2; exit 7")), BaselineExitCode: &code, BaselineOutputContains: []string{"BUG"}}}
	var evidence bytes.Buffer
	if err := ExecuteBaseline(c, t.TempDir(), &evidence); err != nil {
		t.Fatal(err)
	}
	events, err := spec.DecodeEvidence(evidence.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[1].Status != spec.StatusFail || events[2].Event != "oracle_checked" || events[3].Status != spec.StatusPass {
		t.Fatalf("events=%+v", events)
	}
}

func TestExecuteBaselineRejectsWrongOracle(t *testing.T) {
	code := 2
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: ptrCmd(cmd("sh", "-c", "printf other >&2; exit 7")), BaselineExitCode: &code, BaselineOutputContains: []string{"BUG"}}}
	if err := ExecuteBaseline(c, t.TempDir(), &bytes.Buffer{}); err == nil {
		t.Fatal("expected oracle rejection")
	}
}

func TestExecuteTargetRunsChangeGates(t *testing.T) {
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	var evidence bytes.Buffer
	if err := ExecuteTarget(c, t.TempDir(), &evidence); err != nil {
		t.Fatal(err)
	}
	events, err := spec.DecodeEvidence(evidence.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 8 {
		t.Fatalf("len=%d events=%+v", len(events), events)
	}
	if events[1].Status != spec.StatusNotApplicable || events[4].Status != spec.StatusPass || events[7].Status != spec.StatusPass {
		t.Fatalf("events=%+v", events)
	}
}

func TestExecuteTargetDefectRequiresRegressionGreen(t *testing.T) {
	code := 1
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: ptrCmd(cmd("false")), BaselineExitCode: &code, BaselineOutputContains: []string{"x"}}}
	if err := ExecuteTarget(c, t.TempDir(), &bytes.Buffer{}); err == nil {
		t.Fatal("expected regression green failure")
	}
}

func ptrCmd(c spec.CommandSpec) *spec.CommandSpec { return &c }

func strictCmd(argv ...string) spec.CommandSpec {
	return spec.CommandSpec{
		Argv: argv, Cwd: ".", TimeoutSeconds: 5,
		Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit},
	}
}

func strictFeatureContract(regression spec.CommandSpec, exit int, token string) spec.ChangeContract {
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	return spec.ChangeContract{
		SchemaVersion:     spec.StrictChangeContractSchemaVersion,
		Kind:              spec.ChangeKindFeature,
		Scope:             &spec.ChangeScope{AllowedPaths: []string{"."}},
		TestScope:         &spec.ChangeScope{AllowedPaths: []string{"."}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "prove strict feature execution",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "feature is Red then Green")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "feature is Red then Green", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "legacy behavior remains valid")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "feature skips Red")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "strict contract")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "strict evidence")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "wrong oracle fails")},
		},
		Behavior: strictCmd("true"),
		Affected: strictCmd("true"),
		Regression: spec.RegressionContract{
			Mode:                   spec.RegressionModeRedGreen,
			Command:                &regression,
			BaselineExitCode:       &exit,
			BaselineOutputContains: []string{token},
		},
	}
}

func TestExecuteBaselineAcceptsStrictFeatureRed(t *testing.T) {
	regression := strictCmd("sh", "-c", "printf STRICT-FEATURE-RED >&2; exit 7")
	c := strictFeatureContract(regression, 7, "STRICT-FEATURE-RED")
	var evidence bytes.Buffer
	if err := ExecuteBaseline(c, t.TempDir(), &evidence); err != nil {
		t.Fatalf("strict feature baseline: %v", err)
	}
}

func TestExecuteTargetRequiresStrictFeatureRegressionGreen(t *testing.T) {
	regression := strictCmd("false")
	c := strictFeatureContract(regression, 1, "STRICT-FEATURE-RED")
	if err := ExecuteTarget(c, t.TempDir(), &bytes.Buffer{}); err == nil {
		t.Fatal("strict feature target skipped regression Green")
	}
}

func strictBehaviorPreservingContract(regression spec.CommandSpec) spec.ChangeContract {
	c := strictFeatureContract(regression, 1, "unused")
	c.Kind = spec.ChangeKindBehaviorPreserving
	c.Regression = spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression}
	return c
}

func TestExecuteBaselineAcceptsStrictGreenGreen(t *testing.T) {
	c := strictBehaviorPreservingContract(strictCmd("true"))
	var evidence bytes.Buffer
	if err := ExecuteBaseline(c, t.TempDir(), &evidence); err != nil {
		t.Fatalf("strict behavior-preserving baseline: %v", err)
	}
	events, err := spec.DecodeEvidence(evidence.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[1].Status != spec.StatusPass || events[2].Status != spec.StatusPass {
		t.Fatalf("events=%+v", events)
	}
}

func TestExecuteTargetRequiresStrictGreenGreen(t *testing.T) {
	c := strictBehaviorPreservingContract(strictCmd("false"))
	if err := ExecuteTarget(c, t.TempDir(), &bytes.Buffer{}); err == nil {
		t.Fatal("strict behavior-preserving target skipped characterization Green")
	}
}
