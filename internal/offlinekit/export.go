package offlinekit

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	BundleFormatVersion = 1
	bundlePrefix        = "polis-offline/"
	manifestMember      = bundlePrefix + "manifest.json"
	checksumsMember     = bundlePrefix + "SHA256SUMS"
	maxExecutableBytes  = 128 << 20
)

type Options struct {
	Out           string
	Executable    string
	TargetRuntime string
	Version       string
}

type Result struct {
	Path            string
	SHA256          string
	Version         string
	Runtime         string
	Executable      string
	NetworkRequired bool
	Entries         []string
}

type bundleFile struct {
	Path   string
	Data   []byte
	Source string
	Mode   os.FileMode
	Bytes  int64
	SHA256 string
}

type manifest struct {
	FormatVersion   int              `json:"format_version"`
	ArtifactType    string           `json:"artifact_type"`
	Protocol        string           `json:"protocol"`
	PolisVersion    string           `json:"polis_version"`
	Runtime         runtimeManifest  `json:"runtime"`
	Executable      string           `json:"executable"`
	NetworkRequired bool             `json:"network_required"`
	Prerequisites   []string         `json:"prerequisites"`
	Checksums       string           `json:"checksums"`
	Resources       []resourceDigest `json:"resources"`
}

type runtimeManifest struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	GoVersion    string `json:"go_version"`
}

type resourceDigest struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func Export(opts Options) (Result, error) {
	if strings.TrimSpace(opts.Out) == "" {
		return Result{}, errors.New("offline bundle output path is required")
	}
	if strings.TrimSpace(opts.Version) == "" {
		return Result{}, errors.New("POLIS version is required")
	}
	targetOS, targetArch, err := resolveTargetRuntime(opts.TargetRuntime)
	if err != nil {
		return Result{}, err
	}

	source, err := resolveExecutable(opts.Executable)
	if err != nil {
		return Result{}, err
	}
	executable, err := executableFile(source)
	if err != nil {
		return Result{}, err
	}
	out, err := filepath.Abs(opts.Out)
	if err != nil {
		return Result{}, fmt.Errorf("resolve offline bundle output: %w", err)
	}
	if err := ensureOutputAbsent(out); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return Result{}, fmt.Errorf("create offline bundle directory: %w", err)
	}

	executableMember := bundlePrefix + "bin/polis"
	if targetOS == "windows" {
		executableMember += ".exe"
	}
	executable.Path = executableMember
	executable.Mode = 0o755
	files := []bundleFile{executable}
	for _, resource := range spec.OfflineResources() {
		resourceMember := bundlePrefix + "spec/" + resource.Path
		if resource.Path == "POLIS-OFFLINE.md" {
			resourceMember = bundlePrefix + resource.Path
		}
		files = append(files, inMemoryBundleFile(resourceMember, resource.Data, 0o644))
	}

	resources := digestResources(files)
	manifestRaw, err := encodeManifest(opts.Version, executableMember, targetOS, targetArch, resources)
	if err != nil {
		return Result{}, err
	}
	files = append(files, inMemoryBundleFile(manifestMember, manifestRaw, 0o644))
	checksumsRaw := encodeChecksums(files)
	files = append(files, inMemoryBundleFile(checksumsMember, checksumsRaw, 0o644))
	files, err = sortedFiles(files)
	if err != nil {
		return Result{}, err
	}

	candidate, err := writeArchive(filepath.Dir(out), files)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(candidate)
	if err := verifyArchive(candidate, files); err != nil {
		return Result{}, err
	}
	digest, err := copyExclusive(candidate, out)
	if err != nil {
		return Result{}, err
	}

	entries := make([]string, 0, len(files))
	for _, file := range files {
		entries = append(entries, file.Path)
	}
	return Result{
		Path:            out,
		SHA256:          digest,
		Version:         opts.Version,
		Runtime:         targetOS + "/" + targetArch,
		Executable:      executableMember,
		NetworkRequired: false,
		Entries:         entries,
	}, nil
}

func resolveTargetRuntime(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validRuntimeComponent(parts[0]) || !validRuntimeComponent(parts[1]) {
		return "", "", fmt.Errorf("invalid target runtime %q: expected GOOS/GOARCH", value)
	}
	return parts[0], parts[1], nil
}

func validRuntimeComponent(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func resolveExecutable(filename string) (string, error) {
	if filename == "" {
		var err error
		filename, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current POLIS executable: %w", err)
		}
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("resolve POLIS executable: %w", err)
	}
	return absolute, nil
}

func executableFile(filename string) (bundleFile, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return bundleFile{}, fmt.Errorf("stat POLIS executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return bundleFile{}, errors.New("POLIS executable must be a regular file")
	}
	if info.Size() > maxExecutableBytes {
		return bundleFile{}, errors.New("POLIS executable exceeds offline bundle limit")
	}
	f, err := os.Open(filename)
	if err != nil {
		return bundleFile{}, fmt.Errorf("open POLIS executable: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return bundleFile{}, fmt.Errorf("hash POLIS executable: %w", err)
	}
	if n != info.Size() {
		return bundleFile{}, errors.New("POLIS executable changed while hashing")
	}
	return bundleFile{Source: filename, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func ensureOutputAbsent(filename string) error {
	if _, err := os.Lstat(filename); err == nil {
		return fmt.Errorf("offline bundle output already exists: %s", filename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check offline bundle output: %w", err)
	}
	return nil
}

func digestResources(files []bundleFile) []resourceDigest {
	resources := make([]resourceDigest, 0, len(files))
	for _, file := range files {
		resources = append(resources, resourceDigest{Path: file.Path, Bytes: int(file.Bytes), SHA256: file.SHA256})
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Path < resources[j].Path })
	return resources
}

func inMemoryBundleFile(path string, data []byte, mode os.FileMode) bundleFile {
	sum := sha256.Sum256(data)
	return bundleFile{Path: path, Data: data, Mode: mode, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func encodeManifest(version, executable, targetOS, targetArch string, resources []resourceDigest) ([]byte, error) {
	document := manifest{
		FormatVersion:   BundleFormatVersion,
		ArtifactType:    "polis_offline_bundle",
		Protocol:        "POLIS V6",
		PolisVersion:    version,
		Runtime:         runtimeManifest{OS: targetOS, Architecture: targetArch, GoVersion: runtime.Version()},
		Executable:      executable,
		NetworkRequired: false,
		Prerequisites:   []string{"git"},
		Checksums:       checksumsMember,
		Resources:       resources,
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode offline bundle manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

func encodeChecksums(files []bundleFile) []byte {
	filesWithoutChecksums := make([]bundleFile, 0, len(files))
	for _, file := range files {
		if file.Path == checksumsMember {
			continue
		}
		filesWithoutChecksums = append(filesWithoutChecksums, file)
	}
	filesWithoutChecksums, _ = sortedFiles(filesWithoutChecksums)
	var checksums strings.Builder
	for _, file := range filesWithoutChecksums {
		checksums.WriteString(file.SHA256)
		checksums.WriteString("  ")
		checksums.WriteString(file.Path)
		checksums.WriteByte('\n')
	}
	return []byte(checksums.String())
}

func sortedFiles(files []bundleFile) ([]bundleFile, error) {
	sorted := append([]bundleFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].Path == sorted[i].Path {
			return nil, fmt.Errorf("duplicate offline bundle member %q", sorted[i].Path)
		}
	}
	return sorted, nil
}

func writeArchive(directory string, files []bundleFile) (string, error) {
	f, err := os.CreateTemp(directory, ".polis-offline-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create offline bundle archive: %w", err)
	}
	candidate := f.Name()
	zw := zip.NewWriter(f)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.Path, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
		header.SetMode(file.Mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", fmt.Errorf("create offline bundle member %s: %w", file.Path, err)
		}
		if err := writeBundleFile(w, file); err != nil {
			_ = zw.Close()
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", fmt.Errorf("write offline bundle member %s: %w", file.Path, err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(candidate)
		return "", fmt.Errorf("finalize offline bundle archive: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(candidate)
		return "", fmt.Errorf("close offline bundle archive: %w", err)
	}
	return candidate, nil
}

func writeBundleFile(w io.Writer, file bundleFile) error {
	if file.Source == "" {
		_, err := w.Write(file.Data)
		return err
	}
	in, err := os.Open(file.Source)
	if err != nil {
		return err
	}
	defer in.Close()
	written, err := io.Copy(w, in)
	if err != nil {
		return err
	}
	if written != file.Bytes {
		return fmt.Errorf("source changed while writing: wrote %d bytes want %d", written, file.Bytes)
	}
	return nil
}

func verifyArchive(filename string, expected []bundleFile) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("verify offline bundle archive: %w", err)
	}
	defer archive.Close()
	if len(archive.File) != len(expected) {
		return fmt.Errorf("offline bundle member count=%d want=%d", len(archive.File), len(expected))
	}
	byPath := make(map[string]bundleFile, len(expected))
	for _, file := range expected {
		byPath[file.Path] = file
	}
	seen := make(map[string]bool, len(expected))
	for _, member := range archive.File {
		want, ok := byPath[member.Name]
		if !ok {
			return fmt.Errorf("unexpected offline bundle member %q", member.Name)
		}
		if seen[member.Name] {
			return fmt.Errorf("duplicate offline bundle member %q", member.Name)
		}
		seen[member.Name] = true
		if member.UncompressedSize64 != uint64(want.Bytes) {
			return fmt.Errorf("offline bundle member %s size=%d want=%d", member.Name, member.UncompressedSize64, want.Bytes)
		}
		reader, err := member.Open()
		if err != nil {
			return fmt.Errorf("open offline bundle member %s: %w", member.Name, err)
		}
		h := sha256.New()
		n, readErr := io.Copy(h, reader)
		closeErr := reader.Close()
		if readErr != nil {
			return fmt.Errorf("read offline bundle member %s: %w", member.Name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close offline bundle member %s: %w", member.Name, closeErr)
		}
		if n != want.Bytes || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
			return fmt.Errorf("offline bundle member %s changed while writing", member.Name)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("offline bundle member coverage=%d want=%d", len(seen), len(expected))
	}
	return nil
}

func copyExclusive(source, target string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open verified offline bundle: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("offline bundle output already exists: %s", target)
		}
		return "", fmt.Errorf("create offline bundle output: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(target)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		return "", fmt.Errorf("copy offline bundle: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close offline bundle output: %w", err)
	}
	ok = true
	return hex.EncodeToString(h.Sum(nil)), nil
}
