package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunWorkspaceValidate(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("workspace target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHead := cliGit(t, repo, "rev-parse", "HEAD")
	beforeIndex := cliGit(t, repo, "write-tree")
	beforeStatus := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")

	args := []string{"workspace", "validate", "--repo", repo, "--contract", contract, "--jobs", "2", "--format", "json"}
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("workspace validate command should pass: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("workspace validation JSON: %v raw=%s", err, out.String())
	}
	if report["status"] != "PASS" || report["workspace_validated"] != true {
		t.Fatalf("unexpected workspace result: %v", report)
	}
	if report["delivery_artifact_built"] != false || report["delivery_artifact_verified"] != false {
		t.Fatalf("workspace validation must not claim a delivery artifact: %v", report)
	}
	for _, field := range []string{"base_commit", "target_tree", "contract_sha256", "policy_sha256", "validation_level"} {
		if value, ok := report[field].(string); !ok || value == "" {
			t.Errorf("workspace result missing %s: %v", field, report)
		}
	}
	gateResults, ok := report["producer_gate_statuses"].(map[string]any)
	if !ok || gateResults["test.complete"] != "PASS" || gateResults["coverage"] != "PASS" {
		t.Fatalf("workspace result lacks complete configured gate outcomes: %v", report["producer_gate_statuses"])
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis", "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("workspace validation created retained artifacts: stat error=%v", err)
	}

	reportPath := filepath.Join(t.TempDir(), "workspace-validation.json")
	args = append(args, "--out-report", reportPath)
	out.Reset()
	errOut.Reset()
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("workspace validation report should be written: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	stored, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read workspace report: %v", err)
	}
	var storedReport map[string]any
	if err := json.Unmarshal(stored, &storedReport); err != nil {
		t.Fatalf("stored workspace report JSON: %v raw=%s", err, stored)
	}
	if storedReport["workspace_validated"] != true || !strings.Contains(string(stored), `"schema_version":1`) {
		t.Fatalf("stored report lacks versioned workspace validation state: %s", stored)
	}
	info, err := os.Stat(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("report permissions=%#o, want 0600", got)
		}
	}

	nestedRepo := filepath.Join(repo, "nested")
	if err := os.Mkdir(nestedRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	inRepositoryReport := filepath.Join(repo, "workspace-validation-inside.json")
	nestedArgs := []string{"workspace", "validate", "--repo", nestedRepo, "--contract", contract, "--format", "json", "--out-report", inRepositoryReport}
	var nestedOut, nestedErr bytes.Buffer
	if code := run(nestedArgs, &nestedOut, &nestedErr); code != exitValidationFailed || !strings.Contains(nestedErr.String(), "output must be outside target worktree") {
		t.Fatalf("workspace report inside the physical repository should be rejected: code=%d stdout=%q stderr=%q", code, nestedOut.String(), nestedErr.String())
	}
	if _, err := os.Stat(inRepositoryReport); !os.IsNotExist(err) {
		t.Fatalf("in-repository report was created: stat error=%v", err)
	}

	if got := cliGit(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Errorf("workspace validation changed HEAD: got %s want %s", got, beforeHead)
	}
	if got := cliGit(t, repo, "write-tree"); got != beforeIndex {
		t.Errorf("workspace validation changed index tree: got %s want %s", got, beforeIndex)
	}
	if got := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Errorf("workspace validation changed source worktree status: got %q want %q", got, beforeStatus)
	}
}

func TestRunWorkspaceValidateRejectsInvalidJobs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"workspace", "validate", "--jobs", "0"}, &out, &errOut); code != exitUsage {
		t.Fatalf("invalid jobs code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "jobs must be between 1 and 16") {
		t.Fatalf("invalid jobs diagnostic missing: %q", errOut.String())
	}
}
