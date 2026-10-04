package spec

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeWorkspaceValidationReportAcceptsValidV1(t *testing.T) {
	report, err := DecodeWorkspaceValidationReport(validWorkspaceValidationReportJSON())
	if err != nil {
		t.Fatalf("DecodeWorkspaceValidationReport() error = %v", err)
	}
	if report.SchemaVersion != 1 || report.Status != "PASS" || report.BaseCommit != strings.Repeat("a", 40) {
		t.Fatalf("decoded report = %+v", report)
	}
}

func TestDecodeWorkspaceValidationReportRejectsUnknownAndDuplicateFields(t *testing.T) {
	valid := validWorkspaceValidationReportJSON()
	unknown := append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"unexpected":true}`)...)
	if _, err := DecodeWorkspaceValidationReport(unknown); err == nil {
		t.Fatal("report with an unknown field was accepted")
	}
	duplicate := bytes.Replace(valid, []byte(`"status":"PASS"`), []byte(`"status":"PASS","status":"PASS"`), 1)
	if bytes.Equal(duplicate, valid) {
		t.Fatal("fixture status field not found")
	}
	if _, err := DecodeWorkspaceValidationReport(duplicate); err == nil {
		t.Fatal("report with a duplicate field was accepted")
	}
	caseVariant := bytes.Replace(valid, []byte(`"status":"PASS"`), []byte(`"status":"PASS","Status":"PASS"`), 1)
	if _, err := DecodeWorkspaceValidationReport(caseVariant); err == nil {
		t.Fatal("report with a case-variant extra field was accepted")
	}
}

func TestDecodeWorkspaceValidationReportRejectsFalseAuthorityAndInvalidIdentity(t *testing.T) {
	artifactClaim := bytes.Replace(validWorkspaceValidationReportJSON(), []byte(`"delivery_artifact_verified":false`), []byte(`"delivery_artifact_verified":true`), 1)
	if _, err := DecodeWorkspaceValidationReport(artifactClaim); err == nil {
		t.Fatal("workspace report claiming artifact verification was accepted")
	}
	invalidDigest := bytes.Replace(validWorkspaceValidationReportJSON(), []byte(strings.Repeat("c", 64)), []byte("not-a-digest"), 1)
	if _, err := DecodeWorkspaceValidationReport(invalidDigest); err == nil {
		t.Fatal("report with an invalid digest was accepted")
	}
}

func TestDecodeWorkspaceValidationReportRejectsMissingOrNullFields(t *testing.T) {
	valid := validWorkspaceValidationReportJSON()
	nullField := bytes.Replace(valid, []byte(`"delivery_artifact_built":false`), []byte(`"delivery_artifact_built":null`), 1)
	if _, err := DecodeWorkspaceValidationReport(nullField); err == nil {
		t.Fatal("report with a null required boolean was accepted")
	}
	missingField := bytes.Replace(valid, []byte(`"delivery_artifact_built":false,`), nil, 1)
	if _, err := DecodeWorkspaceValidationReport(missingField); err == nil {
		t.Fatal("report missing a required boolean was accepted")
	}
}

func TestDecodeWorkspaceValidationReportRejectsOversizedInput(t *testing.T) {
	if _, err := DecodeWorkspaceValidationReport(bytes.Repeat([]byte(" "), MaxWorkspaceValidationReportBytes+1)); err == nil {
		t.Fatal("oversized workspace validation report was accepted")
	}
}

func validWorkspaceValidationReportJSON() []byte {
	return []byte(`{"schema_version":1,"status":"PASS","validation_kind":"workspace_validation","workspace_validated":true,"delivery_artifact_built":false,"delivery_artifact_verified":false,"base_commit":"` + strings.Repeat("a", 40) + `","target_tree":"` + strings.Repeat("b", 40) + `","contract_sha256":"` + strings.Repeat("c", 64) + `","policy_sha256":"` + strings.Repeat("d", 64) + `","validation_level":"strict","enabled_gates":[],"disabled_gates":[],"producer_gate_statuses":{}}`)
}
