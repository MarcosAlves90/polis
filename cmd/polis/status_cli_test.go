package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/changestatus"
	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/spec"
)

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
		t.Fatal("build did not retain workflow evidence")
	}
	for _, path := range built.RetainedPaths {
		if strings.HasPrefix(path, ".polis/artifacts/packages/") {
			t.Fatalf("build retained delivery package in repository: %s", path)
		}
	}
	withoutExplicitPackage := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
	if withoutExplicitPackage["state"] != changestatus.StateImplementationPending {
		t.Fatalf("external package was inferred as persisted state: %v", withoutExplicitPackage)
	}
	complete := runCLIJSON(t, "status", "--repo", repo, "--package", built.Path, "--format", "json")
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
	assertCLITextContains(t, []string{"status", "--repo", repo, "--package", built.Path}, "POLIS STATUS: complete", "Contract:", "Baseline commit:", "Evidence: complete", "Next action: none")
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

func TestRunStatusReportsDeferredGatesAsConsumerValidationRequired(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: repo, Project: "polis", Change: "status-deferred", Out: t.TempDir(), Contract: contract, DeferredGates: []string{"coverage"},
	})
	if err != nil {
		t.Fatal(err)
	}
	status := runCLIJSON(t, "status", "--repo", repo, "--package", built.Path, "--format", "json")
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
