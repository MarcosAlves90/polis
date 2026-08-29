package packageapply

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MarcosAlves90/polis/v4/internal/gitutil"
	"github.com/MarcosAlves90/polis/v4/spec"
)

func TestFailurePathCoverageMargin(t *testing.T) {
	ctx := context.Background()
	nonRepo := t.TempDir()
	if err := verifyBaseline(ctx, nonRepo, spec.Manifest{}); err == nil {
		t.Fatal("expected baseline inspection failure outside repository")
	}
	if _, err := gitutil.ChangedIndexPaths(ctx, nonRepo, "--cached"); err == nil {
		t.Fatal("expected changed-index inspection failure outside repository")
	}
	if _, err := workingTreeID(ctx, nonRepo, "not-a-commit"); err == nil {
		t.Fatal("expected working-tree identity failure outside repository")
	}
	if _, err := fileSHA256(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected hash failure for missing file")
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
	if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "x.txt")
	run("-c", "user.name=POLIS", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")

	if _, err := workingTreeID(ctx, repo, "not-a-commit"); err == nil {
		t.Fatal("expected invalid base rejection")
	}
}
