package offlinekit

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testVersion = "6.3.0"

type testManifest struct {
	FormatVersion int    `json:"format_version"`
	ArtifactType  string `json:"artifact_type"`
	Protocol      string `json:"protocol"`
	PolisVersion  string `json:"polis_version"`
	Runtime       struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		GoVersion    string `json:"go_version"`
	} `json:"runtime"`
	Executable      string   `json:"executable"`
	NetworkRequired bool     `json:"network_required"`
	Prerequisites   []string `json:"prerequisites"`
	Checksums       string   `json:"checksums"`
	Resources       []struct {
		Path   string `json:"path"`
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"resources"`
}

func TestExportCreatesVerifiableOfflineBundle(t *testing.T) {
	source := writeExecutableFixture(t, []byte("POLIS TEST EXECUTABLE"))
	bundle := filepath.Join(t.TempDir(), "nested", "polis-offline.zip")
	result, err := Export(Options{Out: bundle, Executable: source, Version: testVersion})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Path != bundle || result.Version != testVersion || result.Runtime != runtime.GOOS+"/"+runtime.GOARCH || result.NetworkRequired {
		t.Fatalf("result=%+v", result)
	}

	files := readArchive(t, bundle)
	if len(files) != 12 {
		t.Fatalf("member count=%d want=12", len(files))
	}
	manifestRaw := files[manifestMember]
	var document testManifest
	if err := json.Unmarshal(manifestRaw, &document); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if document.FormatVersion != BundleFormatVersion || document.ArtifactType != "polis_offline_bundle" || document.Protocol != "POLIS V6" || document.PolisVersion != testVersion {
		t.Fatalf("manifest=%+v", document)
	}
	if document.Runtime.OS != runtime.GOOS || document.Runtime.Architecture != runtime.GOARCH || document.Runtime.GoVersion == "" {
		t.Fatalf("runtime=%+v", document.Runtime)
	}
	if document.NetworkRequired || len(document.Prerequisites) != 1 || document.Prerequisites[0] != "git" || document.Checksums != checksumsMember {
		t.Fatalf("manifest portability metadata=%+v", document)
	}

	if got := files[document.Executable]; string(got) != "POLIS TEST EXECUTABLE" {
		t.Fatalf("embedded executable=%q", got)
	}
	if len(document.Resources) != len(files)-2 {
		t.Fatalf("resource count=%d want=%d", len(document.Resources), len(files)-2)
	}
	for _, resource := range document.Resources {
		data, ok := files[resource.Path]
		if !ok {
			t.Fatalf("manifest resource %q missing from archive", resource.Path)
		}
		sum := sha256.Sum256(data)
		if resource.Bytes != len(data) || resource.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("resource digest=%+v", resource)
		}
	}
	verifyChecksums(t, files, document.Checksums)

	if got, want := result.SHA256, fileDigestForTest(t, bundle); got != want {
		t.Fatalf("result sha256=%s want=%s", got, want)
	}
	for name, member := range openArchiveMembers(t, bundle) {
		if name == document.Executable && member.Mode().Perm() != 0o755 {
			t.Fatalf("executable mode=%o want=755", member.Mode().Perm())
		}
	}
}

func TestExportIsDeterministicAndDoesNotIncludeProjectData(t *testing.T) {
	source := writeExecutableFixture(t, []byte("same bytes"))
	one := filepath.Join(t.TempDir(), "one.zip")
	two := filepath.Join(t.TempDir(), "two.zip")
	first, err := Export(Options{Out: one, Executable: source, Version: testVersion})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Export(Options{Out: two, Executable: source, Version: testVersion})
	if err != nil {
		t.Fatal(err)
	}
	oneBytes, err := os.ReadFile(one)
	if err != nil {
		t.Fatal(err)
	}
	twoBytes, err := os.ReadFile(two)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oneBytes, twoBytes) || first.SHA256 != second.SHA256 {
		t.Fatalf("bundle is not deterministic: first=%s second=%s", first.SHA256, second.SHA256)
	}
	for name := range readArchive(t, one) {
		if strings.Contains(name, ".polis/policy.json") || strings.Contains(name, "target") {
			t.Fatalf("project data leaked into offline bundle: %s", name)
		}
	}
}

func TestExportRejectsExistingOutputWithoutOverwriting(t *testing.T) {
	source := writeExecutableFixture(t, []byte("source"))
	bundle := filepath.Join(t.TempDir(), "polis-offline.zip")
	want := []byte("keep this file")
	if err := os.WriteFile(bundle, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(Options{Out: bundle, Executable: source, Version: testVersion}); err == nil {
		t.Fatal("expected existing output rejection")
	}
	got, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("existing output changed: %q", got)
	}
}

func TestExportRequiresExecutableAndVersion(t *testing.T) {
	if _, err := Export(Options{Out: filepath.Join(t.TempDir(), "bundle.zip")}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("missing version error=%v", err)
	}
	source := filepath.Join(t.TempDir(), "missing-polis")
	if _, err := Export(Options{Out: filepath.Join(t.TempDir(), "bundle.zip"), Executable: source, Version: testVersion}); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("missing executable error=%v", err)
	}
}

func writeExecutableFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "polis")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readArchive(t *testing.T, filename string) map[string][]byte {
	t.Helper()
	archive, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	files := make(map[string][]byte, len(archive.File))
	for _, member := range archive.File {
		if member.FileInfo().IsDir() {
			t.Fatalf("unexpected directory member %q", member.Name)
		}
		if _, exists := files[member.Name]; exists {
			t.Fatalf("duplicate member %q", member.Name)
		}
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: read=%v close=%v", member.Name, readErr, closeErr)
		}
		files[member.Name] = data
	}
	return files
}

func verifyChecksums(t *testing.T, files map[string][]byte, memberName string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(files[memberName])), "\n")
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid checksum line %q", line)
		}
		data, ok := files[parts[1]]
		if !ok {
			t.Fatalf("checksum member %q missing", parts[1])
		}
		sum := sha256.Sum256(data)
		if parts[0] != hex.EncodeToString(sum[:]) {
			t.Fatalf("checksum for %s=%s", parts[1], parts[0])
		}
		seen[parts[1]] = true
	}
	if len(seen) != len(files)-1 || seen[memberName] {
		t.Fatalf("checksum coverage=%v files=%v", seen, files)
	}
}

func openArchiveMembers(t *testing.T, filename string) map[string]*zip.File {
	t.Helper()
	archive, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	// The returned zip.File values remain valid while the reader is open. The
	// caller only inspects metadata before this test returns.
	defer archive.Close()
	members := make(map[string]*zip.File, len(archive.File))
	for _, member := range archive.File {
		members[member.Name] = member
	}
	return members
}

func fileDigestForTest(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
