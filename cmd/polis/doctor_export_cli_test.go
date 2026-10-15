package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestRunDoctor(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"doctor"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS doctor 6.11.1") {
		t.Fatalf("doctor version mismatch: stdout=%q", out.String())
	}
}

func TestRunExportCreatesSelfContainedOfflineBundle(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "polis-offline.zip")
	var out, errOut bytes.Buffer
	code := run([]string{"export", "--out", bundlePath, "--format", "json"}, &out, &errOut)
	if code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var result struct {
		Status          string `json:"status"`
		Bundle          string `json:"bundle"`
		Version         string `json:"polis_version"`
		Runtime         string `json:"runtime"`
		NetworkRequired bool   `json:"network_required"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid export JSON: %v\n%s", err, out.String())
	}
	if result.Status != "PASS" || result.Bundle != bundlePath || result.Version != version || result.Runtime != runtime.GOOS+"/"+runtime.GOARCH || result.NetworkRequired {
		t.Fatalf("export result=%+v", result)
	}

	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	executableMember := "polis-offline/bin/polis"
	if runtime.GOOS == "windows" {
		executableMember += ".exe"
	}
	want := map[string]bool{
		executableMember:                                               false,
		"polis-offline/manifest.json":                                  false,
		"polis-offline/POLIS-OFFLINE.md":                               false,
		"polis-offline/spec/POLIS-SPEC-v6.md":                          false,
		"polis-offline/spec/schemas/policy.schema.json":                false,
		"polis-offline/spec/schemas/artifact-retention-v1.schema.json": false,
	}
	for _, member := range archive.File {
		if _, ok := want[member.Name]; ok {
			want[member.Name] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("offline bundle missing %s", name)
		}
	}
}

func TestRunExportSupportsTargetRuntime(t *testing.T) {
	source := filepath.Join(t.TempDir(), "polis-windows.exe")
	if err := os.WriteFile(source, []byte("WINDOWS POLIS EXECUTABLE"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "polis-windows-offline.zip")
	var out, errOut bytes.Buffer
	code := run([]string{"export", "--out", bundlePath, "--executable", source, "--runtime", "windows/amd64", "--format", "json"}, &out, &errOut)
	if code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var result struct {
		Runtime    string `json:"runtime"`
		Executable string `json:"executable"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid export JSON: %v\n%s", err, out.String())
	}
	if result.Runtime != "windows/amd64" || result.Executable != "polis-offline/bin/polis.exe" {
		t.Fatalf("export result=%+v", result)
	}
}

func TestRunExportRejectsInvalidTargetRuntime(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "polis-offline.zip")
	var out, errOut bytes.Buffer
	code := run([]string{"export", "--out", bundlePath, "--runtime", "windows"}, &out, &errOut)
	if code != exitValidationFailed || !strings.Contains(errOut.String(), "invalid target runtime") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestRunExportRejectsExistingOutput(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "polis-offline.zip")
	if err := os.WriteFile(bundlePath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"export", "--out", bundlePath}, &out, &errOut)
	if code != exitValidationFailed || !strings.Contains(errOut.String(), "output already exists") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestRunExportReportsExecutableMember(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "polis-offline.zip")
	var out, errOut bytes.Buffer
	if code := run([]string{"export", "--out", bundlePath}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, member := range archive.File {
		if member.Name != "polis-offline/manifest.json" {
			continue
		}
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Executable string `json:"executable"`
		}
		if err := json.Unmarshal(manifest, &document); err != nil {
			t.Fatal(err)
		}
		want := "polis-offline/bin/polis"
		if runtime.GOOS == "windows" {
			want += ".exe"
		}
		if document.Executable != want {
			t.Fatalf("executable=%q want=%q", document.Executable, want)
		}
		return
	}
	t.Fatal("offline bundle manifest not found")
}

func TestRunDoctorBlocksWithoutGit(t *testing.T) {
	t.Setenv("PATH", "")
	var out, errOut bytes.Buffer
	code := run([]string{"doctor"}, &out, &errOut)
	if code != 4 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "BLOCKED") {
		t.Fatalf("stderr=%q", errOut.String())
	}
}

func TestRunDoctorJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v %s", err, out.String())
	}
	if payload["status"] != "PASS" || payload["version"] != version {
		t.Fatalf("payload=%v", payload)
	}
}

func TestRunV6TrustBoundaryCommands(t *testing.T) {
	repo, built := buildV6CLIArtifact(t)

	assertCLITextContains(t, []string{"inspect", built.Path}, "POLIS INSPECT: PASS", built.TargetTree, "Deferred gates: coverage", "Consumer validation required: true", "Implementation plan: false")
	inspection := runCLIJSON(t, "inspect", "--format", "json", built.Path)
	assertJSONFields(t, inspection, map[string]string{
		"project":     "polis",
		"target_tree": built.TargetTree,
	})
	if present, ok := inspection["implementation_plan_present"].(bool); !ok || present {
		t.Fatalf("unplanned artifact implementation_plan_present=%v", inspection["implementation_plan_present"])
	}
	if deferred, ok := inspection["deferred_gates"].([]any); !ok || len(deferred) != 1 || deferred[0] != "coverage" || inspection["consumer_validation_required"] != true {
		t.Fatalf("inspect deferral=%v consumer_required=%v", inspection["deferred_gates"], inspection["consumer_validation_required"])
	}
	verify := runCLIJSON(t, "verify", "--format", "json", built.Path)
	assertJSONFields(t, verify, map[string]string{
		"status": "PASS",
	})
	if verify["validation_level"] != spec.ValidationLevelStrict {
		t.Fatalf("verify validation_level=%v", verify["validation_level"])
	}
	if gates, ok := verify["enabled_gates"].([]any); !ok || len(gates) != 2 {
		t.Fatalf("verify enabled_gates=%v", verify["enabled_gates"])
	}
	if deferred, ok := verify["deferred_gates"].([]any); !ok || len(deferred) != 1 || deferred[0] != "coverage" || verify["consumer_validation_required"] != true {
		t.Fatalf("verify deferral=%v consumer_required=%v", verify["deferred_gates"], verify["consumer_validation_required"])
	}
	assertCLITextContains(t, []string{"verify", built.Path}, "Deferred gates: coverage", "Consumer validation required: true")
	preflight := runCLIJSON(t, "preflight", "--repo", repo, "--format", "json", built.Path)
	assertJSONFields(t, preflight, map[string]string{
		"status": "PASS",
	})
	producerDeferred, producerDeferredOK := preflight["producer_deferred_gates"].([]any)
	outstandingDeferred, outstandingDeferredOK := preflight["outstanding_deferred_gates"].([]any)
	preflightGateStatuses, gateStatusesOK := preflight["consumer_gate_statuses"].(map[string]any)
	if preflight["consumer_validation_status"] != string(spec.StatusPass) || preflight["consumer_validation_required"] != true || !producerDeferredOK || len(producerDeferred) != 1 || producerDeferred[0] != "coverage" || !outstandingDeferredOK || len(outstandingDeferred) != 0 || !gateStatusesOK || preflightGateStatuses["coverage"] != string(spec.StatusPass) {
		t.Fatalf("preflight consumer validation=%v", preflight)
	}
	assertCLITextContains(t, []string{"preflight", "--repo", repo, built.Path}, "Producer deferred gates: coverage", "Outstanding deferred gates: none", "Consumer validation status: PASS", "coverage:PASS")
	assertFileContents(t, filepath.Join(repo, "app.txt"), "base\n")
	apply := runCLIJSON(t, "apply", "--repo", repo, "--format", "json", built.Path)
	consumerGateStatuses, ok := apply["consumer_gate_statuses"].(map[string]any)
	producerDeferred, producerDeferredOK = apply["producer_deferred_gates"].([]any)
	outstandingDeferred, outstandingDeferredOK = apply["outstanding_deferred_gates"].([]any)
	if apply["consumer_validation_status"] != string(spec.StatusPass) || !ok || consumerGateStatuses["coverage"] != string(spec.StatusPass) || !producerDeferredOK || len(producerDeferred) != 1 || producerDeferred[0] != "coverage" || !outstandingDeferredOK || len(outstandingDeferred) != 0 {
		t.Fatalf("apply consumer validation=%v", apply)
	}
	assertFileContents(t, filepath.Join(repo, "app.txt"), "changed\n")

	privatePath, publicPath := writeCLIKeyPair(t)
	signaturePath := filepath.Join(t.TempDir(), "artifact.polis.sig")
	assertJSONFields(t, runCLIJSON(t, "sign", "--key", privatePath, "--out", signaturePath, "--format", "json", built.Path), map[string]string{
		"status": "PASS",
	})
	assertCLITextContains(t, []string{"verify", "--signature", signaturePath, "--trusted-key", publicPath, built.Path}, "POLIS VERIFY: PASS")
	artifactFile, err := os.OpenFile(built.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifactFile.Write([]byte("tampered")); err != nil {
		_ = artifactFile.Close()
		t.Fatal(err)
	}
	if err := artifactFile.Close(); err != nil {
		t.Fatal(err)
	}
	var tamperOut, tamperErr bytes.Buffer
	if code := run([]string{"verify", "--signature", signaturePath, "--trusted-key", publicPath, built.Path}, &tamperOut, &tamperErr); code != exitInvalidArtifact || !strings.Contains(tamperErr.String(), "signature") {
		t.Fatalf("signed deferred artifact tampering code=%d stderr=%s", code, tamperErr.String())
	}
}

func TestRunV5CommandUsageRejectsPartialSignatureAndBadFormat(t *testing.T) {
	for _, args := range [][]string{
		{"inspect", "--format", "yaml", "x.polis"},
		{"preflight"},
		{"sign", "x.polis"},
		{"verify", "--signature", "x.sig", "x.polis"},
		{"apply", "--trusted-key", "x.pem", "x.polis"},
		{"doctor", "--format", "yaml"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), errOut.String())
		}
	}
}
