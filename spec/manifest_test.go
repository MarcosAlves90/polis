package spec

import (
	"encoding/json"
	"testing"
)

const hA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const hC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
const hD = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
const hE = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func canonicalManifestJSON() []byte {
	return []byte(`{"format_version":2,"project":"gitrex","change":"fix-wsl-docker","git_object_format":"sha1","base_commit":"1111111111111111111111111111111111111111","target_tree":"2222222222222222222222222222222222222222","policy_sha256":"` + hA + `","change_contract_sha256":"` + hB + `","regression_patch_sha256":"` + hC + `","payload_sha256":"` + hD + `"}`)
}

func TestCanonicalManifestSchemaMatchesCurrentFormat(t *testing.T) {
	var raw []byte
	for _, resource := range OfflineResources() {
		if resource.Path == "schemas/manifest.schema.json" {
			raw = resource.Data
			break
		}
	}
	if len(raw) == 0 {
		t.Fatal("canonical manifest schema not found")
	}
	var schema struct {
		Title      string                     `json:"title"`
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var version struct {
		Const int `json:"const"`
	}
	if err := json.Unmarshal(schema.Properties["format_version"], &version); err != nil {
		t.Fatal(err)
	}
	if version.Const != FormatVersion {
		t.Fatalf("manifest schema format_version=%d want=%d", version.Const, FormatVersion)
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	if !required["baseline_sha256"] {
		t.Fatal("current manifest schema does not require baseline_sha256")
	}
	if _, ok := schema.Properties["baseline_sha256"]; !ok {
		t.Fatal("current manifest schema does not define baseline_sha256")
	}
}

func TestDecodeManifestAcceptsCanonicalManifest(t *testing.T) {
	m, err := DecodeManifest(canonicalManifestJSON())
	if err != nil {
		t.Fatal(err)
	}
	if m.Project != "gitrex" || m.Change != "fix-wsl-docker" || m.FormatVersion != 2 {
		t.Fatalf("manifest=%+v", m)
	}
}

func TestDecodeManifestAcceptsSHA256GitObjectFormat(t *testing.T) {
	m := Manifest{FormatVersion: 2, Project: "gitrex", Change: "fix-wsl-docker", GitObjectFormat: "sha256", BaseCommit: string(makeHex('1', 64)), TargetTree: string(makeHex('2', 64)), PolicySHA256: hA, ChangeContractSHA256: hB, RegressionPatchSHA256: hC, PayloadSHA256: hD}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManifestV4RequiresEmbeddedBaselineDigest(t *testing.T) {
	m := Manifest{FormatVersion: FormatVersion, Project: "gitrex", Change: "self-contained", GitObjectFormat: "sha1", BaseCommit: string(makeHex('1', 40)), TargetTree: string(makeHex('2', 40)), PolicySHA256: hA, ChangeContractSHA256: hB, RegressionPatchSHA256: hC, PayloadSHA256: hD, BaselineSHA256: hE}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid v4 manifest rejected: %v", err)
	}
	m.BaselineSHA256 = ""
	if err := m.Validate(); err == nil {
		t.Fatal("format v4 manifest without baseline_sha256 accepted")
	}
}

func TestHistoricalManifestRejectsBaselineDigest(t *testing.T) {
	for _, version := range []int{LegacyFormatVersion, PreviousFormatVersion} {
		m := Manifest{FormatVersion: version, Project: "gitrex", Change: "historical", GitObjectFormat: "sha1", BaseCommit: string(makeHex('1', 40)), TargetTree: string(makeHex('2', 40)), PolicySHA256: hA, ChangeContractSHA256: hB, RegressionPatchSHA256: hC, PayloadSHA256: hD, BaselineSHA256: hE}
		if err := m.Validate(); err == nil {
			t.Fatalf("format v%d accepted baseline_sha256", version)
		}
	}
}

func makeHex(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestDecodeManifestRejectsUnknownField(t *testing.T) {
	raw := append(canonicalManifestJSON()[:len(canonicalManifestJSON())-1], []byte(`,"surprise":true}`)...)
	if _, err := DecodeManifest(raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeManifestRejectsUnsupportedVersion(t *testing.T) {
	m := Manifest{FormatVersion: 1, Project: "gitrex", Change: "x", GitObjectFormat: "sha1", BaseCommit: string(makeHex('1', 40)), TargetTree: string(makeHex('2', 40)), PolicySHA256: hA, ChangeContractSHA256: hB, RegressionPatchSHA256: hC, PayloadSHA256: hD}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeManifestRejectsMalformedAndTrailingJSON(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"format_version":`), append(canonicalManifestJSON(), []byte(` {}`)...)} {
		if _, err := DecodeManifest(raw); err == nil {
			t.Fatalf("expected rejection")
		}
	}
}

func TestManifestValidationRejectsInvalidFields(t *testing.T) {
	valid := Manifest{FormatVersion: 2, Project: "gitrex", Change: "fix-wsl-docker", GitObjectFormat: "sha1", BaseCommit: string(makeHex('1', 40)), TargetTree: string(makeHex('2', 40)), PolicySHA256: hA, ChangeContractSHA256: hB, RegressionPatchSHA256: hC, PayloadSHA256: hD}
	cases := []Manifest{
		func() Manifest { m := valid; m.Project = "Bad Project"; return m }(), func() Manifest { m := valid; m.Change = "Bad Change"; return m }(), func() Manifest { m := valid; m.GitObjectFormat = "md5"; return m }(), func() Manifest { m := valid; m.BaseCommit = "ABC"; return m }(), func() Manifest { m := valid; m.TargetTree = "ABC"; return m }(), func() Manifest { m := valid; m.PolicySHA256 = "ABC"; return m }(), func() Manifest { m := valid; m.ChangeContractSHA256 = "ABC"; return m }(), func() Manifest { m := valid; m.RegressionPatchSHA256 = "ABC"; return m }(), func() Manifest { m := valid; m.PayloadSHA256 = "ABC"; return m }(),
	}
	for _, m := range cases {
		if err := m.Validate(); err == nil {
			t.Fatalf("expected invalid %+v", m)
		}
	}
}

func FuzzDecodeManifest(f *testing.F) {
	f.Add([]byte(`{"format_version":3}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodeManifest(raw) })
}
