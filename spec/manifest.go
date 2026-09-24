package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const (
	FormatVersion                   = 5
	PreviousFormatVersion           = 4
	IntermediateFormatVersion       = 3
	LegacyFormatVersion             = 2
	ImplementationPlanFormatVersion = 6
)

var (
	projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	changePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Manifest struct {
	FormatVersion            int    `json:"format_version"`
	Project                  string `json:"project"`
	Change                   string `json:"change"`
	GitObjectFormat          string `json:"git_object_format"`
	BaseCommit               string `json:"base_commit"`
	TargetTree               string `json:"target_tree"`
	PolicySHA256             string `json:"policy_sha256"`
	ChangeContractSHA256     string `json:"change_contract_sha256"`
	RegressionPatchSHA256    string `json:"regression_patch_sha256"`
	PayloadSHA256            string `json:"payload_sha256"`
	BaselineSHA256           string `json:"baseline_sha256,omitempty"`
	ImplementationPlanSHA256 string `json:"implementation_plan_sha256,omitempty"`
}

func DecodeManifest(raw []byte) (Manifest, error) {
	if err := validateManifestJSONProperties(raw); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureDecoderEOF(dec, "manifest"); err != nil {
		return Manifest{}, err
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func validateManifestJSONProperties(raw []byte) error {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(raw, &properties); err != nil || properties == nil {
		return nil // DecodeManifest reports malformed and non-object JSON.
	}
	allowed := map[string]struct{}{
		"format_version": {}, "project": {}, "change": {}, "git_object_format": {},
		"base_commit": {}, "target_tree": {}, "policy_sha256": {}, "change_contract_sha256": {},
		"regression_patch_sha256": {}, "payload_sha256": {}, "baseline_sha256": {},
		"implementation_plan_sha256": {},
	}
	for name := range properties {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unknown manifest property %q", name)
		}
	}
	var formatVersion int
	if err := json.Unmarshal(properties["format_version"], &formatVersion); err != nil {
		return nil // DecodeManifest reports an invalid format_version type.
	}
	if _, present := properties["implementation_plan_sha256"]; present && formatVersion != ImplementationPlanFormatVersion {
		return errors.New("implementation_plan_sha256 is only valid for format v6")
	}
	if _, present := properties["baseline_sha256"]; present && !FormatHasEmbeddedBaseline(formatVersion) {
		return fmt.Errorf("baseline_sha256 is not valid before format v4")
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.FormatVersion != FormatVersion && m.FormatVersion != PreviousFormatVersion && m.FormatVersion != IntermediateFormatVersion && m.FormatVersion != LegacyFormatVersion && m.FormatVersion != ImplementationPlanFormatVersion {
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
	if FormatHasEmbeddedBaseline(m.FormatVersion) {
		if !sha256Pattern.MatchString(m.BaselineSHA256) {
			return fmt.Errorf("baseline_sha256 must be 64 lowercase hexadecimal characters for format v%d", m.FormatVersion)
		}
	} else if m.BaselineSHA256 != "" {
		return errors.New("baseline_sha256 is not valid before format v4")
	}
	if m.FormatVersion == ImplementationPlanFormatVersion {
		if !sha256Pattern.MatchString(m.ImplementationPlanSHA256) {
			return errors.New("implementation_plan_sha256 must be 64 lowercase hexadecimal characters for format v6")
		}
	} else if m.ImplementationPlanSHA256 != "" {
		return errors.New("implementation_plan_sha256 is only valid for format v6")
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
