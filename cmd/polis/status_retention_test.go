package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/changestatus"
	"github.com/MarcosAlves90/polis/v6/internal/retentioncompleteness"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func statusRetentionReadiness(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	readiness, ok := payload["retention_readiness"].(map[string]any)
	if !ok {
		t.Fatalf("status has no retention_readiness object: %v", payload)
	}
	return readiness
}

func statusRetentionStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("status string field is %T: %v", value, value)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("status string field contains %T: %v", item, item)
		}
		result = append(result, text)
	}
	return result
}

func TestRunStatusProjectsRepositoryRetentionReadiness(t *testing.T) {
	t.Run("external", func(t *testing.T) {
		repo := makeBuildRepo(t)
		status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		readiness := statusRetentionReadiness(t, status)
		if readiness["state"] != "external" || readiness["corrective"] != "none" {
			t.Fatalf("external readiness = %v", readiness)
		}
		if status["retention_mode"] != "external" {
			t.Fatalf("retention mode = %v", status["retention_mode"])
		}
		assertCLITextContains(t, []string{"status", "--repo", repo, "--format", "text"}, "Retention readiness: external", "Retention readiness action: none")
	})

	t.Run("empty", func(t *testing.T) {
		repo := makeBuildRepo(t)
		enableCLIRetention(t, repo)
		status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		readiness := statusRetentionReadiness(t, status)
		if readiness["state"] != "empty" || readiness["corrective"] != "polis start" {
			t.Fatalf("empty readiness = %v", readiness)
		}
		if status["state"] != changestatus.StateEmpty {
			t.Fatalf("operational state = %v", status["state"])
		}
		if !statusStringSliceContains(status["missing"], "locked workflow state for retained readiness") {
			t.Fatalf("empty readiness was not projected as missing: %v", status["missing"])
		}
		assertCLITextContains(t, []string{"status", "--repo", repo}, "Retention readiness: empty", "Retention readiness action: polis start")
	})

	t.Run("incomplete", func(t *testing.T) {
		repo := makeBuildRepo(t)
		enableCLIRetention(t, repo)
		lockedCLIContract(t, repo)
		status := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		readiness := statusRetentionReadiness(t, status)
		if readiness["state"] != "incomplete" || readiness["corrective"] != "polis workspace validate" {
			t.Fatalf("incomplete readiness = %v", readiness)
		}
		if readiness["target_tree"] == nil || readiness["target_tree"] == "" {
			t.Fatalf("incomplete readiness has no current target tree: %v", readiness)
		}
		if status["state"] != changestatus.StateImplementationPending || status["consistent"] != true {
			t.Fatalf("incomplete evidence changed operational status: %v", status)
		}
		if !statusStringSliceContains(status["missing"], "current retained validation evidence") {
			t.Fatalf("missing retained evidence was not projected: %v", status["missing"])
		}
		if statusStringSliceContains(status["proven"], "retained validation evidence") {
			t.Fatalf("missing evidence was projected as proven: %v", status["proven"])
		}
		assertCLITextContains(t, []string{"status", "--repo", repo}, "Retention readiness: incomplete", "Retention readiness action: polis workspace validate")
	})

	t.Run("complete ambiguous stale", func(t *testing.T) {
		repo := makeBuildRepo(t)
		enableCLIRetention(t, repo)
		contract := lockedCLIContract(t, repo)
		if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("validated implementation\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		validated := runCLIJSON(t, "workspace", "validate", "--repo", repo, "--contract", contract, "--format", "json")
		if validated["workspace_validated"] != true {
			t.Fatalf("workspace validation = %v", validated)
		}
		targetTree, ok := validated["target_tree"].(string)
		if !ok || targetTree == "" {
			t.Fatalf("workspace target tree = %v", validated["target_tree"])
		}

		complete := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		completeReadiness := statusRetentionReadiness(t, complete)
		if completeReadiness["state"] != "complete" || completeReadiness["operation"] != "workspace_validate" {
			t.Fatalf("complete readiness = %v", completeReadiness)
		}
		if completeReadiness["target_tree"] != targetTree || completeReadiness["corrective"] != "none" {
			t.Fatalf("complete readiness is not bound to the current run: %v", completeReadiness)
		}
		completePaths := statusRetentionStrings(t, completeReadiness["record_paths"])
		if len(completePaths) != 1 || completePaths[0] == "" {
			t.Fatalf("complete record paths = %v", completeReadiness["record_paths"])
		}
		if complete["state"] != changestatus.StateImplementationPending || complete["consistent"] != true {
			t.Fatalf("retained evidence overclaimed operational completion: %v", complete)
		}
		if !statusStringSliceContains(complete["proven"], "retained validation evidence is complete") {
			t.Fatalf("complete retained evidence was not projected as proven: %v", complete["proven"])
		}
		if statusStringSliceContains(complete["missing"], "current retained validation evidence") ||
			statusStringSliceContains(complete["stale_or_unproven"], "retained validation evidence") {
			t.Fatalf("complete status still reports retained evidence gaps: %v", complete)
		}
		assertCLITextContains(t, []string{"status", "--repo", repo},
			"Retention readiness: complete",
			"Retention readiness operation: workspace_validate",
			"Retention readiness evidence: "+completePaths[0],
			"retained validation evidence is complete")

		alternate := publishAmbiguousRetentionRecord(t, repo, completePaths[0], targetTree)
		ambiguous := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		ambiguousReadiness := statusRetentionReadiness(t, ambiguous)
		if ambiguousReadiness["state"] != "ambiguous" {
			t.Fatalf("ambiguous readiness = %v", ambiguousReadiness)
		}
		if paths := statusRetentionStrings(t, ambiguousReadiness["record_paths"]); len(paths) != 2 {
			t.Fatalf("ambiguous record paths = %v", ambiguousReadiness["record_paths"])
		}
		if !statusStringSliceContains(ambiguous["stale_or_unproven"], "multiple retained validation records") {
			t.Fatalf("ambiguity was not projected: %v", ambiguous["stale_or_unproven"])
		}
		if statusStringSliceContains(ambiguous["proven"], "retained validation evidence is complete") {
			t.Fatalf("ambiguous evidence was projected as complete: %v", ambiguous["proven"])
		}
		assertCLITextContains(t, []string{"status", "--repo", repo}, "Retention readiness: ambiguous")
		if err := os.Remove(filepath.Join(repo, filepath.FromSlash(alternate))); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("later implementation change\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stale := runCLIJSON(t, "status", "--repo", repo, "--format", "json")
		staleReadiness := statusRetentionReadiness(t, stale)
		if staleReadiness["state"] != "stale" || staleReadiness["target_tree"] == targetTree {
			t.Fatalf("stale readiness = %v, validated target = %s", staleReadiness, targetTree)
		}
		if stale["state"] != changestatus.StateImplementationPending {
			t.Fatalf("stale evidence changed operational status: %v", stale)
		}
		if !statusStringSliceContains(stale["stale_or_unproven"], "retained validation evidence belongs to a different source tree") {
			t.Fatalf("stale retained evidence was not projected: %v", stale["stale_or_unproven"])
		}
		if statusStringSliceContains(stale["proven"], "retained validation evidence is complete") {
			t.Fatalf("stale evidence was projected as complete: %v", stale["proven"])
		}
		assertCLITextContains(t, []string{"status", "--repo", repo}, "Retention readiness: stale")
	})
}

func publishAmbiguousRetentionRecord(t *testing.T, repo, recordPath, targetTree string) string {
	t.Helper()
	ctx := context.Background()
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read retained %s: %v", path, err)
		}
		return raw
	}

	record, err := spec.DecodeRetainedValidationRecord(read(recordPath))
	if err != nil {
		t.Fatal(err)
	}
	contractRaw := read(record.RetainedContract)
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	evidence := append(append([]byte(nil), read(record.RetainedEvidence)...), []byte("{\"event\":\"gate_started\",\"gate\":\"regression\"}\n")...)
	var planRaw []byte
	if record.RetainedPlan != "" {
		planRaw = read(record.RetainedPlan)
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := retentioncompleteness.Complete(ctx, retention, repo, retentioncompleteness.Inputs{
		Operation:             record.Operation,
		ContractRaw:           contractRaw,
		Contract:              contract,
		PolicyRaw:             policyRaw,
		Policy:                policy,
		ImplementationPlanRaw: planRaw,
		Evidence:              evidence,
		BaseCommit:            contract.BaselineLock.BaseCommit,
		TargetTree:            targetTree,
		ValidationLevel:       record.ValidationLevel,
		EnabledGates:          record.EnabledGates,
		DisabledGates:         record.DisabledGates,
		DeferredGates:         record.DeferredGates,
		ProducerGateStatuses:  record.ProducerGateStatuses,
	}, nil)
	if err != nil {
		t.Fatalf("publish alternate completion record: %v", err)
	}
	return outcome.RecordPath
}
