package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
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

func lockedCLIContract(t *testing.T, repo string, externalPolicy ...string) string {
	t.Helper()
	draftPath := cliDraftContract(t, repo, spec.StrictChangeContractSchemaVersion, nil)
	locked := filepath.Join(t.TempDir(), "cli-locked-v4.json")
	startOptions := devstart.Options{Repo: repo, Contract: draftPath, Out: locked}
	if len(externalPolicy) > 0 {
		startOptions.Policy = externalPolicy[0]
	}
	if _, err := devstart.Start(context.Background(), startOptions); err != nil {
		t.Fatalf("polis start CLI fixture: %v", err)
	}
	return locked
}

func externalCLIPolicy(t *testing.T, repo string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	path := filepath.Join(t.TempDir(), "policy-v3.json")
	if err := os.WriteFile(path, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mismatchedCLIPolicy(t *testing.T, repo string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for index := range policy.Gates {
		if policy.Gates[index].Command == nil {
			continue
		}
		policy.Gates[index].Command.TimeoutSeconds++
		changed = true
		break
	}
	if !changed {
		t.Fatal("canonical test policy needs at least one command gate")
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	path := filepath.Join(t.TempDir(), "mismatched-policy-v3.json")
	if err := os.WriteFile(path, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func enableCLIRetention(t *testing.T, repo string) {
	t.Helper()
	manifest := []byte("{\"schema_version\":1,\"mode\":\"repository\"}\n")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "artifact-retention.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", repo, "add", ".polis/artifact-retention.json"},
		{"-C", repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-m", "configure artifact retention"},
	} {
		cmd := exec.Command("git", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func cliDraftContract(t *testing.T, repo string, schemaVersion int, commitMessage *string) string {
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
	draft.SchemaVersion = schemaVersion
	draftName := "cli-draft-v3.json"
	if schemaVersion == spec.CommitIntentDraftChangeContractSchemaVersion {
		draftName = "cli-draft-v5.json"
	}
	if commitMessage != nil {
		draft.Commit = &spec.CommitMetadata{Message: *commitMessage}
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), draftName)
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return draftPath
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

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitPass {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), errOut.String())
		}
		for _, fragment := range []string{"POLIS V6", "Usage:", "doctor", "implementation-plan", "gates", "export", "polis help <command>"} {
			if !strings.Contains(out.String(), fragment) {
				t.Errorf("args=%v help missing %q: %s", args, fragment, out.String())
			}
		}
		if errOut.Len() != 0 {
			t.Errorf("args=%v unexpected stderr=%q", args, errOut.String())
		}
	}
}

func statusStringSliceContains(value any, fragment string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		text, ok := item.(string)
		if ok && strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func assertStatusNextAction(t *testing.T, status map[string]any, action, command string) {
	t.Helper()
	next, ok := status["next_action"].(map[string]any)
	commandValue, _ := next["command"].(string)
	if !ok || next["action"] != action || commandValue != command {
		t.Fatalf("next action=%v, want action %q command %q", status["next_action"], action, command)
	}
}

func TestRunCommandHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "doctor"}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"Usage:", "polis doctor [--repo <path>", "Git on PATH", "static"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("command help missing %q: %s", fragment, out.String())
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr=%q", errOut.String())
	}
}

func TestRunHelpRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"help", "unknown"}, {"help", "doctor", "extra"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), errOut.String())
		}
		if out.Len() != 0 {
			t.Errorf("args=%v unexpected stdout=%q", args, out.String())
		}
		if errOut.Len() == 0 {
			t.Errorf("args=%v expected stderr", args)
		}
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

func cliGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func buildV6CLIArtifact(t *testing.T) (string, packagebuild.Result) {
	t.Helper()
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "polis", Change: "v6-cli-contracts", Out: t.TempDir(), Contract: contract, DeferredGates: []string{"coverage"}})
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
	payload, code := runCLIJSONCode(t, args)
	if code != exitPass {
		t.Fatalf("args=%v code=%d", args, code)
	}
	return payload
}

func runCLIJSONCode(t *testing.T, args []string) (map[string]any, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("args=%v code=%d invalid JSON: %v raw=%s stderr=%s", args, code, err, out.String(), errOut.String())
	}
	if code != exitPass && errOut.Len() != 0 {
		t.Logf("args=%v code=%d stderr=%s", args, code, errOut.String())
	}
	return payload, code
}

func assertStatusGate(t *testing.T, payload map[string]any, id, want string) {
	t.Helper()
	gates, ok := payload["gates"].([]any)
	if !ok {
		t.Fatalf("status gates=%T %v", payload["gates"], payload["gates"])
	}
	for _, raw := range gates {
		gate, ok := raw.(map[string]any)
		if ok && gate["id"] == id {
			if gate["status"] != want {
				t.Fatalf("gate %s status=%v want=%s", id, gate["status"], want)
			}
			return
		}
	}
	t.Fatalf("gate %s missing from status: %v", id, gates)
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

func TestIssue12BuildFailureHasEquivalentTextAndJSONGateDiagnostics(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := issue12FailingPolicyPath(t)
	contract := lockedCLIContract(t, repo, policyPath)
	t.Setenv("POLIS_ISSUE12_GATE_HELPER", "1")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runBuild := func(format string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		args := []string{"build", "--repo", repo, "--policy", policyPath, "--project", "polis", "--change", "issue-12-diagnostics", "--contract", contract, "--format", format, "--out", filepath.Join(t.TempDir(), "artifact")}
		if code := run(args, &out, &errOut); code == exitPass {
			t.Fatalf("ISSUE12-RED: configured gate failure unexpectedly passed: stdout=%q stderr=%q", out.String(), errOut.String())
		}
		return errOut.String()
	}

	textOutput := runBuild("text")
	for _, fragment := range []string{"test.complete=FAIL", "TestGateCommandDiagnosticHelper", "ISSUE12-GATE-STDOUT", "ISSUE12-GATE-STDERR", "artifact packaging"} {
		if !strings.Contains(textOutput, fragment) {
			t.Fatalf("ISSUE12-RED: text diagnostic missing %q: %s", fragment, textOutput)
		}
	}

	jsonOutput := runBuild("json")
	payload := finalJSONRecordAfterProgress(t, jsonOutput)
	diagnostic, ok := payload["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("ISSUE12-RED: JSON failure output has no diagnostic object: %s", jsonOutput)
	}
	if diagnostic["stage"] != "project gate validation" {
		t.Fatalf("ISSUE12-RED: unexpected diagnostic stage: %v", diagnostic["stage"])
	}
	statuses, ok := diagnostic["gate_statuses"].(map[string]any)
	if !ok || statuses["test.complete"] != "FAIL" {
		t.Fatalf("ISSUE12-RED: missing failed gate status: %v", diagnostic["gate_statuses"])
	}
	command, ok := diagnostic["command"].(map[string]any)
	if !ok {
		t.Fatalf("ISSUE12-RED: missing failed command context: %v", diagnostic["command"])
	}
	for _, fragment := range []string{"TestGateCommandDiagnosticHelper", "ISSUE12-GATE-STDOUT", "ISSUE12-GATE-STDERR", "artifact packaging"} {
		if !strings.Contains(jsonOutput, fragment) {
			t.Fatalf("ISSUE12-RED: JSON diagnostic missing %q: %s", fragment, jsonOutput)
		}
	}
	if command["cwd"] != "." {
		t.Fatalf("ISSUE12-RED: unexpected failed-command cwd: %v", command["cwd"])
	}
}

func TestIssue12CaptureRedScopeHasEquivalentTextAndJSONDiagnostics(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedIssue12RedGreenContract(t, repo, []string{"calc_test.go"})
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("outside test scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCapture := func(format string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		args := []string{"capture-red", "--repo", repo, "--contract", contract, "--out", filepath.Join(t.TempDir(), "red.patch"), "--format", format}
		if code := run(args, &out, &errOut); code == exitPass {
			t.Fatalf("ISSUE12-RED: out-of-scope Red probe unexpectedly passed: stdout=%q stderr=%q", out.String(), errOut.String())
		}
		return errOut.String()
	}

	textOutput := runCapture("text")
	for _, fragment := range []string{"Red probe scope validation", "app.txt", "calc_test.go"} {
		if !strings.Contains(textOutput, fragment) {
			t.Fatalf("ISSUE12-RED: capture-red text diagnostic missing %q: %s", fragment, textOutput)
		}
	}
	jsonOutput := runCapture("json")
	payload := finalJSONRecordAfterProgress(t, jsonOutput)
	diagnostic, ok := payload["diagnostic"].(map[string]any)
	if !ok || diagnostic["stage"] != "Red probe scope validation" {
		t.Fatalf("ISSUE12-RED: capture-red JSON lacks scope stage: %s", jsonOutput)
	}
	for _, fragment := range []string{"app.txt", "calc_test.go"} {
		if !strings.Contains(jsonOutput, fragment) {
			t.Fatalf("ISSUE12-RED: capture-red JSON diagnostic missing %q: %s", fragment, jsonOutput)
		}
	}
}

func issue12FailingPolicyPath(t *testing.T) string {
	t.Helper()
	var policy spec.Policy
	if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].ID == "test.complete" {
			policy.Gates[i].Command = &spec.CommandSpec{
				Argv: []string{executable, "-test.run=TestGateCommandDiagnosticHelper"}, Cwd: ".", TimeoutSeconds: 60,
				Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit},
			}
		}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "issue12-policy.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func lockedIssue12RedGreenContract(t *testing.T, repo string, testPaths []string) string {
	t.Helper()
	draftPath := cliDraftContract(t, repo, spec.StrictChangeContractSchemaVersion, nil)
	raw, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatal(err)
	}
	var draft spec.ChangeContract
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	draft.Kind = spec.ChangeKindFeature
	draft.TestScope = &spec.ChangeScope{AllowedPaths: append([]string(nil), testPaths...)}
	exit := 128
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	regression := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "ISSUE12-RED-MISSING-REF"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	draft.Regression = spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exit, BaselineOutputContains: []string{"fatal"}}
	raw, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath = filepath.Join(t.TempDir(), "issue12-redgreen-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "issue12-redgreen-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("polis start issue-12 fixture: %v", err)
	}
	return locked
}

func TestGateCommandDiagnosticHelper(t *testing.T) {
	if os.Getenv("POLIS_ISSUE12_GATE_HELPER") != "1" {
		return
	}
	_, _ = io.WriteString(os.Stdout, "ISSUE12-GATE-STDOUT\n")
	_, _ = io.WriteString(os.Stderr, "ISSUE12-GATE-STDERR\n")
	os.Exit(23)
}

func lockedIssue13ScopeContract(t *testing.T, repo string) string {
	t.Helper()
	draftPath := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, nil)
	raw, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatal(err)
	}
	var draft spec.ChangeContract
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	draft.Kind = spec.ChangeKindFeature
	draft.TestScope = &spec.ChangeScope{AllowedPaths: []string{"calc_test.go", "tests/"}}
	exit := 1
	draft.Regression = spec.RegressionContract{
		Mode: spec.RegressionModeRedGreen,
		Command: &spec.CommandSpec{
			Argv: []string{"polis-issue13-must-not-run"}, Cwd: ".", TimeoutSeconds: 30,
			Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit},
		},
		BaselineExitCode: &exit, BaselineOutputContains: []string{"ISSUE13-NEVER-EXECUTED"},
	}
	raw, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath = filepath.Join(t.TempDir(), "issue13-draft-v5.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "issue13-locked-v6.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("lock issue-13 scope fixture: %v", err)
	}
	return locked
}

func TestIssue13ScopePreflightReportsValidAndRejectedPaths(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedIssue13ScopeContract(t, repo)
	beforeHead := cliGit(t, repo, "rev-parse", "HEAD")
	beforeStatus := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")

	runScope := func(format string, paths ...string) (int, string, string) {
		t.Helper()
		args := []string{"check-red-scope", "--repo", repo, "--contract", contract, "--format", format}
		for _, path := range paths {
			args = append(args, "--path", path)
		}
		var out, errOut bytes.Buffer
		code := run(args, &out, &errOut)
		return code, out.String(), errOut.String()
	}

	passCode, passJSON, passErr := runScope("json", "tests/unit/new_test.go", "calc_test.go")
	if passCode != exitPass {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: valid proposed paths code=%d stdout=%s stderr=%s", passCode, passJSON, passErr)
	}
	var pass struct {
		Status           string   `json:"status"`
		AllowedTestPaths []string `json:"allowed_test_paths"`
		ProbeExecuted    bool     `json:"probe_executed"`
		ProofCaptured    bool     `json:"proof_captured"`
		Paths            []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(passJSON), &pass); err != nil {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: valid JSON: %v: %s", err, passJSON)
	}
	if pass.Status != "PASS" || !slices.Equal(pass.AllowedTestPaths, []string{"calc_test.go", "tests/"}) || pass.ProbeExecuted || pass.ProofCaptured || len(pass.Paths) != 2 || pass.Paths[0].Path != "calc_test.go" || pass.Paths[0].Status != "PASS" || pass.Paths[1].Path != "tests/unit/new_test.go" || pass.Paths[1].Status != "PASS" {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: valid report=%+v", pass)
	}

	failCode, failJSON, failErr := runScope("json", "docs/readme.md", "calc_test.go", "app.txt")
	if failCode != exitValidationFailed {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: rejected paths code=%d stdout=%s stderr=%s", failCode, failJSON, failErr)
	}
	var failed struct {
		Status           string   `json:"status"`
		AllowedTestPaths []string `json:"allowed_test_paths"`
		ProbeExecuted    bool     `json:"probe_executed"`
		ProofCaptured    bool     `json:"proof_captured"`
		RejectedPaths    []struct {
			Path string `json:"path"`
			Rule string `json:"rule"`
		} `json:"rejected_paths"`
	}
	if err := json.Unmarshal([]byte(failJSON), &failed); err != nil {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: rejected JSON: %v: %s", err, failJSON)
	}
	if failed.Status != "FAIL" || !slices.Equal(failed.AllowedTestPaths, []string{"calc_test.go", "tests/"}) || failed.ProbeExecuted || failed.ProofCaptured || len(failed.RejectedPaths) != 2 || failed.RejectedPaths[0].Path != "app.txt" || failed.RejectedPaths[0].Rule != "test_scope.allowed_paths" || failed.RejectedPaths[1].Path != "docs/readme.md" || failed.RejectedPaths[1].Rule != "test_scope.allowed_paths" {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: rejected report=%+v", failed)
	}
	textCode, textOutput, textErr := runScope("text", "docs/readme.md", "calc_test.go", "app.txt")
	if textCode != exitValidationFailed {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: text code=%d stdout=%s stderr=%s", textCode, textOutput, textErr)
	}
	assertOnlyTextProgress(t, textErr)
	for _, fragment := range []string{"POLIS CHECK-RED-SCOPE: FAIL", "app.txt", "docs/readme.md", "test_scope.allowed_paths", "calc_test.go", "tests/"} {
		if !strings.Contains(textOutput, fragment) {
			t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: text report missing %q: %s", fragment, textOutput)
		}
	}
	if cliGit(t, repo, "rev-parse", "HEAD") != beforeHead || cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all") != beforeStatus {
		t.Fatal("ISSUE13-SCOPE-PREFLIGHT: scope preflight mutated the source repository")
	}
}

func TestIssue13ScopePreflightRejectsInvalidInputs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "check-red-scope"}, &out, &errOut); code != exitPass || !strings.Contains(out.String(), "--path <file>") {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: help code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"check-red-scope", "--contract", "locked.json"}, &out, &errOut); code != exitUsage {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: missing --path code=%d stderr=%s", code, errOut.String())
	}
	repo := makeBuildRepo(t)
	contract := lockedIssue13ScopeContract(t, repo)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"check-red-scope", "--repo", repo, "--contract", contract, "--path", "../escape.go", "--format", "json"}, &out, &errOut); code != exitValidationFailed || !strings.Contains(out.String(), `"rule":"repository_relative_path"`) {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: invalid path code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, nil)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"check-red-scope", "--repo", repo, "--contract", draft, "--path", "calc_test.go"}, &out, &errOut); code == exitPass || !strings.Contains(errOut.String(), "locked") {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: unlocked contract code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "drift.txt"), []byte("new baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", "drift.txt")
	cliGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "drift")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"check-red-scope", "--repo", repo, "--contract", contract, "--path", "calc_test.go"}, &out, &errOut); code == exitPass || !strings.Contains(errOut.String(), "baseline") {
		t.Fatalf("ISSUE13-SCOPE-PREFLIGHT: stale baseline code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestIssue10BaselineConstraintHasDistinctTextAndJSON(t *testing.T) {
	failure := &diagnostic.Error{
		Summary: "locked baseline constraint: projected size exceeds maximum",
		Report: diagnostic.Report{
			Stage:    "locked baseline constraint",
			Expected: map[string]any{"maximum_bytes": uint64(128 << 20)},
			Actual:   map[string]any{"projected_bytes": uint64((128 << 20) + 4096)},
			NotRun:   []string{"test.complete", "coverage", "artifact packaging"},
		},
	}
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			if got := writeFailure(&output, format, "POLIS BUILD", exitUsage, failure); got != exitUsage {
				t.Fatalf("exit code=%d", got)
			}
			for _, fragment := range []string{"locked baseline constraint", "test.complete", "coverage", "artifact packaging"} {
				if !strings.Contains(output.String(), fragment) {
					t.Fatalf("missing %q: %s", fragment, output.String())
				}
			}
			if strings.Contains(output.String(), "Gate statuses:") || strings.Contains(output.String(), `"gate_statuses"`) {
				t.Fatalf("baseline constraint was represented as a gate result: %s", output.String())
			}
			if format == "json" {
				var report struct {
					Diagnostic diagnostic.Report `json:"diagnostic"`
				}
				if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Diagnostic.Stage != "locked baseline constraint" {
					t.Fatalf("invalid JSON baseline diagnostic: %v %s", err, output.String())
				}
			}
		})
	}
}

func TestOperationalDiagnosticCLIFailureClassesPreserveContracts(t *testing.T) {
	cases := []struct {
		exitCode              int
		code, category, cause string
	}{
		{exitUsage, "POLIS_OPERATION_REJECTED", "usage", "invalid_operation_input"},
		{exitInvalidArtifact, "POLIS_INVALID_ARTIFACT", "artifact", "artifact_validation_failed"},
		{exitBlocked, "POLIS_OPERATION_BLOCKED", "blocked", "operation_blocked"},
		{exitBaselineMismatch, "POLIS_BASELINE_MISMATCH", "baseline", "baseline_validation_failed"},
		{exitValidationFailed, "POLIS_VALIDATION_FAILED", "validation", "operation_validation_failed"},
		{exitApplyFailed, "POLIS_APPLY_FAILED", "apply", "apply_not_completed"},
	}
	for _, tc := range cases {
		var output bytes.Buffer
		// Error text is intentionally misleading; semantic codes must use the exit
		// category, not regex matching human-readable messages.
		cause := errors.New("missing executable in human message")
		if got := writeFailure(&output, "json", "POLIS BUILD", tc.exitCode, cause); got != tc.exitCode {
			t.Fatalf("exit=%d; expected %d", got, tc.exitCode)
		}
		var result struct {
			Status     string            `json:"status"`
			Error      string            `json:"error"`
			ExitCode   int               `json:"exit_code"`
			Diagnostic diagnostic.Report `json:"diagnostic"`
		}
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "FAIL" || result.ExitCode != tc.exitCode || result.Error != cause.Error() {
			t.Fatalf("legacy fields changed: %+v", result)
		}
		if result.Diagnostic.Code != tc.code || result.Diagnostic.Category != tc.category || result.Diagnostic.ObservedCause != tc.cause || result.Diagnostic.Stage != "build" || !slices.Equal(result.Diagnostic.AffectedOperations, []string{"build"}) || result.Diagnostic.Remediation != "" {
			t.Fatalf("unexpected fallback diagnostic: %+v", result.Diagnostic)
		}
		output.Reset()
		if got := writeFailure(&output, "text", "POLIS BUILD", tc.exitCode, cause); got != tc.exitCode || output.String() != "POLIS BUILD: FAIL: missing executable in human message\n" {
			t.Fatalf("legacy text changed: %q", output.String())
		}
	}
}

func TestOperationalDiagnosticSpecificClassOverridesFallback(t *testing.T) {
	detail := &diagnostic.Error{Summary: "scope was rejected", Report: diagnostic.Report{
		Code: "POLIS_RED_PROBE_SCOPE_VIOLATION", Category: "scope", Stage: "Red probe scope validation",
		ObservedCause: "out_of_scope_red_probe_paths", Remediation: "restrict_red_probe_to_test_scope",
		Expected: map[string]any{"test_paths": []string{"a_test.go"}}, NotRun: []string{"Red proof capture"},
	}}
	var output bytes.Buffer
	if got := writeFailure(&output, "json", "POLIS CAPTURE-RED", exitUsage, detail); got != exitUsage {
		t.Fatal(got)
	}
	var decoded struct {
		Status     string            `json:"status"`
		Error      string            `json:"error"`
		Diagnostic diagnostic.Report `json:"diagnostic"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "FAIL" || decoded.Error != detail.Summary || decoded.Diagnostic.Code != detail.Report.Code || decoded.Diagnostic.Category != "scope" || !slices.Equal(decoded.Diagnostic.NotRun, detail.Report.NotRun) || !slices.Equal(decoded.Diagnostic.AffectedOperations, []string{"capture-red"}) {
		t.Fatalf("known structured error was lost: %+v", decoded)
	}
}

func TestOperationalDiagnosticDoctorMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	if got := runDoctor([]string{"--format", "json"}, &out, &errOut); got != exitBlocked {
		t.Fatalf("exit=%d output=%s", got, errOut.String())
	}
	got := finalJSONRecordAfterProgress(t, errOut.String())
	report, ok := got["diagnostic"].(map[string]any)
	if !ok || got["status"] != "BLOCKED" || got["error"] != "git not found" || report["code"] != "POLIS_GIT_PREREQUISITE_MISSING" || report["remediation"] != "install_git" {
		t.Fatalf("doctor prerequisite: %+v", got)
	}
}

func TestGatePrerequisiteDiagnosticsNeverExposeProcessDerivedValues(t *testing.T) {
	const secret = "sentinel-process-output-secret"
	tests := []struct {
		input, expected string
	}{
		{"missing executable " + secret, "missing executable (details omitted)"},
		{"missing dependency " + secret, "missing dependency (details omitted)"},
		{"missing environment condition " + secret, "missing environment condition (details omitted)"},
		{"other prerequisite " + secret, ""},
		{"missing executable", ""},
	}
	for _, tt := range tests {
		got := safeGatePrerequisite(tt.input)
		if got != tt.expected || strings.Contains(got, secret) {
			t.Errorf("unsafe prerequisite category: got %q, want %q", got, tt.expected)
		}
	}
}
