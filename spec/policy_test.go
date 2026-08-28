package spec

import (
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
