package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

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
	if plan.DependencyEdges[0].Provenance.Source != policyplan.DecisionSourceMandatoryDependency || plan.Gates[0].Provenance.State.Source != policyplan.DecisionSourcePolicyExplicit || plan.Gates[2].Provenance.State.Source != policyplan.DecisionSourceNotApplicable {
		t.Fatalf("plan provenance: edges=%+v gates=%+v", plan.DependencyEdges, plan.Gates[:3])
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
	if !strings.Contains(out.String(), "source=mandatory_dependency reason=built_in_prerequisite") || !strings.Contains(out.String(), "state_source=not_applicable state_reason=declared_not_applicable") || !strings.Contains(out.String(), "Validation level: strict (source=policy_default") {
		t.Fatalf("text plan omitted decision provenance: %s", out.String())
	}
}

func TestRunInitAndPlanExposePersistedProfileProvenance(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/plan\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", "go.mod")
	cliGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--repo", repo, "--profile", "go", "--validation-level", "standard", "--disable-gate", "lint"}, &out, &errOut); code != exitPass {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Provenance: ") || !strings.Contains(out.String(), ".polis/policy-provenance.json") {
		t.Fatalf("init did not report provenance path: %s", out.String())
	}
	cliGit(t, repo, "add", ".polis/policy.json", ".polis/policy-provenance.json")
	cliGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "generated policy and provenance")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"plan", "--repo", repo, "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("plan code=%d stderr=%s", code, errOut.String())
	}
	var plan policyplan.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.PlanVersion != 4 || plan.Gates[0].Provenance.Configuration.Source != policyplan.DecisionSourceProfileDefault || plan.Gates[0].Provenance.State.Source != policyplan.DecisionSourceProfileDefault {
		t.Fatalf("generated gate origin=%+v", plan.Gates[0].Provenance)
	}
	if plan.ValidationLevelProvenance != (policyplan.Decision{Source: policyplan.DecisionSourcePolicyExplicit, Reason: "init_explicit_validation_level"}) {
		t.Fatalf("CLI lost explicit validation level: %+v", plan.ValidationLevelProvenance)
	}
	if plan.Gates[1].Provenance.Configuration.Source != policyplan.DecisionSourceProfileDefault || plan.Gates[1].Provenance.State.Source != policyplan.DecisionSourceNotApplicable || plan.Guarantees[1].Provenance.Source != policyplan.DecisionSourceNotApplicable {
		t.Fatalf("default disabled coverage origin=%+v", plan.Gates[1].Provenance)
	}
	if plan.Gates[2].Provenance.Configuration.Reason != "init_explicit_disable_gate" || plan.Gates[2].Provenance.Configuration.Source != policyplan.DecisionSourcePolicyExplicit || plan.DependencyEdges[0].Provenance.Source != policyplan.DecisionSourceMandatoryDependency {
		t.Fatalf("override/dependency origin=%+v edges=%+v", plan.Gates[2].Provenance, plan.DependencyEdges)
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"plan", "--repo", repo, "--format", "text"}, &out, &errOut); code != exitPass {
		t.Fatalf("text plan code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "configuration_source=profile_default") || !strings.Contains(out.String(), "configuration_reason=init_explicit_disable_gate") || !strings.Contains(out.String(), "source=mandatory_dependency") || !strings.Contains(out.String(), "Validation level: standard (source=policy_explicit reason=init_explicit_validation_level)") {
		t.Fatalf("text plan lacks generated provenance: %s", out.String())
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
	if plan.Gates[1].Provenance.ProducerAction.Source != policyplan.DecisionSourceExecutorDeferral || plan.Gates[1].Provenance.State.Source != policyplan.DecisionSourcePolicyExplicit || plan.Guarantees[1].Provenance.Source != policyplan.DecisionSourceExecutorDeferral {
		t.Fatalf("deferred decision provenance=%+v guarantee=%+v", plan.Gates[1], plan.Guarantees[1])
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
	if !strings.Contains(out.String(), "producer_source=executor_deferral producer_reason=requested_by_executor") || !strings.Contains(out.String(), "consumer_source=executor_deferral consumer_reason=deferred_gate_requires_consumer_validation") {
		t.Fatalf("text plan omitted deferral provenance: %s", out.String())
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
	if plan.Gates[0].Provenance.Configuration.Source != policyplan.DecisionSourcePolicyExplicit {
		t.Fatalf("external policy claimed generated-profile provenance: %+v", plan.Gates[0].Provenance)
	}
	if strings.Contains(out.String(), policyPath) {
		t.Fatalf("external policy pathname leaked into plan output: %q", policyPath)
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
