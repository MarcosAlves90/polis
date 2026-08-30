package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV5ArtifactCommandsFailClosedOnInvalidArtifacts(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "invalid.polis")
	if err := os.WriteFile(invalid, []byte("not a POLIS archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		code int
	}{
		{"verify text", []string{"verify", invalid}, exitInvalidArtifact},
		{"verify json", []string{"verify", "--format", "json", invalid}, exitInvalidArtifact},
		{"inspect text", []string{"inspect", invalid}, exitInvalidArtifact},
		{"inspect json", []string{"inspect", "--format", "json", invalid}, exitInvalidArtifact},
		{"preflight", []string{"preflight", "--repo", t.TempDir(), invalid}, exitInvalidArtifact},
		{"apply", []string{"apply", "--repo", t.TempDir(), invalid}, exitInvalidArtifact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if got := run(tc.args, &out, &errOut); got != tc.code {
				t.Fatalf("code=%d want=%d stdout=%s stderr=%s", got, tc.code, out.String(), errOut.String())
			}
			if strings.Contains(tc.name, "json") {
				var payload map[string]any
				if err := json.Unmarshal(errOut.Bytes(), &payload); err != nil || payload["status"] != "FAIL" {
					t.Fatalf("payload=%v err=%v raw=%s", payload, err, errOut.String())
				}
			} else if !strings.Contains(errOut.String(), "FAIL") {
				t.Fatalf("stderr=%q", errOut.String())
			}
		})
	}
}

func TestV5CommandsRejectMalformedFlags(t *testing.T) {
	commands := []string{"verify", "inspect", "preflight", "init", "capture-red", "build", "apply", "sign", "doctor"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if got := run([]string{command, "--definitely-not-a-real-flag"}, &out, &errOut); got != exitUsage {
				t.Fatalf("code=%d stdout=%s stderr=%s", got, out.String(), errOut.String())
			}
		})
	}
}

func TestSignFailureUsesValidationExitCode(t *testing.T) {
	artifact := filepath.Join(t.TempDir(), "artifact.polis")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"sign", "--key", filepath.Join(t.TempDir(), "missing.pem"), "--out", filepath.Join(t.TempDir(), "x.sig"), artifact}, &out, &errOut)
	if code != exitValidationFailed || !strings.Contains(errOut.String(), "POLIS SIGN: FAIL") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}
