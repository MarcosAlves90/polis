package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/gaterun"
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

func TestInspectRunEscapesTerminalControlCharacters(t *testing.T) {
	var policy spec.Policy
	if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	manifest := gaterun.Manifest{
		SchemaVersion: 1,
		Nonce:         "0123456789abcdef0123456789abcdef",
		Inputs: gaterun.Inputs{
			SourceSHA256: "source\x1b[31m",
			PolicySHA256: "policy\x1b[2J",
		},
		SelectedGates: []string{"test.complete\x1b[2J"},
		Jobs:          1,
		Current:       true,
		Status:        spec.StatusPass,
	}
	for _, definition := range policy.Gates {
		manifest.Gates = append(manifest.Gates, gaterun.Gate{
			ID:              definition.ID,
			Definition:      definition,
			Status:          spec.StatusPass,
			Action:          "executed\x1b[2J",
			Reason:          "reason\x1b[2J",
			StaleCategories: []string{"source\x1b[2J"},
		})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	manifest.RunID = hex.EncodeToString(sum[:])
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"gates", "--inspect-run", path, "--format", "text"}, &out, &errOut); code != exitPass {
		t.Fatalf("inspect code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	got := out.String() + errOut.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("inspection emitted a raw terminal control character: %q", got)
	}
	for _, want := range []string{
		`Source: "source\x1b[31m"`,
		`Policy: "policy\x1b[2J"`,
		`Selected gates: "test.complete\x1b[2J"`,
		`executed\x1b[2J`,
		`reason="reason\x1b[2J"`,
		`stale inputs="source\x1b[2J"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("inspection output missing escaped field %q: %s", want, got)
		}
	}
}
