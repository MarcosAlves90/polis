package spec

import (
	"encoding/json"
	"testing"
)

func TestDecodeEvidenceAcceptsCanonicalEvents(t *testing.T) {
	raw := []byte(
		`{"event":"gate_started","gate":"test.complete"}` + "\n" +
			`{"event":"command_finished","gate":"test.complete","status":"PASS","argv":["go","test","./..."],"cwd":".","exit_code":0,"duration_ms":12,"stdout":"ok","stderr":""}` + "\n" +
			`{"event":"gate_finished","gate":"test.complete","status":"PASS"}` + "\n" +
			`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n" +
			`{"event":"gate_finished","gate":"lint","status":"NOT_APPLICABLE","reason":"project has no lint gate"}` + "\n")
	events, err := DecodeEvidence(raw)
	if err != nil {
		t.Fatalf("DecodeEvidence() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events=%d", len(events))
	}
}

func TestDecodeEvidenceAcceptsValidationConfiguration(t *testing.T) {
	raw := []byte(`{"event":"validation_configured","gate":"policy","validation_level":"standard","enabled_gates":["test.complete","lint"],"disabled_gates":["coverage","typecheck","build","smoke","compatibility","dependency","migration","security","platform"]}` + "\n")
	events, err := DecodeEvidence(raw)
	if err != nil {
		t.Fatalf("validation configuration evidence rejected: %v", err)
	}
	if len(events) != 1 || events[0].ValidationLevel != ValidationLevelStandard || len(events[0].EnabledGates) != 2 || len(events[0].DisabledGates) != 9 {
		t.Fatalf("events=%+v", events)
	}
}

func TestMarshalValidationConfigurationPreservesEmptyInventory(t *testing.T) {
	raw, err := json.Marshal(EvidenceEvent{
		Event: "validation_configured", Gate: "policy", ValidationLevel: ValidationLevelMinimal,
		EnabledGates: []string{}, DisabledGates: append([]string{}, ProjectGateOrder...),
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := DecodeEvidence(append(raw, '\n'))
	if err != nil {
		t.Fatalf("marshaled validation configuration rejected: %v; raw=%s", err, raw)
	}
	if events[0].EnabledGates == nil || len(events[0].EnabledGates) != 0 {
		t.Fatalf("enabled inventory=%v", events[0].EnabledGates)
	}
}

func TestDecodeEvidenceRejectsInvalidEvents(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"event":"unknown","gate":"lint"}` + "\n"),
		[]byte(`{"event":"gate_started","gate":"made-up"}` + "\n"),
		[]byte(`{"event":"validation_configured","gate":"policy","validation_level":"standard","enabled_gates":["test.complete"],"disabled_gates":["coverage"]}` + "\n"),
		[]byte(`{"event":"validation_configured","gate":"policy","validation_level":"standard","enabled_gates":["lint","test.complete"],"disabled_gates":["coverage","typecheck","build","smoke","compatibility","dependency","migration","security","platform"]}` + "\n"),
		[]byte(`{"event":"validation_configured","gate":"policy","validation_level":"standard","enabled_gates":["test.complete","test.complete"],"disabled_gates":["coverage","lint","typecheck","build","smoke","compatibility","dependency","migration","security","platform"]}` + "\n"),
		[]byte(`{"event":"validation_configured","gate":"integrity","validation_level":"standard","enabled_gates":["test.complete"],"disabled_gates":["coverage","lint","typecheck","build","smoke","compatibility","dependency","migration","security","platform"]}` + "\n"),
		[]byte(`{"event":"gate_started","gate":"lint","status":"PASS"}` + "\n"),
		[]byte(`{"event":"gate_finished","gate":"lint","status":"NOT_APPLICABLE"}` + "\n"),
		[]byte(`{"event":"gate_finished","gate":"lint","status":"PASS","reason":"extra"}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"NOT_APPLICABLE","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":[],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":80,"total_lines":100,"value_percent":80,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"lint","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`not-json` + "\n"),
	}
	for _, raw := range cases {
		if _, err := DecodeEvidence(raw); err == nil {
			t.Fatalf("expected evidence rejection for %q", raw)
		}
	}
}

func TestDecodeEvidenceRejectsMalformedFieldVariants(t *testing.T) {
	cases := [][]byte{
		[]byte("\n"),
		[]byte(`{"gate":"lint"}` + "\n"),
		[]byte(`{"event":"gate_started"}` + "\n"),
		[]byte(`{"event":"gate_started","gate":1}` + "\n"),
		[]byte(`{"event":"gate_started","gate":"lint"} {}` + "\n"),
		[]byte(`{"event":"gate_finished","gate":"lint","status":"MAYBE"}` + "\n"),
		[]byte(`{"event":"gate_finished","gate":"lint","status":1}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go",1],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go",""],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":"../x","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":-2,"duration_ms":1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":-1,"stdout":"","stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout":1,"stderr":""}` + "\n"),
		[]byte(`{"event":"command_finished","gate":"lint","status":"PASS","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":1}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"BLOCKED","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"bad","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":"../coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"wrong","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">=","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":-1,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":101,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":101,"operator":">","threshold_percent":80}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"PASS","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":79}` + "\n"),
		[]byte(`{"event":"coverage_measured","gate":"coverage","status":"FAIL","adapter":"go-coverprofile-v1","report":".polis/coverage.out","metric":"line_coverage_percent","covered_lines":81,"total_lines":100,"value_percent":81,"operator":">","threshold_percent":80}` + "\n"),
	}
	for _, raw := range cases {
		if _, err := DecodeEvidence(raw); err == nil {
			t.Fatalf("expected rejection for %q", raw)
		}
	}
}

func FuzzDecodeEvidence(f *testing.F) {
	f.Add([]byte("{\"event\":\"gate_started\",\"gate\":\"behavior\"}\n"))
	f.Add([]byte("not-json\n"))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodeEvidence(raw) })
}
