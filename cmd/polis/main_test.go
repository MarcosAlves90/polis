package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
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
	draftPath := cliDraftContract(t, repo, nil)
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

func cliDraftContract(t *testing.T, repo string, commitMessage *string) string {
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
	draftName := "cli-draft-v3.json"
	if commitMessage != nil {
		draft.SchemaVersion = spec.CommitIntentDraftChangeContractSchemaVersion
		draft.Commit = &spec.CommitMetadata{Message: *commitMessage}
		draftName = "cli-draft-v5.json"
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

func TestRunDoctor(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"doctor"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS doctor 6.7.0") {
		t.Fatalf("doctor version mismatch: stdout=%q", out.String())
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitPass {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, out.String(), errOut.String())
		}
		for _, fragment := range []string{"POLIS V6", "Usage:", "doctor", "export", "polis help <command>"} {
			if !strings.Contains(out.String(), fragment) {
				t.Errorf("args=%v help missing %q: %s", args, fragment, out.String())
			}
		}
		if errOut.Len() != 0 {
			t.Errorf("args=%v unexpected stderr=%q", args, errOut.String())
		}
	}
}

func TestRunCommandHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "doctor"}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"Usage:", "polis doctor [--format text|json]", "Git and runtime prerequisites"} {
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
		executableMember:                                false,
		"polis-offline/manifest.json":                   false,
		"polis-offline/POLIS-OFFLINE.md":                false,
		"polis-offline/spec/POLIS-SPEC-v6.md":           false,
		"polis-offline/spec/schemas/policy.schema.json": false,
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

func TestRunPlanReportsEffectiveExecutionPlan(t *testing.T) {
	repo := makeBuildRepo(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "--repo", repo, "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var plan policyplan.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("invalid plan JSON: %v\n%s", err, out.String())
	}
	if plan.PlanVersion != policyplan.PlanVersion || plan.PolicySource != policyplan.PolicySourceCommitted || plan.ValidationLevel != spec.ValidationLevelStrict {
		t.Fatalf("plan metadata=%+v", plan)
	}
	if len(plan.Gates) != len(spec.ProjectGateOrder) || len(plan.Guarantees) != len(spec.ProjectGateOrder) || len(plan.MandatoryInvariants) == 0 {
		t.Fatalf("plan inventory: gates=%d guarantees=%d invariants=%d", len(plan.Gates), len(plan.Guarantees), len(plan.MandatoryInvariants))
	}
	if len(plan.DependencyEdges) != 1 || len(plan.ExecutionOrder) != len(spec.ProjectGateOrder) {
		t.Fatalf("plan dependencies: edges=%v order=%v", plan.DependencyEdges, plan.ExecutionOrder)
	}
	if plan.Gates[0].Command == nil || plan.Gates[1].Mode != spec.GateModeCoverage {
		t.Fatalf("plan gates=%+v", plan.Gates[:2])
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"plan", "--repo", repo}, &out, &errOut); code != 0 {
		t.Fatalf("text code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS PLAN: PASS") || !strings.Contains(out.String(), "Dependency edges:") || !strings.Contains(out.String(), "Mandatory invariants:") || !strings.Contains(out.String(), "Guarantees:") {
		t.Fatalf("text plan=%q", out.String())
	}
}

func TestRunPlanDeferralReportsResponsibilitiesInTextAndJSON(t *testing.T) {
	repo := makeBuildRepo(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "--repo", repo, "--defer-gate", "coverage", "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("json code=%d stderr=%s", code, errOut.String())
	}
	var plan policyplan.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("invalid plan JSON: %v\n%s", err, out.String())
	}
	if !plan.ConsumerValidationRequired || len(plan.DeferredGates) != 1 || plan.DeferredGates[0] != "coverage" || plan.Gates[1].ProducerAction != policyplan.ProducerActionDeferred || plan.Gates[1].ConsumerRequirement != policyplan.ConsumerRequirementRequired {
		t.Fatalf("plan deferral=%+v gate=%+v", plan.DeferredGates, plan.Gates[1])
	}
	if !slices.Contains(plan.EnabledGates, "coverage") || !slices.Contains(plan.DisabledGates, "lint") {
		t.Fatalf("deferred and disabled inventories collapsed: enabled=%v disabled=%v", plan.EnabledGates, plan.DisabledGates)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"plan", "--repo", repo, "--defer-gate", "test.complete", "--defer-gate", "coverage"}, &out, &errOut); code != exitPass {
		t.Fatalf("multiple deferrals code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Deferred gates: test.complete, coverage") || !strings.Contains(out.String(), "producer_action=deferred") || !strings.Contains(out.String(), "consumer_requirement=required") {
		t.Fatalf("text plan omitted responsibility details: %s", out.String())
	}
}

func TestRunPlanRejectsInvalidDeferredGateFlags(t *testing.T) {
	repo := makeBuildRepo(t)
	for _, scenario := range []struct {
		args []string
		want string
	}{
		{args: []string{"--defer-gate", "missing.gate"}, want: "unknown project gate"},
		{args: []string{"--defer-gate", "coverage", "--defer-gate", "coverage"}, want: "more than once"},
		{args: []string{"--defer-gate", "lint"}, want: "not applicable"},
	} {
		args := append([]string{"plan", "--repo", repo}, scenario.args...)
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitValidationFailed || !strings.Contains(errOut.String(), scenario.want) {
			t.Fatalf("args=%v code=%d stderr=%s", scenario.args, code, errOut.String())
		}
	}
}

func TestRunPlanReportsExternalPolicySourceWithoutPath(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := filepath.Join(t.TempDir(), "policy-v3.json")
	if err := os.WriteFile(policyPath, canonicalPolicyBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "--repo", repo, "--policy", policyPath, "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var plan policyplan.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("invalid plan JSON: %v\n%s", err, out.String())
	}
	if plan.PolicySource != policyplan.PolicySourceExternal {
		t.Fatalf("policy source=%q", plan.PolicySource)
	}
	if strings.Contains(out.String(), policyPath) {
		t.Fatalf("external policy pathname leaked into plan output: %q", policyPath)
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
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded spec.Policy
	if err := json.Unmarshal(policyRaw, &decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	if err := os.WriteFile(policyPath, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	contract := lockedCLIContract(t, repo, policyPath)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	code := run([]string{"build", "--repo", repo, "--policy", policyPath, "--project", "gitrex", "--change", "cli-build", "--contract", contract, "--defer-gate", "coverage", "--out", outDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS BUILD: PASS") || !strings.Contains(out.String(), "Deferred gates: coverage") || !strings.Contains(out.String(), "Consumer validation required: true") || !strings.Contains(out.String(), "DEFERRED") {
		t.Fatalf("stdout=%q", out.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".polis") {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestRunBuildDeferralTextAndJSONReporting(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	args := []string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-defer", "--contract", contract, "--defer-gate", "coverage", "--format", "json", "--out", outDir}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("json code=%d stderr=%s", code, errOut.String())
	}
	var result struct {
		DeferredGates              []string          `json:"deferred_gates"`
		ConsumerValidationRequired bool              `json:"consumer_validation_required"`
		ProducerGateStatuses       map[string]string `json:"producer_gate_statuses"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid build JSON: %v\n%s", err, out.String())
	}
	if !result.ConsumerValidationRequired || len(result.DeferredGates) != 1 || result.DeferredGates[0] != "coverage" || result.ProducerGateStatuses["coverage"] != string(spec.StatusDeferred) {
		t.Fatalf("build output=%+v", result)
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
	if code := run([]string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-apply", "--contract", contract, "--defer-gate", "coverage", "--out", outDir}, &buildOut, &buildErr); code != 0 {
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
	if !strings.Contains(out.String(), "POLIS APPLY: PASS") || strings.Contains(out.String(), "Evidence:") || !strings.Contains(out.String(), "Producer deferred gates: coverage") || !strings.Contains(out.String(), "Outstanding deferred gates: none") || !strings.Contains(out.String(), "Consumer validation required: true") || !strings.Contains(out.String(), "Consumer validation status: PASS") {
		t.Fatalf("stdout=%q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
}

func TestRunApplyAutoCommitsArtifactIntentEndToEnd(t *testing.T) {
	repo := makeBuildRepo(t)
	message := "feat(apply): commit the validated artifact tree\n\nsecond line with preserved spaces  \n"
	draft := cliDraftContract(t, repo, &message)
	locked := filepath.Join(t.TempDir(), "cli-locked-v6.json")
	var startOut, startErr bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--contract", draft, "--out", locked}, &startOut, &startErr); code != exitPass {
		t.Fatalf("start code=%d stderr=%s", code, startErr.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	built := runCLIJSON(t, "build", "--repo", repo, "--project", "polis", "--change", "artifact-commit-e2e", "--contract", locked, "--defer-gate", "coverage", "--out", outDir, "--format", "json")
	artifact, ok := built["artifact"].(string)
	if !ok || artifact == "" {
		t.Fatalf("build output has no artifact path: %v", built)
	}
	if output, err := exec.Command("git", "-C", repo, "restore", "--", "app.txt").CombinedOutput(); err != nil {
		t.Fatalf("restore producer worktree: %v\n%s", err, output)
	}
	verified := runCLIJSON(t, "verify", "--format", "json", artifact)
	if verified["status"] != "PASS" {
		t.Fatalf("verify result=%v", verified)
	}
	inspection := runCLIJSON(t, "inspect", "--format", "json", artifact)
	commitIntent, ok := inspection["commit"].(map[string]any)
	if !ok || commitIntent["message"] != message {
		t.Fatalf("inspect commit intent=%v want exact message %q", inspection["commit"], message)
	}
	if output, err := exec.Command("git", "-C", repo, "config", "user.name", "POLIS CLI Consumer").CombinedOutput(); err != nil {
		t.Fatalf("configure consumer name: %v\n%s", err, output)
	}
	if output, err := exec.Command("git", "-C", repo, "config", "user.email", "polis-cli@example.invalid").CombinedOutput(); err != nil {
		t.Fatalf("configure consumer email: %v\n%s", err, output)
	}
	parent := cliGit(t, repo, "rev-parse", "HEAD")
	targetTree, ok := built["target_tree"].(string)
	if !ok || targetTree == "" {
		t.Fatalf("build output has no target tree: %v", built)
	}
	apply := runCLIJSON(t, "apply", "--repo", repo, "--commit-mode", "auto", "--format", "json", artifact)
	commitSHA, ok := apply["commit_sha"].(string)
	if !ok || commitSHA == "" || apply["committed"] != true || apply["commit_message"] != message {
		t.Fatalf("apply commit result=%v", apply)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD"); got != commitSHA {
		t.Fatalf("HEAD=%s want commit_sha %s", got, commitSHA)
	}
	if got := cliGit(t, repo, "show", "-s", "--format=%P", "HEAD"); got != parent {
		t.Fatalf("commit parent=%s want validated consumer HEAD %s", got, parent)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD^{tree}"); got != targetTree {
		t.Fatalf("commit tree=%s want validated target tree %s", got, targetTree)
	}
	commitObject, err := exec.Command("git", "-C", repo, "cat-file", "commit", "HEAD").Output()
	if err != nil {
		t.Fatalf("read created commit: %v", err)
	}
	separator := bytes.Index(commitObject, []byte("\n\n"))
	if separator < 0 || !bytes.Equal(commitObject[separator+2:], []byte(message)) {
		t.Fatalf("commit message bytes=%q want exact %q", commitObject[separator+2:], message)
	}
	if status := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("successful auto commit left worktree changes: %q", status)
	}
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

func TestRunInitExposesValidationLevelAndSelectiveDisabledGate(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/cli-level\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, b)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"init", "--repo", repo, "--profile", "go", "--validation-level", "standard", "--disable-gate", "lint", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	policy, err := spec.DecodePolicy(out.Bytes())
	if err != nil {
		t.Fatalf("policy invalid: %v\n%s", err, out.String())
	}
	if policy.EffectiveValidationLevel() != spec.ValidationLevelStandard {
		t.Fatalf("level=%q", policy.EffectiveValidationLevel())
	}
	if policy.Gates[1].Mode != spec.GateModeNotApplicable || policy.Gates[2].Mode != spec.GateModeNotApplicable {
		t.Fatalf("coverage=%+v lint=%+v", policy.Gates[1], policy.Gates[2])
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

	assertCLITextContains(t, []string{"inspect", built.Path}, "POLIS INSPECT: PASS", built.TargetTree, "Deferred gates: coverage", "Consumer validation required: true")
	inspection := runCLIJSON(t, "inspect", "--format", "json", built.Path)
	assertJSONFields(t, inspection, map[string]string{
		"project":     "polis",
		"target_tree": built.TargetTree,
	})
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
		Project: "polis", Change: "strict", FormatVersion: 3, PolicySchemaVersion: 3, ValidationLevel: spec.ValidationLevelStrict, ChangeContractSchemaVersion: 3,
		Kind: spec.ChangeKindFeature, BaseCommit: "base", TargetTree: "target", AllowedPaths: []string{"spec/"}, EvidenceEvents: 1,
		Traceability: []spec.TraceabilityLink{{RequirementID: "REQ-001", AcceptanceCriterionID: "AC-001", Proof: spec.ProofGateRegression}},
	}
	var out bytes.Buffer
	writeInspectionText(&out, inspection)
	if !strings.Contains(out.String(), "Validation level: strict") || !strings.Contains(out.String(), "Trace: REQ-001 -> AC-001 -> regression") {
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
	policyPath := filepath.Join(ext, "policy.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, policyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"start", "--repo", repo, "--policy", policyPath, "--contract", draftPath, "--out", outPath}, &out, &errOut)
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

func TestRunConsumerBaselineModes(t *testing.T) {
	repo, built := buildV6CLIArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "consumer.txt"), []byte("consumer-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "consumer.txt"},
		{"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "consumer unrelated change"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	var strictOut, strictErr bytes.Buffer
	if code := run([]string{"preflight", "--repo", repo, built.Path}, &strictOut, &strictErr); code != exitBaselineMismatch {
		t.Fatalf("strict code=%d stdout=%s stderr=%s", code, strictOut.String(), strictErr.String())
	}

	payload := runCLIJSON(t, "preflight", "--repo", repo, "--baseline-mode", "compatible", "--format", "json", built.Path)
	assertJSONFields(t, payload, map[string]string{"status": "PASS", "baseline_mode": "compatible", "baseline_source": "local", "baseline_ancestry": "proven_descendant"})
	if payload["consumer_base_commit"] == "" || payload["baseline_compatibility"] == "" {
		t.Fatalf("missing compatibility details: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || active {
		t.Fatalf("compatible preflight override_active=%v", payload["override_active"])
	}

	permissivePayload := runCLIJSON(t, "preflight", "--repo", repo, "--baseline-mode", "permissive", "--allow-missing-baseline-proof", "--format", "json", built.Path)
	assertJSONFields(t, permissivePayload, map[string]string{"status": "PASS", "baseline_mode": "permissive", "baseline_source": "local", "baseline_ancestry": "proven_descendant"})
	if active, ok := permissivePayload["override_active"].(bool); !ok || active {
		t.Fatalf("override should not activate while local proof exists: %v", permissivePayload)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"apply", "--repo", repo, "--baseline-mode", "compatible", built.Path}, &out, &errOut); code != exitPass {
		t.Fatalf("compatible apply code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"POLIS APPLY: PASS", "Baseline mode: compatible", "Consumer base:", "Baseline compatibility:"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("compatible output missing %q: %s", fragment, out.String())
		}
	}
	assertFileContents(t, filepath.Join(repo, "app.txt"), "changed\n")
}

func TestRunRejectsMissingBaselineOverrideOutsidePermissiveMode(t *testing.T) {
	for _, command := range []string{"preflight", "apply"} {
		for _, mode := range []string{"strict", "compatible"} {
			var out, errOut bytes.Buffer
			code := run([]string{command, "--baseline-mode", mode, "--allow-missing-baseline-proof", "x.polis"}, &out, &errOut)
			if code != exitUsage {
				t.Fatalf("command=%s mode=%s code=%d stdout=%s stderr=%s", command, mode, code, out.String(), errOut.String())
			}
			if !strings.Contains(errOut.String(), "requires --baseline-mode permissive") {
				t.Fatalf("command=%s mode=%s stderr=%q", command, mode, errOut.String())
			}
		}
	}
}

func TestBaselineReportingIncludesStrictMode(t *testing.T) {
	result := packageapply.Result{
		BaselineMode:          packageapply.BaselineModeStrict,
		BaselineSource:        packageapply.BaselineSourceLocal,
		BaselineAncestry:      packageapply.BaselineAncestryExact,
		ConsumerBaseCommit:    "consumer",
		BaselineCompatibility: "accepted exact artifact baseline",
	}
	payload := map[string]any{}
	addBaselineResultFields(payload, result)
	if payload["baseline_mode"] != packageapply.BaselineModeStrict || payload["baseline_source"] != packageapply.BaselineSourceLocal || payload["baseline_ancestry"] != packageapply.BaselineAncestryExact {
		t.Fatalf("strict baseline fields missing or incorrect: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || active {
		t.Fatalf("strict override_active=%v", payload["override_active"])
	}

	var out bytes.Buffer
	writeBaselineResultText(&out, result)
	for _, fragment := range []string{"Baseline mode: strict", "Baseline source: local", "Baseline ancestry: exact"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("strict text output missing %q: %s", fragment, out.String())
		}
	}
}

func TestBaselineOverrideReportingIsExplicit(t *testing.T) {
	result := packageapply.Result{
		BaselineMode:          packageapply.BaselineModePermissive,
		BaselineSource:        packageapply.BaselineSourceOverridden,
		BaselineAncestry:      packageapply.BaselineAncestryUnproven,
		ConsumerBaseCommit:    "consumer",
		BaselineCompatibility: "accepted with reduced baseline proof",
		OverrideActive:        true,
		BypassedGuarantees:    []string{"locked baseline behavior replay", "Red proof reconstruction"},
		Warnings:              []string{"locked producer baseline proof is unavailable and explicitly waived"},
	}
	payload := map[string]any{}
	addBaselineResultFields(payload, result)
	if payload["baseline_mode"] != packageapply.BaselineModePermissive || payload["baseline_source"] != packageapply.BaselineSourceOverridden || payload["baseline_ancestry"] != packageapply.BaselineAncestryUnproven {
		t.Fatalf("unexpected baseline fields: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || !active {
		t.Fatalf("override_active=%v", payload["override_active"])
	}
	bypassed, ok := payload["bypassed_guarantees"].([]string)
	if !ok || len(bypassed) != 2 {
		t.Fatalf("bypassed_guarantees=%T %v", payload["bypassed_guarantees"], payload["bypassed_guarantees"])
	}

	var out bytes.Buffer
	writeBaselineResultText(&out, result)
	for _, fragment := range []string{"Baseline source: overridden", "Baseline ancestry: unproven", "WARNING: USER SAFETY OVERRIDE ACTIVE", "Bypassed guarantee: locked baseline behavior replay", "Warning: locked producer baseline proof is unavailable"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("text output missing %q: %s", fragment, out.String())
		}
	}
}

func TestRunRejectsInvalidBaselineMode(t *testing.T) {
	for _, command := range []string{"preflight", "apply"} {
		var out, errOut bytes.Buffer
		if code := run([]string{command, "--baseline-mode", "force", "x.polis"}, &out, &errOut); code != exitUsage {
			t.Fatalf("command=%s code=%d stdout=%s stderr=%s", command, code, out.String(), errOut.String())
		}
		if !strings.Contains(errOut.String(), "strict, compatible, or permissive") {
			t.Fatalf("command=%s stderr=%q", command, errOut.String())
		}
	}
}
