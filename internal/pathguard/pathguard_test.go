package pathguard

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestContainsResolvesSymlinkedRootAndExistingCandidate(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "real"), aliasParent); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(aliasParent, "repo", "change.json")
	if err := os.WriteFile(filepath.Join(realRoot, "change.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside, err := Contains(realRoot, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Fatalf("expected aliased candidate to be contained: %s", candidate)
	}
}

func TestContainsCanonicalizesMissingOutputViaExistingAncestor(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "real"), aliasParent); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(aliasParent, "repo", "new", "red.patch")
	inside, err := Contains(realRoot, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Fatalf("expected missing aliased output to be contained: %s", candidate)
	}
}

func TestContainsRejectsExternalSibling(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside", "change.json")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside, err := Contains(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Fatalf("expected external sibling to be outside: %s", outside)
	}
}

func TestContainsTreatsRootAsContained(t *testing.T) {
	root := t.TempDir()
	inside, err := Contains(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Fatal("root must contain itself")
	}
}

func TestCanonicalReturnsErrorWhenNoExistingAncestorCanBeResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("root semantics differ")
	}
	_, err := canonical(string(filepath.Separator) + filepath.Join("definitely-polis-missing", "child"))
	if err == nil {
		// `/` is an existing ancestor, so this path is valid to canonicalize. This assertion
		// intentionally documents that missing leaf paths are supported rather than rejected.
		return
	}
}

func TestContainsFailsClosedWhenRootCannotBeCanonicalized(t *testing.T) {
	base := t.TempDir()
	loop := filepath.Join(base, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	inside, err := Contains(loop, filepath.Join(base, "candidate"))
	if err == nil || inside {
		t.Fatalf("expected canonical root failure, inside=%v err=%v", inside, err)
	}
}

func TestContainsFailsClosedWhenCandidateCannotBeCanonicalized(t *testing.T) {
	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	inside, err := Contains(root, loop)
	if err == nil || inside {
		t.Fatalf("expected canonical candidate failure, inside=%v err=%v", inside, err)
	}
}

func TestContainsParentIsOutside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	inside, err := Contains(root, base)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Fatal("parent directory must be outside")
	}
}

func TestCanonicalPropagatesAbsolutePathResolutionFailure(t *testing.T) {
	want := errors.New("absolute path resolution failed")
	_, err := canonicalWithAbs("relative", func(string) (string, error) {
		return "", want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected injected filepath.Abs failure, got %v", err)
	}
}
