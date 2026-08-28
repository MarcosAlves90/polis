package packageverify

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/polis-dev/polis-v4/spec"
)

var expectedMembers = []string{
	"polis/polis-change.json",
	"polis/polis-checksums.sha256",
	"polis/polis-evidence.ndjson",
	"polis/polis-manifest.json",
	"polis/polis-payload.patch",
	"polis/polis-policy.json",
	"polis/polis-regression.patch",
}

var lowerSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Result struct {
	Project    string
	Change     string
	BaseCommit string
	TargetTree string
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

func Verify(filename string) (Result, error) {
	pkg, err := Load(filename)
	if err != nil {
		return Result{}, err
	}
	return pkg.Result, nil
}

func Load(filename string) (Package, error) {
	zr, err := zip.OpenReader(filename)
	if err != nil {
		return Package{}, fmt.Errorf("open POLIS archive: %w", err)
	}
	defer zr.Close()

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if err := validateMemberPath(f.Name); err != nil {
			return Package{}, err
		}
		if !f.Mode().IsRegular() {
			return Package{}, fmt.Errorf("archive member %q is not a regular file", f.Name)
		}
		if _, exists := files[f.Name]; exists {
			return Package{}, fmt.Errorf("duplicate archive member %q", f.Name)
		}
		files[f.Name] = f
	}
	if err := validateInventory(files); err != nil {
		return Package{}, err
	}

	contents := make(map[string][]byte, len(files))
	for name, f := range files {
		b, err := readZipFile(f)
		if err != nil {
			return Package{}, fmt.Errorf("read %s: %w", name, err)
		}
		contents[name] = b
	}

	manifest, err := spec.DecodeManifest(contents["polis/polis-manifest.json"])
	if err != nil {
		return Package{}, err
	}
	policy, err := spec.DecodePolicy(contents["polis/polis-policy.json"])
	if err != nil {
		return Package{}, err
	}
	change, err := spec.DecodeChangeContract(contents["polis/polis-change.json"])
	if err != nil {
		return Package{}, err
	}
	regressionPatch := contents["polis/polis-regression.patch"]
	if change.Kind == spec.ChangeKindDefect && len(regressionPatch) == 0 {
		return Package{}, errors.New("defect package requires non-empty regression patch")
	}
	if change.Kind != spec.ChangeKindDefect && len(regressionPatch) != 0 {
		return Package{}, errors.New("non-defect package requires empty regression patch")
	}
	events, err := spec.DecodeEvidence(contents["polis/polis-evidence.ndjson"])
	if err != nil {
		return Package{}, err
	}
	if err := spec.ValidatePassEvidence(events, change, policy); err != nil {
		return Package{}, fmt.Errorf("validate evidence contract: %w", err)
	}
	if err := verifyManifestDigests(manifest, contents); err != nil {
		return Package{}, err
	}
	if err := verifyChecksumFile(contents); err != nil {
		return Package{}, err
	}

	result := Result{Project: manifest.Project, Change: manifest.Change, BaseCommit: manifest.BaseCommit, TargetTree: manifest.TargetTree}
	return Package{
		Result: result, Manifest: manifest, Policy: policy, Change: change,
		Patch:           append([]byte(nil), contents["polis/polis-payload.patch"]...),
		RegressionPatch: append([]byte(nil), regressionPatch...),
		Evidence:        append([]byte(nil), contents["polis/polis-evidence.ndjson"]...),
	}, nil
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
	if len(name) >= 2 && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) && name[1] == ':' {
		return fmt.Errorf("archive member %q uses drive-letter path", name)
	}
	if path.Clean(name) != name {
		return fmt.Errorf("archive member %q is not normalized", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("archive member %q contains prohibited path segment", name)
		}
	}
	if !strings.HasPrefix(name, "polis/") {
		return fmt.Errorf("archive member %q is outside polis root", name)
	}
	return nil
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
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func verifyManifestDigests(m spec.Manifest, contents map[string][]byte) error {
	checks := []struct{ name, want string }{
		{"polis/polis-policy.json", m.PolicySHA256},
		{"polis/polis-change.json", m.ChangeContractSHA256},
		{"polis/polis-regression.patch", m.RegressionPatchSHA256},
		{"polis/polis-payload.patch", m.PayloadSHA256},
	}
	for _, check := range checks {
		sum := sha256.Sum256(contents[check.name])
		if hex.EncodeToString(sum[:]) != check.want {
			return fmt.Errorf("manifest digest mismatch for %s", check.name)
		}
	}
	return nil
}

func verifyChecksumFile(contents map[string][]byte) error {
	expected := make([]string, 0, len(contents)-1)
	for name := range contents {
		if name != "polis/polis-checksums.sha256" {
			expected = append(expected, name)
		}
	}
	sort.Strings(expected)

	scanner := bufio.NewScanner(strings.NewReader(string(contents["polis/polis-checksums.sha256"])))
	gotNames := make([]string, 0, len(expected))
	seen := map[string]bool{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if len(line) < 67 || line[64:66] != "  " {
			return fmt.Errorf("malformed checksum line %d", lineNo)
		}
		digest, name := line[:64], line[66:]
		if !lowerSHA256.MatchString(digest) {
			return fmt.Errorf("malformed checksum digest on line %d", lineNo)
		}
		if err := validateMemberPath(name); err != nil {
			return fmt.Errorf("invalid checksum path on line %d: %w", lineNo, err)
		}
		if name == "polis/polis-checksums.sha256" {
			return errors.New("checksum file must not hash itself")
		}
		if seen[name] {
			return fmt.Errorf("duplicate checksum entry %q", name)
		}
		seen[name] = true
		data, ok := contents[name]
		if !ok {
			return fmt.Errorf("checksum references unknown member %q", name)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != digest {
			return fmt.Errorf("checksum mismatch for %q", name)
		}
		gotNames = append(gotNames, name)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read checksum file: %w", err)
	}
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
