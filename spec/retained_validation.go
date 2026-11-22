package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	RetainedValidationRecordSchemaVersion = 1
	RetainedValidationKind                = "repository_validation"
	RetainedValidationStatusPASS          = "PASS"
	RetainedValidationOperationWorkspace  = "workspace_validate"
	RetainedValidationOperationBuild      = "build"
	MaxRetainedValidationRecordBytes      = 256 << 10
)

// RetainedValidationRecord is the bounded, versioned completion record that
// binds a repository-mode validation run to the exact artifacts it retained.
// It is local evidence, not a signed delivery artifact.
type RetainedValidationRecord struct {
	SchemaVersion            int               `json:"schema_version"`
	Kind                     string            `json:"kind"`
	Operation                string            `json:"operation"`
	Status                   string            `json:"status"`
	GitObjectFormat          string            `json:"git_object_format"`
	BaseCommit               string            `json:"base_commit"`
	BaseTree                 string            `json:"base_tree"`
	PolicySHA256             string            `json:"policy_sha256"`
	SpecificationSHA256      string            `json:"specification_sha256"`
	TargetTree               string            `json:"target_tree"`
	ContractSHA256           string            `json:"contract_sha256"`
	ImplementationPlanSHA256 string            `json:"implementation_plan_sha256"`
	RegressionPatchSHA256    string            `json:"regression_patch_sha256"`
	EvidenceSHA256           string            `json:"evidence_sha256"`
	ValidationLevel          string            `json:"validation_level"`
	EnabledGates             []string          `json:"enabled_gates"`
	DisabledGates            []string          `json:"disabled_gates"`
	DeferredGates            []string          `json:"deferred_gates"`
	ProducerGateStatuses     map[string]Status `json:"producer_gate_statuses"`
	RetainedContract         string            `json:"retained_contract"`
	RetainedPlan             string            `json:"retained_implementation_plan"`
	RetainedProof            string            `json:"retained_regression_patch"`
	RetainedEvidence         string            `json:"retained_evidence"`
}

// EncodeRetainedValidationRecord emits the canonical bounded record bytes used
// for publication and content addressing.
func EncodeRetainedValidationRecord(record RetainedValidationRecord) ([]byte, error) {
	if err := ValidateRetainedValidationRecord(record); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode retained validation record: %w", err)
	}
	return append(raw, '\n'), nil
}

// DecodeRetainedValidationRecord strictly decodes one bounded v1 record.
func DecodeRetainedValidationRecord(raw []byte) (RetainedValidationRecord, error) {
	var record RetainedValidationRecord
	if err := decodeRetainedValidationRecord(raw, &record); err != nil {
		return RetainedValidationRecord{}, err
	}
	if err := ValidateRetainedValidationRecord(record); err != nil {
		return RetainedValidationRecord{}, err
	}
	return record, nil
}

func decodeRetainedValidationRecord(raw []byte, record *RetainedValidationRecord) error {
	duplicateErr := rejectDuplicateJSONKeys(raw)
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{len(raw) > 0 && len(raw) <= MaxRetainedValidationRecordBytes, fmt.Errorf("retained validation record size must be between 1 and %d bytes", MaxRetainedValidationRecordBytes)},
		{utf8.Valid(raw), errors.New("retained validation record must be valid UTF-8")},
		{duplicateErr == nil, fmt.Errorf("decode retained validation record: %w", duplicateErr)},
	} {
		if !check.ok {
			return check.err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil || fields == nil {
		return errors.New("retained validation record must be a JSON object")
	}
	required := retainedValidationRecordFields()
	for field, value := range fields {
		if _, ok := required[field]; !ok {
			return fmt.Errorf("retained validation record contains unknown field %q", field)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("retained validation record requires non-null field %q", field)
		}
	}
	for field := range required {
		if _, ok := fields[field]; !ok {
			return fmt.Errorf("retained validation record requires non-null field %q", field)
		}
	}
	recordDec := json.NewDecoder(bytes.NewReader(raw))
	recordDec.DisallowUnknownFields()
	if err := recordDec.Decode(record); err != nil {
		return fmt.Errorf("decode retained validation record: %w", err)
	}
	return ensureDecoderEOF(recordDec, "retained validation record")
}

func retainedValidationRecordFields() map[string]struct{} {
	names := []string{
		"schema_version", "kind", "operation", "status", "git_object_format",
		"base_commit", "base_tree", "policy_sha256", "specification_sha256",
		"target_tree", "contract_sha256", "implementation_plan_sha256",
		"regression_patch_sha256", "evidence_sha256", "validation_level",
		"enabled_gates", "disabled_gates", "deferred_gates", "producer_gate_statuses",
		"retained_contract", "retained_implementation_plan",
		"retained_regression_patch", "retained_evidence",
	}
	fields := make(map[string]struct{}, len(names))
	for _, name := range names {
		fields[name] = struct{}{}
	}
	return fields
}

// ValidateRetainedValidationRecord enforces the bounded, identity-complete
// record shape without relying on a file's location or name.
func ValidateRetainedValidationRecord(record RetainedValidationRecord) error {
	levelErr := ValidateValidationLevel(record.ValidationLevel)
	inventoryErr := validateRetainedGateInventory(record)
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{record.SchemaVersion == RetainedValidationRecordSchemaVersion, fmt.Errorf("unsupported retained validation record schema_version %d", record.SchemaVersion)},
		{record.Kind == RetainedValidationKind, errors.New("retained validation record kind must be repository_validation")},
		{record.Operation == RetainedValidationOperationWorkspace || record.Operation == RetainedValidationOperationBuild, fmt.Errorf("unsupported retained validation record operation %q", record.Operation)},
		{record.Status == RetainedValidationStatusPASS, errors.New("retained validation record must record PASS")},
		{record.GitObjectFormat == "sha1" || record.GitObjectFormat == "sha256", errors.New("retained validation record contains an invalid Git object format")},
		{levelErr == nil, fmt.Errorf("retained validation record validation level: %w", levelErr)},
		{validWorkspaceObjectID(record.BaseCommit) && validWorkspaceObjectID(record.BaseTree) && validWorkspaceObjectID(record.TargetTree), errors.New("retained validation record contains an invalid object ID")},
		{validWorkspaceDigest(record.PolicySHA256) && validWorkspaceDigest(record.SpecificationSHA256) && validWorkspaceDigest(record.ContractSHA256) && validWorkspaceDigest(record.EvidenceSHA256), errors.New("retained validation record contains an invalid digest")},
		{inventoryErr == nil, fmt.Errorf("retained validation record gate inventory: %w", inventoryErr)},
		{validRetainedPath(record.RetainedContract, "contracts", ".json", record.ContractSHA256, true), errors.New("retained validation record has an invalid retained contract path")},
		{validRetainedPath(record.RetainedEvidence, "evidence", ".json", record.EvidenceSHA256, true), errors.New("retained validation record has an invalid retained evidence path")},
		{validRetainedPath(record.RetainedPlan, "plans", ".json", record.ImplementationPlanSHA256, false), errors.New("retained validation record has an invalid retained plan path")},
		{validRetainedPath(record.RetainedProof, "proofs", ".patch", record.RegressionPatchSHA256, false), errors.New("retained validation record has an invalid retained proof path")},
	} {
		if !check.ok {
			return check.err
		}
	}
	return nil
}

func validRetainedPath(path, class, extension, digest string, required bool) bool {
	if path == "" {
		return !required && digest == ""
	}
	return validWorkspaceDigest(digest) && path == ".polis/artifacts/"+class+"/sha256-"+digest+extension
}

func validateRetainedGateInventory(record RetainedValidationRecord) error {
	enabled, disabled, deferred, known := retainedGateSets(record)
	for _, check := range []struct {
		ok  bool
		err error
	}{
		{known, errors.New("retained validation record contains an unknown gate")},
		{reflect.DeepEqual(record.EnabledGates, retainedGateList(enabled, disabled, true)), errors.New("retained validation record enabled_gates must use project gate order and cover every gate")},
		{reflect.DeepEqual(record.DisabledGates, retainedGateList(enabled, disabled, false)), errors.New("retained validation record disabled_gates must use project gate order and cover every gate")},
		{retainedGatesSubset(deferred, enabled), errors.New("retained validation record defers a gate that is not enabled")},
		{reflect.DeepEqual(record.ProducerGateStatuses, retainedGateStatuses(enabled, deferred)), errors.New("retained validation record producer gate outcomes are inconsistent with the gate inventory")},
	} {
		if !check.ok {
			return check.err
		}
	}
	return nil
}

func retainedGateSets(record RetainedValidationRecord) (enabled, disabled, deferred map[string]struct{}, known bool) {
	enabled, disabled, deferred = make(map[string]struct{}), make(map[string]struct{}), make(map[string]struct{})
	for _, group := range []struct {
		gates []string
		set   map[string]struct{}
	}{{record.EnabledGates, enabled}, {record.DisabledGates, disabled}, {record.DeferredGates, deferred}} {
		for _, gate := range group.gates {
			if !IsProjectGate(gate) {
				return nil, nil, nil, false
			}
			group.set[gate] = struct{}{}
		}
	}
	return enabled, disabled, deferred, true
}

func retainedGateList(enabled, disabled map[string]struct{}, wantEnabled bool) []string {
	ids := make([]string, 0, len(ProjectGateOrder))
	for _, gate := range ProjectGateOrder {
		_, isEnabled := enabled[gate]
		_, isDisabled := disabled[gate]
		if isEnabled == wantEnabled && !isDisabled == wantEnabled {
			ids = append(ids, gate)
		}
	}
	return ids
}

func retainedGatesSubset(left, right map[string]struct{}) bool {
	for gate := range left {
		if _, ok := right[gate]; !ok {
			return false
		}
	}
	return true
}

func retainedGateStatuses(enabled, deferred map[string]struct{}) map[string]Status {
	statuses := make(map[string]Status, len(ProjectGateOrder))
	for _, gate := range ProjectGateOrder {
		status := StatusNotApplicable
		if _, ok := enabled[gate]; ok {
			status = StatusPass
		}
		if _, ok := deferred[gate]; ok {
			status = StatusDeferred
		}
		statuses[gate] = status
	}
	return statuses
}

// RetainedValidationOperations returns the supported operations in stable order.
func RetainedValidationOperations() []string {
	return []string{RetainedValidationOperationWorkspace, RetainedValidationOperationBuild}
}

// CanonicalRetainedRecordJSON returns deterministic JSON for a validated record.
func CanonicalRetainedRecordJSON(record RetainedValidationRecord) (string, error) {
	raw, err := EncodeRetainedValidationRecord(record)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(raw), "\n"), nil
}

// SortedRetainedGateIDs returns producer gate outcomes in deterministic order.
func SortedRetainedGateIDs(statuses map[string]Status) []string {
	ids := make([]string, 0, len(statuses))
	for id := range statuses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
