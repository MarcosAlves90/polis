package spec

import "testing"

const hA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const hC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
const hD = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

func canonicalManifestJSON() []byte {
	return []byte(`{"format_version":2,"project":"gitrex","change":"fix-wsl-docker","git_object_format":"sha1","base_commit":"1111111111111111111111111111111111111111","target_tree":"2222222222222222222222222222222222222222","policy_sha256":"` + hA + `","change_contract_sha256":"` + hB + `","regression_patch_sha256":"` + hC + `","payload_sha256":"` + hD + `"}`)
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
