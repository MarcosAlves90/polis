package changeexec

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestChangeExecPrerequisiteHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "missing-runner":
		_, _ = os.Stderr.WriteString("sh: missing-regression-runner: command not found\n")
		os.Exit(127)
	case "assertion":
		_, _ = os.Stderr.WriteString("RED-ASSERTION\n")
		os.Exit(127)
	default:
		os.Exit(9)
	}
}

func TestBaselineDoesNotAcceptMissingPrerequisiteAsRed(t *testing.T) {
	code := 127
	regression := cmd(os.Args[0], "-test.run=^TestChangeExecPrerequisiteHelper$", "--", "missing-runner")
	contract := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &code, BaselineOutputContains: []string{"missing-regression-runner"}}}
	var evidence bytes.Buffer
	err := ExecuteBaseline(contract, t.TempDir(), &evidence)
	if err == nil || !strings.Contains(err.Error(), "missing executable missing-regression-runner") || !strings.Contains(err.Error(), "intended checks did not run") {
		t.Fatalf("missing prerequisite accepted as Red: err=%v evidence=%s", err, evidence.String())
	}
	if !strings.Contains(evidence.String(), `"gate":"regression","status":"BLOCKED"`) {
		t.Fatalf("blocked regression not recorded: %s", evidence.String())
	}
}

func TestBaselineStillAcceptsRealAssertionWithExit127(t *testing.T) {
	code := 127
	regression := cmd(os.Args[0], "-test.run=^TestChangeExecPrerequisiteHelper$", "--", "assertion")
	contract := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: cmd("true"), Affected: cmd("true"), Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &code, BaselineOutputContains: []string{"RED-ASSERTION"}}}
	var evidence bytes.Buffer
	if err := ExecuteBaseline(contract, t.TempDir(), &evidence); err != nil {
		t.Fatalf("real Red assertion rejected: %v", err)
	}
	if !strings.Contains(evidence.String(), `"gate":"regression","status":"PASS"`) {
		t.Fatalf("Red oracle did not pass: %s", evidence.String())
	}
}

func TestTargetNamesMissingRegressionDependency(t *testing.T) {
	regression := strictCmd(os.Args[0], "-test.run=^TestChangeExecPrerequisiteHelper$", "--", "missing-runner")
	contract := strictBehaviorPreservingContract(regression)
	var evidence bytes.Buffer
	err := ExecuteTarget(contract, t.TempDir(), &evidence)
	if err == nil || !strings.Contains(err.Error(), "missing executable missing-regression-runner") || !strings.Contains(err.Error(), "intended checks did not run") {
		t.Fatalf("missing target prerequisite not reported: %v evidence=%s", err, evidence.String())
	}
}

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
