package packagebuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue10LargeBaselineAboveOldArchiveCapAdmitsConsumer(t *testing.T) {
	repo := newV6Repo(t, false)
	largePath := filepath.Join(repo, "large.bin")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(65 << 20); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "large.bin")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "large locked baseline")
	contract := lockedCharacterizationContract(t, repo, "app.txt", "calc_test.go")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "large-archive", Out: filepath.Join(t.TempDir(), "out"), Contract: contract})
	if err != nil {
		t.Fatalf("build a complete baseline above the former archive cap: %v", err)
	}
	info, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 64<<20 {
		t.Fatalf("archive did not exercise former 64 MiB limit: %d", info.Size())
	}
	consumer := filepath.Join(t.TempDir(), "consumer")
	runGit(t, repo, "clone", "--quiet", repo, consumer)
	preflight, err := packageapply.Preflight(context.Background(), result.Path, consumer)
	if err != nil {
		t.Fatalf("consumer rejected large identity-checked baseline: %v", err)
	}
	if preflight.ConsumerBaseCommit != result.BaseCommit || preflight.ConsumerGateStatuses["test.complete"] != spec.StatusPass {
		t.Fatalf("unexpected consumer baseline/gates: %+v", preflight)
	}
}
