package artifactretention

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestDecodeRejectsNonCanonicalManifestShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown field", raw: `{"schema_version":1,"mode":"repository","extra":true}`},
		{name: "case alias", raw: `{"schema_version":1,"Mode":"repository"}`},
		{name: "duplicate field", raw: `{"schema_version":1,"mode":"external","mode":"repository"}`},
		{name: "missing field", raw: `{"schema_version":1}`},
		{name: "null version", raw: `{"schema_version":null,"mode":"repository"}`},
		{name: "fractional version", raw: `{"schema_version":1.0,"mode":"repository"}`},
		{name: "unsupported version", raw: `{"schema_version":2,"mode":"repository"}`},
		{name: "unsupported mode", raw: `{"schema_version":1,"mode":"local"}`},
		{name: "null mode", raw: `{"schema_version":1,"mode":null}`},
		{name: "trailing JSON", raw: `{"schema_version":1,"mode":"external"}{}`},
		{name: "not object", raw: `[]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode([]byte(test.raw)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestDecodeRejectsMalformedAndOversizedManifestBytes(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "invalid UTF-8", raw: []byte{0xff}},
		{name: "malformed JSON", raw: []byte(`{"schema_version":`)},
		{name: "unclosed object", raw: []byte(`{"schema_version":1,"mode":"repository"`)},
		{name: "wrong mode type", raw: []byte(`{"schema_version":1,"mode":1}`)},
		{name: "malformed trailing data", raw: []byte(`{"schema_version":1,"mode":"repository"} garbage`)},
		{name: "oversized", raw: bytes.Repeat([]byte("x"), maxManifest+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(test.raw); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestCheckRegularPathRejectsTraversalAndUnsafeComponents(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tests := []string{"../outside", "regular/child", "directory", "link/file"}
	for _, relative := range tests {
		t.Run(relative, func(t *testing.T) {
			if err := checkRegularPath(root, relative); err == nil {
				t.Fatalf("unsafe path %q accepted", relative)
			}
		})
	}
}

func TestDecodeAcceptsExternalAndRepositoryModes(t *testing.T) {
	for _, mode := range []Mode{ModeExternal, ModeRepository} {
		raw := []byte(`{"schema_version":1,"mode":"` + string(mode) + `"}`)
		config, err := Decode(raw)
		if err != nil {
			t.Fatalf("Decode(%s): %v", mode, err)
		}
		if config.SchemaVersion != 1 || config.Mode != mode {
			t.Fatalf("config=%+v", config)
		}
	}
}

func TestLoadUsesCommittedManifestAndDefaultsOnlyWhenAbsent(t *testing.T) {
	t.Run("absent defaults external", func(t *testing.T) {
		repo := newRetentionRepo(t, nil)
		state, err := Load(context.Background(), repo)
		if err != nil {
			t.Fatal(err)
		}
		if state.Mode() != ModeExternal {
			t.Fatalf("mode=%q", state.Mode())
		}
	})
	t.Run("uncommitted manifest rejected", func(t *testing.T) {
		repo := newRetentionRepo(t, nil)
		writeManifest(t, repo, ModeRepository)
		if _, err := Load(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "must be committed") {
			t.Fatalf("Load error=%v", err)
		}
	})
	t.Run("committed repository mode", func(t *testing.T) {
		repo := newRetentionRepo(t, manifestBytes(ModeRepository))
		state, err := Load(context.Background(), repo)
		if err != nil {
			t.Fatal(err)
		}
		if !state.RepositoryEnabled() {
			t.Fatalf("mode=%q", state.Mode())
		}
	})
	t.Run("working mismatch rejected", func(t *testing.T) {
		repo := newRetentionRepo(t, manifestBytes(ModeRepository))
		writeManifest(t, repo, ModeExternal)
		if _, err := Load(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "differs from committed HEAD") {
			t.Fatalf("Load error=%v", err)
		}
	})
	t.Run("oversized committed manifest rejected before decode", func(t *testing.T) {
		manifest := append(manifestBytes(ModeRepository), bytes.Repeat([]byte(" "), maxManifest)...)
		repo := newRetentionRepo(t, manifest)
		if _, err := Load(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "manifest size") {
			t.Fatalf("Load error=%v", err)
		}
	})
	t.Run("manifest symlink rejected", func(t *testing.T) {
		repo := newRetentionRepo(t, manifestBytes(ModeRepository))
		filename := filepath.Join(repo, filepath.FromSlash(ManifestPath))
		if err := os.Remove(filename); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(outside, manifestBytes(ModeRepository), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filename); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Load(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Load error=%v", err)
		}
	})
}

func TestLoadFailsClosedWhenCommittedManifestIsMissingOrRepositoryIsInvalid(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(ManifestPath))); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), repo); err == nil {
		t.Fatal("missing committed manifest accepted")
	}
	if _, err := Load(context.Background(), t.TempDir()); err == nil {
		t.Fatal("non-repository accepted")
	}
}

func TestValidateTreeEntryRequiresARegularManifestBlob(t *testing.T) {
	for _, entry := range []string{
		"malformed",
		"100644 blob 0123456789abcdef\t.polis/other.json",
		"120000 blob 0123456789abcdef\t" + ManifestPath,
		"040000 tree 0123456789abcdef\t" + ManifestPath,
	} {
		if err := validateTreeEntry(entry); err == nil {
			t.Fatalf("invalid Git tree entry %q accepted", entry)
		}
	}
	if err := validateTreeEntry("100644 blob 0123456789abcdef\t" + ManifestPath); err != nil {
		t.Fatalf("valid Git tree entry rejected: %v", err)
	}
}

func TestRelativePathUnderRootAcceptsTheRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	relative, err := relativePathUnderRoot(root, root)
	if err != nil || relative != "." {
		t.Fatalf("relativePathUnderRoot(%q, %q)=%q err=%v", root, root, relative, err)
	}
}

func TestPublishIsContentAddressedIdempotentAndNonOverwriting(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("exact contract bytes\n")
	sum := sha256.Sum256(data)
	want := ".polis/artifacts/contracts/sha256-" + hex.EncodeToString(sum[:]) + ".json"
	path, err := state.Publish(repo, "contracts", data)
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("path=%q want=%q", path, want)
	}
	if path2, err := state.Publish(repo, "contracts", data); err != nil || path2 != path {
		t.Fatalf("idempotent publish path=%q err=%v", path2, err)
	}
	filename := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.WriteFile(filename, []byte("different bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Publish(repo, "contracts", data); err == nil || !strings.Contains(err.Error(), "different bytes") {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestPublishManyPreflightsAndPublishesExactArtifacts(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := state.PublishMany(repo, []Artifact{
		{Class: "contracts", Data: []byte("contract")},
		{Class: "proofs", Data: []byte("patch")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "/contracts/") || !strings.Contains(paths[1], "/proofs/") {
		t.Fatalf("paths=%v", paths)
	}
	for index, data := range [][]byte{[]byte("contract"), []byte("patch")} {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(paths[index])))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("artifact %q bytes=%q err=%v", paths[index], got, err)
		}
	}
}

func TestPublishManyStreamsFileBackedArtifact(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("package-bytes-"), 4096)
	source := filepath.Join(t.TempDir(), "artifact.polis")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := state.PublishMany(repo, []Artifact{{Class: "packages", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.Contains(paths[0], "/packages/") {
		t.Fatalf("paths=%v", paths)
	}
	got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(paths[0])))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("retained package bytes differ from source")
	}
	if _, err := state.PublishMany(repo, []Artifact{{Class: "packages", Source: source}}); err != nil {
		t.Fatalf("file-backed publish should be idempotent: %v", err)
	}
	if _, err := state.PublishMany(repo, []Artifact{{Class: "packages", Data: data, Source: source}}); err == nil || !strings.Contains(err.Error(), "data or source") {
		t.Fatalf("ambiguous file-backed artifact error=%v", err)
	}
}

func TestPublishManyRejectsInvalidFileBackedArtifacts(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	empty := filepath.Join(t.TempDir(), "empty.polis")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "missing", source: filepath.Join(t.TempDir(), "missing.polis"), want: "open retained packages source"},
		{name: "directory", source: directory, want: "source must be a regular file"},
		{name: "empty", source: empty, want: "artifact must not be empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := state.PublishMany(repo, []Artifact{{Class: "packages", Source: tc.source}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishMany() error=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestPublishManyValidatesBeforeWritingAndDeduplicatesPaths(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if paths, err := state.PublishMany(repo, []Artifact{{Class: "contracts", Data: []byte("contract")}, {Class: "unknown", Data: []byte("proof")}}); err == nil || paths != nil {
		t.Fatalf("invalid publish paths=%v err=%v", paths, err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(ManagedRoot))); !os.IsNotExist(err) {
		t.Fatalf("invalid batch created managed artifacts: stat err=%v", err)
	}
	paths, err := state.PublishMany(repo, []Artifact{{Class: "contracts", Data: []byte("contract")}, {Class: "contracts", Data: []byte("contract")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != paths[1] {
		t.Fatalf("duplicate paths=%v", paths)
	}
}

func TestPublishNoOpsInExternalModeAndRejectsEmptyBytes(t *testing.T) {
	repo := newRetentionRepo(t, nil)
	var external State
	if path, err := external.Publish(repo, "contracts", []byte("contract")); err != nil || path != "" {
		t.Fatalf("external Publish path=%q err=%v", path, err)
	}
	if paths, err := (State{mode: ModeRepository}).PublishMany(repo, nil); err != nil || paths != nil {
		t.Fatalf("empty PublishMany paths=%v err=%v", paths, err)
	}
	state := State{mode: ModeRepository}
	if _, err := state.Publish(repo, "contracts", nil); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("empty artifact error=%v", err)
	}
}

func TestPublishRejectsSymlinkedManagedRoot(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, ".polis", "artifacts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := state.Publish(repo, "contracts", []byte("contract")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Publish error=%v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside entries=%v err=%v", entries, err)
	}
}

func TestReadInputRequiresCorrectManagedClassAndDigest(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("locked contract")
	path, err := state.Publish(repo, "contracts", data)
	if err != nil {
		t.Fatal(err)
	}
	readPath := filepath.Join(repo, filepath.FromSlash(path))
	got, err := state.ReadInput(repo, readPath, "contracts", 100, "too large")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("ReadInput bytes=%q err=%v", got, err)
	}
	if _, err := state.ReadInput(repo, readPath, "plans", 100, "too large"); err == nil {
		t.Fatal("retained artifact accepted from the wrong class")
	}
	if _, err := state.ReadInput(repo, filepath.Join(repo, "app.txt"), "contracts", 100, "too large"); err == nil {
		t.Fatal("arbitrary in-worktree input accepted")
	}
	if err := os.WriteFile(readPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ReadInput(repo, readPath, "contracts", 100, "too large"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered input error=%v", err)
	}
}

func TestListPathsEnumeratesManagedClassWithoutTrustingArtifactBytes(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	first, err := state.Publish(repo, "contracts", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := state.Publish(repo, "contracts", []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := state.ListPaths(repo, "contracts")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{first, second}
	sort.Strings(want)
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("ListPaths()=%v want=%v", paths, want)
	}

	classDir := filepath.Join(repo, filepath.FromSlash(ManagedRoot), "plans")
	outside := t.TempDir()
	if err := os.Symlink(outside, classDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := state.ListPaths(repo, "plans"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ListPaths symlink error=%v", err)
	}
}

func TestReadInputRejectsOversizeBeforeLoadingRetainedBytes(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	path, err := state.Publish(repo, "contracts", bytes.Repeat([]byte("x"), 1024))
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.ReadInput(repo, filepath.Join(repo, filepath.FromSlash(path)), "contracts", 100, "input exceeds maximum size %d")
	if err == nil || !strings.Contains(err.Error(), "input exceeds maximum size 100") {
		t.Fatalf("ReadInput oversize error=%v", err)
	}
	_, err = state.ReadInput(repo, filepath.Join(repo, filepath.FromSlash(path)), "contracts", 100, "input too large")
	if err == nil || !strings.Contains(err.Error(), "input too large") {
		t.Fatalf("ReadInput oversize without format placeholder error=%v", err)
	}
}

func TestReadInputRequiresEnabledModeAndContentAddressedName(t *testing.T) {
	repo := newRetentionRepo(t, nil)
	var external State
	filename := filepath.Join(repo, filepath.FromSlash(ManagedRoot), "contracts", "sha256-not-a-digest.json")
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := external.ReadInput(repo, filename, "contracts", 100, "too large"); err == nil || !strings.Contains(err.Error(), "enabled retained artifact") {
		t.Fatalf("disabled retained input error=%v", err)
	}
	state := State{mode: ModeRepository}
	if _, err := state.ReadInput(repo, filename, "contracts", 100, "too large"); err == nil || !strings.Contains(err.Error(), "not content-addressed") {
		t.Fatalf("invalid retained name error=%v", err)
	}
}

func TestReadInputRejectsRetainedSymlinksAndExternalOversize(t *testing.T) {
	repo := newRetentionRepo(t, manifestBytes(ModeRepository))
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(outside, []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := state.Publish(repo, "contracts", []byte("contract"))
	if err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, retained); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := state.ReadInput(repo, retained, "contracts", 100, "too large"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("retained symlink error=%v", err)
	}
	if _, err := state.ReadInput(repo, outside, "contracts", 2, "too large"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("external oversize error=%v", err)
	}
}

func TestReadInputKeepsExternalInputsCompatible(t *testing.T) {
	repo := newRetentionRepo(t, nil)
	state, err := Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := state.ReadInput(repo, external, "contracts", 100, "too large")
	if err != nil || string(got) != "external" {
		t.Fatalf("ReadInput bytes=%q err=%v", got, err)
	}
}

func TestFilterPorcelainStatusIgnoresOnlyManagedRecords(t *testing.T) {
	raw := []byte(" M .polis/artifacts/contracts/a.json\x00?? .polis/artifacts/proofs/b.patch\x00 M src/main.go\x00R  .polis/artifacts/proofs/c.patch\x00src/c.patch\x00R  .polis/artifacts/proofs/d.patch\x00.polis/artifacts/proofs/e.patch\x00C  .polis/artifacts/proofs/f.patch\x00.polis/artifacts/proofs/g.patch\x00C  .polis/artifacts/proofs/h.patch\x00src/h.patch\x00")
	got, err := FilterPorcelainStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(" M src/main.go\x00R  .polis/artifacts/proofs/c.patch\x00src/c.patch\x00C  .polis/artifacts/proofs/h.patch\x00src/h.patch\x00")
	if !bytes.Equal(got, want) {
		t.Fatalf("filtered status=%q want=%q", got, want)
	}
}

func TestHasStagedChangesIgnoresOnlyManagedPaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "staged artifact", raw: "M  .polis/artifacts/contracts/a.json\x00", want: false},
		{name: "unstaged artifact", raw: " M .polis/artifacts/contracts/a.json\x00", want: false},
		{name: "staged source", raw: "M  src/main.go\x00", want: true},
		{name: "untracked source", raw: "?? src/new.go\x00", want: false},
		{name: "rename crosses boundary", raw: "R  .polis/artifacts/proofs/new.patch\x00src/old.patch\x00", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HasStagedChanges([]byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("HasStagedChanges=%t want=%t", got, test.want)
			}
		})
	}
}

func TestHasStagedChangesRejectsMalformedGitStatus(t *testing.T) {
	if _, err := HasStagedChanges([]byte("M  src/main.go")); err == nil {
		t.Fatal("malformed Git status accepted")
	}
}

func TestFilterPorcelainStatusRejectsMalformedInput(t *testing.T) {
	for _, raw := range [][]byte{[]byte(" M src/main.go"), []byte("bad\x00"), []byte("R  src/new.go\x00")} {
		if _, err := FilterPorcelainStatus(raw); err == nil {
			t.Fatalf("malformed status %q accepted", raw)
		}
	}
}

func manifestBytes(mode Mode) []byte {
	return []byte(`{"schema_version":1,"mode":"` + string(mode) + `"}` + "\n")
}

func writeManifest(t *testing.T, repo string, mode Mode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(ManifestPath)), manifestBytes(mode), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRetentionRepo(t *testing.T, manifest []byte) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest != nil {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(ManifestPath)), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base"}} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	return repo
}
