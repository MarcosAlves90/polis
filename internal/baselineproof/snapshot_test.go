package baselineproof

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func testRepo(t *testing.T, objectFormat string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q", "--object-format="+objectFormat)
	runGit(t, repo, "config", "user.name", "POLIS Test")
	runGit(t, repo, "config", "user.email", "polis@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "plain.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain.txt", filepath.Join(repo, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "binary.dat"), []byte{0, 1, 2, 3, 0xff, 0x00, 0x80}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "baseline")
	return repo
}

func TestBuildVerifyAndMaterializePreservesNativeBaseline(t *testing.T) {
	for _, objectFormat := range []string{"sha1", "sha256"} {
		t.Run(objectFormat, func(t *testing.T) {
			repo := testRepo(t, objectFormat)
			base := runGit(t, repo, "rev-parse", "HEAD")
			tree := runGit(t, repo, "rev-parse", "HEAD^{tree}")

			snapshot, err := Build(context.Background(), repo, base, 64<<20)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if err := Verify(snapshot, objectFormat, base, tree); err != nil {
				t.Fatalf("Verify() error = %v", err)
			}

			baselineRepo, cleanup, err := Materialize(context.Background(), snapshot, objectFormat, base, tree)
			if err != nil {
				t.Fatalf("Materialize() error = %v", err)
			}
			defer cleanup()
			if got := runGit(t, baselineRepo, "rev-parse", base+"^{tree}"); got != tree {
				t.Fatalf("tree=%s want=%s", got, tree)
			}
			worktree := filepath.Join(t.TempDir(), "checkout")
			runGit(t, baselineRepo, "clone", "--quiet", "--no-checkout", "--shared", ".", worktree)
			runGit(t, worktree, "checkout", "--quiet", "--detach", base)
			if info, err := os.Lstat(filepath.Join(worktree, "link.txt")); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlink not preserved: info=%v err=%v", info, err)
			}
			if info, err := os.Stat(filepath.Join(worktree, "run.sh")); err != nil || info.Mode()&0o111 == 0 {
				t.Fatalf("executable bit not preserved: info=%v err=%v", info, err)
			}
			gotBinary, err := os.ReadFile(filepath.Join(worktree, "binary.dat"))
			if err != nil {
				t.Fatal(err)
			}
			if string(gotBinary) != string([]byte{0, 1, 2, 3, 0xff, 0x00, 0x80}) {
				t.Fatalf("binary content changed: %v", gotBinary)
			}
		})
	}
}

func TestVerifyRejectsTamperingAndWrongTree(t *testing.T) {
	repo := testRepo(t, "sha1")
	base := runGit(t, repo, "rev-parse", "HEAD")
	tree := runGit(t, repo, "rev-parse", "HEAD^{tree}")
	snapshot, err := Build(context.Background(), repo, base, 64<<20)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), snapshot...)
	tampered[len(tampered)/2] ^= 0x01
	if err := Verify(tampered, "sha1", base, tree); err == nil {
		t.Fatal("expected tampered snapshot rejection")
	}
	if err := Verify(snapshot, "sha1", base, strings.Repeat("0", 40)); err == nil {
		t.Fatal("expected wrong tree rejection")
	}
}

func TestVerifyRejectsIncompleteAndUnrelatedObjectSets(t *testing.T) {
	repo := testRepo(t, "sha1")
	base := runGit(t, repo, "rev-parse", "HEAD")
	tree := runGit(t, repo, "rev-parse", "HEAD^{tree}")
	snapshot, err := Build(context.Background(), repo, base, 64<<20)
	if err != nil {
		t.Fatal(err)
	}

	objects, err := parse(snapshot, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	missing := make(map[string]object, len(objects))
	for oid, obj := range objects {
		missing[oid] = obj
	}
	for oid, obj := range missing {
		if obj.Type == "blob" {
			delete(missing, oid)
			break
		}
	}
	incompleteRaw, err := encode(mapValues(missing))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(incompleteRaw, "sha1", base, tree); err == nil || !strings.Contains(err.Error(), "missing blob") {
		t.Fatalf("expected missing blob rejection, got %v", err)
	}

	extraData := []byte("unrelated baseline object\n")
	extraOID, err := objectHash("sha1", "blob", extraData)
	if err != nil {
		t.Fatal(err)
	}
	withExtra := make(map[string]object, len(objects)+1)
	for oid, obj := range objects {
		withExtra[oid] = obj
	}
	withExtra[extraOID] = object{Type: "blob", OID: extraOID, Data: extraData}
	extraRaw, err := encode(mapValues(withExtra))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(extraRaw, "sha1", base, tree); err == nil || !strings.Contains(err.Error(), "unrelated objects") {
		t.Fatalf("expected unrelated object rejection, got %v", err)
	}
}

func TestVerifyRejectsTruncatedAndWrongCommitIdentity(t *testing.T) {
	repo := testRepo(t, "sha1")
	base := runGit(t, repo, "rev-parse", "HEAD")
	tree := runGit(t, repo, "rev-parse", "HEAD^{tree}")
	snapshot, err := Build(context.Background(), repo, base, 64<<20)
	if err != nil {
		t.Fatal(err)
	}

	if err := Verify(snapshot[:len(snapshot)/2], "sha1", base, tree); err == nil {
		t.Fatal("expected truncated snapshot rejection")
	}
	if err := Verify(snapshot, "sha1", strings.Repeat("0", 40), tree); err == nil || !strings.Contains(err.Error(), "missing commit") {
		t.Fatalf("expected wrong commit identity rejection, got %v", err)
	}
}

func TestVerifyRejectsMalformedCommitObject(t *testing.T) {
	repo := testRepo(t, "sha1")
	tree := runGit(t, repo, "rev-parse", "HEAD^{tree}")
	malformed := []byte("tree " + tree + "\nnot-a-valid-commit-body\n")
	oid, err := objectHash("sha1", "commit", malformed)
	if err != nil {
		t.Fatal(err)
	}
	original, err := Build(context.Background(), repo, runGit(t, repo, "rev-parse", "HEAD"), 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := parse(original, "sha1")
	if err != nil {
		t.Fatal(err)
	}
	delete(objects, runGit(t, repo, "rev-parse", "HEAD"))
	objects[oid] = object{Type: "commit", OID: oid, Data: malformed}
	raw, err := encode(mapValues(objects))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(raw, "sha1", oid, tree); err == nil {
		t.Fatal("expected malformed commit rejection")
	}
}

func TestBuildRejectsProjectedSnapshotAboveLimit(t *testing.T) {
	repo := testRepo(t, "sha1")
	if err := os.WriteFile(filepath.Join(repo, "large.txt"), []byte(strings.Repeat("x", 4096)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "large.txt")
	runGit(t, repo, "commit", "-qm", "large baseline")
	base := runGit(t, repo, "rev-parse", "HEAD")
	if _, err := Build(context.Background(), repo, base, 2048); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected projected size rejection, got %v", err)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	repo := testRepo(t, "sha1")
	base := runGit(t, repo, "rev-parse", "HEAD")
	first, err := Build(context.Background(), repo, base, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), repo, base, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("baseline snapshot bytes are not deterministic")
	}
}
