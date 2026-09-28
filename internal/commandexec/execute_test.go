package commandexec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestPrerequisiteHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "nested-command":
		_, _ = os.Stderr.WriteString("sh: missing-test-runner: command not found\n")
		os.Exit(127)
	case "missing-module":
		_, _ = os.Stderr.WriteString("ModuleNotFoundError: No module named 'pytest'\n")
		os.Exit(1)
	case "missing-env":
		_, _ = os.Stderr.WriteString("TEST_DATABASE_URL: parameter not set\n")
		os.Exit(1)
	case "assertion-127":
		_, _ = os.Stderr.WriteString("assertion failed: expected 127\n")
		os.Exit(127)
	case "assertion-quotes-prerequisite":
		_, _ = os.Stdout.WriteString("test started\n")
		_, _ = os.Stderr.WriteString("sh: missing-test-runner: command not found\n")
		os.Exit(127)
	case "assertion-mentions-module":
		_, _ = os.Stderr.WriteString("AssertionError: No module named 'pytest'\n")
		os.Exit(1)
	default:
		os.Exit(9)
	}
}

func prerequisiteCommand(mode string) spec.CommandSpec {
	return spec.CommandSpec{Argv: []string{os.Args[0], "-test.run=^TestPrerequisiteHelper$", "--", mode}, Cwd: ".", TimeoutSeconds: 5}
}

func TestRunClassifiesOnlyClearPrerequisiteFailures(t *testing.T) {
	for _, tc := range []struct {
		mode, want string
		status     spec.Status
	}{
		{"nested-command", "missing executable missing-test-runner", spec.StatusBlocked},
		{"missing-module", "missing dependency pytest", spec.StatusBlocked},
		{"missing-env", "missing environment condition TEST_DATABASE_URL", spec.StatusBlocked},
		{"assertion-127", "", spec.StatusFail},
		{"assertion-quotes-prerequisite", "", spec.StatusFail},
		{"assertion-mentions-module", "", spec.StatusFail},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			got := Run(t.TempDir(), prerequisiteCommand(tc.mode))
			if got.Status != tc.status || got.Prerequisite != tc.want {
				t.Fatalf("observation=%+v", got)
			}
			if got.Status == spec.StatusBlocked && (BlockedReason(got) == nil || !strings.Contains(*BlockedReason(got), "intended checks did not run")) {
				t.Fatalf("blocked reason=%v", BlockedReason(got))
			}
		})
	}
}

func TestRunPassAndFail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses sh")
	}
	root := t.TempDir()
	pass := Run(root, spec.CommandSpec{Argv: []string{"sh", "-c", "printf ok"}, Cwd: ".", TimeoutSeconds: 5})
	if pass.Status != spec.StatusPass || pass.ExitCode != 0 || pass.Stdout != "ok" {
		t.Fatalf("pass=%+v", pass)
	}
	fail := Run(root, spec.CommandSpec{Argv: []string{"sh", "-c", "printf bad >&2; exit 7"}, Cwd: ".", TimeoutSeconds: 5})
	if fail.Status != spec.StatusFail || fail.ExitCode != 7 || fail.Stderr != "bad" {
		t.Fatalf("fail=%+v", fail)
	}
}

func TestRunMissingExecutableIsBlocked(t *testing.T) {
	got := Run(t.TempDir(), spec.CommandSpec{Argv: []string{"definitely-not-a-polis-command"}, Cwd: ".", TimeoutSeconds: 1})
	if got.Status != spec.StatusBlocked || got.ExitCode != -1 {
		t.Fatalf("got=%+v", got)
	}
	if got.Prerequisite != "missing executable definitely-not-a-polis-command" {
		t.Fatalf("missing executable was not identified: %+v", got)
	}
}

func TestRunTimeoutFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses sh")
	}
	got := Run(t.TempDir(), spec.CommandSpec{Argv: []string{"sh", "-c", "sleep 2"}, Cwd: ".", TimeoutSeconds: 1})
	if got.Status != spec.StatusFail || got.ExitCode != -1 {
		t.Fatalf("got=%+v", got)
	}
}

func TestRunDoesNotInterpretShellTokens(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "pwned")
	got := Run(root, spec.CommandSpec{Argv: []string{"printf", "%s", "&& touch " + marker}, Cwd: ".", TimeoutSeconds: 5})
	if got.Status != spec.StatusPass || !strings.Contains(got.Stdout, "&& touch") {
		t.Fatalf("got=%+v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("shell token executed, stat=%v", err)
	}
}
