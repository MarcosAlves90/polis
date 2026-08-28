package policyexec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/spec"
)

func TestPolicyExecHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	mode := args[len(args)-1]
	switch mode {
	case "pass":
		fmt.Fprint(os.Stdout, "helper-pass")
		os.Exit(0)
	case "fail":
		fmt.Fprint(os.Stderr, "helper-fail")
		os.Exit(7)
	case "sleep":
		time.Sleep(3 * time.Second)
		os.Exit(0)
	case "coverage-pass":
		_ = os.WriteFile("coverage.out", []byte("mode: set\nexample/a.go:1.1,5.1 1 1\n"), 0o644)
		os.Exit(0)
	case "coverage-80":
		_ = os.WriteFile("coverage.out", []byte("mode: set\nexample/a.go:1.1,4.1 1 1\nexample/a.go:5.1,5.1 1 0\n"), 0o644)
		os.Exit(0)
	case "coverage-malformed":
		_ = os.WriteFile("coverage.out", []byte("not a coverprofile\n"), 0o644)
		os.Exit(0)
	default:
		os.Exit(9)
	}
}

func testPolicy(t *testing.T, mode string) spec.Policy {
	t.Helper()
	reason := "not applicable in executor unit fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: helperCommand(t, "pass", 10)})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: helperCommand(t, "coverage-pass", 10), Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	if mode != "" {
		gates[0].Command = helperCommand(t, mode, 10)
	}
	return spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates}
}

func helperCommand(t *testing.T, mode string, timeout int) *spec.CommandSpec {
	t.Helper()
	return &spec.CommandSpec{
		Argv:           []string{os.Args[0], "-test.run=TestPolicyExecHelper", "--", mode},
		Cwd:            ".",
		TimeoutSeconds: timeout,
	}
}

func TestExecutePassAndEvidence(t *testing.T) {
	var evidence bytes.Buffer
	result := Execute(testPolicy(t, "pass"), t.TempDir(), &evidence)
	if result.Overall != spec.StatusPass {
		t.Fatalf("overall=%s", result.Overall)
	}
	events, err := spec.DecodeEvidence(evidence.Bytes())
	if err != nil {
		t.Fatalf("evidence invalid: %v\n%s", err, evidence.String())
	}
	if len(events) == 0 || !strings.Contains(evidence.String(), "helper-pass") {
		t.Fatalf("evidence=%s", evidence.String())
	}
}

func TestExecuteNonZeroIsFail(t *testing.T) {
	var evidence bytes.Buffer
	result := Execute(testPolicy(t, "fail"), t.TempDir(), &evidence)
	if result.Overall != spec.StatusFail {
		t.Fatalf("overall=%s evidence=%s", result.Overall, evidence.String())
	}
	if !strings.Contains(evidence.String(), `"exit_code":7`) || !strings.Contains(evidence.String(), "helper-fail") {
		t.Fatalf("evidence=%s", evidence.String())
	}
}

func TestExecuteMissingExecutableIsBlocked(t *testing.T) {
	p := testPolicy(t, "pass")
	p.Gates[0].Command = &spec.CommandSpec{Argv: []string{"definitely-not-a-polis-test-executable-404"}, Cwd: ".", TimeoutSeconds: 10}
	var evidence bytes.Buffer
	result := Execute(p, t.TempDir(), &evidence)
	if result.Overall != spec.StatusBlocked {
		t.Fatalf("overall=%s evidence=%s", result.Overall, evidence.String())
	}
	if !strings.Contains(evidence.String(), `"exit_code":-1`) {
		t.Fatalf("evidence=%s", evidence.String())
	}
}

func TestExecuteTimeoutIsFail(t *testing.T) {
	p := testPolicy(t, "pass")
	p.Gates[0].Command = helperCommand(t, "sleep", 1)
	var evidence bytes.Buffer
	result := Execute(p, t.TempDir(), &evidence)
	if result.Overall != spec.StatusFail {
		t.Fatalf("overall=%s evidence=%s", result.Overall, evidence.String())
	}
	if !strings.Contains(evidence.String(), `"exit_code":-1`) {
		t.Fatalf("evidence=%s", evidence.String())
	}
}

func TestExecuteUsesDeclaredCWDWithoutShell(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := testPolicy(t, "pass")
	p.Gates[0].Command.Cwd = "sub"
	// A shell would interpret this. Direct exec must instead fail to find an executable with this literal name.
	p.Gates[0].Command.Argv = []string{"echo should-not-run && exit 0"}
	var evidence bytes.Buffer
	result := Execute(p, root, &evidence)
	if result.Overall != spec.StatusBlocked {
		t.Fatalf("overall=%s evidence=%s", result.Overall, evidence.String())
	}
}

func TestExecuteCoverageUsesRuntimeLineMetric(t *testing.T) {
	p := testPolicy(t, "pass")
	var evidence bytes.Buffer
	root := t.TempDir()
	result := Execute(p, root, &evidence)
	if result.Gates["coverage"] != spec.StatusPass {
		t.Fatalf("coverage=%s evidence=%s", result.Gates["coverage"], evidence.String())
	}
	if !strings.Contains(evidence.String(), `"event":"coverage_measured"`) || !strings.Contains(evidence.String(), `"value_percent":100`) {
		t.Fatalf("evidence=%s", evidence.String())
	}
	if _, err := spec.DecodeEvidence(evidence.Bytes()); err != nil {
		t.Fatalf("evidence invalid: %v\n%s", err, evidence.String())
	}
}

func TestExecuteCoverageExactlyThresholdFails(t *testing.T) {
	p := testPolicy(t, "pass")
	p.Gates[1].Command = helperCommand(t, "coverage-80", 10)
	var evidence bytes.Buffer
	result := Execute(p, t.TempDir(), &evidence)
	if result.Overall != spec.StatusFail || result.Gates["coverage"] != spec.StatusFail {
		t.Fatalf("result=%+v evidence=%s", result, evidence.String())
	}
	if !strings.Contains(evidence.String(), `"value_percent":80`) {
		t.Fatalf("evidence=%s", evidence.String())
	}
}

func TestExecuteCoverageMalformedReportFails(t *testing.T) {
	p := testPolicy(t, "pass")
	p.Gates[1].Command = helperCommand(t, "coverage-malformed", 10)
	var evidence bytes.Buffer
	result := Execute(p, t.TempDir(), &evidence)
	if result.Gates["coverage"] != spec.StatusFail {
		t.Fatalf("result=%+v evidence=%s", result, evidence.String())
	}
}

func TestReadCoverageReportPathSafetyAndLimits(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.out")
	if _, err := readCoverageReport(root, missing); err == nil {
		t.Fatal("expected missing report error")
	}
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoverageReport(root, dir); err == nil {
		t.Fatal("expected directory report error")
	}
	good := filepath.Join(root, "good.out")
	if err := os.WriteFile(good, []byte("mode: set\nexample/a.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoverageReport(root, good); err != nil {
		t.Fatalf("good report rejected: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.out")
	if err := os.WriteFile(outside, []byte("mode: set\nexample/a.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.out")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := readCoverageReport(root, link); err == nil {
			t.Fatal("expected symlink escape rejection")
		}
	}
	big := filepath.Join(root, "big.out")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxCoverageReportBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := readCoverageReport(root, big); err == nil {
		t.Fatal("expected oversized report rejection")
	}
}
