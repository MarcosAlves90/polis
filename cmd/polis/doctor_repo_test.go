package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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

func TestDoctorRepositoryChecksEnvDelegatedExecutables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	for _, argv := range [][]string{
		{"env", "polis-doctor-missing-delegate-73931"},
		{"env", "-u", "UNRELATED", "polis-doctor-missing-delegate-73931"},
		{"env", "FOO=bar", "polis-doctor-missing-delegate-73931"},
		{"env", "--", "polis-doctor-missing-delegate-73931"},
		{"env", "env", "FOO=bar", "polis-doctor-missing-delegate-73931"},
	} {
		t.Run(strings.Join(argv[1:], "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[0].Command.Argv = argv })
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "executable", "test.complete")
			// A wrapper PASS must never hide a missing delegated executable.
			if code != exitBlocked || payload["status"] != "BLOCKED" || check["status"] != "BLOCKED" ||
				!strings.Contains(check["detail"].(string), "polis-doctor-missing-delegate-73931") || report["gates_executed"] != false {
				t.Fatalf("env delegated executable was not blocked: code=%d report=%v", code, report)
			}
		})
	}
}

func TestDoctorRepositoryTreatsPostAssignmentOptionsAsExecutables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	for _, argv := range [][]string{
		{"env", "FOO=x", "-u", "FOO", "/bin/true"},
		{"env", "FOO=x", "--", "/bin/true"},
	} {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[0].Command.Argv = argv })
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "executable", "test.complete")
			if code != exitBlocked || check["status"] != "BLOCKED" ||
				!strings.Contains(check["detail"].(string), "executable \""+argv[2]+"\" is unavailable") ||
				report["gates_executed"] != false {
				t.Fatalf("command after assignment not diagnosed: code=%d report=%v", code, report)
			}
		})
	}
}

func TestDoctorRepositoryAllowsPOSIXLowercasePathVariables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX case-sensitive environment")
	}
	for _, program := range []string{"env", "sh"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not on PATH", program)
		}
	}
	repo := makeBuildRepo(t)
	for _, argv := range [][]string{
		{"env", "path=/nonexistent", "sh"},
		{"env", "-u", "path", "sh"},
		{"env", "--unset=path", "sh"},
		{"env", "-upath", "sh"},
	} {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[0].Command.Argv = argv })
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "executable", "test.complete")
			if code != exitPass || check["status"] != "PASS" || report["gates_executed"] != false {
				t.Fatalf("unrelated POSIX variable blocked PATH: code=%d report=%v", code, report)
			}
		})
	}
}

func TestDoctorRepositoryDoesNotClaimEnvLookupWithChangedEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	for _, argv := range [][]string{
		{"env", "PATH=/nonexistent", "go"},
		{"env", "-u", "PATH", "go"},
		{"env", "-i", "go"},
		{"env", "-", "go"},
		{"env", "--chdir=other-directory", "./runner"},
		{"env", "--chdir=other-directory", "/bin/sh"},
		{"env", "-P", "/nonexistent", "go"},
		{"env", "--path=/nonexistent", "go"},
		{"env", "env", "-iuPATH", "go"},
	} {
		t.Run(strings.Join(argv[1:], "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) { p.Gates[0].Command.Argv = argv })
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "executable", "test.complete")
			if code != exitBlocked || check["status"] != "BLOCKED" || !strings.Contains(check["detail"].(string), "cannot reliably inspect") {
				t.Fatalf("modified lookup was claimed as available: code=%d report=%v", code, report)
			}
		})
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
		p.Gates[0].Command.Argv = []string{"env", "go"}
		p.Gates[0].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}
	})
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitBlocked || !strings.Contains(findDoctorCheck(t, payload["repository"].(map[string]any), "executable", "test.complete")["detail"].(string), "cannot reliably inspect") {
		t.Fatalf("clean PATH missing from env wrapper was accepted: code=%d payload=%v", code, payload)
	}
}

func TestDoctorRepositoryRejectsAbbreviatedEnvChdirWithoutExecutingGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	workdir := filepath.Join(repo, "tools")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(workdir, "gate-executed")
	if err := os.WriteFile(filepath.Join(workdir, "runner"), []byte("#!/bin/sh\ntouch gate-executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
		p.Gates[0].Command.Cwd = "tools"
		p.Gates[0].Command.Argv = []string{"env", "--ch=/tmp", "./runner"}
	})
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	report := payload["repository"].(map[string]any)
	check := findDoctorCheck(t, report, "policy_plan", "")
	if code != exitValidationFailed || payload["status"] != "FAIL" || check["status"] != "FAIL" ||
		!strings.Contains(check["detail"].(string), "unsupported env long option") || report["gates_executed"] != false {
		t.Fatalf("abbreviated env chdir was incorrectly accepted: code=%d payload=%v", code, payload)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor executed an env-wrapped gate: %v", err)
	}
}

func TestDoctorRepositoryRejectsUnsupportedEnvShortOptionWithoutExecutingGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	workdir := filepath.Join(repo, "tools")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(workdir, "gate-executed")
	if err := os.WriteFile(filepath.Join(workdir, "runner"), []byte("#!/bin/sh\ntouch gate-executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"-0", "-i0", "-Z"} {
		t.Run(option, func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
				p.Gates[0].Command.Cwd = "tools"
				p.Gates[0].Command.Argv = []string{"env", option, "./runner"}
			})
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "policy_plan", "")
			if code != exitValidationFailed || payload["status"] != "FAIL" || check["status"] != "FAIL" ||
				!strings.Contains(check["detail"].(string), "unsupported env short option") || report["gates_executed"] != false {
				t.Fatalf("unsupported env option %s accepted: code=%d payload=%v", option, code, payload)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("doctor executed a gate: %v", err)
			}
		})
	}
}

func TestDoctorRepositoryRejectsEnvMissingOperandsWithoutExecutingGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	marker := filepath.Join(repo, "gate-executed")
	for _, argv := range [][]string{
		{"env", "--unset"},
		{"env", "--chdir"},
		{"env", "--path"},
		{"env", "--argv0"},
		{"env", "-u"},
		{"env", "-iC"},
		{"env", "-a"},
		{"env", "--chdir="},
	} {
		t.Run(strings.Join(argv, "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
				p.Gates[0].Command.Argv = argv
			})
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "policy_plan", "")
			if code != exitValidationFailed || check["status"] != "FAIL" || !strings.Contains(check["detail"].(string), "operand") || report["gates_executed"] != false {
				t.Fatalf("invalid env invocation %q accepted: code=%d payload=%v", argv, code, payload)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("doctor executed a gate: %v", err)
			}
		})
	}
}

func TestDoctorRepositoryRejectsInvalidEnvUnsetOperands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX env wrapper")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	marker := filepath.Join(repo, "gate-executed")
	runner := filepath.Join(repo, "runner")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntouch gate-executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"env", "--unset", "FOO=bar", "./runner"},
		{"env", "--unset=FOO=bar", "./runner"},
		{"env", "-u", "FOO=bar", "./runner"},
		{"env", "-uFOO=bar", "./runner"},
	} {
		t.Run(strings.Join(argv[1:], "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
				p.Gates[0].Command.Argv = argv
			})
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			report := payload["repository"].(map[string]any)
			check := findDoctorCheck(t, report, "policy_plan", "")
			if code != exitValidationFailed || check["status"] != "FAIL" || !strings.Contains(check["detail"].(string), "unset operand must be a variable name") || report["gates_executed"] != false {
				t.Fatalf("invalid env unset operand accepted for %q: code=%d report=%v", argv, code, report)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("doctor executed a gate: %v", err)
			}
		})
	}
}

func TestDoctorRepositoryChecksEnvDelegatedRelativeExecutableWithoutExecuting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	if _, err := exec.LookPath("env"); err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	workdir := filepath.Join(repo, "tools")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(workdir, "doctor-runner")
	marker := filepath.Join(workdir, "gate-executed")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntouch gate-executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"env", "VAR=value", "./doctor-runner"},
		{"env", "PATH=/nonexistent", "./doctor-runner"},
		{"env", "-i", runner},
		{"env", "-", runner},
	} {
		t.Run(strings.Join(argv[1:], "_"), func(t *testing.T) {
			policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
				p.Gates[0].Command.Cwd = "tools"
				p.Gates[0].Command.Argv = argv
			})
			code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
			if code != exitPass || payload["status"] != "PASS" || payload["repository"].(map[string]any)["gates_executed"] != false {
				t.Fatalf("existing executable should pass: code=%d payload=%v", code, payload)
			}
		})
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor executed env-wrapped gate: %v", err)
	}
}

func TestDoctorRepositoryResolvesEnvDelegateFromRelativePATHAtGateWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX relative PATH lookup")
	}
	env, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env is not on PATH")
	}
	repo := makeBuildRepo(t)
	workdir := filepath.Join(repo, "tools")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	program := "doctor-cwd-env-runner"
	runner := filepath.Join(workdir, program)
	marker := filepath.Join(workdir, "gate-executed")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\ntouch gate-executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := doctorExternalPolicy(t, repo, func(p *spec.Policy) {
		p.Gates[0].Command.Cwd = "tools"
		p.Gates[0].Command.Argv = []string{env, program}
	})
	// The env wrapper starts in the gate's CWD and searches relative PATH there.
	// Include the original PATH so other policy commands stay discoverable.
	t.Setenv("PATH", "."+string(os.PathListSeparator)+os.Getenv("PATH"))
	code, payload := doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitPass || payload["status"] != "PASS" {
		t.Fatalf("env delegate should resolve from gate cwd: code=%d payload=%v", code, payload)
	}
	if got := findDoctorCheck(t, payload["repository"].(map[string]any), "executable", "test.complete")["detail"].(string); !strings.Contains(got, runner) {
		t.Fatalf("expected resolved gate executable path in %q", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor executed the delegated gate: %v", err)
	}
	if err := os.Remove(runner); err != nil {
		t.Fatal(err)
	}
	code, payload = doctorJSON(t, "--repo", repo, "--policy", policy)
	if code != exitBlocked || findDoctorCheck(t, payload["repository"].(map[string]any), "executable", "test.complete")["status"] != "BLOCKED" {
		t.Fatalf("missing env delegate should block: code=%d payload=%v", code, payload)
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
