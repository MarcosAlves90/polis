package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const FormatVersion = 2

var (
	projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	changePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Manifest struct {
	FormatVersion         int    `json:"format_version"`
	Project               string `json:"project"`
	Change                string `json:"change"`
	GitObjectFormat       string `json:"git_object_format"`
	BaseCommit            string `json:"base_commit"`
	TargetTree            string `json:"target_tree"`
	PolicySHA256          string `json:"policy_sha256"`
	ChangeContractSHA256  string `json:"change_contract_sha256"`
	RegressionPatchSHA256 string `json:"regression_patch_sha256"`
	PayloadSHA256         string `json:"payload_sha256"`
}

func DecodeManifest(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return Manifest{}, err
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("manifest contains trailing JSON value")
	}
	return fmt.Errorf("manifest trailing data: %w", err)
}

func (m Manifest) Validate() error {
	if m.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported format_version %d", m.FormatVersion)
	}
	if !projectPattern.MatchString(m.Project) {
		return fmt.Errorf("invalid project %q", m.Project)
	}
	if !changePattern.MatchString(m.Change) {
		return fmt.Errorf("invalid change %q", m.Change)
	}
	objectLength := 0
	switch m.GitObjectFormat {
	case "sha1":
		objectLength = 40
	case "sha256":
		objectLength = 64
	default:
		return fmt.Errorf("unsupported git_object_format %q", m.GitObjectFormat)
	}
	if !isLowerHex(m.BaseCommit, objectLength) {
		return fmt.Errorf("base_commit must be %d lowercase hexadecimal characters for %s", objectLength, m.GitObjectFormat)
	}
	if !isLowerHex(m.TargetTree, objectLength) {
		return fmt.Errorf("target_tree must be %d lowercase hexadecimal characters for %s", objectLength, m.GitObjectFormat)
	}
	if !sha256Pattern.MatchString(m.PolicySHA256) {
		return errors.New("policy_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(m.ChangeContractSHA256) {
		return errors.New("change_contract_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(m.RegressionPatchSHA256) {
		return errors.New("regression_patch_sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(m.PayloadSHA256) {
		return errors.New("payload_sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
