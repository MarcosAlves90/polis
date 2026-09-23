package spec

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestDecodeEvidenceVersionsKeepDeferredSemanticsOutOfV2(t *testing.T) {
	configured := validationConfigurationV3([]string{"coverage"})
	deferred := `{"event":"gate_finished","gate":"coverage","status":"DEFERRED","reason":"deferred to consumer"}` + "\n"
	if _, err := DecodeEvidence([]byte(configured)); err == nil {
		t.Fatal("Evidence v2 accepted deferred_gates")
	}
	if _, err := DecodeEvidence([]byte(deferred)); err == nil {
		t.Fatal("Evidence v2 accepted DEFERRED")
	}
	events, err := DecodeEvidenceVersion([]byte(configured+deferred), EvidenceVersionV3)
	if err != nil {
		t.Fatalf("Evidence v3 rejected deferred events: %v", err)
	}
	if len(events) != 2 || !reflect.DeepEqual(events[0].DeferredGates, []string{"coverage"}) || events[1].Status != StatusDeferred {
		t.Fatalf("decoded v3 events=%+v", events)
	}
}

func TestDecodeEvidenceV3RequiresCanonicalDeferredInventoryAndReason(t *testing.T) {
	valid := validationConfigurationV3([]string{"coverage"})
	cases := []struct {
		name string
		raw  string
	}{
		{name: "missing inventory", raw: strings.Replace(valid, `,"deferred_gates":["coverage"]`, "", 1)},
		{name: "not enabled", raw: validationConfigurationV3([]string{"lint"})},
		{name: "deferred reason absent", raw: valid + `{"event":"gate_finished","gate":"coverage","status":"DEFERRED"}` + "\n"},
		{name: "deferred reason empty", raw: valid + `{"event":"gate_finished","gate":"coverage","status":"DEFERRED","reason":" "}` + "\n"},
		{name: "command cannot be deferred", raw: valid + `{"event":"command_finished","gate":"coverage","status":"DEFERRED","argv":["go"],"cwd":".","exit_code":0,"duration_ms":1,"stdout":"","stderr":""}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeEvidenceVersion([]byte(tc.raw), EvidenceVersionV3); err == nil {
				t.Fatalf("invalid v3 evidence accepted: %s", tc.raw)
			}
		})
	}
}

func TestMarshalEvidenceV3PreservesExplicitEmptyDeferredInventory(t *testing.T) {
	raw, err := json.Marshal(EvidenceEvent{
		Event: "validation_configured", Gate: "policy", ValidationLevel: ValidationLevelMinimal,
		EnabledGates: []string{}, DisabledGates: append([]string{}, ProjectGateOrder...), DeferredGates: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"deferred_gates":[]`) {
		t.Fatalf("explicit empty deferred inventory omitted: %s", raw)
	}
	if _, err := DecodeEvidenceVersion(append(raw, '\n'), EvidenceVersionV3); err != nil {
		t.Fatalf("marshaled v3 configuration rejected: %v", err)
	}
}

func TestOfflineEvidenceSchemaDefinesCurrentDeferredContract(t *testing.T) {
	var raw []byte
	for _, resource := range OfflineResources() {
		if resource.Path == "schemas/evidence-event.schema.json" {
			raw = resource.Data
			break
		}
	}
	if len(raw) == 0 {
		t.Fatal("canonical evidence schema not found")
	}
	var schema struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Title != "POLIS Evidence Event v3" || !strings.Contains(string(raw), `"deferred_gates"`) || !strings.Contains(string(raw), `"DEFERRED"`) {
		t.Fatalf("current evidence schema is missing deferral semantics: title=%q", schema.Title)
	}
}

func validationConfigurationV3(deferred []string) string {
	enabled := []string{"test.complete", "coverage"}
	disabled := ProjectGateOrder[2:]
	encode := func(ids []string) string {
		data, _ := json.Marshal(ids)
		return string(data)
	}
	deferredJSON := encode(deferred)
	return `{"event":"validation_configured","gate":"policy","validation_level":"strict","enabled_gates":` + encode(enabled) + `,"disabled_gates":` + encode(disabled) + `,"deferred_gates":` + deferredJSON + `}` + "\n"
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
