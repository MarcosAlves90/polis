package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestRunGatesReportsConfiguredResultsWithoutArtifact(t *testing.T) {
	repo := makeBuildRepo(t)
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			args := []string{"gates", "--repo", repo, "--format", format}
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != exitPass {
				t.Fatalf("RED: gates command should execute configured project gates without a delivery artifact: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			if format == "text" {
				for _, fragment := range []string{
					"POLIS GATES: PASS",
					"Gate test.complete: PASS (executed)",
					"Gate coverage: PASS (executed)",
					"Gate lint: NOT_APPLICABLE (not executed)",
					"Delivery artifact: not built or verified",
					"Gate validation does not mean a delivery artifact was built or verified.",
				} {
					if !strings.Contains(out.String(), fragment) {
						t.Errorf("text gates output missing %q: %s", fragment, out.String())
					}
				}
				return
			}

			var report struct {
				Status                   string   `json:"status"`
				ValidationOnly           bool     `json:"validation_only"`
				ExecutedGates            []string `json:"executed_gates"`
				DeliveryArtifactBuilt    bool     `json:"delivery_artifact_built"`
				DeliveryArtifactVerified bool     `json:"delivery_artifact_verified"`
				DeliveryArtifactNotice   string   `json:"delivery_artifact_notice"`
				GateResults              []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Executed bool   `json:"executed"`
				} `json:"gate_results"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("invalid gates JSON: %v\n%s", err, out.String())
			}
			if report.Status != string(spec.StatusPass) || !report.ValidationOnly || !slices.Equal(report.ExecutedGates, []string{"test.complete", "coverage"}) || report.DeliveryArtifactBuilt || report.DeliveryArtifactVerified || report.DeliveryArtifactNotice != "Gate validation does not mean a delivery artifact was built or verified." {
				t.Fatalf("gates report=%+v", report)
			}
			statuses := make(map[string]struct {
				status   string
				executed bool
			}, len(report.GateResults))
			for _, result := range report.GateResults {
				statuses[result.ID] = struct {
					status   string
					executed bool
				}{status: result.Status, executed: result.Executed}
			}
			if len(statuses) != len(spec.ProjectGateOrder) || statuses["test.complete"].status != string(spec.StatusPass) || !statuses["test.complete"].executed || statuses["coverage"].status != string(spec.StatusPass) || !statuses["coverage"].executed || statuses["lint"].status != string(spec.StatusNotApplicable) || statuses["lint"].executed {
				t.Fatalf("gate results=%+v", report.GateResults)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(repo, ".polis", "artifacts", "packages")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-only command created a delivery package directory: stat err=%v", err)
	}
}

func TestIssue14GateHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "missing-dependency":
		_, _ = os.Stderr.WriteString("ModuleNotFoundError: No module named 'pytest'\n")
		os.Exit(1)
	case "assertion":
		_, _ = os.Stderr.WriteString("assertion failed: expected true\n")
		os.Exit(1)
	default:
		os.Exit(9)
	}
}

func TestRunGatesExplainsUnrunChecksAndPreservesAssertions(t *testing.T) {
	for _, tc := range []struct {
		name, missing string
		argv          []string
		wantStatus    spec.Status
		wantExecuted  bool
	}{
		{"missing executable", "missing executable", []string{"polis-issue14-no-such-command"}, spec.StatusBlocked, false},
		{"missing dependency", "missing dependency", []string{os.Args[0], "-test.run=^TestIssue14GateHelper$", "--", "missing-dependency"}, spec.StatusBlocked, false},
		{"real assertion", "", []string{os.Args[0], "-test.run=^TestIssue14GateHelper$", "--", "assertion"}, spec.StatusFail, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := makeBuildRepo(t)
			var policy spec.Policy
			if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
				t.Fatal(err)
			}
			for i := range policy.Gates {
				if policy.Gates[i].ID == "test.complete" {
					policy.Gates[i].Command.Argv = tc.argv
				}
			}
			raw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(t.TempDir(), "issue14-policy.json")
			if err := os.WriteFile(policyPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"json", "text"} {
				var out, errOut bytes.Buffer
				code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--format", format}, &out, &errOut)
				if code == exitPass {
					t.Fatalf("gate unexpectedly passed: %s", out.String())
				}
				if format == "text" {
					if tc.missing != "" && (!strings.Contains(out.String(), tc.missing) || !strings.Contains(out.String(), "intended checks did not run") || !strings.Contains(out.String(), "not executed")) {
						t.Fatalf("text did not explain prerequisite: %s", out.String())
					}
					continue
				}
				var report gatesCLIResult
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatalf("invalid JSON: %v\n%s", err, out.String())
				}
				if report.Status != tc.wantStatus {
					t.Fatalf("status=%s want=%s report=%+v", report.Status, tc.wantStatus, report)
				}
				for _, gate := range report.GateResults {
					if gate.ID != "test.complete" {
						continue
					}
					if gate.Status != tc.wantStatus || gate.Executed != tc.wantExecuted || !strings.Contains(gate.Reason, tc.missing) {
						t.Fatalf("gate=%+v", gate)
					}
					if tc.missing != "" && !strings.Contains(gate.Reason, "intended checks did not run") {
						t.Fatalf("blocked gate omitted unrun checks: %+v", gate)
					}
				}
			}
		})
	}
}

func TestRunGatesReturnsFailureWithPerGateResults(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		argv         []string
		overall      spec.Status
		gateStatus   spec.Status
		gateExecuted bool
		exitCode     int
		executed     []string
	}{
		{name: "failed process", argv: []string{"git", "polis-test-command-that-does-not-exist"}, overall: spec.StatusFail, gateStatus: spec.StatusFail, gateExecuted: true, exitCode: exitValidationFailed, executed: []string{"test.complete"}},
		{name: "blocked process start", argv: []string{"polis-issue11-no-such-executable"}, overall: spec.StatusBlocked, gateStatus: spec.StatusBlocked, gateExecuted: false, exitCode: exitBlocked, executed: []string{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := makeBuildRepo(t)
			policy := spec.Policy{}
			if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
				t.Fatal(err)
			}
			for i := range policy.Gates {
				if policy.Gates[i].ID == "test.complete" {
					policy.Gates[i].Command.Argv = scenario.argv
				}
			}
			policyRaw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(t.TempDir(), "failing-policy.json")
			if err := os.WriteFile(policyPath, append(policyRaw, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--format", "json"}, &out, &errOut)
			if code != scenario.exitCode {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			var report struct {
				Status                   string   `json:"status"`
				ExecutedGates            []string `json:"executed_gates"`
				DeliveryArtifactBuilt    bool     `json:"delivery_artifact_built"`
				DeliveryArtifactVerified bool     `json:"delivery_artifact_verified"`
				DeliveryArtifactNotice   string   `json:"delivery_artifact_notice"`
				GateResults              []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Executed bool   `json:"executed"`
				} `json:"gate_results"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("invalid gates failure JSON: %v\n%s", err, out.String())
			}
			statuses := make(map[string]struct {
				status   string
				executed bool
			}, len(report.GateResults))
			for _, result := range report.GateResults {
				statuses[result.ID] = struct {
					status   string
					executed bool
				}{status: result.Status, executed: result.Executed}
			}
			if report.Status != string(scenario.overall) || !slices.Equal(report.ExecutedGates, scenario.executed) || report.DeliveryArtifactBuilt || report.DeliveryArtifactVerified || report.DeliveryArtifactNotice != "Gate validation does not mean a delivery artifact was built or verified." || statuses["test.complete"].status != string(scenario.gateStatus) || statuses["test.complete"].executed != scenario.gateExecuted || statuses["coverage"].status != string(spec.StatusBlocked) || statuses["coverage"].executed {
				t.Fatalf("failure report=%+v statuses=%v", report, statuses)
			}
		})
	}
}

func TestRunGatesDoesNotMarkCoverageCommandRunWhenReportCleanupFails(t *testing.T) {
	repo := makeBuildRepo(t)
	reportPath := filepath.Join(repo, ".polis", "coverage.out")
	if err := os.Remove(reportPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(reportPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportPath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"gates", "--repo", repo, "--format", "json"}, &out, &errOut)
	if code != exitValidationFailed {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	// Provenance now rejects a tracked source file replaced by a directory
	// before any command runs, rather than reaching coverage cleanup.
	if out.Len() != 0 || !strings.Contains(errOut.String(), "not a regular file") {
		t.Fatalf("invalid source did not fail closed: stdout=%s stderr=%s", out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(reportPath, "keep.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestRunGatesRejectsInvalidArgumentsAndReportsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "gates"}, &out, &errOut); code != exitPass {
		t.Fatalf("help code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"polis gates [--repo <path>]", "run configured project gates", "without building a delivery artifact"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("gates help missing %q: %s", fragment, out.String())
		}
	}
	for _, args := range [][]string{{"gates", "--format", "xml"}, {"gates", "--repo", ".", "unexpected"}} {
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "usage: polis gates") {
			t.Errorf("args=%v code=%d stderr=%s", args, code, errOut.String())
		}
	}

	repo := makeBuildRepo(t)
	missingPolicy := filepath.Join(t.TempDir(), "missing-policy.json")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"gates", "--repo", repo, "--policy", missingPolicy}, &out, &errOut); code != exitValidationFailed || !strings.Contains(errOut.String(), "POLIS GATES: FAIL") || !strings.Contains(errOut.String(), "read external Project Policy") {
		t.Fatalf("missing policy code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
