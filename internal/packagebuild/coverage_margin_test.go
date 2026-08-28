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

func TestBoundaryAndArtifactHelpers(t *testing.T) {
	ctx := context.Background()

	nonRepo := t.TempDir()
	if _, err := resolveRepo(ctx, nonRepo); err == nil {
		t.Fatal("expected non-repository rejection")
	}
	if err := requireCleanIndex(ctx, nonRepo); err == nil {
		t.Fatal("expected index inspection failure outside repository")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("-c", "user.name=POLIS", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")

	inside := filepath.Join(repo, "inside.json")
	if err := os.WriteFile(inside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(large, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readExternalInput(repo, large, 4); err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("expected size rejection, got %v", err)
	}

	valid := filepath.Join(t.TempDir(), "valid.txt")
	want := []byte("artifact-bytes")
	if err := os.WriteFile(valid, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readExternalInput(repo, valid, 1024)
	if err != nil || string(got) != string(want) {
		t.Fatalf("readExternalInput got=%q err=%v", got, err)
	}

	if _, err := fileSHA256(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected missing hash input rejection")
	}
	sum, err := fileSHA256(valid)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(want)
	if sum != hex.EncodeToString(expected[:]) {
		t.Fatalf("hash=%s", sum)
	}

	if err := copyExclusive(filepath.Join(t.TempDir(), "missing-source"), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected missing source rejection")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
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

	if _, err := changedIndexPaths(ctx, nonRepo); err == nil {
		t.Fatal("expected changed-index failure outside repository")
	}
	if _, _, err := buildTargetWithTemporaryIndex(ctx, repo, "not-a-commit"); err == nil {
		t.Fatal("expected invalid base rejection")
	}
	if _, cleanup, err := detachedWorktree(ctx, repo, "not-a-commit", "polis-invalid-worktree-*"); err == nil {
		cleanup()
		t.Fatal("expected invalid worktree base rejection")
	}
}
