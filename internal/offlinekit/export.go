package offlinekit

import (
	"archive/zip"
	"bytes"
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
	Out        string
	Executable string
	Version    string
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
	Path string
	Data []byte
	Mode os.FileMode
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

	source, err := resolveExecutable(opts.Executable)
	if err != nil {
		return Result{}, err
	}
	executable, err := readExecutable(source)
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
	if runtime.GOOS == "windows" {
		executableMember += ".exe"
	}
	files := []bundleFile{{Path: executableMember, Data: executable, Mode: 0o755}}
	for _, resource := range spec.OfflineResources() {
		resourceMember := bundlePrefix + "spec/" + resource.Path
		if resource.Path == "POLIS-OFFLINE.md" {
			resourceMember = bundlePrefix + resource.Path
		}
		files = append(files, bundleFile{Path: resourceMember, Data: resource.Data, Mode: 0o644})
	}

	resources := digestResources(files)
	manifestRaw, err := encodeManifest(opts.Version, executableMember, resources)
	if err != nil {
		return Result{}, err
	}
	files = append(files, bundleFile{Path: manifestMember, Data: manifestRaw, Mode: 0o644})
	checksumsRaw := encodeChecksums(files)
	files = append(files, bundleFile{Path: checksumsMember, Data: checksumsRaw, Mode: 0o644})
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
	if err := copyExclusive(candidate, out); err != nil {
		return Result{}, err
	}
	digest, err := fileSHA256(out)
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
		Runtime:         runtime.GOOS + "/" + runtime.GOARCH,
		Executable:      executableMember,
		NetworkRequired: false,
		Entries:         entries,
	}, nil
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

func readExecutable(filename string) ([]byte, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("stat POLIS executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("POLIS executable must be a regular file")
	}
	if info.Size() > maxExecutableBytes {
		return nil, errors.New("POLIS executable exceeds offline bundle limit")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read POLIS executable: %w", err)
	}
	return data, nil
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
		sum := sha256.Sum256(file.Data)
		resources = append(resources, resourceDigest{Path: file.Path, Bytes: len(file.Data), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Path < resources[j].Path })
	return resources
}

func encodeManifest(version, executable string, resources []resourceDigest) ([]byte, error) {
	document := manifest{
		FormatVersion:   BundleFormatVersion,
		ArtifactType:    "polis_offline_bundle",
		Protocol:        "POLIS V6",
		PolisVersion:    version,
		Runtime:         runtimeManifest{OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version()},
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
		sum := sha256.Sum256(file.Data)
		checksums.WriteString(hex.EncodeToString(sum[:]))
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
		header := &zip.FileHeader{Name: file.Path, Method: zip.Store, Modified: time.Unix(0, 0).UTC()}
		header.SetMode(file.Mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", fmt.Errorf("create offline bundle member %s: %w", file.Path, err)
		}
		if _, err := w.Write(file.Data); err != nil {
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

func verifyArchive(filename string, expected []bundleFile) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("verify offline bundle archive: %w", err)
	}
	defer archive.Close()
	if len(archive.File) != len(expected) {
		return fmt.Errorf("offline bundle member count=%d want=%d", len(archive.File), len(expected))
	}
	byPath := make(map[string][]byte, len(expected))
	for _, file := range expected {
		byPath[file.Path] = file.Data
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
		reader, err := member.Open()
		if err != nil {
			return fmt.Errorf("open offline bundle member %s: %w", member.Name, err)
		}
		got, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return fmt.Errorf("read offline bundle member %s: %w", member.Name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close offline bundle member %s: %w", member.Name, closeErr)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("offline bundle member %s changed while writing", member.Name)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("offline bundle member coverage=%d want=%d", len(seen), len(expected))
	}
	return nil
}

func copyExclusive(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open verified offline bundle: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("offline bundle output already exists: %s", target)
		}
		return fmt.Errorf("create offline bundle output: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(target)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy offline bundle: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close offline bundle output: %w", err)
	}
	ok = true
	return nil
}

func fileSHA256(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("open offline bundle for hashing: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash offline bundle: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
