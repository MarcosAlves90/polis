package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeConcurrencyFindingsDeterministic(t *testing.T) {
	findings := []ConcurrencyFinding{
		{Reason: ConcurrencyMissingOptIn, Gates: []string{"lint", "dependency"}, Detail: "review"},
		{Reason: ConcurrencyJobsOne, Detail: "serial"},
		{Reason: ConcurrencyMissingOptIn, Gates: []string{"dependency", "lint"}, Detail: "review"},
	}
	got := NormalizeConcurrencyFindings(findings)
	want := []ConcurrencyFinding{
		{Reason: ConcurrencyJobsOne, Detail: "serial"},
		{Reason: ConcurrencyMissingOptIn, Gates: []string{"dependency", "lint"}, Detail: "review"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized=%+v want=%+v", got, want)
	}
}

func TestConcurrencyCoverageCannotDeclareParallelSafe(t *testing.T) {
	raw := strings.Replace(canonicalPolicyJSON(), `"id":"coverage","mode":"coverage"`, `"id":"coverage","mode":"coverage","parallel_safe":true`, 1)
	if raw == canonicalPolicyJSON() {
		t.Fatal("coverage fixture was not modified")
	}
	if _, err := DecodePolicy([]byte(raw)); err == nil || !strings.Contains(err.Error(), "coverage cannot declare parallel safety") {
		t.Fatalf("coverage parallel_safe error=%v", err)
	}
}

func TestConcurrencyCoverageSchemaAllowsExplicitFalse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("schemas", "policy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	coverage, ok := schema.Defs["coverageGate"]
	if !ok {
		t.Fatal("coverageGate definition missing")
	}
	decl, ok := coverage.Properties["parallel_safe"]
	if !ok || strings.TrimSpace(string(decl)) != `{"const":false}` {
		t.Fatalf("coverage parallel_safe schema=%s, want explicit false only", decl)
	}
	policyRaw := strings.Replace(canonicalPolicyJSON(), `"id":"coverage","mode":"coverage"`, `"id":"coverage","mode":"coverage","parallel_safe":false`, 1)
	if policyRaw == canonicalPolicyJSON() {
		t.Fatal("coverage fixture was not modified")
	}
	if _, err := DecodePolicy([]byte(policyRaw)); err != nil {
		t.Fatalf("explicit false on coverage must remain compatible: %v", err)
	}
}

func TestConcurrencyReportJSONUsesObservedVocabulary(t *testing.T) {
	report := ConcurrencyReport{
		Jobs:        2,
		Observation: ConcurrencyObservation{OverlapObserved: true, MaxConcurrent: 2, LaunchedGates: []string{"lint", "dependency"}},
		Diagnosis:   ConcurrencyDiagnosis{Jobs: 2, CanOverlap: true, EligibleGates: []string{"lint", "dependency"}},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`"overlap_observed":true`, `"max_concurrent":2`, `"can_overlap":true`, `"launched_gates":["lint","dependency"]`} {
		if !strings.Contains(string(raw), token) {
			t.Fatalf("missing %s in %s", token, raw)
		}
	}
	for _, forbidden := range []string{`"overlapped"`, `"peak_concurrent"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("stale field %s in %s", forbidden, raw)
		}
	}
}

func TestConcurrencyDiagnosisAddReuseBlocker(t *testing.T) {
	diagnosis := ConcurrencyDiagnosis{Jobs: 4, CanOverlap: true, EligibleGates: []string{"lint", "dependency"}}
	diagnosis.AddReuseBlocker([]string{"lint", "dependency"})
	if !reflect.DeepEqual(diagnosis.Blockers, []ConcurrencyFinding{{Reason: ConcurrencyAllReused, Gates: []string{"dependency", "lint"},
		Detail: "eligible gates were reused from matching evidence and did not launch"}}) {
		t.Fatalf("blockers=%+v", diagnosis.Blockers)
	}
}
