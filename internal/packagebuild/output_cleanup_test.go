package packagebuild

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPackageOutputCleanupReportsAndContinues(t *testing.T) {
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
	candidate, name, err := output.CreateTemp("candidate-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPackageOutputFiles(output, "../invalid", name); err == nil {
		t.Fatal("cleanup validation error was suppressed")
	}
	if _, err := os.Stat(filepath.Join(requested, name)); !os.IsNotExist(err) {
		t.Fatalf("cleanup stopped before removing the valid candidate: %v", err)
	}
}

func TestPackageOutputCleanupReportsDeniedUnlinkAfterMove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL behavior is not modeled by POSIX mode bits")
	}
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
	candidate, err := output.CreateFileExclusive("candidate.dat", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(requested, moved); err != nil {
		t.Skipf("filesystem does not permit renaming an open output directory: %v", err)
	}
	if err := os.Chmod(moved, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(moved, 0o700) })
	probe, err := os.OpenFile(filepath.Join(moved, "permission-probe"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err == nil {
		probe.Close()
		os.Remove(filepath.Join(moved, "permission-probe"))
		os.Chmod(moved, 0o700)
		t.Skip("filesystem does not enforce directory write permission for this user")
	}
	if err := cleanupPackageOutputFiles(output, "candidate.dat"); err == nil {
		t.Fatal("cleanup failure after relocation was suppressed")
	}
	if _, err := os.Stat(filepath.Join(moved, "candidate.dat")); err != nil {
		t.Fatalf("test precondition failed; denied cleanup unexpectedly removed the file: %v", err)
	}
	if err := os.Chmod(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPackageOutputFiles(output, "candidate.dat"); err != nil {
		t.Fatalf("cleanup retry after restoring permissions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "candidate.dat")); !os.IsNotExist(err) {
		t.Fatalf("cleanup retry left a candidate: %v", err)
	}
}

func TestPackageOutputCleanupRevalidatesMovedDirectoryBeforeSuccess(t *testing.T) {
	repo := t.TempDir()
	base := t.TempDir()
	output, candidate, candidateName, finalName := prepareCleanupTestArtifacts(t, repo, base)
	defer output.Close()
	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(output.Path(), moved); err != nil {
		t.Skipf("filesystem does not permit moving an open output directory: %v", err)
	}
	if err := finishPackageOutput(output, candidate, candidateName, finalName, true, nil); err == nil {
		t.Fatal("finalization reported success after moving its output directory into the repository")
	}
	entries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("late relocation cleanup left repository artifacts: %v", entries)
	}
}

func TestPackageOutputCleanupPreservesOnlyValidFinalArtifact(t *testing.T) {
	repo := t.TempDir()
	base := t.TempDir()
	output, candidate, candidateName, finalName := prepareCleanupTestArtifacts(t, repo, base)
	defer output.Close()
	if err := finishPackageOutput(output, candidate, candidateName, finalName, true, nil); err != nil {
		t.Fatalf("finish valid external output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output.Path(), candidateName)); !os.IsNotExist(err) {
		t.Fatalf("candidate was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output.Path(), finalName)); err != nil {
		t.Fatalf("final artifact missing after successful finish: %v", err)
	}
}

func TestPackageOutputCleanupRetainsPrimaryErrorAndRemovesFinal(t *testing.T) {
	repo := t.TempDir()
	base := t.TempDir()
	output, candidate, candidateName, finalName := prepareCleanupTestArtifacts(t, repo, base)
	defer output.Close()
	primaryErr := errors.New("injected producer failure")
	if err := finishPackageOutput(output, candidate, candidateName, finalName, true, primaryErr); !errors.Is(err, primaryErr) {
		t.Fatalf("primary failure was lost during cleanup: %v", err)
	}
	for _, name := range []string{candidateName, finalName} {
		if _, err := os.Stat(filepath.Join(output.Path(), name)); !os.IsNotExist(err) {
			t.Fatalf("failed finalization left %s: %v", name, err)
		}
	}
}

func prepareCleanupTestArtifacts(t *testing.T, repo, base string) (*packageOutput, *os.File, string, string) {
	t.Helper()
	requested := filepath.Join(base, "delivery")
	if err := os.Mkdir(requested, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := preparePackageOutput(repo, requested)
	if err != nil {
		t.Fatal(err)
	}
	candidate, candidateName, err := output.CreateTemp("candidate-*.tmp")
	if err != nil {
		output.Close()
		t.Fatal(err)
	}
	if _, err := candidate.Write([]byte("candidate archive")); err != nil {
		candidate.Close()
		output.Close()
		t.Fatal(err)
	}
	finalName := "final.polis"
	final, err := output.CreateFileExclusive(finalName, 0o600)
	if err != nil {
		candidate.Close()
		output.Close()
		t.Fatal(err)
	}
	if _, err := final.Write([]byte("final archive")); err != nil {
		final.Close()
		candidate.Close()
		output.Close()
		t.Fatal(err)
	}
	if err := final.Close(); err != nil {
		candidate.Close()
		output.Close()
		t.Fatal(err)
	}
	return output, candidate, candidateName, finalName
}

func TestPackageOutputCleanupReportsFinalRemovalFailureAfterMove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL behavior is not modeled by POSIX mode bits")
	}
	repo := t.TempDir()
	base := t.TempDir()
	output, candidate, candidateName, finalName := prepareCleanupTestArtifacts(t, repo, base)
	defer output.Close()
	moved := filepath.Join(repo, "moved-output")
	if err := os.Rename(output.Path(), moved); err != nil {
		t.Skipf("filesystem does not permit renaming an open output directory: %v", err)
	}
	if err := os.Chmod(moved, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(moved, 0o700) })
	probe, err := os.OpenFile(filepath.Join(moved, "permission-probe"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err == nil {
		probe.Close()
		os.Remove(filepath.Join(moved, "permission-probe"))
		os.Chmod(moved, 0o700)
		t.Skip("filesystem does not enforce directory write permission for this user")
	}
	if err := finishPackageOutput(output, candidate, candidateName, finalName, true, nil); err == nil {
		t.Fatal("final cleanup failure after relocation was suppressed")
	}
	entries, err := os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected both artifacts to remain after denied unlink, got %v", entries)
	}
	if err := os.Chmod(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPackageOutputFiles(output, candidateName, finalName); err != nil {
		t.Fatalf("cleanup retry after restoring permissions: %v", err)
	}
	entries, err = os.ReadDir(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cleanup retry left artifacts: %v", entries)
	}
}
