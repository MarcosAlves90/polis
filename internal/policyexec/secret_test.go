package policyexec

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestSecretDiagnosticHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "secret-diagnostic" {
		return
	}
	fmt.Fprintf(os.Stderr, "ModuleNotFoundError: No module named '%s'\n", os.Getenv("POLIS_TEST_SECRET"))
	os.Exit(1)
}

func TestEvidenceDoesNotPersistOutputDerivedSecretNames(t *testing.T) {
	t.Setenv("POLIS_TEST_SECRET", "sentinel-gate-evidence-secret")
	// Instrumented helper binaries otherwise append a coverage warning to
	// their startup diagnostic. Keep coverage data outside the fixture source.
	t.Setenv("GOCOVERDIR", t.TempDir())
	p := testPolicy(t, "pass")
	p.Gates[0].Command.Argv = []string{os.Args[0], "-test.run=^TestSecretDiagnosticHelper$", "--", "secret-diagnostic"}
	p.Gates[0].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"POLIS_TEST_SECRET", "GOCOVERDIR"}}
	var evidence bytes.Buffer
	r := Execute(p, t.TempDir(), &evidence)
	if r.Overall != spec.StatusBlocked || !strings.Contains(evidence.String(), "missing dependency; intended checks did not run") || strings.Contains(evidence.String(), os.Getenv("POLIS_TEST_SECRET")) {
		t.Fatal("gate evidence must persist a safe blocked category without output-derived environment values")
	}
}
