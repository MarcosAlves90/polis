package spec

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxWorkspaceValidationReportBytes = 1 << 20

// WorkspaceValidationReport is a local, unsigned checkpoint, not a delivery artifact.
type WorkspaceValidationReport struct {
	SchemaVersion            int               `json:"schema_version"`
	Status                   string            `json:"status"`
	ValidationKind           string            `json:"validation_kind"`
	WorkspaceValidated       bool              `json:"workspace_validated"`
	DeliveryArtifactBuilt    bool              `json:"delivery_artifact_built"`
	DeliveryArtifactVerified bool              `json:"delivery_artifact_verified"`
	BaseCommit               string            `json:"base_commit"`
	TargetTree               string            `json:"target_tree"`
	ContractSHA256           string            `json:"contract_sha256"`
	PolicySHA256             string            `json:"policy_sha256"`
	ValidationLevel          string            `json:"validation_level"`
	EnabledGates             []string          `json:"enabled_gates"`
	DisabledGates            []string          `json:"disabled_gates"`
	ProducerGateStatuses     map[string]Status `json:"producer_gate_statuses"`
}

// DecodeWorkspaceValidationReport strictly decodes the bounded v1 checkpoint.
func DecodeWorkspaceValidationReport(raw []byte) (WorkspaceValidationReport, error) {
	if len(raw) == 0 || len(raw) > MaxWorkspaceValidationReportBytes {
		return WorkspaceValidationReport{}, fmt.Errorf("workspace validation report size must be between 1 and %d bytes", MaxWorkspaceValidationReportBytes)
	}
	if !utf8.Valid(raw) {
		return WorkspaceValidationReport{}, errors.New("workspace validation report must be valid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return WorkspaceValidationReport{}, fmt.Errorf("decode workspace validation report: %w", err)
	}
	if err := requireWorkspaceValidationReportFields(raw); err != nil {
		return WorkspaceValidationReport{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var report WorkspaceValidationReport
	if err := dec.Decode(&report); err != nil {
		return WorkspaceValidationReport{}, fmt.Errorf("decode workspace validation report: %w", err)
	}
	if err := ensureDecoderEOF(dec, "workspace validation report"); err != nil {
		return WorkspaceValidationReport{}, err
	}
	if report.SchemaVersion != 1 || report.Status != "PASS" || report.ValidationKind != "workspace_validation" || !report.WorkspaceValidated {
		return WorkspaceValidationReport{}, errors.New("workspace validation report does not record a version-1 PASS")
	}
	if report.DeliveryArtifactBuilt || report.DeliveryArtifactVerified {
		return WorkspaceValidationReport{}, errors.New("workspace validation report must not claim a delivery artifact")
	}
	if !validWorkspaceObjectID(report.BaseCommit) || !validWorkspaceObjectID(report.TargetTree) || !validWorkspaceDigest(report.ContractSHA256) || !validWorkspaceDigest(report.PolicySHA256) {
		return WorkspaceValidationReport{}, errors.New("workspace validation report contains an invalid identity digest")
	}
	if report.ValidationLevel != "strict" && report.ValidationLevel != "standard" && report.ValidationLevel != "minimal" {
		return WorkspaceValidationReport{}, errors.New("workspace validation report contains an invalid validation level")
	}
	if report.EnabledGates == nil || report.DisabledGates == nil || report.ProducerGateStatuses == nil {
		return WorkspaceValidationReport{}, errors.New("workspace validation report must include gate arrays and outcomes")
	}
	for _, gate := range append(append([]string(nil), report.EnabledGates...), report.DisabledGates...) {
		if strings.TrimSpace(gate) == "" {
			return WorkspaceValidationReport{}, errors.New("workspace validation report contains an empty gate identifier")
		}
	}
	for gate, status := range report.ProducerGateStatuses {
		if strings.TrimSpace(gate) == "" || strings.TrimSpace(string(status)) == "" {
			return WorkspaceValidationReport{}, errors.New("workspace validation report contains an empty gate outcome")
		}
	}
	return report, nil
}

func requireWorkspaceValidationReportFields(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("workspace validation report must be a JSON object")
	}
	required := []string{
		"schema_version", "status", "validation_kind", "workspace_validated",
		"delivery_artifact_built", "delivery_artifact_verified", "base_commit", "target_tree",
		"contract_sha256", "policy_sha256", "validation_level", "enabled_gates",
		"disabled_gates", "producer_gate_statuses",
	}
	allowed := make(map[string]struct{}, len(required))
	for _, field := range required {
		allowed[field] = struct{}{}
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("workspace validation report contains unknown field %q", field)
		}
	}
	for _, field := range required {
		value, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("workspace validation report requires non-null field %q", field)
		}
	}
	return nil
}

func validWorkspaceObjectID(value string) bool {
	return (len(value) == 40 || len(value) == 64) && validWorkspaceHex(value)
}

func validWorkspaceDigest(value string) bool {
	return len(value) == 64 && validWorkspaceHex(value)
}

func validWorkspaceHex(value string) bool {
	if value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
