package baselineproof

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
)

const (
	objectRoot  = "objects"
	gitRevParse = "rev-parse"
	gitCatFile  = "cat-file"
)

type object struct {
	Type string
	OID  string
	Data []byte
}

type objectDescriptor struct {
	Type string
	OID  string
	Size uint64
}

func Build(ctx context.Context, repo, baseCommit string, maxBytes uint64) ([]byte, error) {
	if maxBytes == 0 {
		return nil, errors.New("baseline snapshot maximum size must be greater than zero")
	}
	objectFormat, baseCommit, baseTree, err := resolveBaselineIdentity(ctx, repo, baseCommit)
	if err != nil {
		return nil, err
	}
	ids, err := baselineObjectIDs(ctx, repo, baseCommit, baseTree)
	if err != nil {
		return nil, err
	}
	descriptors, projected, err := describeBaselineObjects(ctx, repo, baseCommit, ids)
	if err != nil {
		return nil, err
	}
	if projected > maxBytes {
		pathsByObject, pathErr := baselineBlobPaths(ctx, repo, baseTree)
		contributors, totalPaths := baselineContributors(descriptors, pathsByObject)
		actual := map[string]any{"projected_bytes": projected, "contributing_path_count": totalPaths}
		if pathErr != nil {
			actual["path_discovery_error"] = pathErr.Error()
		}
		if totalPaths > len(contributors) {
			actual["additional_paths_omitted"] = totalPaths - len(contributors)
		}
		paths := make([]string, len(contributors))
		for i, contributor := range contributors {
			paths[i] = contributor.Path
		}
		return nil, &diagnostic.Error{
			Summary: fmt.Sprintf("embedded baseline projected size %d exceeds maximum size %d", projected, maxBytes),
			Report: diagnostic.Report{
				Stage:        "baseline snapshot",
				Condition:    "projected embedded baseline size exceeds the configured member limit",
				Expected:     map[string]any{"maximum_bytes": maxBytes},
				Actual:       actual,
				Paths:        paths,
				Contributors: contributors,
			},
		}
	}
	objects, err := readBaselineObjects(ctx, repo, descriptors)
	if err != nil {
		return nil, err
	}
	raw, err := encode(objects)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) != projected {
		return nil, fmt.Errorf("embedded baseline size accounting mismatch: got %d want %d", len(raw), projected)
	}
	if err := Verify(raw, objectFormat, baseCommit, baseTree); err != nil {
		return nil, fmt.Errorf("verify generated baseline snapshot: %w", err)
	}
	return raw, nil
}

func resolveBaselineIdentity(ctx context.Context, repo, baseCommit string) (string, string, string, error) {
	objectFormat, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return "", "", "", fmt.Errorf("detect Git object format: %w", err)
	}
	hashLen, err := objectIDHexLength(objectFormat)
	if err != nil {
		return "", "", "", err
	}
	baseCommit, err = gitutil.Output(ctx, repo, nil, nil, gitRevParse, "--verify", baseCommit+"^{commit}")
	if err != nil {
		return "", "", "", fmt.Errorf("resolve baseline commit: %w", err)
	}
	if len(baseCommit) != hashLen {
		return "", "", "", fmt.Errorf("baseline commit has invalid object id length %d", len(baseCommit))
	}
	baseTree, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, baseCommit+"^{tree}")
	if err != nil {
		return "", "", "", fmt.Errorf("resolve baseline tree: %w", err)
	}
	return objectFormat, baseCommit, baseTree, nil
}

func baselineObjectIDs(ctx context.Context, repo, baseCommit, baseTree string) ([]string, error) {
	listed, err := gitutil.Output(ctx, repo, nil, nil, "rev-list", "--objects", "--no-object-names", baseTree)
	if err != nil {
		return nil, fmt.Errorf("list baseline tree objects: %w", err)
	}
	idsSet := map[string]struct{}{baseCommit: {}}
	for _, oid := range strings.Fields(listed) {
		idsSet[oid] = struct{}{}
	}
	ids := make([]string, 0, len(idsSet))
	for oid := range idsSet {
		ids = append(ids, oid)
	}
	sort.Strings(ids)
	return ids, nil
}

func describeBaselineObjects(ctx context.Context, repo, baseCommit string, ids []string) ([]objectDescriptor, uint64, error) {
	projected := uint64(1024) // POSIX tar end-of-archive blocks.
	descriptors := make([]objectDescriptor, 0, len(ids))
	var input strings.Builder
	for _, oid := range ids {
		input.WriteString(oid)
		input.WriteByte('\n')
	}
	raw, err := gitutil.Bytes(ctx, repo, nil, strings.NewReader(input.String()), gitCatFile, "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return nil, 0, fmt.Errorf("describe baseline objects: %w", err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\n'})
	if len(lines) != len(ids) {
		return nil, 0, fmt.Errorf("describe baseline objects returned %d records, want %d", len(lines), len(ids))
	}
	for index, line := range lines {
		fields := strings.Fields(string(line))
		if len(fields) != 3 {
			return nil, 0, fmt.Errorf("describe baseline object %s returned invalid metadata", ids[index])
		}
		oid, typ := fields[0], fields[1]
		if oid != ids[index] {
			return nil, 0, fmt.Errorf("describe baseline object mismatch: got %s want %s", oid, ids[index])
		}
		if err := validateBaselineObjectType(oid, baseCommit, typ); err != nil {
			return nil, 0, err
		}
		size, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("parse baseline object size %s: %w", oid, err)
		}
		descriptor := objectDescriptor{Type: typ, OID: oid, Size: size}
		projected, err = addTarEntrySize(projected, descriptor.Size, ^uint64(0))
		if err != nil {
			return nil, 0, err
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, projected, nil
}

func baselineBlobPaths(ctx context.Context, repo, baseTree string) (map[string][]string, error) {
	raw, err := gitutil.Bytes(ctx, repo, nil, nil, "ls-tree", "-r", "-z", "--full-tree", baseTree)
	if err != nil {
		return nil, fmt.Errorf("list baseline file paths: %w", err)
	}
	paths := make(map[string][]string)
	for _, entry := range bytes.Split(raw, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		separator := bytes.IndexByte(entry, '\t')
		if separator < 0 {
			return nil, errors.New("parse baseline file path: missing metadata separator")
		}
		metadata := strings.Fields(string(entry[:separator]))
		if len(metadata) != 3 || metadata[1] != "blob" {
			continue
		}
		oid := metadata[2]
		paths[oid] = append(paths[oid], string(entry[separator+1:]))
	}
	for oid := range paths {
		sort.Strings(paths[oid])
	}
	return paths, nil
}

func baselineContributors(descriptors []objectDescriptor, pathsByObject map[string][]string) ([]diagnostic.PathContributor, int) {
	const maxReportedPaths = 10
	contributors := make([]diagnostic.PathContributor, 0)
	for _, descriptor := range descriptors {
		if descriptor.Type != "blob" {
			continue
		}
		for _, path := range pathsByObject[descriptor.OID] {
			contributors = append(contributors, diagnostic.PathContributor{Path: path, BlobBytes: descriptor.Size})
		}
	}
	sort.Slice(contributors, func(i, j int) bool {
		if contributors[i].BlobBytes != contributors[j].BlobBytes {
			return contributors[i].BlobBytes > contributors[j].BlobBytes
		}
		return contributors[i].Path < contributors[j].Path
	})
	total := len(contributors)
	if len(contributors) > maxReportedPaths {
		contributors = contributors[:maxReportedPaths]
	}
	return contributors, total
}

func validateBaselineObjectType(oid, baseCommit, typ string) error {
	if oid == baseCommit {
		if typ != "commit" {
			return fmt.Errorf("baseline object %s is %s, want commit", oid, typ)
		}
		return nil
	}
	if typ != "tree" && typ != "blob" {
		return fmt.Errorf("unexpected baseline tree object %s type %s", oid, typ)
	}
	return nil
}

func readBaselineObjects(ctx context.Context, repo string, descriptors []objectDescriptor) ([]object, error) {
	if len(descriptors) == 0 {
		return nil, nil
	}
	var input strings.Builder
	for _, descriptor := range descriptors {
		input.WriteString(descriptor.OID)
		input.WriteByte('\n')
	}
	raw, err := gitutil.Bytes(ctx, repo, nil, strings.NewReader(input.String()), gitCatFile, "--batch")
	if err != nil {
		return nil, fmt.Errorf("read baseline objects: %w", err)
	}
	objects := make([]object, 0, len(descriptors))
	offset := 0
	for _, descriptor := range descriptors {
		if offset >= len(raw) {
			return nil, fmt.Errorf("read baseline object %s: missing batch record", descriptor.OID)
		}
		relativeEnd := bytes.IndexByte(raw[offset:], '\n')
		if relativeEnd < 0 {
			return nil, fmt.Errorf("read baseline object %s: unterminated batch header", descriptor.OID)
		}
		headerEnd := offset + relativeEnd
		fields := strings.Fields(string(raw[offset:headerEnd]))
		if len(fields) != 3 {
			return nil, fmt.Errorf("read baseline object %s: invalid batch header", descriptor.OID)
		}
		if fields[0] != descriptor.OID || fields[1] != descriptor.Type {
			return nil, fmt.Errorf("read baseline object mismatch: got %s %s want %s %s", fields[0], fields[1], descriptor.OID, descriptor.Type)
		}
		size, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse baseline object size %s: %w", descriptor.OID, err)
		}
		if size != descriptor.Size {
			return nil, fmt.Errorf("baseline object %s size changed during snapshot: got %d want %d", descriptor.OID, size, descriptor.Size)
		}
		dataStart := headerEnd + 1
		if descriptor.Size > uint64(len(raw)-dataStart) {
			return nil, fmt.Errorf("read baseline object %s: truncated batch payload", descriptor.OID)
		}
		dataEnd := dataStart + int(descriptor.Size)
		if dataEnd >= len(raw) || raw[dataEnd] != '\n' {
			return nil, fmt.Errorf("read baseline object %s: invalid batch payload terminator", descriptor.OID)
		}
		objects = append(objects, object{Type: descriptor.Type, OID: descriptor.OID, Data: raw[dataStart:dataEnd]})
		offset = dataEnd + 1
	}
	if offset != len(raw) {
		return nil, fmt.Errorf("read baseline objects returned %d unexpected trailing bytes", len(raw)-offset)
	}
	return objects, nil
}

func addTarEntrySize(current, payload, max uint64) (uint64, error) {
	const block = uint64(512)
	if payload > ^uint64(0)-(block-1) {
		return 0, errors.New("embedded baseline size overflow")
	}
	padded := ((payload + block - 1) / block) * block
	if current > ^uint64(0)-block || current+block > ^uint64(0)-padded {
		return 0, errors.New("embedded baseline size overflow")
	}
	next := current + block + padded
	if next > max {
		return 0, fmt.Errorf("embedded baseline exceeds maximum size %d", max)
	}
	return next, nil
}

func Verify(raw []byte, objectFormat, baseCommit, baseTree string) error {
	objects, err := validateSnapshot(raw, objectFormat, baseCommit, baseTree)
	if err != nil {
		return err
	}
	repo, cleanup, err := materializeObjects(context.Background(), objects, objectFormat, baseCommit, baseTree)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return fmt.Errorf("validate baseline Git objects: %w", err)
	}
	_ = repo
	return nil
}

func Materialize(ctx context.Context, raw []byte, objectFormat, baseCommit, baseTree string) (string, func(), error) {
	objects, err := validateSnapshot(raw, objectFormat, baseCommit, baseTree)
	if err != nil {
		return "", nil, err
	}
	return materializeObjects(ctx, objects, objectFormat, baseCommit, baseTree)
}

func validateSnapshot(raw []byte, objectFormat, baseCommit, baseTree string) (map[string]object, error) {
	objects, err := parse(raw, objectFormat)
	if err != nil {
		return nil, err
	}
	if err := validateClosure(objects, objectFormat, baseCommit, baseTree); err != nil {
		return nil, err
	}
	comparison := exactBytesWriter{expected: raw, matching: true}
	if err := encodeTo(&comparison, mapValues(objects)); err != nil {
		return nil, err
	}
	if !comparison.matches() {
		return nil, errors.New("baseline snapshot is not canonical")
	}
	return objects, nil
}

type exactBytesWriter struct {
	expected []byte
	offset   int
	matching bool
}

func (w *exactBytesWriter) Write(p []byte) (int, error) {
	end := w.offset + len(p)
	if end > len(w.expected) || !bytes.Equal(p, w.expected[w.offset:min(end, len(w.expected))]) {
		w.matching = false
	}
	w.offset = end
	return len(p), nil
}

func (w *exactBytesWriter) matches() bool {
	return w.matching && w.offset == len(w.expected)
}

func materializeObjects(ctx context.Context, objects map[string]object, objectFormat, baseCommit, baseTree string) (string, func(), error) {
	repo, err := os.MkdirTemp("", "polis-baseline-repo-*")
	if err != nil {
		return "", nil, fmt.Errorf("create baseline repository: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(repo) }
	if _, err := gitutil.Bytes(ctx, repo, nil, nil, "init", "--bare", "--quiet", "--object-format="+objectFormat); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("initialize baseline repository: %w", err)
	}
	ordered := mapValues(objects)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Type != ordered[j].Type {
			return ordered[i].Type < ordered[j].Type
		}
		return ordered[i].OID < ordered[j].OID
	})
	staging, err := os.MkdirTemp("", "polis-baseline-objects-*")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("stage baseline objects: %w", err)
	}
	defer os.RemoveAll(staging)
	for start := 0; start < len(ordered); {
		end := start + 1
		for end < len(ordered) && ordered[end].Type == ordered[start].Type {
			end++
		}
		var paths strings.Builder
		for index := start; index < end; index++ {
			filename := filepath.Join(staging, strconv.Itoa(index))
			if err := os.WriteFile(filename, ordered[index].Data, 0o600); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("stage baseline object %s: %w", ordered[index].OID, err)
			}
			paths.WriteString(filename)
			paths.WriteByte('\n')
		}
		raw, err := gitutil.Bytes(ctx, repo, nil, strings.NewReader(paths.String()), "hash-object", "-t", ordered[start].Type, "-w", "--stdin-paths")
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("materialize baseline %s objects: %w", ordered[start].Type, err)
		}
		got := strings.Fields(string(raw))
		if len(got) != end-start {
			cleanup()
			return "", nil, fmt.Errorf("materialize baseline %s objects returned %d ids, want %d", ordered[start].Type, len(got), end-start)
		}
		for index := start; index < end; index++ {
			if got[index-start] != ordered[index].OID {
				cleanup()
				return "", nil, fmt.Errorf("materialized baseline object mismatch: got %s want %s", got[index-start], ordered[index].OID)
			}
			_ = os.Remove(filepath.Join(staging, strconv.Itoa(index)))
		}
		start = end
	}
	gotTree, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, baseCommit+"^{tree}")
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resolve materialized baseline tree: %w", err)
	}
	if gotTree != baseTree {
		cleanup()
		return "", nil, fmt.Errorf("materialized baseline tree mismatch: got %s want %s", gotTree, baseTree)
	}
	return repo, cleanup, nil
}

func encode(objects []object) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeTo(&buf, objects); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeTo(w io.Writer, objects []object) error {
	ordered := append([]object(nil), objects...)
	sort.Slice(ordered, func(i, j int) bool { return objectName(ordered[i]) < objectName(ordered[j]) })
	tw := tar.NewWriter(w)
	for _, obj := range ordered {
		hdr := &tar.Header{
			Name: objectName(obj), Mode: 0o644, Size: int64(len(obj.Data)), Typeflag: tar.TypeReg,
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Time{}, ChangeTime: time.Time{}, Format: tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write baseline tar header: %w", err)
		}
		if _, err := tw.Write(obj.Data); err != nil {
			return fmt.Errorf("write baseline tar object: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("finalize baseline tar: %w", err)
	}
	return nil
}

func objectName(obj object) string {
	return objectRoot + "/" + obj.Type + "/" + obj.OID
}

func parse(raw []byte, objectFormat string) (map[string]object, error) {
	hashLen, err := objectIDHexLength(objectFormat)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	objects := map[string]object{}
	names := map[string]struct{}{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read baseline tar: %w", err)
		}
		obj, err := readSnapshotMember(tr, hdr, len(raw), hashLen, objectFormat, names, objects)
		if err != nil {
			return nil, err
		}
		objects[obj.OID] = obj
	}
	if len(objects) == 0 {
		return nil, errors.New("baseline snapshot is empty")
	}
	return objects, nil
}

func readSnapshotMember(tr *tar.Reader, hdr *tar.Header, rawLen, hashLen int, objectFormat string, names map[string]struct{}, objects map[string]object) (object, error) {
	if err := validateSnapshotHeader(hdr, names); err != nil {
		return object{}, err
	}
	typ, oid, err := parseObjectName(hdr.Name, hashLen)
	if err != nil {
		return object{}, err
	}
	if _, ok := objects[oid]; ok {
		return object{}, fmt.Errorf("duplicate baseline object %s", oid)
	}
	data, err := readSnapshotPayload(tr, hdr, rawLen, oid)
	if err != nil {
		return object{}, err
	}
	got, err := objectHash(objectFormat, typ, data)
	if err != nil {
		return object{}, err
	}
	if got != oid {
		return object{}, fmt.Errorf("baseline object id mismatch for %s: got %s", hdr.Name, got)
	}
	return object{Type: typ, OID: oid, Data: data}, nil
}

func validateSnapshotHeader(hdr *tar.Header, names map[string]struct{}) error {
	if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
		return fmt.Errorf("baseline tar member %q is not a regular file", hdr.Name)
	}
	if hdr.Mode != 0o644 || hdr.Uid != 0 || hdr.Gid != 0 || hdr.Uname != "" || hdr.Gname != "" || !hdr.ModTime.Equal(time.Unix(0, 0).UTC()) {
		return fmt.Errorf("baseline tar member %q has non-canonical metadata", hdr.Name)
	}
	if _, ok := names[hdr.Name]; ok {
		return fmt.Errorf("duplicate baseline tar member %q", hdr.Name)
	}
	names[hdr.Name] = struct{}{}
	return nil
}

func readSnapshotPayload(tr *tar.Reader, hdr *tar.Header, rawLen int, oid string) ([]byte, error) {
	if hdr.Size < 0 || hdr.Size > int64(rawLen) {
		return nil, fmt.Errorf("baseline object %s has invalid size %d", oid, hdr.Size)
	}
	data, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
	if err != nil {
		return nil, fmt.Errorf("read baseline object %s: %w", oid, err)
	}
	if int64(len(data)) != hdr.Size {
		return nil, fmt.Errorf("baseline object %s size mismatch", oid)
	}
	return data, nil
}

func parseObjectName(name string, hashLen int) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 3 || parts[0] != objectRoot {
		return "", "", fmt.Errorf("invalid baseline tar member %q", name)
	}
	typ, oid := parts[1], parts[2]
	if typ != "commit" && typ != "tree" && typ != "blob" {
		return "", "", fmt.Errorf("unsupported baseline object type %q", typ)
	}
	if len(oid) != hashLen {
		return "", "", fmt.Errorf("invalid baseline object id %q", oid)
	}
	if _, err := hex.DecodeString(oid); err != nil || strings.ToLower(oid) != oid {
		return "", "", fmt.Errorf("invalid baseline object id %q", oid)
	}
	return typ, oid, nil
}

func objectHash(objectFormat, typ string, data []byte) (string, error) {
	var h hash.Hash
	switch objectFormat {
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	default:
		return "", fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	_, _ = io.WriteString(h, typ+" "+strconv.Itoa(len(data))+"\x00")
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validateClosure(objects map[string]object, objectFormat, baseCommit, baseTree string) error {
	hashLen, err := objectIDHexLength(objectFormat)
	if err != nil {
		return err
	}
	if len(baseCommit) != hashLen || len(baseTree) != hashLen {
		return errors.New("baseline commit/tree object id length does not match Git object format")
	}
	commit, ok := objects[baseCommit]
	if !ok || commit.Type != "commit" {
		return fmt.Errorf("baseline snapshot is missing commit %s", baseCommit)
	}
	commitTree, err := commitTreeOID(commit.Data, hashLen)
	if err != nil {
		return err
	}
	if commitTree != baseTree {
		return fmt.Errorf("baseline commit tree mismatch: got %s want %s", commitTree, baseTree)
	}
	reachable := map[string]struct{}{baseCommit: {}}
	visiting := map[string]bool{}
	if err := walkTree(objects, baseTree, hashLen, reachable, visiting); err != nil {
		return err
	}
	if len(reachable) != len(objects) {
		extras := make([]string, 0)
		for oid := range objects {
			if _, ok := reachable[oid]; !ok {
				extras = append(extras, oid)
			}
		}
		sort.Strings(extras)
		return fmt.Errorf("baseline snapshot contains unrelated objects: %s", strings.Join(extras, ", "))
	}
	return nil
}

func commitTreeOID(data []byte, hashLen int) (string, error) {
	if !bytes.HasPrefix(data, []byte("tree ")) {
		return "", errors.New("baseline commit is missing leading tree header")
	}
	end := bytes.IndexByte(data, '\n')
	if end < 0 {
		return "", errors.New("baseline commit has malformed tree header")
	}
	oid := string(data[len("tree "):end])
	if len(oid) != hashLen {
		return "", errors.New("baseline commit tree id has invalid length")
	}
	if _, err := hex.DecodeString(oid); err != nil || strings.ToLower(oid) != oid {
		return "", errors.New("baseline commit tree id is not lowercase hexadecimal")
	}
	return oid, nil
}

type treeEntry struct {
	Mode  string
	Child string
	Next  int
}

func walkTree(objects map[string]object, oid string, hashLen int, reachable map[string]struct{}, visiting map[string]bool) error {
	if visiting[oid] {
		return fmt.Errorf("baseline tree cycle at %s", oid)
	}
	obj, ok := objects[oid]
	if !ok || obj.Type != "tree" {
		return fmt.Errorf("baseline snapshot is missing tree %s", oid)
	}
	if _, done := reachable[oid]; done {
		return nil
	}
	reachable[oid] = struct{}{}
	visiting[oid] = true
	defer delete(visiting, oid)

	for pos := 0; pos < len(obj.Data); {
		entry, err := parseTreeEntry(obj.Data, pos, hashLen, oid)
		if err != nil {
			return err
		}
		if err := validateTreeEntry(objects, entry, hashLen, reachable, visiting); err != nil {
			return err
		}
		pos = entry.Next
	}
	return nil
}

func parseTreeEntry(data []byte, pos, hashLen int, treeOID string) (treeEntry, error) {
	space := bytes.IndexByte(data[pos:], ' ')
	if space < 0 {
		return treeEntry{}, fmt.Errorf("malformed baseline tree %s", treeOID)
	}
	space += pos
	nul := bytes.IndexByte(data[space+1:], 0)
	if nul < 0 {
		return treeEntry{}, fmt.Errorf("malformed baseline tree %s", treeOID)
	}
	nul += space + 1
	name := data[space+1 : nul]
	if len(name) == 0 || bytes.Contains(name, []byte{'/'}) {
		return treeEntry{}, fmt.Errorf("malformed baseline tree %s entry name", treeOID)
	}
	rawStart := nul + 1
	rawEnd := rawStart + hashLen/2
	if rawEnd > len(data) {
		return treeEntry{}, fmt.Errorf("malformed baseline tree %s object id", treeOID)
	}
	return treeEntry{
		Mode:  string(data[pos:space]),
		Child: hex.EncodeToString(data[rawStart:rawEnd]),
		Next:  rawEnd,
	}, nil
}

func validateTreeEntry(objects map[string]object, entry treeEntry, hashLen int, reachable map[string]struct{}, visiting map[string]bool) error {
	switch entry.Mode {
	case "40000", "040000":
		return walkTree(objects, entry.Child, hashLen, reachable, visiting)
	case "100644", "100755", "120000":
		childObj, ok := objects[entry.Child]
		if !ok || childObj.Type != "blob" {
			return fmt.Errorf("baseline snapshot is missing blob %s", entry.Child)
		}
		reachable[entry.Child] = struct{}{}
		return nil
	case "160000":
		// Gitlink identity belongs to the submodule and is not an object owned by this repository.
		return nil
	default:
		return fmt.Errorf("unsupported baseline tree mode %q", entry.Mode)
	}
}

func mapValues(objects map[string]object) []object {
	result := make([]object, 0, len(objects))
	for _, obj := range objects {
		result = append(result, obj)
	}
	return result
}

func objectIDHexLength(objectFormat string) (int, error) {
	switch objectFormat {
	case "sha1":
		return 40, nil
	case "sha256":
		return 64, nil
	default:
		return 0, fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
}
