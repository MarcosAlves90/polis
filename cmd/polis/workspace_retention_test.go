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

func TestRepositoryWorkspaceValidateRequiresRetainedEvidence(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"workspace", "validate", "--repo", repo, "--contract", contract, "--format", "json"}, &out, &errOut)
	if code != exitPass {
		t.Fatalf("workspace validate code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode workspace validate output: %v", err)
	}
	if payload["workspace_validated"] != true {
		t.Fatalf("workspace_validated=%v payload=%v", payload["workspace_validated"], payload)
	}

	contractRaw, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contractRaw)
	retainedContract := filepath.Join(repo, ".polis", "artifacts", "contracts", "sha256-"+hex.EncodeToString(sum[:])+".json")
	if _, err := os.Stat(retainedContract); err != nil {
		t.Fatalf("issue38: repository workspace validation must publish verified retained evidence: missing retained contract: %v", err)
	}

	evidenceDir := filepath.Join(repo, ".polis", "artifacts", "evidence")
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		t.Fatalf("issue38: repository workspace validation must publish verified retained evidence: missing retained evidence: %v", err)
	}
	foundRecord := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(evidenceDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"kind":"repository_validation"`) && strings.Contains(string(raw), `"target_tree":`) {
			foundRecord = true
			break
		}
	}
	if !foundRecord {
		t.Fatal("issue38: repository workspace validation must publish verified retained evidence: no current-run completeness record")
	}
}
