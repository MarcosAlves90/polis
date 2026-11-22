package spec

import (
	"strings"
	"testing"
)

func validRetainedValidationRecordForTest() RetainedValidationRecord {
	statuses := make(map[string]Status, len(ProjectGateOrder))
	for _, gate := range ProjectGateOrder {
		statuses[gate] = StatusNotApplicable
	}
	statuses["test.complete"] = StatusPass
	contractDigest := strings.Repeat("1", 64)
	evidenceDigest := strings.Repeat("2", 64)
	return RetainedValidationRecord{
		SchemaVersion:        RetainedValidationRecordSchemaVersion,
		Kind:                 RetainedValidationKind,
		Operation:            RetainedValidationOperationWorkspace,
		Status:               RetainedValidationStatusPASS,
		GitObjectFormat:      "sha1",
		BaseCommit:           strings.Repeat("a", 40),
		BaseTree:             strings.Repeat("b", 40),
		PolicySHA256:         strings.Repeat("c", 64),
		SpecificationSHA256:  strings.Repeat("d", 64),
		TargetTree:           strings.Repeat("e", 40),
		ContractSHA256:       contractDigest,
		EvidenceSHA256:       evidenceDigest,
		ValidationLevel:      ValidationLevelMinimal,
		EnabledGates:         []string{"test.complete"},
		DisabledGates:        append([]string(nil), ProjectGateOrder[1:]...),
		DeferredGates:        []string{},
		ProducerGateStatuses: statuses,
		RetainedContract:     ".polis/artifacts/contracts/sha256-" + contractDigest + ".json",
		RetainedEvidence:     ".polis/artifacts/evidence/sha256-" + evidenceDigest + ".json",
	}
}

func TestRetainedValidationRecordStrictRoundTrip(t *testing.T) {
	want := validRetainedValidationRecordForTest()
	raw, err := EncodeRetainedValidationRecord(want)
	if err != nil {
		t.Fatalf("encode record: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("canonical record must end with one newline")
	}
	got, err := DecodeRetainedValidationRecord(raw)
	if err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if got.SchemaVersion != want.SchemaVersion || got.Kind != want.Kind || got.TargetTree != want.TargetTree ||
		got.Operation != want.Operation || got.Status != want.Status {
		t.Fatalf("record identity = %+v, want %+v", got, want)
	}
	if !equalRetainedStringSlices(got.EnabledGates, want.EnabledGates) || !equalRetainedStringSlices(got.DisabledGates, want.DisabledGates) || !equalRetainedStringSlices(got.DeferredGates, want.DeferredGates) {
		t.Fatalf("gate inventories = %v/%v/%v", got.EnabledGates, got.DisabledGates, got.DeferredGates)
	}
	for _, gate := range ProjectGateOrder {
		if got.ProducerGateStatuses[gate] != want.ProducerGateStatuses[gate] {
			t.Fatalf("gate %s = %s, want %s", gate, got.ProducerGateStatuses[gate], want.ProducerGateStatuses[gate])
		}
	}
}

func TestRetainedValidationRecordRejectsTampering(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RetainedValidationRecord)
	}{
		{"wrong schema", func(r *RetainedValidationRecord) { r.SchemaVersion++ }},
		{"wrong kind", func(r *RetainedValidationRecord) { r.Kind = "workspace_validation" }},
		{"invalid target", func(r *RetainedValidationRecord) { r.TargetTree = "not-an-object-id" }},
		{"missing gate outcome", func(r *RetainedValidationRecord) { delete(r.ProducerGateStatuses, "build") }},
		{"wrong enabled outcome", func(r *RetainedValidationRecord) { r.ProducerGateStatuses["test.complete"] = StatusNotApplicable }},
		{"wrong disabled outcome", func(r *RetainedValidationRecord) { r.ProducerGateStatuses["build"] = StatusPass }},
		{"deferred disabled gate", func(r *RetainedValidationRecord) { r.DeferredGates = []string{"build"} }},
		{"missing deferred status", func(r *RetainedValidationRecord) { r.ProducerGateStatuses["test.complete"] = StatusDeferred }},
		{"unaddressed contract path", func(r *RetainedValidationRecord) { r.RetainedContract = ".polis/artifacts/contracts/latest.json" }},
		{"optional path without digest", func(r *RetainedValidationRecord) {
			r.RetainedPlan = ".polis/artifacts/plans/sha256-" + strings.Repeat("7", 64) + ".json"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := validRetainedValidationRecordForTest()
			tc.mutate(&record)
			if err := ValidateRetainedValidationRecord(record); err == nil {
				t.Fatal("tampered record was accepted")
			}
		})
	}
}

func TestDecodeRetainedValidationRecordRejectsUnsafeJSON(t *testing.T) {
	valid, err := EncodeRetainedValidationRecord(validRetainedValidationRecordForTest())
	if err != nil {
		t.Fatal(err)
	}
	object := strings.TrimSuffix(string(valid), "\n")
	samples := []struct {
		name string
		raw  string
	}{
		{"duplicate key", strings.Replace(object, `"kind":"repository_validation"`, `"kind":"repository_validation","kind":"repository_validation"`, 1)},
		{"unknown field", strings.Replace(object, `{"schema_version":`, `{"extra":1,"schema_version":`, 1)},
		{"trailing value", object + "\n{}"},
		{"null field", strings.Replace(object, `"kind":"repository_validation"`, `"kind":null`, 1)},
		{"empty", ""},
		{"invalid utf-8", string([]byte{0xff, 0xfe})},
		{"not an object", "[]"},
		{"missing field", strings.Replace(object, `"kind":"repository_validation",`, "", 1)},
	}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			if _, err := DecodeRetainedValidationRecord([]byte(sample.raw)); err == nil {
				t.Fatal("unsafe record JSON was accepted")
			}
		})
	}
}

func TestRetainedValidationRecordRejectsMoreTampering(t *testing.T) {
	build := validRetainedValidationRecordForTest()
	planDigest := strings.Repeat("7", 64)
	plan := build
	plan.ImplementationPlanSHA256 = planDigest
	plan.RetainedPlan = ".polis/artifacts/plans/sha256-" + planDigest + ".json"
	proofDigest := strings.Repeat("8", 64)
	proof := build
	proof.RegressionPatchSHA256 = proofDigest
	proof.RetainedProof = ".polis/artifacts/proofs/sha256-" + proofDigest + ".patch"
	cases := []struct {
		name   string
		record RetainedValidationRecord
	}{
		{"wrong status", func() RetainedValidationRecord { r := build; r.Status = "FAIL"; return r }()},
		{"invalid object format", func() RetainedValidationRecord { r := build; r.GitObjectFormat = "sha512"; return r }()},
		{"invalid base tree", func() RetainedValidationRecord { r := build; r.BaseTree = "x"; return r }()},
		{"invalid policy digest", func() RetainedValidationRecord { r := build; r.PolicySHA256 = "x"; return r }()},
		{"invalid level", func() RetainedValidationRecord { r := build; r.ValidationLevel = "unknown"; return r }()},
		{"nil gate arrays", func() RetainedValidationRecord { r := build; r.EnabledGates = nil; return r }()},
		{"unknown gate", func() RetainedValidationRecord { r := build; r.EnabledGates = []string{"unknown"}; return r }()},
		{"repeated gate", func() RetainedValidationRecord {
			r := build
			r.DisabledGates = append([]string(nil), r.DisabledGates...)
			r.DisabledGates[0] = "test.complete"
			return r
		}()},
		{"unordered gates", func() RetainedValidationRecord {
			r := build
			r.EnabledGates = []string{"build"}
			r.DisabledGates = []string{"test.complete"}
			return r
		}()},
		{"unknown deferred gate", func() RetainedValidationRecord { r := build; r.DeferredGates = []string{"build"}; return r }()},
		{"missing contract path", func() RetainedValidationRecord { r := build; r.RetainedContract = ""; return r }()},
		{"invalid evidence digest", func() RetainedValidationRecord { r := build; r.EvidenceSHA256 = "x"; return r }()},
		{"wrong evidence path", func() RetainedValidationRecord {
			r := build
			r.RetainedEvidence = ".polis/artifacts/evidence/latest.json"
			return r
		}()},
		{"plan digest without path", func() RetainedValidationRecord { r := build; r.ImplementationPlanSHA256 = planDigest; return r }()},
		{"wrong plan path", func() RetainedValidationRecord {
			r := plan
			r.RetainedPlan = ".polis/artifacts/plans/latest.json"
			return r
		}()},
		{"proof digest without path", func() RetainedValidationRecord { r := build; r.RegressionPatchSHA256 = proofDigest; return r }()},
		{"wrong proof path", func() RetainedValidationRecord {
			r := proof
			r.RetainedProof = ".polis/artifacts/proofs/latest.patch"
			return r
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRetainedValidationRecord(tc.record); err == nil {
				t.Fatal("tampered record was accepted")
			}
			if _, err := EncodeRetainedValidationRecord(tc.record); err == nil {
				t.Fatal("EncodeRetainedValidationRecord accepted a tampered record")
			}
		})
	}
}

func TestRetainedValidationRecordHelpers(t *testing.T) {
	if got := RetainedValidationOperations(); len(got) != 2 || got[0] != RetainedValidationOperationWorkspace {
		t.Fatalf("operations = %v", got)
	}
	record := validRetainedValidationRecordForTest()
	record.ImplementationPlanSHA256 = ""
	record.RetainedPlan = ""
	record.RegressionPatchSHA256 = ""
	record.RetainedProof = ""
	raw, err := EncodeRetainedValidationRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRetainedValidationRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.RetainedPlan != "" || got.RetainedProof != "" {
		t.Fatalf("optional retained paths = %q %q", got.RetainedPlan, got.RetainedProof)
	}
	canon, err := CanonicalRetainedRecordJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(canon, "{") || strings.Contains(canon, "\n") {
		t.Fatalf("canonical json = %q", canon)
	}
	if ids := SortedRetainedGateIDs(map[string]Status{"b": StatusPass, "a": StatusFail}); len(ids) != 2 || ids[0] != "a" {
		t.Fatalf("sorted ids = %v", ids)
	}
}
func equalRetainedStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
