package gaterun

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestScopedSnapshotTracksEmptyDirectories(t *testing.T) {
	repo, _, _ := fixture(t)
	root := filepath.Join(repo, "backend")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	baseline, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := scopedSnapshot(context.Background(), repo, []string{"backend/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "missing"), 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	if baseline == created {
		t.Fatal("adding an empty in-scope directory did not change the scoped identity")
	}
	present, err := scopedSnapshot(context.Background(), repo, []string{"backend/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if missing == present {
		t.Fatal("creating a declared empty directory did not change the scoped identity")
	}
	if err := os.Remove(filepath.Join(root, "missing")); err != nil {
		t.Fatal(err)
	}
	restored, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil || restored != baseline {
		t.Fatalf("removing the empty directory should restore the input identity: %v", err)
	}
}

func TestScopedReuseRerunsGateAfterEmptyDirectoryChange(t *testing.T) {
	repo, policy, _ := fixture(t)
	root := filepath.Join(repo, "backend")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"backend"}
			policy.Gates[i].InputPathsComplete = true
		}
	}
	plan := compiled(t, policy)
	prior := filepath.Join(t.TempDir(), "prior.json")
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: prior}
	runOK(t, repo, plan, opts)
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts.Out = ""
	opts.Reuse = prior
	after := runOK(t, repo, plan, opts)
	gate := priorGate(&after, "lint")
	if gate.Action != "executed" || !slices.Contains(gate.StaleCategories, "source") {
		t.Fatalf("empty directory change reused old passing gate evidence: %+v", gate)
	}
}
