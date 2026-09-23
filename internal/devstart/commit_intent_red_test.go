package devstart

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStartLocksCommitIntentV5Draft(t *testing.T) {
	repo := makeRepo(t)
	ext := t.TempDir()
	draftPath := filepath.Join(ext, "draft.json")
	lockedPath := filepath.Join(ext, "locked.json")

	var draft map[string]json.RawMessage
	if err := json.Unmarshal(draftContractBytes(t), &draft); err != nil {
		t.Fatal(err)
	}
	draft["schema_version"] = json.RawMessage("5")
	draft["scope"] = json.RawMessage(`{"allowed_paths":["."]}`)
	draft["test_scope"] = json.RawMessage(`{"allowed_paths":["internal/devstart/commit_intent_red_test.go"]}`)
	message := "feat(apply): preserve exact message\n\nBody keeps trailing spaces.  \n"
	draft["commit"] = json.RawMessage(`{"message":"feat(apply): preserve exact message\n\nBody keeps trailing spaces.  \n"}`)
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Start(context.Background(), Options{Repo: repo, Contract: draftPath, Out: lockedPath}); err != nil {
		t.Fatalf("RED: start must accept a strict schema-v5 draft with commit intent and lock it as schema v6: %v", err)
	}
	lockedRaw, err := os.ReadFile(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	var locked struct {
		SchemaVersion int `json:"schema_version"`
		Commit        struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(lockedRaw, &locked); err != nil {
		t.Fatal(err)
	}
	if locked.SchemaVersion != 6 {
		t.Fatalf("locked schema_version=%d want 6", locked.SchemaVersion)
	}
	if locked.Commit.Message != message {
		t.Fatalf("locked message bytes changed: got %q want %q", locked.Commit.Message, message)
	}
	if status := runGitCommand(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("start mutated repository: %q", status)
	}
}
