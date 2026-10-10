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
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/changestatus"
	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
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

func TestRunDoctor(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"doctor"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS doctor 6.10.0") {
		t.Fatalf("doctor version mismatch: stdout=%q", out.String())
	}
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

func TestRunStatusReportsPersistedPartialAndCompleteWorkflow(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)

	empty := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if empty["state"] != changestatus.StateEmpty || empty["retention_mode"] != "repository" {
		t.Fatalf("empty status=%v", empty)
	}
	next, ok := empty["next_action"].(map[string]any)
	if !ok || next["action"] != "start" || next["command"] != "polis start" {
		t.Fatalf("empty next action=%v", empty["next_action"])
	}

	contract := lockedCLIContract(t, repo)
	locked := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if locked["state"] != changestatus.StateImplementationPending || locked["consistent"] != true {
		t.Fatalf("locked status=%v", locked)
	}
	assertStatusNextAction(t, locked, "implement", "")
	assertCLITextContains(t, []string{"status", "--repo", repo}, "POLIS STATUS: implementation_pending", "Next action: implement")
	contractSummary, ok := locked["contract"].(map[string]any)
	if !ok || contractSummary["schema_version"] != float64(spec.LockedChangeContractSchemaVersion) || contractSummary["path"] == "" {
		t.Fatalf("locked contract summary=%v", locked["contract"])
	}
	baseline, ok := locked["baseline"].(map[string]any)
	if !ok || baseline["repository_relation"] != "at_baseline" || baseline["policy_status"] != "match" {
		t.Fatalf("locked baseline=%v", locked["baseline"])
	}
	assertStatusGate(t, locked, "test.complete", changestatus.StageMissing)

	planPath := filepath.Join(t.TempDir(), "status-plan.json")
	if result := runCLIJSON(t, "implementation-plan", "--repo", repo, "--contract", contract, "--out", planPath, "--format", "json"); result["status"] != "PASS" {
		t.Fatalf("implementation-plan result=%v", result)
	}
	planned := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	plan, ok := planned["implementation_plan"].(map[string]any)
	if !ok || plan["status"] != changestatus.StageComplete {
		t.Fatalf("planned status=%v", planned)
	}
	if planned["state"] != changestatus.StateImplementationPending {
		t.Fatalf("implementation plan must not imply implementation completion: %v", planned)
	}
	assertStatusNextAction(t, planned, "implement", "")

	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedButUnpackaged := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if changedButUnpackaged["state"] != changestatus.StateImplementationPending {
		t.Fatalf("source changes must not prove implementation completion: %v", changedButUnpackaged)
	}
	assertStatusNextAction(t, changedButUnpackaged, "implement", "")
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "polis", Change: "status-complete", Out: t.TempDir(), Contract: contract})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.RetainedPaths) == 0 {
		t.Fatal("build did not retain package state")
	}
	complete := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if complete["state"] != changestatus.StateComplete {
		t.Fatalf("complete status=%v", complete)
	}
	pkg, ok := complete["package"].(map[string]any)
	if !ok || pkg["status"] != changestatus.StageComplete || pkg["target_tree"] != built.TargetTree {
		t.Fatalf("package status=%v", complete["package"])
	}
	evidence, ok := complete["evidence"].(map[string]any)
	if !ok || evidence["status"] != changestatus.StageComplete {
		t.Fatalf("evidence status=%v", complete["evidence"])
	}
	assertStatusGate(t, complete, "test.complete", changestatus.StageComplete)
	assertCLITextContains(t, []string{"status", "--repo", repo}, "POLIS STATUS: complete", "Contract:", "Baseline commit:", "Evidence: complete", "Next action: none")
}

func TestRunStatusReconstructsExternalWorkflowArtifacts(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := externalCLIPolicy(t, repo)
	contract := lockedCLIContract(t, repo, policyPath)
	planPath := filepath.Join(t.TempDir(), "plan.json")
	planResult := runCLIJSON(t, "implementation-plan", "--repo", repo, "--policy", policyPath, "--contract", contract, "--out", planPath, "--format", "json")
	if planResult["status"] != "PASS" {
		t.Fatalf("implementation-plan result=%v", planResult)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("implemented checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "workspace.json")
	report := runCLIJSON(t, "workspace", "validate", "--repo", repo, "--policy", policyPath, "--contract", contract, "--implementation-plan", planPath, "--out-report", reportPath, "--format", "json")
	if report["status"] != "PASS" {
		t.Fatalf("workspace report=%v", report)
	}

	status := runCLIJSON(t, "status", "--repo", repo, "--policy", policyPath, "--contract", contract, "--implementation-plan", planPath, "--report", reportPath, "--format", "json")
	if status["retention_mode"] != "external" || status["state"] != changestatus.StateImplementationPending {
		t.Fatalf("external status=%v", status)
	}
	plan := status["implementation_plan"].(map[string]any)
	if plan["status"] != changestatus.StageComplete {
		t.Fatalf("external plan status=%v", plan)
	}
	workspace := status["workspace"].(map[string]any)
	if workspace["checkpoint_state"] != "source_snapshot_matches" || workspace["report_authenticated"] != false || workspace["current_validation_established"] != false {
		t.Fatalf("workspace projection=%v", workspace)
	}
	if !statusStringSliceContains(status["proven"], "Implementation Plan validates") {
		t.Fatalf("external status did not report validated plan: %v", status["proven"])
	}
	if !statusStringSliceContains(status["stale_or_unproven"], "unsigned historical evidence") {
		t.Fatalf("external status overclaimed workspace checkpoint: %v", status["stale_or_unproven"])
	}
	if !statusStringSliceContains(status["missing"], "verified POLIS package") {
		t.Fatalf("external status omitted missing package: %v", status["missing"])
	}

	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("implemented package\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: repo, Policy: policyPath, Project: "polis", Change: "external-status", Out: t.TempDir(),
		Contract: contract, ImplementationPlan: planPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	complete := runCLIJSON(t, "status", "--repo", repo, "--policy", policyPath, "--contract", contract, "--implementation-plan", planPath, "--report", reportPath, "--package", built.Path, "--format", "json")
	if complete["state"] != changestatus.StateComplete || complete["consistent"] != true {
		t.Fatalf("complete external status=%v", complete)
	}
	if !statusStringSliceContains(complete["proven"], "POLIS package verified") {
		t.Fatalf("verified package not projected as proven: %v", complete["proven"])
	}
	if !statusStringSliceContains(complete["stale_or_unproven"], "workspace checkpoint is stale") {
		t.Fatalf("stale workspace checkpoint not projected: %v", complete["stale_or_unproven"])
	}
	if missing, ok := complete["missing"].([]any); !ok || len(missing) != 0 {
		t.Fatalf("complete external workflow still reports missing proof: %v", complete["missing"])
	}

	packageOnly := runCLIJSON(t, "status", "--repo", repo, "--package", built.Path, "--format", "json")
	if packageOnly["state"] != changestatus.StateComplete {
		t.Fatalf("package-only status=%v", packageOnly)
	}
	packageWithReport := runCLIJSON(t, "status", "--repo", repo, "--policy", policyPath, "--report", reportPath, "--package", built.Path, "--format", "json")
	if packageWithReport["state"] != changestatus.StateComplete {
		t.Fatalf("package/report status=%v", packageWithReport)
	}
	packageWorkspace := packageWithReport["workspace"].(map[string]any)
	if packageWorkspace["checkpoint_state"] != "source_snapshot_differs" {
		t.Fatalf("package/report checkpoint=%v", packageWorkspace)
	}
	mismatchedPolicy := mismatchedCLIPolicy(t, repo)
	mismatched, code := runCLIJSONCode(t, []string{"status", "--repo", repo, "--policy", mismatchedPolicy, "--package", built.Path, "--format", "json"})
	if code != exitValidationFailed || mismatched["state"] != changestatus.StateInconsistent {
		t.Fatalf("package/mismatched-policy status=%v code=%d", mismatched, code)
	}
	if !statusStringSliceContains(mismatched["stale_or_unproven"], "explicit Project Policy does not match") {
		t.Fatalf("mismatched explicit policy was not projected: %v", mismatched["stale_or_unproven"])
	}
	assertCLITextContains(t,
		[]string{"status", "--repo", repo, "--policy", policyPath, "--contract", contract, "--implementation-plan", planPath, "--report", reportPath, "--package", built.Path},
		"Where: complete", "Proven:", "POLIS package verified", "Stale/unproven:", "workspace checkpoint is stale", "Missing: none", "Next action: none")
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

func TestRunStatusMarksInvalidContractBoundPlanIncompleteAndIgnoresUnrelatedPlan(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contractPath := lockedCLIContract(t, repo)
	planPath := filepath.Join(t.TempDir(), "status-plan.json")
	planResult := runCLIJSON(t, "implementation-plan", "--repo", repo, "--contract", contractPath, "--out", planPath, "--format", "json")
	if planResult["status"] != "PASS" {
		t.Fatalf("implementation-plan result=%v", planResult)
	}
	retainedPlans, ok := planResult["retained_paths"].([]any)
	if !ok || len(retainedPlans) != 1 {
		t.Fatalf("implementation-plan retained paths=%v", planResult["retained_paths"])
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan spec.ImplementationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Objective = ""
	invalidBound, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	foreignPlan := plan
	foreignPlan.ChangeContractSHA256 = strings.Repeat("0", 64)
	foreignPlanRaw, err := json.Marshal(foreignPlan)
	if err != nil {
		t.Fatal(err)
	}
	retention, err := artifactretention.Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	invalidPath, err := retention.Publish(repo, "plans", invalidBound)
	if err != nil {
		t.Fatal(err)
	}
	foreignPath, err := retention.Publish(repo, "plans", foreignPlanRaw)
	if err != nil {
		t.Fatal(err)
	}

	status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	stage, ok := status["implementation_plan"].(map[string]any)
	if !ok || stage["status"] != changestatus.StageComplete {
		t.Fatalf("valid linked plan should remain complete beside an incomplete candidate: %v", status["implementation_plan"])
	}
	incomplete, ok := stage["incomplete_paths"].([]any)
	if !ok || len(incomplete) != 1 || incomplete[0] != invalidPath || incomplete[0] == foreignPath {
		t.Fatalf("incomplete plan paths=%v, expected only linked invalid plan %q", stage["incomplete_paths"], invalidPath)
	}
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(retainedPlans[0].(string)))); err != nil {
		t.Fatal(err)
	}
	status = runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	stage = status["implementation_plan"].(map[string]any)
	if stage["status"] != changestatus.StageIncomplete {
		t.Fatalf("invalid linked plan should be incomplete when no valid plan remains: %v", stage)
	}
	if status["state"] != changestatus.StateImplementationPending {
		t.Fatalf("an optional incomplete plan must not imply completion or block implementation: %v", status)
	}
}

func assertStatusNextAction(t *testing.T, status map[string]any, action, command string) {
	t.Helper()
	next, ok := status["next_action"].(map[string]any)
	commandValue, _ := next["command"].(string)
	if !ok || next["action"] != action || commandValue != command {
		t.Fatalf("next action=%v, want action %q command %q", status["next_action"], action, command)
	}
}

func TestRunStatusReportsDeferredGatesAsConsumerValidationRequired(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: repo, Project: "polis", Change: "status-deferred", Out: t.TempDir(), Contract: contract, DeferredGates: []string{"coverage"},
	}); err != nil {
		t.Fatal(err)
	}
	status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if status["state"] != changestatus.StateConsumerValidationRequired {
		t.Fatalf("deferred status=%v", status)
	}
	assertStatusGate(t, status, "coverage", "deferred")
	next, ok := status["next_action"].(map[string]any)
	if !ok || next["action"] != "consumer_validation" || next["command"] != "polis preflight" {
		t.Fatalf("deferred next action=%v", status["next_action"])
	}
}

func TestRunStatusBlocksWhenLockedBaselineIsNoLongerAnAncestor(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	_ = lockedCLIContract(t, repo)
	tree := cliGit(t, repo, "rev-parse", "HEAD^{tree}")
	commit := cliGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit-tree", tree, "-m", "diverged root")
	if output, err := exec.Command("git", "-C", repo, "reset", "--hard", commit).CombinedOutput(); err != nil {
		t.Fatalf("reset to divergent root: %v\n%s", err, output)
	}
	status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if status["state"] != changestatus.StateBlocked || status["consistent"] != true {
		t.Fatalf("diverged status=%v", status)
	}
	baseline, ok := status["baseline"].(map[string]any)
	if !ok || baseline["repository_relation"] != "diverged" || baseline["resolvable"] != true {
		t.Fatalf("diverged baseline=%v", status["baseline"])
	}
	if status["next_action"] != nil {
		t.Fatalf("diverged next action=%v", status["next_action"])
	}
}

func TestRunStatusDoesNotInferExternalOutputsAsPersistedState(t *testing.T) {
	repo := makeBuildRepo(t)
	_ = lockedCLIContract(t, repo)
	status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if status["state"] != changestatus.StateUnavailable || status["retention_mode"] != "external" || status["contract"] != nil {
		t.Fatalf("external status=%v", status)
	}
}

func TestRunStatusRequiresExplicitContractForAmbiguousPersistedState(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	retained := make([]string, 0, 2)
	for _, message := range []string{"feat(status): first candidate", "feat(status): second candidate"} {
		draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, &message)
		result, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draft, Out: filepath.Join(t.TempDir(), "locked.json")})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.RetainedPaths) != 1 {
			t.Fatalf("retained paths=%v", result.RetainedPaths)
		}
		retained = append(retained, result.RetainedPaths[0])
	}

	ambiguous, code := runCLIJSONCode(t, []string{"status", "--repo", repo, "--format", "json"})
	if code != exitValidationFailed || ambiguous["state"] != changestatus.StateAmbiguous || ambiguous["consistent"] != false {
		t.Fatalf("ambiguous code=%d status=%v", code, ambiguous)
	}
	candidates, ok := ambiguous["candidate_contracts"].([]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("candidate contracts=%v", ambiguous["candidate_contracts"])
	}

	selected := runCLIJSON(t, "status", "--repo", repo, "--contract", retained[0], "--format", "json")
	if selected["state"] != changestatus.StateImplementationPending || selected["consistent"] != true {
		t.Fatalf("selected status=%v", selected)
	}
	assertStatusNextAction(t, selected, "implement", "")
}

func TestRunStatusFailsClosedOnTamperedOrSymlinkedRetainedContract(t *testing.T) {
	for _, mode := range []string{"tampered", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			repo := makeBuildRepo(t)
			enableCLIRetention(t, repo)
			_ = lockedCLIContract(t, repo)
			initial := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
			contract := initial["contract"].(map[string]any)
			retainedPath := filepath.Join(repo, filepath.FromSlash(contract["path"].(string)))
			switch mode {
			case "tampered":
				if err := os.WriteFile(retainedPath, []byte("tampered\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "contract.json")
				raw, err := os.ReadFile(retainedPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, raw, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(retainedPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, retainedPath); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			status, code := runCLIJSONCode(t, []string{"status", "--repo", repo, "--format", "json"})
			if code != exitValidationFailed || status["state"] != changestatus.StateInconsistent || status["consistent"] != false {
				t.Fatalf("mode=%s code=%d status=%v", mode, code, status)
			}
		})
	}
}

func TestRunImplementationPlanHelpAndRequiredArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "implementation-plan"}, &out, &errOut); code != exitPass {
		t.Fatalf("help code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"polis implementation-plan --repo <path>", "--contract <locked-v4-or-v6.json>", "--out <external-plan.json>", "--format text|json"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("implementation-plan help missing %q: %s", fragment, out.String())
		}
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"implementation-plan"}, &out, &errOut); code != exitUsage {
		t.Fatalf("missing-argument code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "usage: polis implementation-plan") {
		t.Fatalf("missing-argument usage=%q", errOut.String())
	}
}

func TestRunImplementationPlanProducesTextAndJSON(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			repo := makeBuildRepo(t)
			policyPath := filepath.Join(t.TempDir(), "policy.json")
			policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policyPath, policyRaw, 0o600); err != nil {
				t.Fatal(err)
			}
			contract := lockedCLIContract(t, repo, policyPath)
			planPath := filepath.Join(t.TempDir(), "implementation-plan.json")
			args := []string{"implementation-plan", "--repo", repo, "--policy", policyPath, "--contract", contract, "--out", planPath, "--format", format}
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != exitPass {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			planRaw, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := spec.DecodeImplementationPlan(planRaw)
			if err != nil {
				t.Fatalf("generated plan is invalid: %v", err)
			}
			if plan.Strategy != spec.ImplementationPlanStrategyGreenGreen || len(plan.Steps) == 0 {
				t.Fatalf("unexpected generated plan: %+v", plan)
			}
			if format == "text" {
				for _, fragment := range []string{"POLIS IMPLEMENTATION PLAN: PASS", "Strategy: Green -> Green", "Requirements: 1", "Acceptance criteria: 1", "Steps:", "PLAN-001  TEST", "PLAN-002  IMPLEMENTATION", planPath, "SHA256:"} {
					if !strings.Contains(out.String(), fragment) {
						t.Errorf("text output missing %q: %s", fragment, out.String())
					}
				}
				if strings.Contains(out.String(), policyPath) {
					t.Fatalf("output leaked external policy path %q: %s", policyPath, out.String())
				}
				return
			}
			var payload struct {
				Status string                  `json:"status"`
				Path   string                  `json:"plan_path"`
				SHA256 string                  `json:"sha256"`
				Plan   spec.ImplementationPlan `json:"plan"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("decode JSON output: %v: %s", err, out.String())
			}
			if payload.Status != "PASS" || payload.Path != planPath || payload.SHA256 == "" || payload.Plan.Strategy != plan.Strategy || len(payload.Plan.Steps) != len(plan.Steps) {
				t.Fatalf("unexpected JSON result: %+v", payload)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["retained_paths"]; exists {
				t.Fatalf("legacy external mode added retained_paths: %s", out.String())
			}
		})
	}
}

func TestRunImplementationPlanReportsRetainedPath(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contract := lockedCLIContract(t, repo)
	planPath := filepath.Join(t.TempDir(), "implementation-plan.json")
	var out, errOut bytes.Buffer
	args := []string{"implementation-plan", "--repo", repo, "--contract", contract, "--out", planPath, "--format", "json"}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var payload struct {
		Status        string   `json:"status"`
		RetainedPaths []string `json:"retained_paths"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON output: %v: %s", err, out.String())
	}
	if payload.Status != "PASS" || len(payload.RetainedPaths) != 1 || !strings.HasPrefix(payload.RetainedPaths[0], ".polis/artifacts/plans/") {
		t.Fatalf("unexpected retained plan result: %+v; output=%s", payload, out.String())
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(payload.RetainedPaths[0]))); err != nil {
		t.Fatalf("CLI reported missing retained plan: %v", err)
	}
}

func TestRunPlannedGreenGreenCommands(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	planPath := filepath.Join(t.TempDir(), "implementation-plan.json")
	planResult := runCLIJSON(t, "implementation-plan", "--repo", repo, "--contract", contract, "--out", planPath, "--format", "json")
	assertJSONFields(t, planResult, map[string]string{"status": "PASS", "plan_path": planPath})
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := spec.DecodeImplementationPlan(planRaw)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != spec.ImplementationPlanStrategyGreenGreen {
		t.Fatalf("CLI created strategy=%q", plan.Strategy)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("planned change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	runCLIJSON(t, "build", "--repo", repo, "--project", "polis", "--change", "planned-green-green", "--contract", contract, "--implementation-plan", planPath, "--format", "json", "--out", outDir)
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".polis") {
		t.Fatalf("planned build output=%v err=%v", entries, err)
	}
	artifact := filepath.Join(outDir, entries[0].Name())
	verify := runCLIJSON(t, "verify", "--format", "json", artifact)
	assertJSONFields(t, verify, map[string]string{"status": "PASS"})
	inspection := runCLIJSON(t, "inspect", "--format", "json", artifact)
	if present, ok := inspection["implementation_plan_present"].(bool); !ok || !present {
		t.Fatalf("planned inspect presence=%v", inspection["implementation_plan_present"])
	}
	if inspection["implementation_plan_strategy"] != spec.ImplementationPlanStrategyGreenGreen || inspection["implementation_plan_step_count"] != float64(len(plan.Steps)) {
		t.Fatalf("planned inspect metadata=%v", inspection)
	}
	traceability, ok := inspection["implementation_plan_traceability"].([]any)
	if !ok || len(traceability) != 1 {
		t.Fatalf("planned inspect traceability=%v", inspection["implementation_plan_traceability"])
	}
	trace, ok := traceability[0].(map[string]any)
	if !ok || trace["requirement_id"] != "REQ-001" || trace["acceptance_criterion_id"] != "AC-001" || trace["proof"] != spec.ProofGateRegression {
		t.Fatalf("planned inspect trace=%v", traceability[0])
	}
	testStepIDs, ok := trace["test_plan_step_ids"].([]any)
	if !ok || len(testStepIDs) != 2 || testStepIDs[0] != "PLAN-001" || testStepIDs[1] != "PLAN-003" {
		t.Fatalf("planned inspect test step references=%v", trace["test_plan_step_ids"])
	}
	implementationStepIDs, ok := trace["implementation_plan_step_ids"].([]any)
	if !ok || len(implementationStepIDs) != 1 || implementationStepIDs[0] != "PLAN-002" {
		t.Fatalf("planned inspect implementation step references=%v", trace["implementation_plan_step_ids"])
	}
	proofSteps, ok := trace["proof_plan_steps"].([]any)
	if !ok || len(proofSteps) != 2 {
		t.Fatalf("planned inspect proof step references=%v", trace["proof_plan_steps"])
	}
	for i, wantID := range []string{"PLAN-001", "PLAN-003"} {
		proofStep, ok := proofSteps[i].(map[string]any)
		if !ok || proofStep["id"] != wantID || proofStep["kind"] != spec.ImplementationPlanStepTest {
			t.Fatalf("planned inspect proof step %d=%v", i, proofSteps[i])
		}
	}
	assertCLITextContains(t, []string{"inspect", artifact}, "Implementation plan: true", "Plan schema: 1", "Plan strategy: green_green", "Plan trace: REQ-001 -> AC-001; proof=regression; test steps=PLAN-001, PLAN-003; implementation steps=PLAN-002; proof steps=PLAN-001 (test), PLAN-003 (test)")
	if output, err := exec.Command("git", "-C", repo, "restore", "--", "app.txt").CombinedOutput(); err != nil {
		t.Fatalf("restore source before consumer validation: %v\n%s", err, output)
	}
	preflight := runCLIJSON(t, "preflight", "--repo", repo, "--format", "json", artifact)
	assertJSONFields(t, preflight, map[string]string{"status": "PASS"})
	apply := runCLIJSON(t, "apply", "--repo", repo, "--format", "json", artifact)
	assertJSONFields(t, apply, map[string]string{"status": "PASS"})
	assertFileContents(t, filepath.Join(repo, "app.txt"), "planned change\n")
}

func TestCaptureRedAndBuildAcceptImplementationPlanFlag(t *testing.T) {
	for _, command := range [][]string{
		{"capture-red", "--repo", t.TempDir(), "--contract", "missing.json", "--implementation-plan", "missing-plan.json", "--out", filepath.Join(t.TempDir(), "red.patch")},
		{"build", "--repo", t.TempDir(), "--project", "polis", "--change", "planned", "--contract", "missing.json", "--implementation-plan", "missing-plan.json", "--out", t.TempDir()},
	} {
		var out, errOut bytes.Buffer
		_ = run(command, &out, &errOut)
		if strings.Contains(errOut.String(), "flag provided but not defined") {
			t.Fatalf("command rejected optional implementation-plan flag: %s", errOut.String())
		}
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

func TestRunGatesReportsConfiguredResultsWithoutArtifact(t *testing.T) {
	repo := makeBuildRepo(t)
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			args := []string{"gates", "--repo", repo, "--format", format}
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != exitPass {
				t.Fatalf("RED: gates command should execute configured project gates without a delivery artifact: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			if format == "text" {
				for _, fragment := range []string{
					"POLIS GATES: PASS",
					"Gate test.complete: PASS (executed)",
					"Gate coverage: PASS (executed)",
					"Gate lint: NOT_APPLICABLE (not executed)",
					"Delivery artifact: not built or verified",
					"Gate validation does not mean a delivery artifact was built or verified.",
				} {
					if !strings.Contains(out.String(), fragment) {
						t.Errorf("text gates output missing %q: %s", fragment, out.String())
					}
				}
				return
			}

			var report struct {
				Status                   string   `json:"status"`
				ValidationOnly           bool     `json:"validation_only"`
				ExecutedGates            []string `json:"executed_gates"`
				DeliveryArtifactBuilt    bool     `json:"delivery_artifact_built"`
				DeliveryArtifactVerified bool     `json:"delivery_artifact_verified"`
				DeliveryArtifactNotice   string   `json:"delivery_artifact_notice"`
				GateResults              []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Executed bool   `json:"executed"`
				} `json:"gate_results"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("invalid gates JSON: %v\n%s", err, out.String())
			}
			if report.Status != string(spec.StatusPass) || !report.ValidationOnly || !slices.Equal(report.ExecutedGates, []string{"test.complete", "coverage"}) || report.DeliveryArtifactBuilt || report.DeliveryArtifactVerified || report.DeliveryArtifactNotice != "Gate validation does not mean a delivery artifact was built or verified." {
				t.Fatalf("gates report=%+v", report)
			}
			statuses := make(map[string]struct {
				status   string
				executed bool
			}, len(report.GateResults))
			for _, result := range report.GateResults {
				statuses[result.ID] = struct {
					status   string
					executed bool
				}{status: result.Status, executed: result.Executed}
			}
			if len(statuses) != len(spec.ProjectGateOrder) || statuses["test.complete"].status != string(spec.StatusPass) || !statuses["test.complete"].executed || statuses["coverage"].status != string(spec.StatusPass) || !statuses["coverage"].executed || statuses["lint"].status != string(spec.StatusNotApplicable) || statuses["lint"].executed {
				t.Fatalf("gate results=%+v", report.GateResults)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(repo, ".polis", "artifacts", "packages")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-only command created a delivery package directory: stat err=%v", err)
	}
}

func TestIssue14GateHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "missing-dependency":
		_, _ = os.Stderr.WriteString("ModuleNotFoundError: No module named 'pytest'\n")
		os.Exit(1)
	case "assertion":
		_, _ = os.Stderr.WriteString("assertion failed: expected true\n")
		os.Exit(1)
	default:
		os.Exit(9)
	}
}

func TestRunGatesExplainsUnrunChecksAndPreservesAssertions(t *testing.T) {
	for _, tc := range []struct {
		name, missing string
		argv          []string
		wantStatus    spec.Status
		wantExecuted  bool
	}{
		{"missing executable", "missing executable", []string{"polis-issue14-no-such-command"}, spec.StatusBlocked, false},
		{"missing dependency", "missing dependency", []string{os.Args[0], "-test.run=^TestIssue14GateHelper$", "--", "missing-dependency"}, spec.StatusBlocked, false},
		{"real assertion", "", []string{os.Args[0], "-test.run=^TestIssue14GateHelper$", "--", "assertion"}, spec.StatusFail, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := makeBuildRepo(t)
			var policy spec.Policy
			if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
				t.Fatal(err)
			}
			for i := range policy.Gates {
				if policy.Gates[i].ID == "test.complete" {
					policy.Gates[i].Command.Argv = tc.argv
				}
			}
			raw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(t.TempDir(), "issue14-policy.json")
			if err := os.WriteFile(policyPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"json", "text"} {
				var out, errOut bytes.Buffer
				code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--format", format}, &out, &errOut)
				if code == exitPass {
					t.Fatalf("gate unexpectedly passed: %s", out.String())
				}
				if format == "text" {
					if tc.missing != "" && (!strings.Contains(out.String(), tc.missing) || !strings.Contains(out.String(), "intended checks did not run") || !strings.Contains(out.String(), "not executed")) {
						t.Fatalf("text did not explain prerequisite: %s", out.String())
					}
					continue
				}
				var report gatesCLIResult
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatalf("invalid JSON: %v\n%s", err, out.String())
				}
				if report.Status != tc.wantStatus {
					t.Fatalf("status=%s want=%s report=%+v", report.Status, tc.wantStatus, report)
				}
				for _, gate := range report.GateResults {
					if gate.ID != "test.complete" {
						continue
					}
					if gate.Status != tc.wantStatus || gate.Executed != tc.wantExecuted || !strings.Contains(gate.Reason, tc.missing) {
						t.Fatalf("gate=%+v", gate)
					}
					if tc.missing != "" && !strings.Contains(gate.Reason, "intended checks did not run") {
						t.Fatalf("blocked gate omitted unrun checks: %+v", gate)
					}
				}
			}
		})
	}
}

func TestRunGatesReturnsFailureWithPerGateResults(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		argv         []string
		overall      spec.Status
		gateStatus   spec.Status
		gateExecuted bool
		exitCode     int
		executed     []string
	}{
		{name: "failed process", argv: []string{"git", "polis-test-command-that-does-not-exist"}, overall: spec.StatusFail, gateStatus: spec.StatusFail, gateExecuted: true, exitCode: exitValidationFailed, executed: []string{"test.complete"}},
		{name: "blocked process start", argv: []string{"polis-issue11-no-such-executable"}, overall: spec.StatusBlocked, gateStatus: spec.StatusBlocked, gateExecuted: false, exitCode: exitBlocked, executed: []string{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := makeBuildRepo(t)
			policy := spec.Policy{}
			if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
				t.Fatal(err)
			}
			for i := range policy.Gates {
				if policy.Gates[i].ID == "test.complete" {
					policy.Gates[i].Command.Argv = scenario.argv
				}
			}
			policyRaw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			policyPath := filepath.Join(t.TempDir(), "failing-policy.json")
			if err := os.WriteFile(policyPath, append(policyRaw, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--format", "json"}, &out, &errOut)
			if code != scenario.exitCode {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			var report struct {
				Status                   string   `json:"status"`
				ExecutedGates            []string `json:"executed_gates"`
				DeliveryArtifactBuilt    bool     `json:"delivery_artifact_built"`
				DeliveryArtifactVerified bool     `json:"delivery_artifact_verified"`
				DeliveryArtifactNotice   string   `json:"delivery_artifact_notice"`
				GateResults              []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Executed bool   `json:"executed"`
				} `json:"gate_results"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("invalid gates failure JSON: %v\n%s", err, out.String())
			}
			statuses := make(map[string]struct {
				status   string
				executed bool
			}, len(report.GateResults))
			for _, result := range report.GateResults {
				statuses[result.ID] = struct {
					status   string
					executed bool
				}{status: result.Status, executed: result.Executed}
			}
			if report.Status != string(scenario.overall) || !slices.Equal(report.ExecutedGates, scenario.executed) || report.DeliveryArtifactBuilt || report.DeliveryArtifactVerified || report.DeliveryArtifactNotice != "Gate validation does not mean a delivery artifact was built or verified." || statuses["test.complete"].status != string(scenario.gateStatus) || statuses["test.complete"].executed != scenario.gateExecuted || statuses["coverage"].status != string(spec.StatusBlocked) || statuses["coverage"].executed {
				t.Fatalf("failure report=%+v statuses=%v", report, statuses)
			}
		})
	}
}

func TestRunGatesDoesNotMarkCoverageCommandRunWhenReportCleanupFails(t *testing.T) {
	repo := makeBuildRepo(t)
	reportPath := filepath.Join(repo, ".polis", "coverage.out")
	if err := os.Remove(reportPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(reportPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportPath, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"gates", "--repo", repo, "--format", "json"}, &out, &errOut)
	if code != exitValidationFailed {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	// Provenance now rejects a tracked source file replaced by a directory
	// before any command runs, rather than reaching coverage cleanup.
	if out.Len() != 0 || !strings.Contains(errOut.String(), "not a regular file") {
		t.Fatalf("invalid source did not fail closed: stdout=%s stderr=%s", out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(reportPath, "keep.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestRunGatesRejectsInvalidArgumentsAndReportsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "gates"}, &out, &errOut); code != exitPass {
		t.Fatalf("help code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"polis gates [--repo <path>]", "run configured project gates", "without building a delivery artifact"} {
		if !strings.Contains(out.String(), fragment) {
			t.Errorf("gates help missing %q: %s", fragment, out.String())
		}
	}
	for _, args := range [][]string{{"gates", "--format", "xml"}, {"gates", "--repo", ".", "unexpected"}} {
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "usage: polis gates") {
			t.Errorf("args=%v code=%d stderr=%s", args, code, errOut.String())
		}
	}

	repo := makeBuildRepo(t)
	missingPolicy := filepath.Join(t.TempDir(), "missing-policy.json")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"gates", "--repo", repo, "--policy", missingPolicy}, &out, &errOut); code != exitValidationFailed || !strings.Contains(errOut.String(), "POLIS GATES: FAIL") || !strings.Contains(errOut.String(), "read external Project Policy") {
		t.Fatalf("missing policy code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
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
	enableCLIRetention(t, repo)
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
	code := run([]string{"build", "--repo", repo, "--policy", policyPath, "--project", "gitrex", "--change", "cli-build", "--contract", contract, "--defer-gate", "coverage", "--jobs", "2", "--out", outDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS BUILD: PASS") || !strings.Contains(out.String(), "Deferred gates: coverage") || !strings.Contains(out.String(), "Consumer validation required: true") || !strings.Contains(out.String(), "DEFERRED") || !strings.Contains(out.String(), "Retained artifacts: .polis/artifacts/") {
		t.Fatalf("stdout=%q", out.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".polis") {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestRunBuildDeferralTextAndJSONReporting(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
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
		RetainedPaths              []string          `json:"retained_paths"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid build JSON: %v\n%s", err, out.String())
	}
	if !result.ConsumerValidationRequired || len(result.DeferredGates) != 1 || result.DeferredGates[0] != "coverage" || result.ProducerGateStatuses["coverage"] != string(spec.StatusDeferred) || len(result.RetainedPaths) != 3 {
		t.Fatalf("build output=%+v", result)
	}
	for _, path := range result.RetainedPaths {
		if !strings.HasPrefix(path, ".polis/artifacts/") {
			t.Errorf("unexpected retained path %q", path)
		}
	}
}

func TestRunBuildRejectsInvalidJobs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"build", "--jobs", "17"}, &out, &errOut); code != exitUsage {
		t.Fatalf("invalid jobs code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "jobs must be between 1 and 16") {
		t.Fatalf("invalid jobs diagnostic missing: %q", errOut.String())
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
	draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, nil)
	contract := filepath.Join(t.TempDir(), "cli-locked-v6.json")
	var startOut, startErr bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--contract", draft, "--out", contract}, &startOut, &startErr); code != exitPass {
		t.Fatalf("start code=%d stderr=%s", code, startErr.String())
	}
	lockedRaw, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	lockedContract, err := spec.DecodeChangeContract(lockedRaw)
	if err != nil {
		t.Fatalf("decode locked schema-v6 contract: %v", err)
	}
	if lockedContract.SchemaVersion != spec.CommitIntentLockedChangeContractSchemaVersion || lockedContract.Commit != nil {
		t.Fatalf("locked contract=%+v; want schema v6 without commit metadata", lockedContract)
	}
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
	verified := runCLIJSON(t, "verify", "--format", "json", artifact)
	if verified["status"] != "PASS" {
		t.Fatalf("verify result=%v", verified)
	}
	inspection := runCLIJSON(t, "inspect", "--format", "json", artifact)
	if commit, exists := inspection["commit"]; exists && commit != nil {
		t.Fatalf("inspect synthesized commit metadata for schema v6 contract without it: %v", commit)
	}
	head := cliGit(t, repo, "rev-parse", "HEAD")
	apply := runCLIJSON(t, "apply", "--repo", repo, "--format", "json", artifact)
	if apply["committed"] != false || apply["commit_sha"] != nil || apply["commit_message"] != nil {
		t.Fatalf("default apply synthesized commit intent: %v", apply)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("default apply moved HEAD: got %s want %s", got, head)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
}

func TestRunApplyAutoCommitsArtifactIntentEndToEnd(t *testing.T) {
	repo := makeBuildRepo(t)
	message := "feat(apply): commit the validated artifact tree\n\nsecond line with preserved spaces  \n"
	draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, &message)
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
	if err := os.WriteFile(filepath.Join(repo, ".polis", "artifact-retention.json"), []byte("{\"schema_version\":1,\"mode\":\"repository\"}\n"), 0o644); err != nil {
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
	if !strings.Contains(startOut.String(), "Retained artifacts: .polis/artifacts/contracts/") {
		t.Fatalf("start did not report retained contract: %s", startOut.String())
	}
	beforeRed := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if beforeRed["state"] != changestatus.StateRedProofMissing {
		t.Fatalf("status before Red=%v", beforeRed)
	}
	redStage, ok := beforeRed["red_proof"].(map[string]any)
	if !ok || redStage["status"] != changestatus.StageMissing {
		t.Fatalf("Red stage before capture=%v", beforeRed["red_proof"])
	}
	next, ok := beforeRed["next_action"].(map[string]any)
	if !ok || next["action"] != "capture_red" || next["command"] != "polis capture-red" {
		t.Fatalf("next action before Red=%v", beforeRed["next_action"])
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
	if !strings.Contains(out.String(), "Retained artifacts: .polis/artifacts/proofs/") {
		t.Fatalf("capture-red did not report retained proof: %s", out.String())
	}
	explicitAfterRed := runCLIJSON(t, "status", "--repo", repo, "--contract", locked, "--regression-patch", outPath, "--format", "json")
	explicitRedStage, ok := explicitAfterRed["red_proof"].(map[string]any)
	if !ok || explicitRedStage["status"] != changestatus.StageComplete || !statusStringSliceContains(explicitAfterRed["proven"], "Red proof validates") {
		t.Fatalf("explicit Red proof was not projected as valid: %v", explicitAfterRed)
	}
	afterRed := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if afterRed["state"] != changestatus.StateImplementationPending {
		t.Fatalf("status after Red=%v", afterRed)
	}
	assertStatusNextAction(t, afterRed, "implement", "")
	redStage, ok = afterRed["red_proof"].(map[string]any)
	if !ok || redStage["status"] != changestatus.StageComplete {
		t.Fatalf("Red stage after capture=%v", afterRed["red_proof"])
	}
	jsonPath := filepath.Join(t.TempDir(), "red-json.patch")
	var jsonOut, jsonErr bytes.Buffer
	if code := run([]string{"capture-red", "--repo", repo, "--contract", locked, "--out", jsonPath, "--format", "json"}, &jsonOut, &jsonErr); code != exitPass {
		t.Fatalf("capture-red JSON code=%d stderr=%s", code, jsonErr.String())
	}
	var jsonResult struct {
		RetainedPaths []string `json:"retained_paths"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &jsonResult); err != nil {
		t.Fatalf("invalid capture-red JSON: %v\n%s", err, jsonOut.String())
	}
	if len(jsonResult.RetainedPaths) != 1 || !strings.HasPrefix(jsonResult.RetainedPaths[0], ".polis/artifacts/proofs/") {
		t.Fatalf("capture-red JSON omitted retained proof: %+v", jsonResult)
	}
	retention, err := artifactretention.Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	validProofPath := filepath.Join(repo, filepath.FromSlash(jsonResult.RetainedPaths[0]))
	validProof, err := os.ReadFile(validProofPath)
	if err != nil {
		t.Fatal(err)
	}
	incompleteProof := bytes.ReplaceAll(validProof, []byte("CLI-RED"), []byte("CLI-GREEN"))
	if bytes.Equal(incompleteProof, validProof) {
		t.Fatal("test fixture did not alter the Red oracle output")
	}
	incompleteProofPath, err := retention.Publish(repo, "proofs", incompleteProof)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedProof := bytes.ReplaceAll(incompleteProof, []byte("regression.txt"), []byte("unrelated.txt"))
	unrelatedProofPath, err := retention.Publish(repo, "proofs", unrelatedProof)
	if err != nil {
		t.Fatal(err)
	}
	withIncompleteCandidate := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	proofStage, ok := withIncompleteCandidate["red_proof"].(map[string]any)
	if !ok || proofStage["status"] != changestatus.StageComplete {
		t.Fatalf("valid proof should remain complete: %v", withIncompleteCandidate["red_proof"])
	}
	incompletePaths, ok := proofStage["incomplete_paths"].([]any)
	if !ok || len(incompletePaths) != 1 || incompletePaths[0] != incompleteProofPath || incompletePaths[0] == unrelatedProofPath {
		t.Fatalf("incomplete proof paths=%v, expected only the related failing proof %q", proofStage["incomplete_paths"], incompleteProofPath)
	}
	if err := os.Remove(validProofPath); err != nil {
		t.Fatal(err)
	}
	withoutValidProof := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	proofStage, ok = withoutValidProof["red_proof"].(map[string]any)
	if !ok || proofStage["status"] != changestatus.StageIncomplete || withoutValidProof["state"] != changestatus.StateRedProofMissing {
		t.Fatalf("related invalid proof should be incomplete and require recapture: %v", withoutValidProof)
	}
	assertStatusNextAction(t, withoutValidProof, "capture_red", "polis capture-red")
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
