package artifactretention

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
)

const (
	ManifestPath = ".polis/artifact-retention.json"
	ManagedRoot  = ".polis/artifacts"
	maxManifest  = 16 << 10
)

type Mode string

const (
	ModeExternal   Mode = "external"
	ModeRepository Mode = "repository"
)

type Config struct {
	SchemaVersion int  `json:"schema_version"`
	Mode          Mode `json:"mode"`
}

// State is the committed repository preference for POLIS-generated artifacts.
// Its zero value preserves the historical external-output behavior.
type State struct {
	mode Mode
}

func (s State) Mode() Mode {
	if s.mode == "" {
		return ModeExternal
	}
	return s.mode
}

func (s State) RepositoryEnabled() bool {
	return s.Mode() == ModeRepository
}

// Load accepts only a committed manifest whose worktree bytes exactly match
// HEAD. A missing manifest means external output mode.
func Load(ctx context.Context, repo string) (State, error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return State{}, fmt.Errorf("resolve repository path: %w", err)
	}
	entry, err := gitutil.Output(ctx, root, nil, nil, "ls-tree", "HEAD", "--", ManifestPath)
	if err != nil {
		return State{}, fmt.Errorf("inspect committed artifact-retention manifest: %w", err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(ManifestPath))
	if entry == "" {
		if _, err := os.Lstat(manifestPath); err == nil {
			return State{}, errors.New("artifact-retention manifest must be committed before POLIS can use it")
		} else if !errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("inspect artifact-retention manifest: %w", err)
		}
		return State{mode: ModeExternal}, nil
	}
	if err := validateTreeEntry(entry); err != nil {
		return State{}, err
	}
	if err := checkRegularPath(root, ManifestPath); err != nil {
		return State{}, fmt.Errorf("validate artifact-retention manifest path: %w", err)
	}
	committedSizeRaw, err := gitutil.Output(ctx, root, nil, nil, "cat-file", "-s", "HEAD:"+ManifestPath)
	if err != nil {
		return State{}, fmt.Errorf("inspect committed artifact-retention manifest size: %w", err)
	}
	committedSize, err := strconv.ParseInt(committedSizeRaw, 10, 64)
	if err != nil || committedSize < 1 || committedSize > maxManifest {
		return State{}, fmt.Errorf("manifest size must be between 1 and %d bytes", maxManifest)
	}
	committed, err := gitutil.Bytes(ctx, root, nil, nil, "show", "HEAD:"+ManifestPath)
	if err != nil {
		return State{}, fmt.Errorf("read committed artifact-retention manifest: %w", err)
	}
	workingInfo, err := os.Stat(manifestPath)
	if err != nil {
		return State{}, fmt.Errorf("inspect artifact-retention manifest size: %w", err)
	}
	if workingInfo.Size() > maxManifest {
		return State{}, fmt.Errorf("manifest size must be between 1 and %d bytes", maxManifest)
	}
	working, err := os.ReadFile(manifestPath)
	if err != nil {
		return State{}, fmt.Errorf("read artifact-retention manifest: %w", err)
	}
	if !bytes.Equal(working, committed) {
		return State{}, errors.New("working artifact-retention manifest differs from committed HEAD")
	}
	config, err := Decode(working)
	if err != nil {
		return State{}, fmt.Errorf("invalid artifact-retention manifest: %w", err)
	}
	return State{mode: config.Mode}, nil
}

func validateTreeEntry(entry string) error {
	parts := strings.SplitN(entry, "\t", 2)
	if len(parts) != 2 || parts[1] != ManifestPath {
		return errors.New("committed artifact-retention manifest has an invalid Git tree entry")
	}
	fields := strings.Fields(parts[0])
	if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return errors.New("committed artifact-retention manifest must be a regular Git file")
	}
	return nil
}

// Decode strictly decodes the versioned manifest, including exact field names
// and duplicate-key rejection that encoding/json alone does not provide.
func Decode(raw []byte) (Config, error) {
	if len(raw) == 0 || len(raw) > maxManifest {
		return Config{}, fmt.Errorf("manifest size must be between 1 and %d bytes", maxManifest)
	}
	if !utf8.Valid(raw) {
		return Config{}, errors.New("manifest must be valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	opening, err := dec.Token()
	if err != nil {
		return Config{}, fmt.Errorf("decode manifest: %w", err)
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return Config{}, errors.New("manifest must be a JSON object")
	}
	seen := make(map[string]bool, 2)
	var config Config
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return Config{}, fmt.Errorf("decode manifest key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return Config{}, errors.New("manifest keys must be strings")
		}
		if key != "schema_version" && key != "mode" {
			return Config{}, fmt.Errorf("manifest contains unknown field %q", key)
		}
		if seen[key] {
			return Config{}, fmt.Errorf("manifest repeats field %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return Config{}, fmt.Errorf("decode manifest field %q: %w", key, err)
		}
		switch key {
		case "schema_version":
			if err := json.Unmarshal(value, &config.SchemaVersion); err != nil {
				return Config{}, fmt.Errorf("manifest schema_version must be an integer: %w", err)
			}
		case "mode":
			if err := json.Unmarshal(value, &config.Mode); err != nil {
				return Config{}, fmt.Errorf("manifest mode must be a string: %w", err)
			}
		}
	}
	closing, err := dec.Token()
	if err != nil {
		return Config{}, fmt.Errorf("close manifest object: %w", err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return Config{}, errors.New("manifest must be a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("manifest contains trailing JSON")
		}
		return Config{}, fmt.Errorf("decode trailing manifest data: %w", err)
	}
	if !seen["schema_version"] || !seen["mode"] {
		return Config{}, errors.New("manifest requires schema_version and mode")
	}
	if config.SchemaVersion != 1 {
		return Config{}, fmt.Errorf("unsupported artifact-retention schema_version %d", config.SchemaVersion)
	}
	if config.Mode != ModeExternal && config.Mode != ModeRepository {
		return Config{}, fmt.Errorf("unsupported artifact-retention mode %q", config.Mode)
	}
	return config, nil
}

func checkRegularPath(root, relative string) error {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))), "/")
	current := root
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("path is not repository-relative")
		}
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", part)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", part)
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return errors.New("path is not a regular file")
		}
	}
	return nil
}

type Artifact struct {
	Class  string
	Data   []byte
	Source string
}

type pendingArtifact struct {
	relative string
	dest     string
	data     []byte
	source   string
	digest   string
	size     int64
	temp     string
	reused   bool
	created  os.FileInfo
}

// Publish stores exact generated bytes at a deterministic content-addressed
// path. It never stages or commits the artifact.
func (s State) Publish(repo, class string, data []byte) (string, error) {
	paths, err := s.PublishMany(repo, []Artifact{{Class: class, Data: data}})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// PublishMany validates and stages every copy before making any of them
// visible. If a later link fails, it removes only files created by this call.
func (s State) PublishMany(repo string, artifacts []Artifact) ([]string, error) {
	if len(artifacts) == 0 || !s.RepositoryEnabled() {
		return nil, nil
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	items := make([]pendingArtifact, len(artifacts))
	byPath := make(map[string]int, len(artifacts))
	for index, artifact := range artifacts {
		extension, err := extensionForClass(artifact.Class)
		if err != nil {
			return nil, err
		}
		item, err := preparePendingArtifact(artifact, extension)
		if err != nil {
			return nil, err
		}
		item.dest = filepath.Join(root, filepath.FromSlash(item.relative))
		if previous, ok := byPath[item.relative]; ok {
			equal, err := samePendingArtifact(items[previous], item)
			if err != nil {
				return nil, fmt.Errorf("compare duplicate retained artifact %s: %w", item.relative, err)
			}
			if !equal {
				return nil, fmt.Errorf("content-address collision at %s", item.relative)
			}
			items[index] = items[previous]
			continue
		}
		byPath[item.relative] = index
		items[index] = item
	}

	for index := range items {
		if previous, ok := byPath[items[index].relative]; ok && previous != index {
			continue
		}
		if err := ensureArtifactDirectory(root, items[index].relative); err != nil {
			return nil, fmt.Errorf("prepare retained artifact path %s: %w", items[index].relative, err)
		}
		info, err := os.Lstat(items[index].dest)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("retained artifact path is not a regular file: %s", items[index].relative)
			}
			equal, err := identicalRegularArtifact(items[index].dest, items[index])
			if err != nil {
				return nil, fmt.Errorf("read existing retained artifact %s: %w", items[index].relative, err)
			}
			if !equal {
				return nil, fmt.Errorf("retained artifact path contains different bytes: %s", items[index].relative)
			}
			items[index].reused = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect retained artifact %s: %w", items[index].relative, err)
		}
	}

	cleanup := func() {
		for index := range items {
			if items[index].temp != "" {
				_ = os.Remove(items[index].temp)
			}
			if items[index].created != nil {
				if current, err := os.Lstat(items[index].dest); err == nil && os.SameFile(items[index].created, current) {
					_ = os.Remove(items[index].dest)
				}
			}
		}
	}
	for index := range items {
		if items[index].reused {
			continue
		}
		temp, err := os.CreateTemp(filepath.Dir(items[index].dest), ".polis-artifact-*.tmp")
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("stage retained artifact %s: %w", items[index].relative, err)
		}
		items[index].temp = temp.Name()
		if err := temp.Chmod(0o644); err != nil {
			_ = temp.Close()
			cleanup()
			return nil, fmt.Errorf("set retained artifact permissions: %w", err)
		}
		if err := writePendingArtifact(temp, items[index]); err != nil {
			_ = temp.Close()
			cleanup()
			return nil, fmt.Errorf("write retained artifact %s: %w", items[index].relative, err)
		}
		if err := temp.Sync(); err != nil {
			_ = temp.Close()
			cleanup()
			return nil, fmt.Errorf("sync retained artifact %s: %w", items[index].relative, err)
		}
		if err := temp.Close(); err != nil {
			cleanup()
			return nil, fmt.Errorf("close retained artifact %s: %w", items[index].relative, err)
		}
	}
	for index := range items {
		if items[index].reused {
			continue
		}
		if err := os.Link(items[index].temp, items[index].dest); err != nil {
			if errors.Is(err, os.ErrExist) {
				equal, compareErr := identicalRegularArtifact(items[index].dest, items[index])
				if compareErr == nil && equal {
					items[index].reused = true
					continue
				}
			}
			cleanup()
			return nil, fmt.Errorf("publish retained artifact %s: %w", items[index].relative, err)
		}
		items[index].created, err = os.Lstat(items[index].dest)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("inspect published artifact %s: %w", items[index].relative, err)
		}
	}
	for index := range items {
		items[index].created = nil
	}
	cleanup()
	paths := make([]string, len(items))
	for index := range items {
		paths[index] = items[index].relative
	}
	return paths, nil
}

func preparePendingArtifact(artifact Artifact, extension string) (pendingArtifact, error) {
	if artifact.Source != "" && len(artifact.Data) != 0 {
		return pendingArtifact{}, fmt.Errorf("retained %s artifact must use data or source, not both", artifact.Class)
	}
	if artifact.Source == "" {
		if len(artifact.Data) == 0 {
			return pendingArtifact{}, fmt.Errorf("retained %s artifact must not be empty", artifact.Class)
		}
		digest := sha256.Sum256(artifact.Data)
		digestHex := hex.EncodeToString(digest[:])
		return pendingArtifact{
			relative: filepath.ToSlash(filepath.Join(ManagedRoot, artifact.Class, "sha256-"+digestHex+extension)),
			data:     artifact.Data,
			digest:   digestHex,
			size:     int64(len(artifact.Data)),
		}, nil
	}
	f, err := os.Open(artifact.Source)
	if err != nil {
		return pendingArtifact{}, fmt.Errorf("open retained %s source: %w", artifact.Class, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return pendingArtifact{}, fmt.Errorf("inspect retained %s source: %w", artifact.Class, err)
	}
	if !info.Mode().IsRegular() {
		return pendingArtifact{}, fmt.Errorf("retained %s source must be a regular file", artifact.Class)
	}
	if info.Size() == 0 {
		return pendingArtifact{}, fmt.Errorf("retained %s artifact must not be empty", artifact.Class)
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return pendingArtifact{}, fmt.Errorf("hash retained %s source: %w", artifact.Class, err)
	}
	if n != info.Size() {
		return pendingArtifact{}, fmt.Errorf("retained %s source changed while hashing", artifact.Class)
	}
	digestHex := hex.EncodeToString(h.Sum(nil))
	return pendingArtifact{
		relative: filepath.ToSlash(filepath.Join(ManagedRoot, artifact.Class, "sha256-"+digestHex+extension)),
		source:   artifact.Source,
		digest:   digestHex,
		size:     n,
	}, nil
}

func samePendingArtifact(left, right pendingArtifact) (bool, error) {
	if left.size != right.size || left.digest != right.digest {
		return false, nil
	}
	leftReader, err := openPendingArtifact(left)
	if err != nil {
		return false, err
	}
	defer leftReader.Close()
	rightReader, err := openPendingArtifact(right)
	if err != nil {
		return false, err
	}
	defer rightReader.Close()
	return readersEqual(leftReader, rightReader)
}

func openPendingArtifact(item pendingArtifact) (io.ReadCloser, error) {
	if item.source == "" {
		return io.NopCloser(bytes.NewReader(item.data)), nil
	}
	return os.Open(item.source)
}

func readersEqual(left, right io.Reader) (bool, error) {
	leftBuffer := make([]byte, 32<<10)
	rightBuffer := make([]byte, 32<<10)
	for {
		leftN, leftErr := io.ReadFull(left, leftBuffer)
		rightN, rightErr := io.ReadFull(right, rightBuffer)
		if leftN != rightN || !bytes.Equal(leftBuffer[:leftN], rightBuffer[:rightN]) {
			return false, nil
		}
		if errors.Is(leftErr, io.EOF) || errors.Is(leftErr, io.ErrUnexpectedEOF) || errors.Is(rightErr, io.EOF) || errors.Is(rightErr, io.ErrUnexpectedEOF) {
			return leftN == rightN && (errors.Is(leftErr, io.EOF) || errors.Is(leftErr, io.ErrUnexpectedEOF)) && (errors.Is(rightErr, io.EOF) || errors.Is(rightErr, io.ErrUnexpectedEOF)), nil
		}
		if leftErr != nil {
			return false, leftErr
		}
		if rightErr != nil {
			return false, rightErr
		}
	}
}

func writePendingArtifact(w io.Writer, item pendingArtifact) error {
	if item.source == "" {
		_, err := w.Write(item.data)
		return err
	}
	f, err := os.Open(item.source)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), f)
	if err != nil {
		return err
	}
	if n != item.size || hex.EncodeToString(h.Sum(nil)) != item.digest {
		return errors.New("source changed while retaining artifact")
	}
	return nil
}

func identicalRegularArtifact(filename string, want pendingArtifact) (bool, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != want.size {
		return false, nil
	}
	f, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer f.Close()
	wantReader, err := openPendingArtifact(want)
	if err != nil {
		return false, err
	}
	defer wantReader.Close()
	return readersEqual(f, wantReader)
}

func extensionForClass(class string) (string, error) {
	switch class {
	case "contracts", "plans", "evidence":
		return ".json", nil
	case "proofs":
		return ".patch", nil
	case "packages":
		return ".polis", nil
	default:
		return "", fmt.Errorf("unsupported retained artifact class %q", class)
	}
}

// ListPaths returns repository-relative paths for every entry currently
// present in one managed artifact class. Callers must pass each returned path
// through ReadInput before trusting its bytes; this method only establishes a
// safe, non-symlinked class directory boundary and deterministic enumeration.
func (s State) ListPaths(repo, class string) ([]string, error) {
	if !s.RepositoryEnabled() {
		return nil, nil
	}
	if _, err := extensionForClass(class); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path: %w", err)
	}
	relativeDir := filepath.ToSlash(filepath.Join(ManagedRoot, class))
	classDir := filepath.Join(root, filepath.FromSlash(relativeDir))
	if _, err := os.Lstat(classDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect retained artifact class %q: %w", class, err)
	}
	if err := checkDirectoryPath(root, relativeDir); err != nil {
		return nil, fmt.Errorf("validate retained artifact class %q: %w", class, err)
	}
	entries, err := os.ReadDir(classDir)
	if err != nil {
		return nil, fmt.Errorf("list retained artifact class %q: %w", class, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, relativeDir+"/"+entry.Name())
	}
	sort.Strings(paths)
	return paths, nil
}

func checkDirectoryPath(root, relative string) error {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))), "/")
	current := root
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("path is not repository-relative")
		}
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", part)
		}
		if !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", part)
		}
	}
	return nil
}

func ensureArtifactDirectory(root, relative string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errors.New("repository root is not a real directory")
	}
	polisDir := filepath.Join(root, ".polis")
	if err := ensureDirectory(polisDir, false); err != nil {
		return fmt.Errorf(".polis: %w", err)
	}
	artifactsDir := filepath.Join(polisDir, "artifacts")
	if err := ensureDirectory(artifactsDir, true); err != nil {
		return fmt.Errorf("artifacts: %w", err)
	}
	classDir := filepath.Join(root, filepath.Dir(filepath.FromSlash(relative)))
	if err := ensureDirectory(classDir, true); err != nil {
		return fmt.Errorf("class directory: %w", err)
	}
	return nil
}

func ensureDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("directory is a symlink")
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}

// ReadInput permits a producer input from an external path or from the exact
// content-addressed class enabled by this repository's committed preference.
func (s State) ReadInput(repo, filename, class string, maximum int64, oversizeMessage string) ([]byte, error) {
	extension, err := extensionForClass(class)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	lexicalRelative, relativeErr := relativePathUnderRoot(root, abs)
	insideManagedRoot := relativeErr == nil && (lexicalRelative == ManagedRoot || strings.HasPrefix(lexicalRelative, ManagedRoot+"/"))
	contained, err := pathguard.Contains(root, abs)
	if err != nil {
		return nil, fmt.Errorf("resolve input boundary: %w", err)
	}
	if !contained {
		if insideManagedRoot {
			return nil, errors.New("retained artifact path must not traverse a symlink")
		}
		return fileutil.ReadOutside(root, filename, fileutil.OutsideReadOptions{Max: maximum, OversizeMessage: oversizeMessage})
	}
	if relativeErr != nil {
		return nil, fmt.Errorf("resolve retained artifact path: %w", relativeErr)
	}
	if !insideManagedRoot || !s.RepositoryEnabled() {
		return nil, errors.New("input must be outside target worktree or an enabled retained artifact")
	}
	prefix := ManagedRoot + "/" + class + "/sha256-"
	if !strings.HasPrefix(lexicalRelative, prefix) {
		return nil, fmt.Errorf("in-worktree input must be a retained %s artifact", class)
	}
	filenamePart := strings.TrimPrefix(lexicalRelative, prefix)
	digest, ok := strings.CutSuffix(filenamePart, extension)
	if !ok || !validDigest(digest) {
		return nil, errors.New("retained artifact path is not content-addressed")
	}
	if err := checkRegularPath(root, lexicalRelative); err != nil {
		return nil, fmt.Errorf("validate retained artifact path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("inspect retained artifact size: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("retained artifact must be a regular file")
	}
	if info.Size() > maximum {
		return nil, retainedInputOversizeError(oversizeMessage, maximum)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read retained artifact: %w", err)
	}
	if int64(len(data)) > maximum {
		return nil, retainedInputOversizeError(oversizeMessage, maximum)
	}
	actual := sha256.Sum256(data)
	if hex.EncodeToString(actual[:]) != digest {
		return nil, errors.New("retained artifact content does not match its SHA-256 path")
	}
	return data, nil
}

func retainedInputOversizeError(message string, maximum int64) error {
	if strings.Contains(message, "%d") {
		return fmt.Errorf(message, maximum)
	}
	return errors.New(message)
}

// relativePathUnderRoot maps an absolute path back to the repository-relative
// spelling used by the caller. Git may return a physical repository path while
// the caller uses a symlinked spelling of the same root (for example /var on
// macOS). Walking existing path prefixes preserves the caller's in-repository
// components so checkRegularPath can still reject symlinks below the root.
func relativePathUnderRoot(root, filename string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	rootInfo, err := os.Stat(rootReal)
	if err != nil {
		return "", fmt.Errorf("inspect repository root: %w", err)
	}
	filenameAbs, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(filenameAbs)
	remaining := strings.TrimPrefix(filenameAbs, volume)
	remaining = strings.TrimLeft(remaining, string(filepath.Separator))
	components := strings.Split(remaining, string(filepath.Separator))
	current := volume + string(filepath.Separator)
	for index, component := range components {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && os.SameFile(rootInfo, info) {
			relative := filepath.Join(components[index+1:]...)
			if relative == "" {
				relative = "."
			}
			return filepath.ToSlash(filepath.Clean(relative)), nil
		}
	}
	return "", errors.New("input path does not resolve through the repository root")
}

func validDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(digest) == digest
}

// FilterPorcelainStatus removes only records wholly inside the managed artifact
// tree. Rename/copy records crossing that boundary are retained.
func FilterPorcelainStatus(raw []byte) ([]byte, error) {
	records, err := parsePorcelainStatus(raw)
	if err != nil {
		return nil, err
	}
	var filtered []byte
	for _, record := range records {
		if !record.managedOnly {
			filtered = append(filtered, record.raw...)
		}
	}
	return filtered, nil
}

// HasStagedChanges ignores staged changes only when every path in the Git
// status record belongs to the managed artifact tree.
func HasStagedChanges(raw []byte) (bool, error) {
	records, err := parsePorcelainStatus(raw)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.status[0] != ' ' && record.status[0] != '?' && !record.managedOnly {
			return true, nil
		}
	}
	return false, nil
}

type statusRecord struct {
	status      [2]byte
	managedOnly bool
	raw         []byte
}

func parsePorcelainStatus(raw []byte) ([]statusRecord, error) {
	var records []statusRecord
	for len(raw) > 0 {
		end := bytes.IndexByte(raw, 0)
		if end < 0 {
			return nil, errors.New("malformed NUL-delimited Git status")
		}
		entry := raw[:end]
		raw = raw[end+1:]
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, errors.New("malformed porcelain Git status entry")
		}
		status := [2]byte{entry[0], entry[1]}
		paths := [][]byte{entry[3:]}
		endRecord := end + 1
		if status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C' {
			nextEnd := bytes.IndexByte(raw, 0)
			if nextEnd < 0 {
				return nil, errors.New("malformed rename/copy Git status entry")
			}
			paths = append(paths, raw[:nextEnd])
			raw = raw[nextEnd+1:]
			endRecord += nextEnd + 1
		}
		managedOnly := true
		for _, path := range paths {
			value := string(path)
			if value != ManagedRoot && !strings.HasPrefix(value, ManagedRoot+"/") {
				managedOnly = false
				break
			}
		}
		records = append(records, statusRecord{status: status, managedOnly: managedOnly, raw: append([]byte(nil), rawRecordBytes(entry, paths, status)...)})
		_ = endRecord
	}
	return records, nil
}

func rawRecordBytes(entry []byte, paths [][]byte, status [2]byte) []byte {
	result := make([]byte, 0, len(entry)+1)
	result = append(result, entry...)
	result = append(result, 0)
	if status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C' {
		result = append(result, paths[1]...)
		result = append(result, 0)
	}
	return result
}

// WorktreeStatus returns NUL-delimited porcelain status with managed artifacts
// removed, preserving all other staged, unstaged, untracked, and rename paths.
func WorktreeStatus(ctx context.Context, repo string) ([]byte, error) {
	return WorktreeStatusWithEnv(ctx, repo, nil)
}

// WorktreeStatusWithEnv returns NUL-delimited status with managed artifacts
// removed, using the supplied Git environment for callers that must suppress
// optional index refreshes.
func WorktreeStatusWithEnv(ctx context.Context, repo string, env []string) ([]byte, error) {
	raw, err := gitutil.Bytes(ctx, repo, env, nil, "status", "--porcelain=v1", "--untracked-files=all", "-z")
	if err != nil {
		return nil, fmt.Errorf("inspect source state: %w", err)
	}
	return FilterPorcelainStatus(raw)
}

func StagedChanges(ctx context.Context, repo string) (bool, error) {
	raw, err := gitutil.Bytes(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all", "-z")
	if err != nil {
		return false, fmt.Errorf("inspect source index: %w", err)
	}
	return HasStagedChanges(raw)
}
