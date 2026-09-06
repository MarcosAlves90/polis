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

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func cliFixturePassCommand() []string {
	return []string{"git", "rev-parse", "--is-inside-work-tree"}
}

func canonicalPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in cli verification fixture"
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lockedCLIContract(t *testing.T, repo string) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindBehaviorPreserving,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "exercise V6 CLI with locked development",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "existing Add behavior remains Green")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Add remains Green", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "CLI operations preserve baseline contracts")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "build bypasses polis start")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "clean committed baseline")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "verified artifact")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "validation mismatch blocks delivery")},
		},
		Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "cli-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "cli-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("polis start CLI fixture: %v", err)
	}
	return locked
}

func makeValidPackage(t *testing.T) string {
	t.Helper()
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "gitrex", Change: "verify-test", Out: t.TempDir(), Contract: contract})
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
	if !strings.Contains(out.String(), "POLIS doctor 6.0.0") {
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
	return repo
}

func TestRunBuildCreatesPackage(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
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
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var buildOut, buildErr bytes.Buffer
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
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), canonicalPolicyBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	exit := 1
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 30, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindDefect,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"regression.txt"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective: "exercise CLI start then capture-red", Requirements: []spec.SpecificationClause{clause("REQ-001", "Red is captured after baseline lock")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "capture-red succeeds on locked baseline", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "HEAD is not mutated")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "capture from unlocked contract")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "regression delta")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "Red patch")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "wrong oracle blocks capture")},
		},
		Behavior: pass, Affected: pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"CLI-RED"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "cli-capture-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "cli-capture-locked-v4.json")
	var startOut, startErr bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--contract", draftPath, "--out", locked}, &startOut, &startErr); code != 0 {
		t.Fatalf("start code=%d stderr=%s", code, startErr.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("CLI-RED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "red.patch")
	var out, errOut bytes.Buffer
	code := run([]string{"capture-red", "--repo", repo, "--contract", locked, "--out", outPath}, &out, &errOut)
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

func TestRunV6TrustBoundaryCommands(t *testing.T) {
	repo, built := buildV6CLIArtifact(t)

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

func buildV6CLIArtifact(t *testing.T) (string, packagebuild.Result) {
	t.Helper()
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "polis", Change: "v6-cli-contracts", Out: t.TempDir(), Contract: contract})
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

func TestInspectionTextIncludesTraceability(t *testing.T) {
	inspection := packageverify.Inspection{
		Project: "polis", Change: "strict", FormatVersion: 3, PolicySchemaVersion: 3, ChangeContractSchemaVersion: 3,
		Kind: spec.ChangeKindFeature, BaseCommit: "base", TargetTree: "target", AllowedPaths: []string{"spec/"}, EvidenceEvents: 1,
		Traceability: []spec.TraceabilityLink{{RequirementID: "REQ-001", AcceptanceCriterionID: "AC-001", Proof: spec.ProofGateRegression}},
	}
	var out bytes.Buffer
	writeInspectionText(&out, inspection)
	if !strings.Contains(out.String(), "Trace: REQ-001 -> AC-001 -> regression") {
		t.Fatalf("stdout=%q", out.String())
	}
}

func TestRunStartProducesLockedContract(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/startfixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	var initOut, initErr bytes.Buffer
	if code := run([]string{"init", "--repo", repo}, &initOut, &initErr); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, initErr.String())
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	exit := 1
	reg := spec.CommandSpec{Argv: []string{"false"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindFeature,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"feature_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective: "lock before implementation", Requirements: []spec.SpecificationClause{clause("REQ-001", "feature is test-first")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "red then green", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "baseline remains exact")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "implementation predates lock")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "draft")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "locked contract")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "drift fails")},
		},
		Behavior: pass, Affected: pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &reg, BaselineExitCode: &exit, BaselineOutputContains: []string{"RED"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	ext := t.TempDir()
	draftPath := filepath.Join(ext, "draft.json")
	outPath := filepath.Join(ext, "locked.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"start", "--repo", repo, "--contract", draftPath, "--out", outPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS START: PASS") {
		t.Fatalf("stdout=%q", out.String())
	}
	lockedRaw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := spec.DecodeChangeContract(lockedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if locked.SchemaVersion != spec.LockedChangeContractSchemaVersion {
		t.Fatalf("schema=%d", locked.SchemaVersion)
	}
}
