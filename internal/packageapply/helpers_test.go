package packageapply

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/spec"
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
	if err == nil || !strings.Contains(err.Error(), "git object format mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func TestFileSHA256SuccessAndMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("sha=%s want=%s", got, want)
	}
	if _, err := fileSHA256(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing-file error")
	}
}

func TestReversePatchRestoresAppliedChange(t *testing.T) {
	repo := simpleRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, err := gitBytes(context.Background(), repo, nil, nil, "diff", "--binary", "--full-index", "HEAD", "--")
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "restore", "--", "file.txt")
	if _, err := gitBytes(context.Background(), repo, nil, strings.NewReader(string(patch)), "apply", "-"); err != nil {
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
	if _, _, err := detachedWorktree(context.Background(), repo, strings.Repeat("f", 40), "polis-test-bad-*"); err == nil {
		t.Fatal("expected invalid-base error")
	}
}

func TestChangedIndexPathsReportsStagedPath(t *testing.T) {
	repo := simpleRepo(t)
	paths, err := changedIndexPaths(context.Background(), repo)
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
	paths, err = changedIndexPaths(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := paths["file.txt"]; !ok {
		t.Fatalf("paths=%v", paths)
	}
}

func TestCreateEvidenceFileRejectsMissingArtifact(t *testing.T) {
	repo := simpleRepo(t)
	_, f, err := createEvidenceFile(context.Background(), repo, filepath.Join(t.TempDir(), "missing.polis"))
	if f != nil {
		_ = f.Close()
	}
	if err == nil {
		t.Fatal("expected missing artifact error")
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
