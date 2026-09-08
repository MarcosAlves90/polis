package packageapply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func simpleRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func TestVerifyBaselineRejectsObjectFormatMismatch(t *testing.T) {
	repo := simpleRepo(t)
	head := git(t, repo, "rev-parse", "HEAD")
	manifest := spec.Manifest{GitObjectFormat: "sha256", BaseCommit: strings.Repeat("a", 64)}
	if head == manifest.BaseCommit {
		t.Fatal("fixture unexpectedly matches")
	}
	err := verifyBaseline(context.Background(), repo, manifest)
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "git object format") {
		t.Fatalf("error=%v", err)
	}
}

func TestReversePatchRestoresAppliedChange(t *testing.T) {
	repo := simpleRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, err := gitutil.Bytes(context.Background(), repo, nil, nil, "diff", "--binary", "--full-index", "HEAD", "--")
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "restore", "--", "file.txt")
	if _, err := gitutil.Bytes(context.Background(), repo, nil, strings.NewReader(string(patch)), "apply", "-"); err != nil {
		t.Fatal(err)
	}
	if err := reversePatch(context.Background(), repo, patch); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(repo, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "base\n" {
		t.Fatalf("content=%q", b)
	}
}

func TestDetachedWorktreeRejectsUnknownBase(t *testing.T) {
	repo := simpleRepo(t)
	if _, _, err := gitutil.DetachedWorktree(context.Background(), repo, strings.Repeat("f", 40), "polis-test-bad-*", "create isolated worktree staging", "create isolated consumer worktree"); err == nil {
		t.Fatal("expected invalid-base error")
	}
}

func TestChangedIndexPathsReportsStagedPath(t *testing.T) {
	repo := simpleRepo(t)
	paths, err := gitutil.ChangedIndexPaths(context.Background(), repo, "--cached")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("initial paths=%v", paths)
	}
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "file.txt")
	paths, err = gitutil.ChangedIndexPaths(context.Background(), repo, "--cached")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := paths["file.txt"]; !ok {
		t.Fatalf("paths=%v", paths)
	}
}

func TestDiscardTemporaryEvidenceRemovesFile(t *testing.T) {
	f, err := os.CreateTemp("", "polis-apply-test-evidence-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if _, err := f.WriteString("evidence\n"); err != nil {
		t.Fatal(err)
	}
	if err := discardTemporaryEvidence(f, path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary evidence remains: %v", err)
	}
}
func TestWorkingTreeIDIncludesUnstagedAndUntrackedChanges(t *testing.T) {
	repo := simpleRepo(t)
	base := git(t, repo, "rev-parse", "HEAD")
	baseTree := git(t, repo, "rev-parse", "HEAD^{tree}")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := workingTreeID(context.Background(), repo, base)
	if err != nil {
		t.Fatal(err)
	}
	if got == baseTree {
		t.Fatalf("working tree ID unexpectedly equals base tree %s", got)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); !strings.Contains(status, "M file.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("working tree state changed unexpectedly: %q", status)
	}
}
