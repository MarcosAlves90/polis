package policyorigin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestManifestPreservesRecordedDecisionsAndRejectsStaleGate(t *testing.T) {
	reason := "project has no independent gate"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
	}
	policy := spec.Policy{SchemaVersion: spec.PolicySchemaVersion, ValidationLevel: spec.ValidationLevelMinimal, Gates: gates}
	decisions := make(map[string]Decision)
	for _, gate := range gates {
		decisions[gate.ID] = Decision{Source: SourceProfileDefault, Reason: "generated_by_profile"}
	}
	decisions["lint"] = Decision{Source: SourcePolicyExplicit, Reason: "init_explicit_disable_gate"}
	manifest, err := Create("go", policy, decisions)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Match(policy); got["test.complete"] != decisions["test.complete"] || got["lint"] != decisions["lint"] {
		t.Fatalf("recorded decision lost: %+v", got)
	}
	policy.Gates[2].Reason = pointer("edited after init")
	matches := decoded.Match(policy)
	if _, trusted := matches["lint"]; trusted {
		t.Fatal("changed gate retained init provenance")
	}
	if matches["test.complete"] != decisions["test.complete"] {
		t.Fatal("unchanged gate lost init provenance")
	}
}

func TestManifestRejectsMalformedOrUnverifiableEvidence(t *testing.T) {
	reason := "disabled"
	policy := spec.Policy{SchemaVersion: spec.PolicySchemaVersion, ValidationLevel: spec.ValidationLevelMinimal}
	decisions := make(map[string]Decision)
	for _, id := range spec.ProjectGateOrder {
		policy.Gates = append(policy.Gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		decisions[id] = Decision{Source: SourceProfileDefault, Reason: "generated_by_profile"}
	}
	manifest, err := Create("go", policy, decisions)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		string(valid) + "{}",
		strings.Replace(string(valid), "\"profile\": \"go\"", "\"profile\": \"invented\"", 1),
		strings.Replace(string(valid), "\"source\": \"profile_default\"", "\"source\": \"other\"", 1),
		strings.Replace(string(valid), "\"gate_sha256\": \"", "\"gate_sha256\": \"xyz", 1),
		strings.Replace(string(valid), "\"schema_version\": 1", "\"schema_version\": 2", 1),
		strings.Replace(string(valid), "\"reason\": \"generated_by_profile\"", "\"reason\": \"unverified\"", 1),
		strings.Replace(string(valid), "\"gates\": [", "\"gates\": [\"\" ,", 1),
	}
	for i, candidate := range invalid {
		if _, err := Decode([]byte(candidate)); err == nil {
			t.Fatalf("invalid provenance %d accepted", i)
		}
	}
	// A syntactically valid but unrelated digest never attributes a gate.
	manifest.Gates[0].GateSHA256 = strings.Repeat("0", 64)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.Match(policy)["test.complete"]; ok {
		t.Fatal("mismatched digest attributed a profile origin")
	}
}

func pointer(s string) *string { return &s }
