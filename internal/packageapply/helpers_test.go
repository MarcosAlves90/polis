package packageapply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
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

func TestVerifyConsumerStateRejectsObjectFormatMismatch(t *testing.T) {
	repo := simpleRepo(t)
	head := git(t, repo, "rev-parse", "HEAD")
	if head == strings.Repeat("a", 64) {
		t.Fatal("fixture unexpectedly matches")
	}
	_, err := verifyConsumerState(context.Background(), repo, "sha256")
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

func TestWorkingTreeIDWithIgnoredPolisDirectory(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.polis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), []byte("base policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".gitignore")
	git(t, repo, "add", "-f", ".polis/policy.json")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	base := git(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), []byte("changed policy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".polis", "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "artifacts", "ignored.polis"), []byte("ignored artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := workingTreeID(context.Background(), repo, base)
	if err != nil {
		t.Fatalf("workingTreeID() error = %v", err)
	}
	git(t, repo, "add", "-u", "--", ".", ":(exclude).polis/artifacts/**")
	git(t, repo, "add", "--", "new.txt")
	want := git(t, repo, "write-tree")
	if got != want {
		t.Fatalf("workingTreeID() = %s, want %s including tracked and untracked changes", got, want)
	}
	entries := git(t, repo, "ls-tree", "-r", "--name-only", want)
	if !strings.Contains(entries, ".polis/policy.json") || !strings.Contains(entries, "new.txt") {
		t.Fatalf("working tree tree omits tracked or untracked changes: %q", entries)
	}
	if strings.Contains(entries, "ignored.polis") {
		t.Fatalf("working tree tree includes ignored artifact: %q", entries)
	}
}

func TestWorkingTreeIDIncludesUntrackedPathspecMagicFilename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows forbids ':' in filenames, so this Git pathspec-magic filename cannot be represented")
	}
	repo := simpleRepo(t)
	filename := ":(exclude)unexpected.txt"
	if err := os.WriteFile(filepath.Join(repo, filename), []byte("literal path\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ordinary.txt"), []byte("ordinary path\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := workingTreeID(context.Background(), repo, git(t, repo, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatalf("workingTreeID() error = %v", err)
	}
	git(t, repo, "--literal-pathspecs", "add", "--", filename, "ordinary.txt")
	want := git(t, repo, "write-tree")
	if got != want {
		t.Fatalf("workingTreeID() = %s, want %s including literal path %q", got, want, filename)
	}
}

func TestParseBaselineMode(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  BaselineMode
	}{
		{"", BaselineModeStrict},
		{"strict", BaselineModeStrict},
		{"compatible", BaselineModeCompatible},
		{"permissive", BaselineModePermissive},
	} {
		got, err := ParseBaselineMode(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("ParseBaselineMode(%q)=(%q,%v), want %q", tc.input, got, err, tc.want)
		}
	}
	if _, err := ParseBaselineMode("force"); err == nil {
		t.Fatal("expected invalid mode rejection")
	}
}
