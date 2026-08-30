package commandexec

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v5/spec"
)

func TestCleanEnvironmentPassesOnlyAllowlistedValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	t.Setenv("POLIS_SECRET_TEST", "must-not-leak")
	t.Setenv("POLIS_ALLOWED_TEST", "visible")
	cmd := spec.CommandSpec{Argv: []string{"sh", "-c", `printf '%s|%s' "$POLIS_ALLOWED_TEST" "$POLIS_SECRET_TEST"`}, Cwd: ".", TimeoutSeconds: 5, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"PATH", "POLIS_ALLOWED_TEST"}}}
	obs := Run(t.TempDir(), cmd)
	if obs.Status != spec.StatusPass {
		t.Fatalf("run failed: %+v", obs)
	}
	if strings.TrimSpace(obs.Stdout) != "visible|" {
		t.Fatalf("unexpected child environment: %q", obs.Stdout)
	}
	if os.Getenv("POLIS_SECRET_TEST") != "must-not-leak" {
		t.Fatal("parent environment changed")
	}
}
