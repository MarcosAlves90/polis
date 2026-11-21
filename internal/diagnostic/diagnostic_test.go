package diagnostic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOperationalDiagnosticSerializesAdditiveMetadata(t *testing.T) {
	original := Report{
		Code: "POLIS_RED_PROBE_SCOPE_VIOLATION", Category: "scope",
		ObservedCause: "out_of_scope_red_probe_paths", Remediation: "restrict_red_probe_to_test_scope",
		AffectedOperations: []string{"capture-red"},
		Stage:              "Red probe scope validation", Condition: "test scope mismatch",
		NotRun: []string{"Red baseline execution"},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"code": original.Code, "category": original.Category,
		"observed_cause": original.ObservedCause, "remediation": original.Remediation,
		"stage": original.Stage, "condition": original.Condition,
	} {
		if got[key] != want {
			t.Fatalf("%s = %v; want %q", key, got[key], want)
		}
	}
	if !reflect.DeepEqual(got["affected_operations"], []any{"capture-red"}) {
		t.Fatalf("affected_operations=%v", got["affected_operations"])
	}
	if !reflect.DeepEqual(got["not_run"], []any{"Red baseline execution"}) {
		t.Fatalf("not_run=%v", got["not_run"])
	}
	// Classification must not alter existing human-oriented text diagnostics.
	if strings.Contains(original.FormatText(), original.Code) || strings.Contains(original.Inline(), original.Remediation) {
		t.Fatalf("new metadata leaked into legacy text: %q", original.FormatText())
	}
}

func TestOperationalDiagnosticGateClassification(t *testing.T) {
	cases := []struct {
		name, prerequisite, commandStatus, code, category, cause, remediation string
	}{
		{"missing executable", "missing executable go", "BLOCKED", "POLIS_GATE_PREREQUISITE_MISSING", "prerequisite", "missing_executable", "install_required_executable"},
		{"missing dependency", "missing dependency x", "BLOCKED", "POLIS_GATE_PREREQUISITE_MISSING", "prerequisite", "missing_dependency", "restore_required_dependency"},
		{"missing environment", "missing environment condition API_KEY", "BLOCKED", "POLIS_GATE_PREREQUISITE_MISSING", "prerequisite", "missing_environment_condition", "satisfy_required_environment_condition"},
		{"ordinary gate failure", "", "FAIL", "POLIS_GATE_VALIDATION_FAILED", "gate", "gate_non_pass", ""},
		{"unrecognized blocker", "unknown context", "BLOCKED", "POLIS_GATE_VALIDATION_FAILED", "gate", "gate_non_pass", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := Report{
				Stage: "project gate validation", Condition: "one or more configured project gates did not pass",
				GateStatuses: map[string]string{"test.complete": tc.commandStatus, "build": "PASS"},
				Commands:     []Command{{Gate: "test.complete", Status: tc.commandStatus, Prerequisite: tc.prerequisite}},
				NotRun:       []string{"artifact packaging"},
			}
			got := ClassifyGateFailure(original)
			if got.Code != tc.code || got.Category != tc.category || got.ObservedCause != tc.cause || got.Remediation != tc.remediation {
				t.Fatalf("classification: %+v", got)
			}
			if !reflect.DeepEqual(got.AffectedOperations, []string{"test.complete"}) || !reflect.DeepEqual(got.NotRun, original.NotRun) {
				t.Fatalf("affected/not_run: %+v", got)
			}
			if original.Code != "" || original.Category != "" {
				t.Fatal("input report mutated")
			}
		})
	}
}

func TestOperationalDiagnosticMixedFailuresDoNotMisclassify(t *testing.T) {
	report := Report{
		GateStatuses: map[string]string{"test.complete": "BLOCKED", "lint": "FAIL", "build": "PASS"},
		Commands:     []Command{{Gate: "test.complete", Status: "BLOCKED", Prerequisite: "missing executable go"}, {Gate: "lint", Status: "FAIL"}},
	}
	got := ClassifyGateFailure(report)
	if got.Code != "POLIS_GATE_VALIDATION_FAILED" || got.Remediation != "" || !reflect.DeepEqual(got.AffectedOperations, []string{"lint", "test.complete"}) {
		t.Fatalf("mixed gate result: %+v", got)
	}
}

func TestFormatTextIncludesCommandOutputMetadata(t *testing.T) {
	report := Report{Commands: []Command{{
		Gate: "test.complete", Status: "FAIL", ExitCode: 23,
		StdoutSHA256: "stdout-digest", StderrSHA256: "stderr-digest",
	}}}
	text := report.FormatText()
	for _, fragment := range []string{
		"Process output SHA-256: stdout=stdout-digest stderr=stderr-digest",
		"Process output truncated: stdout=false stderr=false",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("formatted diagnostic missing %q: %s", fragment, text)
		}
	}
}

func TestBoundProcessOutputSanitizesAndBoundsPreview(t *testing.T) {
	got, truncated := BoundProcessOutput("safe\x00text")
	if truncated || got != "safe�text" {
		t.Fatalf("sanitized output=%q truncated=%t", got, truncated)
	}
	got, truncated = BoundProcessOutput(strings.Repeat("x", MaxProcessOutputBytes+1))
	if !truncated || len(got) != MaxProcessOutputBytes {
		t.Fatalf("bounded output length=%d truncated=%t", len(got), truncated)
	}
}
