package commandexec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v4/spec"
)

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
