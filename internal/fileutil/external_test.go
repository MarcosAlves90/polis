package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteOutsideExclusiveWritesPrivateReport(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "workspace.json")
	data := []byte("{\"status\":\"PASS\"}\n")
	if err := WriteOutsideExclusive(repo, path, data); err != nil {
		t.Fatalf("WriteOutsideExclusive() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("report=%q want=%q", got, data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions=%#o want=0600", got)
		}
	}
}

func TestWriteOutsideExclusiveRejectsExistingAndInRepositoryPaths(t *testing.T) {
	repo := t.TempDir()
	inside := filepath.Join(repo, "report.json")
	if err := WriteOutsideExclusive(repo, inside, []byte("new")); err == nil {
		t.Fatal("WriteOutsideExclusive() accepted an in-repository output")
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("in-repository output was created: %v", err)
	}

	outside := t.TempDir()
	existing := filepath.Join(outside, "report.json")
	if err := os.WriteFile(existing, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOutsideExclusive(repo, existing, []byte("replace")); err == nil {
		t.Fatal("WriteOutsideExclusive() overwrote an existing output")
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "preserve" {
		t.Fatalf("existing output changed: %q", got)
	}
}

func TestWriteOutsideExclusiveRejectsPhysicalRepositoryAlias(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "repo-alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(alias, "report.json")
	if err := WriteOutsideExclusive(repo, path, []byte("report")); err == nil {
		t.Fatal("WriteOutsideExclusive() accepted a symlinked in-repository output")
	}
	if _, err := os.Stat(filepath.Join(repo, "report.json")); !os.IsNotExist(err) {
		t.Fatalf("aliased in-repository output was created: %v", err)
	}
}
