package packageverify

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v5/spec"
)

const (
	MaxArchiveBytes           int64  = 64 << 20
	MaxTotalUncompressedBytes uint64 = 64 << 20
	MaxContractMemberBytes    uint64 = 1 << 20
	MaxEvidenceMemberBytes    uint64 = 16 << 20
	MaxPatchMemberBytes       uint64 = 32 << 20
	MaxChecksumsMemberBytes   uint64 = 64 << 10
)

const (
	memberChange     = "polis/polis-change.json"
	memberChecksums  = "polis/polis-checksums.sha256"
	memberEvidence   = "polis/polis-evidence.ndjson"
	memberManifest   = "polis/polis-manifest.json"
	memberPayload    = "polis/polis-payload.patch"
	memberPolicy     = "polis/polis-policy.json"
	memberRegression = "polis/polis-regression.patch"
)

var expectedMembers = []string{
	memberChange,
	memberChecksums,
	memberEvidence,
	memberManifest,
	memberPayload,
	memberPolicy,
	memberRegression,
}

var lowerSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Result struct {
	Project    string
	Change     string
	BaseCommit string
	TargetTree string
}

type Inspection struct {
	Project                     string   `json:"project"`
	Change                      string   `json:"change"`
	FormatVersion               int      `json:"format_version"`
	PolicySchemaVersion         int      `json:"policy_schema_version"`
	ChangeContractSchemaVersion int      `json:"change_contract_schema_version"`
	Kind                        string   `json:"kind"`
	BaseCommit                  string   `json:"base_commit"`
	TargetTree                  string   `json:"target_tree"`
	AllowedPaths                []string `json:"allowed_paths"`
	Gates                       []string `json:"gates"`
	EvidenceEvents              int      `json:"evidence_events"`
}

type Package struct {
	Result          Result
	Manifest        spec.Manifest
	Policy          spec.Policy
	Change          spec.ChangeContract
	Patch           []byte
	RegressionPatch []byte
	Evidence        []byte
}

type decodedContracts struct {
	manifest spec.Manifest
	policy   spec.Policy
	change   spec.ChangeContract
}

func Verify(filename string) (Result, error) {
	pkg, err := Load(filename)
	if err != nil {
		return Result{}, err
	}
	return pkg.Result, nil
}

func Inspect(filename string) (Inspection, error) {
	pkg, err := Load(filename)
	if err != nil {
		return Inspection{}, err
	}
	events, err := spec.DecodeEvidence(pkg.Evidence)
	if err != nil {
		return Inspection{}, err
	}
	inspection := Inspection{
		Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, FormatVersion: pkg.Manifest.FormatVersion,
		PolicySchemaVersion: pkg.Policy.SchemaVersion, ChangeContractSchemaVersion: pkg.Change.SchemaVersion,
		Kind: pkg.Change.Kind, BaseCommit: pkg.Manifest.BaseCommit, TargetTree: pkg.Manifest.TargetTree,
		EvidenceEvents: len(events),
	}
	if pkg.Change.Scope != nil {
		inspection.AllowedPaths = append([]string(nil), pkg.Change.Scope.AllowedPaths...)
	} else {
		inspection.AllowedPaths = []string{"."}
	}
	inspection.Gates = make([]string, 0, len(pkg.Policy.Gates))
	for _, gate := range pkg.Policy.Gates {
		inspection.Gates = append(inspection.Gates, gate.ID)
	}
	return inspection, nil
}

func Load(filename string) (Package, error) {
	contents, err := loadArchiveContents(filename)
	if err != nil {
		return Package{}, err
	}
	contracts, err := decodeContracts(contents)
	if err != nil {
		return Package{}, err
	}
	regressionPatch := contents[memberRegression]
	if err := validateRegressionPatch(contracts.change, regressionPatch); err != nil {
		return Package{}, err
	}
	if err := validateEvidenceAndIntegrity(contents, contracts); err != nil {
		return Package{}, err
	}
	return packageFromContents(contents, contracts, regressionPatch), nil
}

func loadArchiveContents(filename string) (map[string][]byte, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("stat POLIS archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("POLIS archive must be a regular file")
	}
	if info.Size() > MaxArchiveBytes {
		return nil, fmt.Errorf("POLIS archive exceeds maximum size %d", MaxArchiveBytes)
	}
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open POLIS archive: %w", err)
	}
	defer zr.Close()
	files, err := indexArchiveFiles(zr.File)
	if err != nil {
		return nil, err
	}
	if err := validateInventory(files); err != nil {
		return nil, err
	}
	return readArchiveContents(files)
}

func indexArchiveFiles(entries []*zip.File) (map[string]*zip.File, error) {
	if len(entries) != len(expectedMembers) {
		return nil, fmt.Errorf("invalid archive inventory: got %d members, want %d", len(entries), len(expectedMembers))
	}
	files := make(map[string]*zip.File, len(entries))
	var total uint64
	for _, f := range entries {
		if err := validateArchiveFile(f, files); err != nil {
			return nil, err
		}
		max, ok := memberLimit(f.Name)
		if !ok {
			return nil, fmt.Errorf("unexpected archive member %q", f.Name)
		}
		if f.UncompressedSize64 > max {
			return nil, fmt.Errorf("archive member %q exceeds maximum uncompressed size %d", f.Name, max)
		}
		total += f.UncompressedSize64
		if total > MaxTotalUncompressedBytes {
			return nil, fmt.Errorf("archive exceeds maximum total uncompressed size %d", MaxTotalUncompressedBytes)
		}
		files[f.Name] = f
	}
	return files, nil
}

func memberLimit(name string) (uint64, bool) {
	switch name {
	case memberManifest, memberPolicy, memberChange:
		return MaxContractMemberBytes, true
	case memberEvidence:
		return MaxEvidenceMemberBytes, true
	case memberPayload, memberRegression:
		return MaxPatchMemberBytes, true
	case memberChecksums:
		return MaxChecksumsMemberBytes, true
	default:
		return 0, false
	}
}

func validateArchiveFile(f *zip.File, indexed map[string]*zip.File) error {
	if err := validateMemberPath(f.Name); err != nil {
		return err
	}
	if !f.Mode().IsRegular() {
		return fmt.Errorf("archive member %q is not a regular file", f.Name)
	}
	if _, exists := indexed[f.Name]; exists {
		return fmt.Errorf("duplicate archive member %q", f.Name)
	}
	return nil
}

func readArchiveContents(files map[string]*zip.File) (map[string][]byte, error) {
	contents := make(map[string][]byte, len(files))
	for name, f := range files {
		b, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		contents[name] = b
	}
	return contents, nil
}

func decodeContracts(contents map[string][]byte) (decodedContracts, error) {
	manifest, err := spec.DecodeManifest(contents[memberManifest])
	if err != nil {
		return decodedContracts{}, err
	}
	policy, err := spec.DecodePolicy(contents[memberPolicy])
	if err != nil {
		return decodedContracts{}, err
	}
	change, err := spec.DecodeChangeContract(contents[memberChange])
	if err != nil {
		return decodedContracts{}, err
	}
	return decodedContracts{manifest: manifest, policy: policy, change: change}, nil
}

func validateRegressionPatch(change spec.ChangeContract, regressionPatch []byte) error {
	if change.Kind == spec.ChangeKindDefect && len(regressionPatch) == 0 {
		return errors.New("defect package requires non-empty regression patch")
	}
	if change.Kind != spec.ChangeKindDefect && len(regressionPatch) != 0 {
		return errors.New("non-defect package requires empty regression patch")
	}
	return nil
}

func validateEvidenceAndIntegrity(contents map[string][]byte, contracts decodedContracts) error {
	events, err := spec.DecodeEvidence(contents[memberEvidence])
	if err != nil {
		return err
	}
	if contracts.manifest.FormatVersion == spec.FormatVersion {
		for i, event := range events {
			if event.Event == "command_finished" && (event.Stdout != nil || event.Stderr != nil) {
				return fmt.Errorf("evidence event %d stores raw command output in format v3", i)
			}
		}
	}
	if err := spec.ValidatePassEvidence(events, contracts.change, contracts.policy); err != nil {
		return fmt.Errorf("validate evidence contract: %w", err)
	}
	if err := verifyManifestDigests(contracts.manifest, contents); err != nil {
		return err
	}
	return verifyChecksumFile(contents)
}

func packageFromContents(contents map[string][]byte, contracts decodedContracts, regressionPatch []byte) Package {
	manifest := contracts.manifest
	result := Result{Project: manifest.Project, Change: manifest.Change, BaseCommit: manifest.BaseCommit, TargetTree: manifest.TargetTree}
	return Package{
		Result: result, Manifest: manifest, Policy: contracts.policy, Change: contracts.change,
		Patch:           append([]byte(nil), contents[memberPayload]...),
		RegressionPatch: append([]byte(nil), regressionPatch...),
		Evidence:        append([]byte(nil), contents[memberEvidence]...),
	}
}

func validateMemberPath(name string) error {
	if name == "" {
		return errors.New("empty archive member path")
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("archive member %q contains backslash", name)
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") {
		return fmt.Errorf("archive member %q is absolute", name)
	}
	if hasDriveLetterPrefix(name) {
		return fmt.Errorf("archive member %q uses drive-letter path", name)
	}
	if path.Clean(name) != name {
		return fmt.Errorf("archive member %q is not normalized", name)
	}
	if hasProhibitedPathSegment(name) {
		return fmt.Errorf("archive member %q contains prohibited path segment", name)
	}
	if !strings.HasPrefix(name, "polis/") {
		return fmt.Errorf("archive member %q is outside polis root", name)
	}
	return nil
}

func hasDriveLetterPrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	return (name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')
}

func hasProhibitedPathSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func validateInventory(files map[string]*zip.File) error {
	if len(files) != len(expectedMembers) {
		return fmt.Errorf("invalid archive inventory: got %d members, want %d", len(files), len(expectedMembers))
	}
	for _, name := range expectedMembers {
		if _, ok := files[name]; !ok {
			return fmt.Errorf("missing required archive member %q", name)
		}
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	max, ok := memberLimit(f.Name)
	if !ok {
		return nil, fmt.Errorf("unexpected archive member %q", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) > max {
		return nil, fmt.Errorf("archive member %q exceeds maximum size %d", f.Name, max)
	}
	return b, nil
}

func verifyManifestDigests(m spec.Manifest, contents map[string][]byte) error {
	checks := []struct{ name, want string }{
		{memberPolicy, m.PolicySHA256},
		{memberChange, m.ChangeContractSHA256},
		{memberRegression, m.RegressionPatchSHA256},
		{memberPayload, m.PayloadSHA256},
	}
	for _, check := range checks {
		if err := verifyDigest(contents[check.name], check.want, check.name); err != nil {
			return err
		}
	}
	return nil
}

func verifyDigest(data []byte, want, name string) error {
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("manifest digest mismatch for %s", name)
	}
	return nil
}

func verifyChecksumFile(contents map[string][]byte) error {
	expected := expectedChecksumNames(contents)
	gotNames, err := scanChecksumEntries(contents, expected)
	if err != nil {
		return err
	}
	return validateChecksumInventory(gotNames, expected)
}

func expectedChecksumNames(contents map[string][]byte) []string {
	expected := make([]string, 0, len(contents)-1)
	for name := range contents {
		if name != memberChecksums {
			expected = append(expected, name)
		}
	}
	sort.Strings(expected)
	return expected
}

func scanChecksumEntries(contents map[string][]byte, expected []string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(contents[memberChecksums])))
	gotNames := make([]string, 0, len(expected))
	seen := map[string]bool{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		name, err := verifyChecksumLine(scanner.Text(), lineNo, contents, seen)
		if err != nil {
			return nil, err
		}
		seen[name] = true
		gotNames = append(gotNames, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksum file: %w", err)
	}
	return gotNames, nil
}

func verifyChecksumLine(line string, lineNo int, contents map[string][]byte, seen map[string]bool) (string, error) {
	digest, name, err := parseChecksumLine(line, lineNo)
	if err != nil {
		return "", err
	}
	if err := validateChecksumName(name, lineNo, seen); err != nil {
		return "", err
	}
	data, ok := contents[name]
	if !ok {
		return "", fmt.Errorf("checksum references unknown member %q", name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return "", fmt.Errorf("checksum mismatch for %q", name)
	}
	return name, nil
}

func parseChecksumLine(line string, lineNo int) (string, string, error) {
	if len(line) < 67 || line[64:66] != "  " {
		return "", "", fmt.Errorf("malformed checksum line %d", lineNo)
	}
	digest, name := line[:64], line[66:]
	if !lowerSHA256.MatchString(digest) {
		return "", "", fmt.Errorf("malformed checksum digest on line %d", lineNo)
	}
	return digest, name, nil
}

func validateChecksumName(name string, lineNo int, seen map[string]bool) error {
	if err := validateMemberPath(name); err != nil {
		return fmt.Errorf("invalid checksum path on line %d: %w", lineNo, err)
	}
	if name == memberChecksums {
		return errors.New("checksum file must not hash itself")
	}
	if seen[name] {
		return fmt.Errorf("duplicate checksum entry %q", name)
	}
	return nil
}

func validateChecksumInventory(gotNames, expected []string) error {
	if len(gotNames) != len(expected) {
		return fmt.Errorf("checksum inventory mismatch: got %d entries, want %d", len(gotNames), len(expected))
	}
	if !sort.StringsAreSorted(gotNames) {
		return errors.New("checksum entries are not sorted")
	}
	for i := range expected {
		if gotNames[i] != expected[i] {
			return fmt.Errorf("checksum inventory mismatch at %d: got %q want %q", i, gotNames[i], expected[i])
		}
	}
	return nil
}
