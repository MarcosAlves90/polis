package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestGateSecretDiagnosticHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "secret-diagnostic" {
		return
	}
	fmt.Fprintf(os.Stderr, "sh: line 1: %s: command not found\n", os.Getenv("POLIS_TEST_SECRET"))
	os.Exit(127)
}

func TestGatesReportsDoNotLeakOutputDerivedSecrets(t *testing.T) {
	repo := makeBuildRepo(t)
	t.Setenv("POLIS_TEST_SECRET", "sentinel-cli-environment-secret")
	t.Setenv("GOCOVERDIR", t.TempDir())
	var policy spec.Policy
	if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	policy.Gates[0].Command.Argv = []string{os.Args[0], "-test.run=^TestGateSecretDiagnosticHelper$", "--", "secret-diagnostic"}
	policy.Gates[0].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"POLIS_TEST_SECRET", "GOCOVERDIR"}}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "text"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"gates", "--repo", repo, "--policy", path, "--gate", "test.complete", "--format", format}, &out, &errOut); code != exitBlocked {
			t.Fatalf("expected BLOCKED: code=%d", code)
		}
		if strings.Contains(out.String()+errOut.String(), os.Getenv("POLIS_TEST_SECRET")) || !strings.Contains(out.String(), "missing executable; intended checks did not run") {
			t.Fatalf("%s report must contain only the safe diagnostic category", format)
		}
	}
}
