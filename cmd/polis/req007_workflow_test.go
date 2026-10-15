package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This disposable CLI flow uses only local Git operations and the controlled
// commands in makeBuildRepo. TempDir provides fixture cleanup, not a security
// sandbox; no network, credentials, real user data, or arbitrary project code is used.
func TestREQ007SimulatedWorkspaceAndDeliveryFlow(t *testing.T) {
	type repositoryState struct {
		head   string
		index  string
		status string
	}
	snapshot := func(repo string) repositoryState {
		t.Helper()
		return repositoryState{
			head:   cliGit(t, repo, "rev-parse", "HEAD"),
			index:  cliGit(t, repo, "write-tree"),
			status: cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"),
		}
	}
	assertOutside := func(root, path string) {
		t.Helper()
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("resolve path %q relative to repo %q: %v", path, root, err)
		}
		if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) || filepath.IsAbs(relative) {
			t.Fatalf("external output %q is inside repository %q", path, root)
		}
	}
	assertNoRetainedArtifacts := func(repo string) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(repo, ".polis", "artifacts")); !os.IsNotExist(err) {
			t.Fatalf("unexpected retained artifact path in %s: stat error=%v", repo, err)
		}
	}

	producer := makeBuildRepo(t)
	contract := lockedCLIContract(t, producer)
	appPath := filepath.Join(producer, "app.txt")
	if err := os.WriteFile(appPath, []byte("workspace target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceBefore := snapshot(producer)
	workspace := runCLIJSON(t, "workspace", "validate", "--repo", producer, "--contract", contract, "--format", "json")
	assertJSONFields(t, workspace, map[string]string{"status": "PASS"})
	if workspace["workspace_validated"] != true || workspace["delivery_artifact_built"] != false || workspace["delivery_artifact_verified"] != false {
		t.Fatalf("workspace validate reported unexpected delivery state: %v", workspace)
	}
	if got := snapshot(producer); got != workspaceBefore {
		t.Fatalf("workspace validate changed producer state: got=%+v want=%+v", got, workspaceBefore)
	}
	assertNoRetainedArtifacts(producer)
	if _, exists := workspace["artifact"]; exists {
		t.Fatalf("workspace validate returned a delivery artifact: %v", workspace)
	}

	cliGit(t, producer, "restore", "--", "app.txt")
	producerBase := snapshot(producer)
	if producerBase.status != "" {
		t.Fatalf("producer fixture is not clean before cloning: %q", producerBase.status)
	}
	consumer := filepath.Join(t.TempDir(), "consumer")
	cloneOutput, err := exec.Command("git", "clone", "--local", producer, consumer).CombinedOutput()
	if err != nil {
		t.Fatalf("clone local consumer fixture: %v\n%s", err, cloneOutput)
	}
	consumerBase := snapshot(consumer)
	if consumerBase.head != producerBase.head || consumerBase.status != "" {
		t.Fatalf("consumer clone does not match clean producer baseline: producer=%+v consumer=%+v", producerBase, consumerBase)
	}

	if err := os.WriteFile(appPath, []byte("delivery target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "package-output")
	built := runCLIJSON(t, "build", "--repo", producer, "--project", "polis", "--change", "req007-simulated-workflow", "--contract", contract, "--out", outputDir, "--format", "json")
	artifact, ok := built["artifact"].(string)
	if !ok || artifact == "" {
		t.Fatalf("build result has no artifact path: %v", built)
	}
	assertOutside(producer, artifact)
	assertOutside(consumer, artifact)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("build artifact is unavailable: %v", err)
	}
	cliGit(t, producer, "restore", "--", "app.txt")
	if got := snapshot(producer); got != producerBase {
		t.Fatalf("build or restore changed producer repository state: got=%+v want=%+v", got, producerBase)
	}

	verified := runCLIJSON(t, "verify", "--format", "json", artifact)
	assertJSONFields(t, verified, map[string]string{"status": "PASS"})
	preflight := runCLIJSON(t, "preflight", "--repo", consumer, "--format", "json", artifact)
	assertJSONFields(t, preflight, map[string]string{"status": "PASS"})
	if got := snapshot(consumer); got != consumerBase {
		t.Fatalf("preflight changed consumer state: got=%+v want=%+v", got, consumerBase)
	}

	applied := runCLIJSON(t, "apply", "--repo", consumer, "--format", "json", artifact)
	assertJSONFields(t, applied, map[string]string{"status": "PASS"})
	if applied["committed"] != false || applied["commit_sha"] != nil {
		t.Fatalf("default apply unexpectedly committed: %v", applied)
	}
	consumerAfter := snapshot(consumer)
	if consumerAfter.head != consumerBase.head || consumerAfter.index != consumerBase.index || consumerAfter.status != "M app.txt" {
		t.Fatalf("apply changed more than the consumer target worktree: before=%+v after=%+v", consumerBase, consumerAfter)
	}
	contents, err := os.ReadFile(filepath.Join(consumer, "app.txt"))
	if err != nil || string(contents) != "delivery target\n" {
		t.Fatalf("consumer target contents=%q err=%v", contents, err)
	}
	if got := snapshot(producer); got != producerBase {
		t.Fatalf("delivery flow changed producer state: got=%+v want=%+v", got, producerBase)
	}
	assertNoRetainedArtifacts(producer)
	assertNoRetainedArtifacts(consumer)
}
