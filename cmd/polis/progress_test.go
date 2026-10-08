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

func TestProgressLifecycleCommandHasPairedEvents(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("doctor exit %d: %s", code, errOut.String())
	}
	records := decodeJSONRecords(t, errOut.String())
	var lifecycle []map[string]any
	for _, r := range records {
		if r["scope"] == "command" {
			lifecycle = append(lifecycle, r)
		}
	}
	if len(lifecycle) != 2 || lifecycle[0]["event"] != "started" {
		t.Fatalf("started lifecycle record missing: %#v", records)
	}
	if lifecycle[0]["type"] != "progress" || lifecycle[0]["scope"] != "command" || lifecycle[0]["id"] != "doctor" || lifecycle[0]["action"] == "" || lifecycle[0]["why"] == "" {
		t.Fatalf("invalid command start: %#v", lifecycle[0])
	}
	completed := lifecycle[1]
	if completed["event"] != "completed" || completed["id"] != "doctor" || completed["scope"] != "command" || completed["status"] != "PASS" || completed["action"] != lifecycle[0]["action"] || completed["why"] != lifecycle[0]["why"] {
		t.Fatalf("completion missing or unmatched: %#v", completed)
	}
	if ms, ok := completed["duration_ms"].(float64); !ok || ms < 0 {
		t.Fatalf("invalid observed duration: %#v", completed)
	}
	var final map[string]any
	if err := json.Unmarshal(out.Bytes(), &final); err != nil || final["status"] != "PASS" {
		t.Fatalf("legacy JSON result changed: %#v %v", final, err)
	}
}

func TestProgressLifecycleFailureKeepsFinalJSONRecord(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"verify", "--format", "json", "/no-such-artifact.polis"}, &out, &errOut); code != exitInvalidArtifact {
		t.Fatalf("verify exit %d: %s", code, errOut.String())
	}
	records := decodeJSONRecords(t, errOut.String())
	starts, completes := 0, 0
	for i, record := range records {
		if record["scope"] == "command" && record["id"] == "verify" && record["event"] == "started" {
			starts++
		}
		if record["scope"] == "command" && record["id"] == "verify" && record["event"] == "completed" {
			completes++
			if record["status"] != "FAIL" {
				t.Fatalf("failure status: %#v", record)
			}
			if i >= len(records)-1 {
				t.Fatalf("completion must precede error JSON: %#v", records)
			}
		}
	}
	if starts != 1 || completes != 1 {
		t.Fatalf("start/completion not paired: %#v", records)
	}
	last := records[len(records)-1]
	if last["status"] != "FAIL" || last["exit_code"] != float64(exitInvalidArtifact) {
		t.Fatalf("legacy failure must be last: %#v", last)
	}
}

func TestProgressLifecycleGateStartIncludesStableIdentity(t *testing.T) {
	var errOut bytes.Buffer
	gateStartProgress(&errOut, "json")(spec.GatePolicy{ID: "test.complete"})
	records := decodeJSONRecords(t, errOut.String())
	if len(records) != 1 || records[0]["event"] != "started" || records[0]["scope"] != "gate" || records[0]["id"] != "test.complete" {
		t.Fatalf("started lifecycle record missing for gate: %#v", records)
	}
}

func TestProgressLifecycleCommandBlockedAndHelp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--format=json"}, &out, &errOut); code != exitBlocked {
		t.Fatalf("expected blocked doctor, got %d", code)
	}
	records := decodeJSONRecords(t, errOut.String())
	if len(records) < 3 || records[0]["event"] != "started" || records[len(records)-2]["event"] != "completed" || records[len(records)-2]["status"] != "BLOCKED" || records[len(records)-1]["status"] != "BLOCKED" {
		t.Fatalf("blocked lifecycle / final failure ordering: %#v", records)
	}
	errOut.Reset()
	out.Reset()
	if code := run([]string{"help"}, &out, &errOut); code != exitPass || errOut.Len() != 0 {
		t.Fatalf("help emitted progress: exit=%d stderr=%s", code, errOut.String())
	}
}
