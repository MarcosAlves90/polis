package packagebuild

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
)

func TestPackageOutputDirectoryPinsExternalTargetAfterAliasRetarget(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "delivery")
	if err := os.Symlink(external, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}

	output, err := preparePackageOutput(repo, alias)
	if err != nil {
		t.Fatalf("prepare external output: %v", err)
	}
	defer output.Close()
	if inside, err := pathguard.Contains(repo, output.Path()); err != nil || inside {
		t.Fatalf("prepared output path=%q inside=%t err=%v", output.Path(), inside, err)
	}

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, alias); err != nil {
		t.Skipf("retarget directory symlink unavailable: %v", err)
	}

	candidate, candidateName, err := output.CreateTemp(".polis-candidate-*.tmp")
	if err != nil {
		t.Fatalf("create candidate through pinned output: %v", err)
	}
	if _, err := candidate.Write([]byte("candidate archive")); err != nil {
		candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	finalName := "polis-project-change-test.polis"
	final, err := output.CreateFileExclusive(finalName, 0o644)
	if err != nil {
		t.Fatalf("create final package through pinned output: %v", err)
	}
	if _, err := final.Write([]byte("final package")); err != nil {
		final.Close()
		t.Fatal(err)
	}
	if err := final.Close(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{candidateName, finalName} {
		outsidePath := filepath.Join(output.Path(), name)
		if inside, err := pathguard.Contains(repo, outsidePath); err != nil || inside {
			t.Fatalf("output file %q inside=%t err=%v", outsidePath, inside, err)
		}
		if _, err := os.Stat(outsidePath); err != nil {
			t.Fatalf("pinned output file %q missing: %v", outsidePath, err)
		}
		if err := output.Remove(name); err != nil {
			t.Fatalf("remove pinned output %q: %v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(repo, name)); !os.IsNotExist(err) {
			t.Fatalf("redirected file appeared in repository at %q: %v", filepath.Join(repo, name), err)
		}
	}
	if err := output.ValidateRequested(); err == nil {
		t.Fatal("retargeted output alias was accepted")
	}
}

func TestPackageOutputDirectoryRejectsInsideTargetBeforeCreating(t *testing.T) {
	repo := t.TempDir()
	inside := filepath.Join(repo, "delivery-output")
	output, err := preparePackageOutput(repo, inside)
	if err == nil {
		if output != nil {
			output.Close()
		}
		t.Fatal("in-worktree output path was accepted")
	}
	if _, err := os.Lstat(inside); !os.IsNotExist(err) {
		t.Fatalf("rejected output directory was created: %v", err)
	}
}

func TestPackageOutputDirectoryDoesNotFollowRetargetedMissingParent(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	missingParent := filepath.Join(external, "new-parent")
	requested := filepath.Join(missingParent, "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatalf("prepare missing external output: %v", err)
	}
	defer output.Close()

	if err := os.Symlink(repo, missingParent); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if err := output.Ensure(); err == nil {
		t.Fatal("retargeted missing parent was accepted")
	}
	if _, err := os.Lstat(filepath.Join(repo, "delivery")); !os.IsNotExist(err) {
		t.Fatalf("retargeted output created directory in repository: %v", err)
	}
}

func TestPackageOutputDirectoryCreatesMissingExternalTree(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "new-parent", "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatalf("prepare missing external output: %v", err)
	}
	defer output.Close()
	if err := output.Ensure(); err != nil {
		t.Fatalf("create external output tree: %v", err)
	}
	file, err := output.CreateFileExclusive("polis-created-output.polis", 0o644)
	if err != nil {
		t.Fatalf("create package in external output: %v", err)
	}
	if _, err := file.Write([]byte("package")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(requested, "polis-created-output.polis")); err != nil {
		t.Fatalf("external package missing: %v", err)
	}
	if err := output.Remove("polis-created-output.polis"); err != nil {
		t.Fatalf("remove external package: %v", err)
	}
}

func TestPackageOutputDirectoryRejectsMovedDirectoryBeforeFileCreation(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "delivery")
	if err := os.Mkdir(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatalf("prepare external output: %v", err)
	}
	defer output.Close()
	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(requested, moved); err != nil {
		t.Skipf("filesystem does not permit renaming an open output directory: %v", err)
	}

	candidate, candidateName, candidateErr := output.CreateTemp(".polis-candidate-*.tmp")
	if candidateErr == nil {
		candidate.Close()
		output.Remove(candidateName)
	}
	final, finalErr := output.CreateFileExclusive("polis-moved-output.polis", 0o644)
	if finalErr == nil {
		final.Close()
		output.Remove("polis-moved-output.polis")
	}
	if candidateErr == nil || finalErr == nil {
		t.Fatalf("file creation after moving output into repository was accepted: candidate=%v final=%v", candidateErr, finalErr)
	}
	entries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("moved repository directory contains package artifacts: %v", entries)
	}
}

func TestPackageOutputDirectoryCleansCandidateAfterDirectoryMove(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "delivery")
	if err := os.Mkdir(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatalf("prepare external output: %v", err)
	}
	defer output.Close()
	candidate, candidateName, err := output.CreateTemp(".polis-candidate-*.tmp")
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if _, err := candidate.Write([]byte("candidate archive")); err != nil {
		candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	defer output.Remove(candidateName)

	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(requested, moved); err != nil {
		t.Skipf("filesystem does not permit renaming an open output directory: %v", err)
	}
	if err := output.ValidateRequested(); err == nil {
		t.Error("moved output directory was still accepted as external")
	}
	if err := output.Remove(candidateName); err != nil {
		t.Fatalf("cleanup through pinned output handle: %v", err)
	}
	entries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("moved repository directory retains package artifacts: %v", entries)
	}
}

func TestPackageOutputDirectoryRejectsChangedExternalAlias(t *testing.T) {
	repo := t.TempDir()
	firstExternal := t.TempDir()
	secondExternal := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "delivery")
	if err := os.Symlink(firstExternal, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	output, err := preparePackageOutput(repo, alias)
	if err != nil {
		t.Fatalf("prepare external output: %v", err)
	}
	defer output.Close()
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secondExternal, alias); err != nil {
		t.Skipf("retarget directory symlink unavailable: %v", err)
	}
	if err := output.ValidateRequested(); err == nil {
		t.Fatal("output alias to another external directory was accepted")
	}
}

func TestPackageOutputDirectoryRejectsCyclicSymlinkPaths(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	loop := filepath.Join(external, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := preparePackageOutput(loop, external); err == nil {
		t.Fatal("cyclic repository root was accepted")
	}
	if _, err := preparePackageOutput(repo, loop); err == nil {
		t.Fatal("cyclic output path was accepted")
	}
}

func TestPackageOutputDirectoryRejectsNULOutputPath(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	invalid := filepath.Join(external, "invalid") + string([]byte{0})
	if _, err := preparePackageOutput(repo, invalid); err == nil {
		t.Fatal("NUL-containing output path was accepted")
	}
}

func TestPackageOutputDirectoryRejectsExistingFileTarget(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	filename := filepath.Join(external, "not-a-directory")
	if err := os.WriteFile(filename, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preparePackageOutput(repo, filename); err == nil {
		t.Fatal("regular file was accepted as an output directory")
	}
}

func TestPackageOutputDirectoryRejectsReplacedPinnedPath(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "delivery")
	if err := os.Mkdir(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.Rename(requested, filepath.Join(external, "moved-output")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := output.ValidatePinnedDirectory(); err == nil {
		t.Fatal("replacement directory was accepted as the pinned output")
	}
}

func TestPackageOutputDirectoryRejectsPinnedPathRepointedInsideRepository(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "delivery")
	if err := os.Mkdir(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(requested, moved); err != nil {
		t.Skipf("filesystem does not permit renaming an open output directory: %v", err)
	}
	if err := os.Symlink(moved, requested); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if err := output.ValidatePinnedDirectory(); err == nil {
		t.Fatal("pinned directory aliased into the repository was accepted")
	}
}

func TestPackageOutputDirectoryRejectsChildSymlink(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	canonicalExternal, err := pathguard.Resolve(external)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := openOutputDirectory(canonicalExternal)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Symlink(repo, filepath.Join(external, "redirect")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, err := openOutputChild(parent, "redirect"); err == nil {
		t.Fatal("directory-relative open followed a child symlink")
	}
}

func TestPackageOutputDirectoryRejectsClosedHandle(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	if err := output.dir.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.ValidatePinnedDirectory(); err == nil {
		t.Fatal("closed directory handle was accepted")
	}
	output.dir = nil
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPackageOutputDirectoryCreateCollisionAndIdempotentRemove(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	name := "collision.polis"
	file, err := output.CreateFileExclusive(name, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := output.CreateFileExclusive(name, 0o644); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected output collision, got %v", err)
	}
	if err := output.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := output.Remove(name); err != nil {
		t.Fatalf("removing an absent output should be idempotent: %v", err)
	}
}

func TestPackageOutputDirectoryRemoveRejectsDirectory(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.Mkdir(filepath.Join(external, "subdirectory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := output.Remove("subdirectory"); err == nil {
		t.Fatal("file cleanup removed a directory")
	}
}

func TestPackageOutputDirectoryCanOpenExistingPreparedTree(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "parent", "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.MkdirAll(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := output.Ensure(); err != nil {
		t.Fatalf("open precreated external output tree: %v", err)
	}
}

func TestPackageOutputDirectoryRejectsInvalidPreparedState(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	canonicalExternal, err := pathguard.Resolve(external)
	if err != nil {
		t.Fatal(err)
	}
	output := &packageOutput{repo: repo, requested: canonicalExternal, requestedAbs: canonicalExternal, path: canonicalExternal}
	if err := output.Ensure(); err == nil || !strings.Contains(err.Error(), "not prepared") {
		t.Fatalf("expected unprepared output directory error, got %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPackageOutputDirectoryRejectsInvalidNamesAndPatterns(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if _, _, err := output.CreateTemp("invalid-pattern"); err == nil {
		t.Fatal("invalid temporary filename pattern was accepted")
	}
	if _, err := output.CreateFileExclusive("../escape.polis", 0o644); err == nil {
		t.Fatal("path-traversing final filename was accepted")
	}
	if err := output.Remove("../escape.polis"); err == nil {
		t.Fatal("path-traversing removal name was accepted")
	}
}

func TestPackageOutputDirectoryRejectsNonDirectoryAncestor(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	file := filepath.Join(external, "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := preparePackageOutput(repo, filepath.Join(file, "delivery")); err == nil {
		t.Fatal("output below a regular-file ancestor was accepted")
	}
}

func TestPackageOutputDirectoryVerifyReaderAtRejectsInvalidReaderAndSize(t *testing.T) {
	if _, err := packageverify.VerifyReaderAt(nil, 0); err == nil {
		t.Fatal("nil archive reader was accepted")
	}
	if _, err := packageverify.VerifyReaderAt(bytes.NewReader(nil), -1); err == nil {
		t.Fatal("negative archive size was accepted")
	}
	if _, err := packageverify.VerifyReaderAt(bytes.NewReader(nil), packageverify.MaxArchiveBytes+1); err == nil {
		t.Fatal("oversize archive was accepted")
	}
}

func TestPackageOutputDirectoryFinalizeRequiresPreparedOutputDirectory(t *testing.T) {
	if _, err := finalizeArtifact(buildArtifact{}, nil, nil); err == nil {
		t.Fatal("finalization without a prepared output directory was accepted")
	}
}

func TestPackageOutputDirectoryCopyRejectsNonDirectoryParent(t *testing.T) {
	scratch := t.TempDir()
	source := filepath.Join(scratch, "source")
	if err := os.WriteFile(source, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(scratch, "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyExclusive(source, filepath.Join(parent, "target.polis")); err == nil {
		t.Fatal("copy to a target below a regular file was accepted")
	}
}

func TestPackageOutputDirectoryRejectsInitialSymlinkIntoRepository(t *testing.T) {
	repo := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "delivery")
	if err := os.Symlink(repo, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, err := preparePackageOutput(repo, alias); err == nil {
		t.Fatal("symlink resolving into repository was accepted")
	}
}

func TestPackageOutputDirectoryRejectsFinalSymlinkPath(t *testing.T) {
	repo := t.TempDir()
	external, err := pathguard.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	redirect := filepath.Join(external, "redirect")
	if err := os.Symlink(repo, redirect); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, err := openOutputDirectory(redirect); err == nil {
		t.Fatal("directory open followed a final symlink")
	}
}

func TestPackageOutputDirectoryRejectsWritesWhenDestinationIsReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL behavior is not modeled by POSIX mode bits")
	}
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.Chmod(external, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(external, 0o700) })
	probe, err := os.OpenFile(filepath.Join(external, "permission-probe"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err == nil {
		probe.Close()
		os.Remove(filepath.Join(external, "permission-probe"))
		t.Skip("filesystem does not enforce directory write permission for this user")
	}
	if file, name, err := output.CreateTemp(".polis-candidate-*.tmp"); err == nil {
		file.Close()
		output.Remove(name)
		t.Fatal("candidate creation succeeded in a read-only output directory")
	}
	if file, err := output.CreateFileExclusive("readonly.polis", 0o644); err == nil {
		file.Close()
		output.Remove("readonly.polis")
		t.Fatal("final artifact creation succeeded in a read-only output directory")
	}
}

func TestPackageOutputDirectoryCopyFromClosedCandidateRemovesFinal(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	candidatePath := filepath.Join(t.TempDir(), "candidate.tmp")
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(candidatePath)
	candidate, err := os.Open(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copyCandidateToOutput(candidate, output, "copy-failure.polis"); err == nil {
		t.Fatal("copy from a closed candidate was accepted")
	}
	if _, err := os.Stat(filepath.Join(output.Path(), "copy-failure.polis")); !os.IsNotExist(err) {
		t.Fatalf("failed final copy left an artifact: %v", err)
	}
}

func TestPackageOutputDirectoryHashRejectsClosedFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed-hash-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fileSHA256Open(file); err == nil {
		t.Fatal("hashing a closed candidate file was accepted")
	}
}

func TestPackageOutputDirectoryCopyRejectsDirectorySourceAndCleansFinal(t *testing.T) {
	sourceDirectory := t.TempDir()
	targetDirectory := t.TempDir()
	target := filepath.Join(targetDirectory, "directory-source.polis")
	if err := copyExclusive(sourceDirectory, target); err == nil {
		t.Fatal("copying a directory as a package was accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed directory copy left a package artifact: %v", err)
	}
}

func TestPackageOutputDirectoryOpenFilesystemRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem root traversal is Unix-specific")
	}
	root, err := openOutputDirectory(string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPackageOutputDirectoryFileHashRejectsMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-candidate.polis")
	if _, err := fileSHA256(missing); err == nil {
		t.Fatal("hashing a missing candidate file was accepted")
	}
}

func TestPackageOutputDirectoryCopyCollisionPreservesExistingFile(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	const name = "preserve-existing.polis"
	existing, err := output.CreateFileExclusive(name, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("original package")
	if _, err := existing.Write(original); err != nil {
		existing.Close()
		t.Fatal(err)
	}
	if err := existing.Close(); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(t.TempDir(), "candidate.tmp")
	if err := os.WriteFile(candidatePath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.Open(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := copyCandidateToOutput(candidate, output, name); err == nil {
		t.Fatal("copy overwrote an existing final artifact")
	}
	got, err := os.ReadFile(filepath.Join(output.Path(), name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("existing package was modified: got %q", got)
	}
}

func TestPackageOutputDirectoryCopyFromDirectoryCandidateCleansFinal(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	sourceDirectory := t.TempDir()
	candidate, err := os.Open(sourceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := copyCandidateToOutput(candidate, output, "directory-candidate.polis"); err == nil {
		t.Fatal("copying a directory candidate was accepted")
	}
	if _, err := os.Stat(filepath.Join(output.Path(), "directory-candidate.polis")); !os.IsNotExist(err) {
		t.Fatalf("failed directory candidate copy left a final artifact: %v", err)
	}
}

func TestPackageOutputDirectoryEnsureRejectsParentMovedIntoRepository(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "nested", "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	moved := filepath.Join(repo, "moved-output-parent")
	if err := os.Rename(external, moved); err != nil {
		t.Skipf("filesystem does not permit moving an open parent directory: %v", err)
	}
	if err := output.Ensure(); err == nil {
		t.Fatal("moved external parent was accepted")
	}
	entries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("moved parent contains generated output: %v", entries)
	}
}

func TestPackageOutputDirectoryEnsureRejectsReplacedPinnedParent(t *testing.T) {
	repo := t.TempDir()
	container := t.TempDir()
	external := filepath.Join(container, "external")
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatal(err)
	}
	requested := filepath.Join(external, "nested", "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	moved := filepath.Join(container, "moved-pinned-parent")
	if err := os.Rename(external, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(external, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := output.Ensure(); err == nil {
		t.Fatal("replacement parent directory was accepted as the pinned parent")
	}
	if _, err := os.Stat(filepath.Join(external, "nested")); !os.IsNotExist(err) {
		t.Fatalf("replacement parent received output directories: %v", err)
	}
}

func TestPackageOutputDirectoryRejectsRetargetedSymlinkLoop(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "delivery")
	if err := os.Symlink(external, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	output, err := preparePackageOutput(repo, alias)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alias, alias); err != nil {
		t.Skipf("symlink loops unavailable: %v", err)
	}
	if err := output.ValidateRequested(); err == nil {
		t.Fatal("retargeted symlink loop was accepted")
	}
}

func TestPackageOutputDirectoryEnsureRejectsReadOnlyParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL behavior is not modeled by POSIX mode bits")
	}
	repo := t.TempDir()
	external := t.TempDir()
	requested := filepath.Join(external, "nested", "delivery")
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := os.Chmod(external, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(external, 0o700) })
	probe, err := os.OpenFile(filepath.Join(external, "permission-probe"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err == nil {
		probe.Close()
		os.Remove(filepath.Join(external, "permission-probe"))
		t.Skip("filesystem does not enforce directory write permission for this user")
	}
	if err := output.Ensure(); err == nil {
		t.Fatal("output directory creation succeeded below a read-only parent")
	}
	if _, err := os.Stat(filepath.Join(external, "nested")); !os.IsNotExist(err) {
		t.Fatalf("failed creation left an output directory: %v", err)
	}
}

func TestPackageOutputDirectoryFileHashRejectsDirectory(t *testing.T) {
	if _, err := fileSHA256(t.TempDir()); err == nil {
		t.Fatal("hashing a directory as an archive was accepted")
	}
}

func TestPackageOutputDirectoryCopyRejectsCyclicTargetParent(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlink loops unavailable: %v", err)
	}
	if err := copyExclusive(source, filepath.Join(loop, "target.polis")); err == nil {
		t.Fatal("copy through a cyclic target parent was accepted")
	}
}

func TestPackageOutputDirectoryCopyRejectsInvalidFinalName(t *testing.T) {
	repo := t.TempDir()
	base := t.TempDir()
	requested := filepath.Join(base, "delivery")
	if err := os.Mkdir(requested, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	candidatePath := filepath.Join(t.TempDir(), "candidate.tmp")
	if err := os.WriteFile(candidatePath, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.Open(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if err := copyCandidateToOutput(candidate, output, "../escape.polis"); err == nil {
		t.Fatal("copy with a path-traversing final name was accepted")
	}
	if _, err := os.Stat(filepath.Join(base, "escape.polis")); !os.IsNotExist(err) {
		t.Fatalf("rejected final name created an artifact outside the output directory: %v", err)
	}
}

func TestPackageOutputDirectoryCopyRejectsMissingSource(t *testing.T) {
	scratch := t.TempDir()
	source := filepath.Join(scratch, "missing-candidate.polis")
	target := filepath.Join(scratch, "missing-source-target.polis")
	if err := copyExclusive(source, target); err == nil {
		t.Fatal("copy from a missing candidate was accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed copy from a missing candidate created output: %v", err)
	}
}

func TestPackageOutputDirectoryRejectsInvalidRepositoryBoundary(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	output, err := preparePackageOutput(repo, external)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	loop := filepath.Join(t.TempDir(), "repository-loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlink loops unavailable: %v", err)
	}
	output.repo = loop
	if err := output.ValidatePinnedDirectory(); err == nil {
		t.Fatal("invalid repository boundary was accepted for the pinned output")
	}
	if err := output.ValidateRequested(); err == nil {
		t.Fatal("invalid repository boundary was accepted for the requested output")
	}
}
