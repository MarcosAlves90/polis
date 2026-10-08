package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func doctorJSON(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"doctor", "--format", "json"}, args...), &out, &errOut)
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("doctor JSON: %v, stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
	return code, payload
}

func doctorExternalPolicy(t *testing.T, repo string, modify func(*spec.Policy)) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	modify(&policy)
	raw, err = json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func findDoctorCheck(t *testing.T, report map[string]any, code, gate string) map[string]any {
	t.Helper()
	for _, value := range report["checks"].([]any) {
		check := value.(map[string]any)
		if check["code"] == code && (gate == "" && check["gate"] == nil || check["gate"] == gate) {
			return check
		}
	}
	t.Fatalf("doctor check %q for %q missing: %v", code, gate, report["checks"])
	return nil
}

func TestDoctorRepositoryStaticPassAndLegacyBehavior(t *testing.T) {
	repo := makeBuildRepo(t)
	code, payload := doctorJSON(t)
	if code != exitPass || payload["status"] != "PASS" || payload["repository"] != nil {
		t.Fatalf("standalone doctor changed: code=%d payload=%v", code, payload)
	}
	code, payload = doctorJSON(t, "--repo", repo)
	if code != exitPass || payload["status"] != "PASS" {
		t.Fatalf("repository doctor: code=%d payload=%v", code, payload)
	}
	report := payload["repository"].(map[string]any)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report["gates_executed"] != false || report["policy_source"] != "committed" || report["root"] != resolvedRepo {
		t.Fatalf("unexpected repository report: %v", report)
	}
	if findDoctorCheck(t, report, "policy_plan", "")["status"] != "PASS" || findDoctorCheck(t, report, "executable", "test.complete")["status"] != "PASS" {
		t.Fatalf("missing policy or executable check: %v", report)
	}
	if findDoctorCheck(t, report, "report_path", "coverage")["status"] != "PASS" {
		t.Fatalf("missing report containment check: %v", report)
	}
	if len(report["execution_order"].([]any)) == 0 {
		t.Fatalf("expected execution order: %v", report)
	}
}

func TestDoctorRepositoryDoesNotRequireGeneratedReport(t *testing.T) {
	repo := makeBuildRepo(t)
	if err := os.Remove(filepath.Join(repo, ".polis", "coverage.out")); err != nil {
		t.Fatal(err)
	}
	code, payload := doctorJSON(t, "--repo", repo)
	if code != exitPass || payload["status"] != "PASS" {
		t.Fatalf("missing generated report should not block static checks: code=%d payload=%v", code, payload)
	}
	report := payload["repository"].(map[string]any)
	if report["gates_executed"] != false || findDoctorCheck(t, report, "report_path", "coverage")["status"] != "PASS" {
		t.Fatalf("report preflight misclassified generated output: %v", report)
	}
}

func TestDoctorRepositoryTextReport(t *testing.T) {
	repo := makeBuildRepo(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--repo", repo}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"Repository diagnostics: PASS", "policy_plan: PASS", "executable [test.complete]: PASS", "Project gates executed: no"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("missing report fragment %q: %s", fragment, out.String())
		}
	}
}

func TestDoctorRepositoryReportsMissingExecutableAndDirectory(t *testing.T) {
	repo := makeBuildRepo(t)
	cases := []struct {
		name   string
		modify func(*spec.Policy)
		check  string
	}{
		{"missing_executable", func(p *spec.Policy) { p.Gates[0].Command.Argv = []string{"polis_nonexistent_doctor_executable_7359"} }, "executable"},
		{"missing_cwd", func(p *spec.Policy) { p.Gates[0].Command.Cwd = "missing-subdirectory" }, "working_directory"},
		{"cwd_is_file", func(p *spec.Policy) { p.Gates[0].Command.Cwd = "app.txt" }, "working_directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, tc.modify)
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			if code != exitBlocked || payload["status"] != "BLOCKED" {
				t.Fatalf("code=%d payload=%v", code, payload)
			}
			report := payload["repository"].(map[string]any)
			if report["gates_executed"] != false || report["policy_source"] != "external" || findDoctorCheck(t, report, tc.check, "test.complete")["status"] != "BLOCKED" {
				t.Fatalf("missing diagnostics: %v", report)
			}
		})
	}
}

func TestDoctorRepositoryRejectsSymlinkWorkdirEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	repo := makeBuildRepo(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(repo, "external-dir")); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[0].Command.Cwd = "external-dir" })
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	report := payload["repository"].(map[string]any)
	if code != exitBlocked || !strings.Contains(findDoctorCheck(t, report, "working_directory", "test.complete")["detail"].(string), "outside") {
		t.Fatalf("symlink escape was accepted: code=%d report=%v", code, report)
	}
}

func TestDoctorRepositoryRejectsSymlinkReportEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	repo := makeBuildRepo(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(repo, "external-dir")); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[1].Report = "external-dir/coverage.out" })
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	report := payload["repository"].(map[string]any)
	if code != exitBlocked || !strings.Contains(findDoctorCheck(t, report, "report_path", "coverage")["detail"].(string), "outside") {
		t.Fatalf("symlink report path escape was accepted: code=%d report=%v", code, report)
	}
}

func TestDoctorRepositoryRejectsInvalidPolicyAndPlan(t *testing.T) {
	repo := makeBuildRepo(t)
	for _, tc := range []struct {
		name string
		edit func(*spec.Policy)
	}{
		{"nonportable_command", func(p *spec.Policy) { p.Gates[0].Command.Argv = []string{"sh", "-c", "echo should-not-run"} }},
		{"invalid_dependency", func(p *spec.Policy) { p.Gates[4].DependsOn = []string{"missing-gate"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, tc.edit)
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			if code != exitValidationFailed || payload["status"] != "FAIL" || findDoctorCheck(t, report, "policy_plan", "")["status"] != "FAIL" {
				t.Fatalf("invalid policy/plan accepted: code=%d report=%v", code, report)
			}
			if report["gates_executed"] != false {
				t.Fatalf("unexpected gate execution: %v", report)
			}
		})
	}
}

func TestDoctorRepositoryDoesNotLaunchConfiguredCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shebang fixture")
	}
	repo := makeBuildRepo(t)
	marker := filepath.Join(repo, "gate-executed.txt")
	runner := filepath.Join(repo, "doctor-runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntouch gate-executed.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
		p.Gates[0].Command.Argv = []string{"./doctor-runner"}
	})
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitPass || payload["status"] != "PASS" {
		t.Fatalf("static doctor should pass without running gate: code=%d payload=%v", code, payload)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor launched the gate: %v", err)
	}
}

func TestDoctorRepositoryResolvesRelativeExecutableFromCommandWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	repo := makeBuildRepo(t)
	workdir := filepath.Join(repo, "tools")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(workdir, "doctor-runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
		p.Gates[0].Command.Cwd = "tools"
		p.Gates[0].Command.Argv = []string{"./doctor-runner"}
	})
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitPass || payload["status"] != "PASS" {
		t.Fatalf("executable should resolve relative to configured cwd: code=%d payload=%v", code, payload)
	}
	if err := os.Remove(runner); err != nil {
		t.Fatal(err)
	}
	code, payload = doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitBlocked || findDoctorCheck(t, payload["repository"].(map[string]any), "executable", "test.complete")["status"] != "BLOCKED" {
		t.Fatalf("missing relative executable should block: code=%d payload=%v", code, payload)
	}
}

func TestDoctorRepositoryRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--repo", ""}, {"doctor", "--policy", "outside.json"}, {"doctor", "--policy", ""}, {"doctor", "--repo", ".", "--policy", ""}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage {
			t.Fatalf("args=%v code=%d out=%q err=%q", args, code, out.String(), errOut.String())
		}
	}
}
