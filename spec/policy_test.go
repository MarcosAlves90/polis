package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func canonicalPolicyJSON() string {
	return `{"schema_version":2,"gates":[` +
		`{"id":"test.complete","mode":"command","command":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":1200}},` +
		`{"id":"coverage","mode":"coverage","command":{"argv":["go","test","./...","-coverprofile=.polis/coverage.out"],"cwd":".","timeout_seconds":1200},"adapter":"go-coverprofile-v1","report":".polis/coverage.out","operator":">","threshold_percent":80},` +
		`{"id":"lint","mode":"not_applicable","reason":"project defines no independent lint gate"},` +
		`{"id":"typecheck","mode":"not_applicable","reason":"compiler checks are covered elsewhere"},` +
		`{"id":"build","mode":"not_applicable","reason":"project has no distributable build"},` +
		`{"id":"smoke","mode":"not_applicable","reason":"project exposes no runtime smoke surface"},` +
		`{"id":"compatibility","mode":"not_applicable","reason":"change has no compatibility surface"},` +
		`{"id":"dependency","mode":"not_applicable","reason":"dependency audit is not project-required here"},` +
		`{"id":"migration","mode":"not_applicable","reason":"project has no persisted-state migrations"},` +
		`{"id":"security","mode":"not_applicable","reason":"project defines no separate security command"},` +
		`{"id":"platform","mode":"not_applicable","reason":"project defines no additional platform command"}` +
		`]}`
}

func TestDecodePolicyAcceptsCanonicalPolicy(t *testing.T) {
	p, err := DecodePolicy([]byte(canonicalPolicyJSON()))
	if err != nil {
		t.Fatalf("DecodePolicy() error = %v", err)
	}
	if len(p.Gates) != len(ProjectGateOrder) {
		t.Fatalf("gates = %d", len(p.Gates))
	}
	if p.Gates[0].Command == nil || p.Gates[0].Command.Argv[0] != "go" {
		t.Fatalf("first gate = %#v", p.Gates[0])
	}
}

func TestDecodePolicyRejectsAlpha1AndUnknownFields(t *testing.T) {
	cases := []string{
		`{"schema_version":1,"gates":["integrity"]}`,
		strings.Replace(canonicalPolicyJSON(), `"schema_version":2`, `"schema_version":2,"extra":true`, 1),
		strings.Replace(canonicalPolicyJSON(), `"timeout_seconds":1200`, `"timeout_seconds":1200,"extra":true`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
}

func TestDecodePolicyRejectsGateInventoryAndOrderingErrors(t *testing.T) {
	canonical := canonicalPolicyJSON()
	cases := []string{
		strings.Replace(canonical, `"id":"lint"`, `"id":"made-up"`, 1),
		strings.Replace(canonical, `"id":"lint"`, `"id":"coverage"`, 1),
		strings.Replace(canonical, `,{"id":"platform","mode":"not_applicable","reason":"project defines no additional platform command"}`, ``, 1),
		strings.Replace(canonical, `"id":"lint"`, `"id":"typecheck"`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatalf("expected gate inventory/order rejection")
		}
	}
}

func TestDecodePolicyRejectsInvalidModeCombinations(t *testing.T) {
	canonical := canonicalPolicyJSON()
	cases := []string{
		strings.Replace(canonical, `"id":"test.complete","mode":"command","command":{"argv":["go","test","./..."],"cwd":".","timeout_seconds":1200}`, `"id":"test.complete","mode":"not_applicable","reason":"not allowed"`, 1),
		strings.Replace(canonical, `"id":"lint","mode":"not_applicable","reason":"project defines no independent lint gate"`, `"id":"lint","mode":"command","reason":"bad","command":{"argv":["go"],"cwd":".","timeout_seconds":1}`, 1),
		strings.Replace(canonical, `"id":"lint","mode":"not_applicable","reason":"project defines no independent lint gate"`, `"id":"lint","mode":"not_applicable"`, 1),
		strings.Replace(canonical, `"id":"lint","mode":"not_applicable","reason":"project defines no independent lint gate"`, `"id":"lint","mode":"weird","reason":"bad"`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatalf("expected mode combination rejection")
		}
	}
}

func TestDecodePolicyRejectsInvalidCommand(t *testing.T) {
	canonical := canonicalPolicyJSON()
	cases := []string{
		strings.Replace(canonical, `["go","test","./..."]`, `[]`, 1),
		strings.Replace(canonical, `["go","test","./..."]`, `["go",""]`, 1),
		strings.Replace(canonical, `"cwd":"."`, `"cwd":"../escape"`, 1),
		strings.Replace(canonical, `"cwd":"."`, `"cwd":"/absolute"`, 1),
		strings.Replace(canonical, `"cwd":"."`, `"cwd":"a\\b"`, 1),
		strings.Replace(canonical, `"timeout_seconds":1200`, `"timeout_seconds":0`, 1),
		strings.Replace(canonical, `"timeout_seconds":1200`, `"timeout_seconds":3601`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatalf("expected invalid command rejection")
		}
	}
}

func TestDecodePolicyRejectsMalformedTrailingAndVersion(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"schema_version":`),
		[]byte(canonicalPolicyJSON() + ` {}`),
		[]byte(strings.Replace(canonicalPolicyJSON(), `"schema_version":2`, `"schema_version":3`, 1)),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy(raw); err == nil {
			t.Fatalf("expected rejection for %q", raw)
		}
	}
}

func TestDecodePolicyRejectsInvalidCoverageContract(t *testing.T) {
	canonical := canonicalPolicyJSON()
	cases := []string{
		strings.Replace(canonical, `"adapter":"go-coverprofile-v1"`, `"adapter":"unknown-v1"`, 1),
		strings.Replace(canonical, `"report":".polis/coverage.out"`, `"report":"../coverage.out"`, 1),
		strings.Replace(canonical, `"operator":">"`, `"operator":">="`, 1),
		strings.Replace(canonical, `"threshold_percent":80`, `"threshold_percent":79.99`, 1),
		strings.Replace(canonical, `"threshold_percent":80`, `"threshold_percent":101`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatalf("expected invalid coverage contract rejection")
		}
	}
}

func FuzzDecodePolicy(f *testing.F) {
	f.Add([]byte(`{"schema_version":3,"gates":[]}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = DecodePolicy(raw) })
}

func TestDecodePolicyAcceptsStandardLevelWithCoverageDisabled(t *testing.T) {
	policy, err := DecodePolicy(configurablePolicyJSON("standard", "command", "not_applicable"))
	if err != nil {
		t.Fatalf("POLIS-RED: standard policy with explicitly disabled coverage was rejected: %v", err)
	}
	if policy.ValidationLevel != ValidationLevelStandard {
		t.Fatalf("validation level = %q", policy.ValidationLevel)
	}
}

func TestDecodePolicyAcceptsMinimalLevelWithRequiredProjectGatesDisabled(t *testing.T) {
	policy, err := DecodePolicy(configurablePolicyJSON(ValidationLevelMinimal, GateModeNotApplicable, GateModeNotApplicable))
	if err != nil {
		t.Fatalf("minimal policy with explicit disabled gates was rejected: %v", err)
	}
	summary := policy.ValidationSummary()
	if summary.Level != ValidationLevelMinimal || len(summary.EnabledGates) != 0 || len(summary.DisabledGates) != len(ProjectGateOrder) {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestDecodePolicyRejectsIncompatibleValidationLevels(t *testing.T) {
	cases := []struct {
		name  string
		level string
		test  string
		cover string
	}{
		{name: "strict coverage disabled", level: ValidationLevelStrict, test: GateModeCommand, cover: GateModeNotApplicable},
		{name: "standard test disabled", level: ValidationLevelStandard, test: GateModeNotApplicable, cover: GateModeNotApplicable},
		{name: "unknown level", level: "fast", test: GateModeCommand, cover: GateModeCoverage},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodePolicy(configurablePolicyJSON(tt.level, tt.test, tt.cover)); err == nil {
				t.Fatal("incompatible validation policy was accepted")
			}
		})
	}
}

func configurablePolicyJSON(level, testMode, coverageMode string) []byte {
	environment := map[string]any{"mode": EnvironmentModeInherit}
	command := func(argv ...string) map[string]any {
		return map[string]any{
			"argv": argv, "cwd": ".", "timeout_seconds": 60, "environment": environment,
		}
	}
	reason := func(id string) map[string]any {
		return map[string]any{"id": id, "mode": GateModeNotApplicable, "reason": "disabled for this execution context"}
	}
	gates := make([]any, 0, len(ProjectGateOrder))
	for _, id := range ProjectGateOrder {
		switch id {
		case "test.complete":
			if testMode == GateModeCommand {
				gates = append(gates, map[string]any{"id": id, "mode": GateModeCommand, "command": command("true")})
			} else {
				gates = append(gates, reason(id))
			}
		case "coverage":
			if coverageMode == GateModeCoverage {
				threshold := 80.0
				gates = append(gates, map[string]any{"id": id, "mode": GateModeCoverage, "command": command("true"), "adapter": CoverageAdapterGoCoverProfileV1, "report": "coverage.out", "operator": CoverageOperatorGreaterThan, "threshold_percent": threshold})
			} else {
				gates = append(gates, reason(id))
			}
		default:
			gates = append(gates, reason(id))
		}
	}
	raw, err := json.Marshal(map[string]any{"schema_version": PolicySchemaVersion, "validation_level": level, "gates": gates})
	if err != nil {
		panic(err)
	}
	return raw
}
