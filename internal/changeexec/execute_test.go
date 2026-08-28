package changeexec

import (
	"bytes"
	"testing"

	"github.com/polis-dev/polis-v4/spec"
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
	if len(events) != 3 || events[1].Status != spec.StatusFail || events[2].Status != spec.StatusPass {
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
