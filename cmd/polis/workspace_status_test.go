package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWorkspaceStatus(t *testing.T) {
	t.Run("matching source snapshot reports evidence limits", func(t *testing.T) {
		repo := makeBuildRepo(t)
		contract := lockedCLIContract(t, repo)
		report := writeWorkspaceStatusFixture(t, repo, contract)
		beforeHead := cliGit(t, repo, "rev-parse", "HEAD")
		beforeIndex := cliGit(t, repo, "write-tree")
		beforeStatus := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")

		code, stdout, stderr := invokeWorkspaceStatus(repo, contract, report)
		if code != exitPass {
			t.Fatalf("workspace status command should report matching source snapshot: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var result struct {
			Status                       string   `json:"status"`
			CheckpointState              string   `json:"checkpoint_state"`
			RecordedValidationStatus     string   `json:"recorded_validation_status"`
			ReportAuthenticated          bool     `json:"report_authenticated"`
			CurrentValidationEstablished bool     `json:"current_validation_established"`
			ProofInputDigestsBound       bool     `json:"proof_input_digests_bound"`
			DeliveryArtifactVerified     bool     `json:"delivery_artifact_verified"`
			Differences                  []string `json:"differences"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("decode workspace status JSON: %v; output=%q", err, stdout)
		}
		if result.Status != "PASS" || result.CheckpointState != "source_snapshot_matches" || result.RecordedValidationStatus != "PASS" || len(result.Differences) != 0 {
			t.Fatalf("unexpected matching workspace status: %+v", result)
		}
		if result.ReportAuthenticated || result.CurrentValidationEstablished || result.ProofInputDigestsBound || result.DeliveryArtifactVerified {
			t.Fatalf("status overclaimed checkpoint authority: %+v", result)
		}
		if got := cliGit(t, repo, "rev-parse", "HEAD"); got != beforeHead {
			t.Errorf("workspace status changed HEAD: got %s want %s", got, beforeHead)
		}
		if got := cliGit(t, repo, "write-tree"); got != beforeIndex {
			t.Errorf("workspace status changed index tree: got %s want %s", got, beforeIndex)
		}
		if got := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
			t.Errorf("workspace status changed source worktree: got %q want %q", got, beforeStatus)
		}
	})

	t.Run("changed target reports stale snapshot", func(t *testing.T) {
		repo := makeBuildRepo(t)
		contract := lockedCLIContract(t, repo)
		report := writeWorkspaceStatusFixture(t, repo, contract)
		if err := os.WriteFile(filepath.Join(repo, "later.txt"), []byte("changed after report\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := invokeWorkspaceStatus(repo, contract, report)
		if code != exitPass {
			t.Fatalf("stale workspace status should be inspectable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var result struct {
			CheckpointState string   `json:"checkpoint_state"`
			Differences     []string `json:"differences"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("decode stale workspace status: %v; output=%q", err, stdout)
		}
		if result.CheckpointState != "source_snapshot_differs" || len(result.Differences) == 0 {
			t.Fatalf("changed source was not reported stale: %+v", result)
		}
	})

	t.Run("identity mismatch reports stale snapshot", func(t *testing.T) {
		repo := makeBuildRepo(t)
		contract := lockedCLIContract(t, repo)
		report := writeWorkspaceStatusFixture(t, repo, contract)
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		data["contract_sha256"] = strings.Repeat("0", 64)
		raw, err = json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(report, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := invokeWorkspaceStatus(repo, contract, report)
		if code != exitPass {
			t.Fatalf("identity mismatch should be inspectable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var result struct {
			CheckpointState string   `json:"checkpoint_state"`
			Differences     []string `json:"differences"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("decode mismatched workspace status: %v; output=%q", err, stdout)
		}
		if result.CheckpointState != "source_snapshot_differs" || len(result.Differences) == 0 {
			t.Fatalf("mismatched contract digest was not reported stale: %+v", result)
		}
	})

	t.Run("staged source is unavailable", func(t *testing.T) {
		repo := makeBuildRepo(t)
		contract := lockedCLIContract(t, repo)
		report := writeWorkspaceStatusFixture(t, repo, contract)
		if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cliGit(t, repo, "add", "staged.txt")
		code, stdout, stderr := invokeWorkspaceStatus(repo, contract, report)
		if code != exitPass {
			t.Fatalf("unavailable workspace status should be inspectable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var result struct {
			CheckpointState string `json:"checkpoint_state"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("decode unavailable workspace status: %v; output=%q", err, stdout)
		}
		if result.CheckpointState != "checkpoint_unavailable" {
			t.Fatalf("staged source was not reported unavailable: %+v", result)
		}
	})

	t.Run("rejects unsafe and malformed reports", func(t *testing.T) {
		repo := makeBuildRepo(t)
		contract := lockedCLIContract(t, repo)
		report := writeWorkspaceStatusFixture(t, repo, contract)

		inside := filepath.Join(repo, "workspace-report.json")
		if err := os.WriteFile(inside, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := invokeWorkspaceStatus(repo, contract, inside); code != exitValidationFailed || !strings.Contains(stderr, "input must be outside target worktree") {
			t.Errorf("in-worktree report was accepted: code=%d stderr=%q", code, stderr)
		}

		symlink := filepath.Join(t.TempDir(), "report-link.json")
		if err := os.Symlink(report, symlink); err != nil {
			t.Fatal(err)
		}
		if code, _, _ := invokeWorkspaceStatus(repo, contract, symlink); code != exitValidationFailed {
			t.Errorf("symlink report was accepted: code=%d", code)
		}

		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		duplicate := bytes.Replace(raw, []byte(`"status":"PASS"`), []byte(`"status":"PASS","status":"PASS"`), 1)
		if bytes.Equal(duplicate, raw) {
			t.Fatal("test fixture did not contain the status property")
		}
		duplicatePath := filepath.Join(t.TempDir(), "duplicate.json")
		if err := os.WriteFile(duplicatePath, duplicate, 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _, _ := invokeWorkspaceStatus(repo, contract, duplicatePath); code != exitValidationFailed {
			t.Errorf("duplicate-key report was accepted: code=%d", code)
		}

		var unknown map[string]any
		if err := json.Unmarshal(raw, &unknown); err != nil {
			t.Fatal(err)
		}
		unknown["unexpected"] = true
		unknownRaw, err := json.Marshal(unknown)
		if err != nil {
			t.Fatal(err)
		}
		unknownPath := filepath.Join(t.TempDir(), "unknown.json")
		if err := os.WriteFile(unknownPath, unknownRaw, 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _, _ := invokeWorkspaceStatus(repo, contract, unknownPath); code != exitValidationFailed {
			t.Errorf("unknown-field report was accepted: code=%d", code)
		}

		oversizedPath := filepath.Join(t.TempDir(), "oversized.json")
		if err := os.WriteFile(oversizedPath, bytes.Repeat([]byte(" "), (1<<20)+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _, _ := invokeWorkspaceStatus(repo, contract, oversizedPath); code != exitValidationFailed {
			t.Errorf("oversized report was accepted: code=%d", code)
		}
	})
}

func invokeWorkspaceStatus(repo, contract, report string) (int, string, string) {
	var out, errOut bytes.Buffer
	args := []string{"workspace", "status", "--repo", repo, "--contract", contract, "--report", report, "--format", "json"}
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeWorkspaceStatusFixture(t *testing.T, repo, contract string) string {
	t.Helper()
	contractRaw, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	contractDigest := sha256.Sum256(contractRaw)
	policyDigest := sha256.Sum256(policyRaw)
	report := map[string]any{
		"schema_version": 1, "status": "PASS", "validation_kind": "workspace_validation",
		"workspace_validated": true, "delivery_artifact_built": false, "delivery_artifact_verified": false,
		"base_commit":     cliGit(t, repo, "rev-parse", "HEAD"),
		"target_tree":     cliGit(t, repo, "rev-parse", "HEAD^{tree}"),
		"contract_sha256": hex.EncodeToString(contractDigest[:]), "policy_sha256": hex.EncodeToString(policyDigest[:]),
		"validation_level": "strict", "enabled_gates": []string{"test.complete"}, "disabled_gates": []string{},
		"producer_gate_statuses": map[string]string{"test.complete": "PASS"},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "workspace-validation.json")
	if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return reportPath
}
