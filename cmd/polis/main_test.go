package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v5/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v5/spec"
)

func cliFixturePassCommand() []string {
	return []string{"git", "rev-parse", "--is-inside-work-tree"}
}

func canonicalPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in cli verification fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.LegacyPolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func featureContractFile(t *testing.T) string {
	t.Helper()
	c := spec.ChangeContract{SchemaVersion: spec.LegacyChangeContractSchemaVersion, Kind: spec.ChangeKindFeature, Behavior: spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60}, Affected: spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60}, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func makeValidPackage(t *testing.T) string {
	t.Helper()
	repo := makeBuildRepo(t)
	result, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "gitrex", Change: "verify-test", Out: t.TempDir(), Contract: featureContractFile(t)})
	if err != nil {
		t.Fatal(err)
	}
	return result.Path
}

func TestRunDoctor(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"doctor"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS doctor 5.0.1") {
		t.Fatalf("doctor version mismatch: stdout=%q", out.String())
	}
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

func TestRunVerifyPassesCanonicalPackage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"verify", makeValidPackage(t)}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS VERIFY: PASS") {
		t.Fatalf("stdout=%q", out.String())
	}
}

func TestRunVerifyRejectsInvalidPackage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.polis")
	if err := os.WriteFile(p, []byte("not zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"verify", p}, &out, &errOut)
	if code != exitInvalidArtifact {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "POLIS VERIFY: FAIL") {
		t.Fatalf("stderr=%q", errOut.String())
	}
}

func TestRunUsageAndUnknownCommand(t *testing.T) {
	cases := [][]string{nil, {"verify"}, {"unknown"}}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Fatalf("args=%v code=%d stderr=%s", args, code, errOut.String())
		}
	}
}

func makeBuildRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), canonicalPolicyBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "coverage.out"), []byte("mode: set\nexample.com/polisfixture/calc.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/polisfixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc.go"), []byte("package polisfixture\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc_test.go"), []byte("package polisfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRunBuildCreatesPackage(t *testing.T) {
	repo := makeBuildRepo(t)
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	contract := featureContractFile(t)
	code := run([]string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-build", "--contract", contract, "--out", outDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS BUILD: PASS") {
		t.Fatalf("stdout=%q", out.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".polis") {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestRunBuildRequiresFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"build"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestRunApplyAppliesBuiltPackage(t *testing.T) {
	repo := makeBuildRepo(t)
	outDir := filepath.Join(t.TempDir(), "out")
	var buildOut, buildErr bytes.Buffer
	contract := featureContractFile(t)
	if code := run([]string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-apply", "--contract", contract, "--out", outDir}, &buildOut, &buildErr); code != 0 {
		t.Fatalf("build code=%d stderr=%s", code, buildErr.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	artifact := filepath.Join(outDir, entries[0].Name())
	cmd := exec.Command("git", "-C", repo, "restore", "--", "app.txt")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore: %v\n%s", err, b)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"apply", "--repo", repo, artifact}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS APPLY: PASS") || !strings.Contains(out.String(), "Evidence:") {
		t.Fatalf("stdout=%q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
}

func TestRunApplyRequiresArtifact(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"apply"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestRunInitCreatesGoPolicy(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/cliinit\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"init", "--repo", repo}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS INIT: PASS") || !strings.Contains(out.String(), "Profile: go") {
		t.Fatalf("stdout=%q", out.String())
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.DecodePolicy(raw); err != nil {
		t.Fatalf("policy invalid: %v", err)
	}
}

func TestRunInitRejectsExtraArgsAndUnknownProfile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "extra"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d", code)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/x\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"init", "--repo", repo, "--profile", "rust"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestRunCaptureRedCreatesPatch(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("CLI-RED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exit := 1
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 30}, Affected: spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 30}, Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30}, BaselineExitCode: &exit, BaselineOutputContains: []string{"CLI-RED"}}}
	raw, _ := json.Marshal(c)
	contract := filepath.Join(t.TempDir(), "change.json")
	os.WriteFile(contract, raw, 0o600)
	outPath := filepath.Join(t.TempDir(), "red.patch")
	var out, errOut bytes.Buffer
	code := run([]string{"capture-red", "--repo", repo, "--contract", contract, "--out", outPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS CAPTURE-RED: PASS") {
		t.Fatalf("out=%s", out.String())
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

func TestRunV5TrustBoundaryCommands(t *testing.T) {
	repo, built := buildV5CLIArtifact(t)

	assertCLITextContains(t, []string{"inspect", built.Path}, "POLIS INSPECT: PASS", built.TargetTree)
	assertJSONFields(t, runCLIJSON(t, "inspect", "--format", "json", built.Path), map[string]string{
		"project":     "polis",
		"target_tree": built.TargetTree,
	})
	assertJSONFields(t, runCLIJSON(t, "verify", "--format", "json", built.Path), map[string]string{
		"status": "PASS",
	})
	assertJSONFields(t, runCLIJSON(t, "preflight", "--repo", repo, "--format", "json", built.Path), map[string]string{
		"status": "PASS",
	})
	assertFileContents(t, filepath.Join(repo, "app.txt"), "base\n")

	privatePath, publicPath := writeCLIKeyPair(t)
	signaturePath := filepath.Join(t.TempDir(), "artifact.polis.sig")
	assertJSONFields(t, runCLIJSON(t, "sign", "--key", privatePath, "--out", signaturePath, "--format", "json", built.Path), map[string]string{
		"status": "PASS",
	})
	assertCLITextContains(t, []string{"verify", "--signature", signaturePath, "--trusted-key", publicPath, built.Path}, "POLIS VERIFY: PASS")
}

func buildV5CLIArtifact(t *testing.T) (string, packagebuild.Result) {
	t.Helper()
	repo := makeBuildRepo(t)
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: repo, Project: "polis", Change: "v5-cli-contracts", Out: t.TempDir(), Contract: featureContractFile(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repo, "restore", "--", "app.txt").CombinedOutput(); err != nil {
		t.Fatalf("restore: %v\n%s", err, output)
	}
	return repo, built
}

func runCLIJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("args=%v code=%d stderr=%s", args, code, errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("args=%v invalid JSON: %v raw=%s", args, err, out.String())
	}
	return payload
}

func assertJSONFields(t *testing.T, payload map[string]any, expected map[string]string) {
	t.Helper()
	for key, want := range expected {
		if got := payload[key]; got != want {
			t.Fatalf("field %q=%v want=%q payload=%v", key, got, want, payload)
		}
	}
}

func assertCLITextContains(t *testing.T, args []string, tokens ...string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("args=%v code=%d stderr=%s", args, code, errOut.String())
	}
	for _, token := range tokens {
		if !strings.Contains(out.String(), token) {
			t.Fatalf("args=%v stdout=%q missing=%q", args, out.String(), token)
		}
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(contents) != want {
		t.Fatalf("%s=%q want=%q", path, contents, want)
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

func writeCLIKeyPair(t *testing.T) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.pem")
	publicPath := filepath.Join(dir, "public.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	return privatePath, publicPath
}

func TestRunInitCustomDryRunPreservesRepeatedArgv(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	var out, errOut bytes.Buffer
	code := run([]string{
		"init", "--repo", repo, "--profile", "custom", "--dry-run",
		"--test-argv", "npm", "--test-argv", "test with spaces", "--test-argv", "&&",
		"--coverage-argv", "npm", "--coverage-argv", "run", "--coverage-argv", "coverage with spaces", "--coverage-argv", "&&",
		"--coverage-adapter", "lcov-v1", "--coverage-report", "coverage/lcov.info",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	policy, err := spec.DecodePolicy(out.Bytes())
	if err != nil {
		t.Fatalf("stdout is not policy JSON: %v\n%s", err, out.String())
	}
	if got := policy.Gates[0].Command.Argv; strings.Join(got, "\x00") != strings.Join([]string{"npm", "test with spaces", "&&"}, "\x00") {
		t.Fatalf("test argv=%q", got)
	}
	if got := policy.Gates[1].Command.Argv; strings.Join(got, "\x00") != strings.Join([]string{"npm", "run", "coverage with spaces", "&&"}, "\x00") {
		t.Fatalf("coverage argv=%q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .polis: %v", err)
	}
}

func TestRunInitRejectsCustomFlagsOutsideCustomProfile(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/cliinit\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"init", "--repo", repo, "--profile", "go", "--test-argv", "go"}, &out, &errOut)
	if code != exitUsage || !strings.Contains(errOut.String(), "--profile custom") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}
