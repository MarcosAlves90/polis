package packagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryBoundaryHelpers(t *testing.T) {
	ctx := context.Background()
	nonRepo := t.TempDir()
	if _, err := resolveRepo(ctx, nonRepo); err == nil {
		t.Fatal("expected non-repository rejection")
	}
	if err := requireCleanIndex(ctx, nonRepo); err == nil {
		t.Fatal("expected index inspection failure outside repository")
	}
	if _, err := changedIndexPaths(ctx, nonRepo); err == nil {
		t.Fatal("expected changed-index failure outside repository")
	}
}

func TestExternalInputBoundaries(t *testing.T) {
	repo := newBoundaryRepo(t)
	inside := filepath.Join(repo, "inside.json")
	writeTestFile(t, inside, []byte("{}"))
	if _, err := readExternalInput(repo, inside, 1024); err == nil || !strings.Contains(err.Error(), "outside target worktree") {
		t.Fatalf("expected inside-input rejection, got %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := readExternalInput(repo, missing, 1024); err == nil {
		t.Fatal("expected missing-input rejection")
	}

	dir := t.TempDir()
	if _, err := readExternalInput(repo, dir, 1024); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected directory rejection, got %v", err)
	}

	large := filepath.Join(t.TempDir(), "large.bin")
	writeTestFile(t, large, []byte("12345"))
	if _, err := readExternalInput(repo, large, 4); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("expected size rejection, got %v", err)
	}
}

func TestArtifactHelpers(t *testing.T) {
	valid := filepath.Join(t.TempDir(), "valid.txt")
	want := []byte("artifact-bytes")
	writeTestFile(t, valid, want)

	got, err := readExternalInput(newBoundaryRepo(t), valid, 1024)
	if err != nil || string(got) != string(want) {
		t.Fatalf("readExternalInput got=%q err=%v", got, err)
	}
	if _, err := fileSHA256(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected missing hash input rejection")
	}
	assertFileHash(t, valid, want)
	assertCopyExclusive(t, valid, want)
}

func TestTemporaryTargetAndWorktreeRejectInvalidBase(t *testing.T) {
	ctx := context.Background()
	repo := newBoundaryRepo(t)
	if _, _, err := buildTargetWithTemporaryIndex(ctx, repo, "not-a-commit"); err == nil {
		t.Fatal("expected invalid base rejection")
	}
	if _, cleanup, err := detachedWorktree(ctx, repo, "not-a-commit", "polis-invalid-worktree-*"); err == nil {
		cleanup()
		t.Fatal("expected invalid worktree base rejection")
	}
}

func newBoundaryRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q")
	writeTestFile(t, filepath.Join(repo, "tracked.txt"), []byte("base\n"))
	runGit("add", "tracked.txt")
	runGit("-c", "user.name=POLIS", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func writeTestFile(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileHash(t *testing.T, filename string, want []byte) {
	t.Helper()
	sum, err := fileSHA256(filename)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(want)
	if sum != hex.EncodeToString(expected[:]) {
		t.Fatalf("hash=%s", sum)
	}
}

func assertCopyExclusive(t *testing.T, valid string, want []byte) {
	t.Helper()
	if err := copyExclusive(filepath.Join(t.TempDir(), "missing-source"), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected missing source rejection")
	}
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, []byte("keep"))
	if err := copyExclusive(valid, target); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected collision rejection, got %v", err)
	}
	copied := filepath.Join(t.TempDir(), "copied")
	if err := copyExclusive(valid, copied); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(copied); err != nil || string(b) != string(want) {
		t.Fatalf("copied=%q err=%v", b, err)
	}
}
