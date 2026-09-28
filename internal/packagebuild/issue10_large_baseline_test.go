package packagebuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/baselineproof"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue10LargeBaselineBuildRunsGatesAndVerifies(t *testing.T) {
	repo := newV6Repo(t, false)
	large := make([]byte, (32<<20)+4096)
	if err := os.WriteFile(filepath.Join(repo, "large.bin"), large, 0o644); err != nil {
		t.Fatal(err)
	}
	large = nil
	runGit(t, repo, "add", "large.bin")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "large locked baseline")
	base := runGit(t, repo, "rev-parse", "HEAD")
	baseTree := runGit(t, repo, "rev-parse", "HEAD^{tree}")
	contract := lockedCharacterizationContract(t, repo, "app.txt", "calc_test.go")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "large-baseline", Out: filepath.Join(t.TempDir(), "out"), Contract: contract})
	if err != nil {
		t.Fatalf("ISSUE10-LARGE-BASELINE: build rejected complete >32 MiB baseline: %v", err)
	}
	if result.BaseCommit != base || result.ProducerGateStatuses["test.complete"] != spec.StatusPass {
		t.Fatalf("ISSUE10-LARGE-BASELINE: base=%s gates=%v", result.BaseCommit, result.ProducerGateStatuses)
	}
	pkg, err := packageverify.Load(result.Path)
	if err != nil {
		t.Fatalf("ISSUE10-LARGE-BASELINE: verify generated package: %v", err)
	}
	if len(pkg.Baseline) <= 32<<20 || pkg.Manifest.BaseCommit != base || pkg.Change.BaselineLock.BaseTree != baseTree {
		t.Fatalf("ISSUE10-LARGE-BASELINE: baseline bytes=%d base=%s tree=%s", len(pkg.Baseline), pkg.Manifest.BaseCommit, pkg.Change.BaselineLock.BaseTree)
	}
	pkg.Baseline[len(pkg.Baseline)/2] ^= 1
	if err := baselineproof.Verify(pkg.Baseline, pkg.Manifest.GitObjectFormat, base, baseTree); err == nil {
		t.Fatal("ISSUE10-LARGE-BASELINE: tampered large baseline passed identity verification")
	}
}
