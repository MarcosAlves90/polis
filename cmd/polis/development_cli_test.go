package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/changestatus"
	"github.com/MarcosAlves90/polis/v6/spec"
)

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
