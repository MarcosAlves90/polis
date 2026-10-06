package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func assertTextProgressPresent(t *testing.T, raw string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.HasPrefix(line, "POLIS PROGRESS: ") && strings.Contains(line, "action=") && strings.Contains(line, "why=") {
			return
		}
	}
	t.Fatalf("missing progress with action and why: %q", raw)
}

func assertOnlyTextProgress(t *testing.T, raw string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		t.Fatal("expected progress output")
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "POLIS PROGRESS: ") || !strings.Contains(line, "action=") || !strings.Contains(line, "why=") {
			t.Fatalf("non-progress stderr line %q in %q", line, raw)
		}
	}
}

func decodeJSONRecords(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid JSON stream record: %v line=%q raw=%q", err, line, raw)
		}
		records = append(records, record)
	}
	return records
}

func finalJSONRecordAfterProgress(t *testing.T, raw string) map[string]any {
	t.Helper()
	records := decodeJSONRecords(t, raw)
	if len(records) < 2 {
		t.Fatalf("expected progress record(s) followed by a final JSON result: %q", raw)
	}
	for _, record := range records[:len(records)-1] {
		if record["type"] != "progress" || record["action"] == "" || record["why"] == "" {
			t.Fatalf("invalid progress record: %v", record)
		}
	}
	return records[len(records)-1]
}

func TestCommandStartProgressAlwaysExplainsActionAndWhy(t *testing.T) {
	for name := range commandProgressByName {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			writeCommandStartProgress(&output, []string{name})
			assertOnlyTextProgress(t, output.String())
		})
	}

	for _, args := range [][]string{{"help"}, {"-h"}, {"doctor", "--help"}, {"workspace", "validate", "--help"}} {
		var output bytes.Buffer
		writeCommandStartProgress(&output, args)
		if output.Len() != 0 {
			t.Fatalf("help should remain documentation-only: args=%v output=%q", args, output.String())
		}
	}
}

func TestJSONProgressUsesStructuredRecords(t *testing.T) {
	var output bytes.Buffer
	writeCommandStartProgress(&output, []string{"status", "--format", "json"})
	records := decodeJSONRecords(t, output.String())
	if len(records) != 1 || records[0]["type"] != "progress" || records[0]["action"] == "" || records[0]["why"] == "" {
		t.Fatalf("unexpected progress records: %v", records)
	}
}

func TestGateProgressNamesTheGateAndReason(t *testing.T) {
	var output bytes.Buffer
	gateStartProgress(&output, "text")(spec.GatePolicy{ID: "test.complete"})
	assertOnlyTextProgress(t, output.String())
	if !strings.Contains(output.String(), "test.complete") {
		t.Fatalf("gate progress does not name gate: %q", output.String())
	}
}
