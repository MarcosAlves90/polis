package spec

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestPolicyValidationFailureCoverageMargin(t *testing.T) {
	decodeCases := []string{
		`{"id":"lint","mode":"command","command":{"argv":["go"],"cwd":".","timeout_seconds":1},"extra":true}`,
		`{"id":1,"mode":"command","command":{"argv":["go"],"cwd":".","timeout_seconds":1}}`,
		`{"id":"lint","mode":1,"command":{"argv":["go"],"cwd":".","timeout_seconds":1}}`,
		`{"id":"coverage","mode":"coverage","command":{"argv":["go"],"cwd":".","timeout_seconds":1},"adapter":1,"report":"x","operator":">","threshold_percent":81}`,
		`{"id":"coverage","mode":"coverage","command":{"argv":["go"],"cwd":".","timeout_seconds":1},"adapter":"go-coverprofile-v1","report":"x","operator":">","threshold_percent":"81"}`,
		`{"id":"lint","mode":"not_applicable","reason":1}`,
	}
	for _, raw := range decodeCases {
		if _, err := decodeGatePolicy(json.RawMessage(raw)); err == nil {
			t.Fatalf("expected decode rejection for %s", raw)
		}
	}

	dec := json.NewDecoder(bytes.NewBufferString(`{} trailing`))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := ensureDecoderEOF(dec, "policy"); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected malformed trailing data, got %v", err)
	}

	reason := "not applicable"
	threshold := 81.0
	validCommand := &CommandSpec{Argv: []string{"go"}, Cwd: ".", TimeoutSeconds: 1}
	invalidGates := []GatePolicy{
		{ID: "lint", Mode: GateModeCommand, Command: validCommand, Reason: &reason},
		{ID: "coverage", Mode: GateModeCoverage, Command: nil, ThresholdPercent: &threshold},
		{ID: "lint", Mode: GateModeNotApplicable, Reason: nil},
		{ID: "lint", Mode: "unknown"},
	}
	for _, gate := range invalidGates {
		if err := gate.Validate(); err == nil {
			t.Fatalf("expected gate validation failure: %+v", gate)
		}
	}

	badThreshold := math.NaN()
	coverage := GatePolicy{
		ID:               "coverage",
		Mode:             GateModeCoverage,
		Command:          validCommand,
		Adapter:          CoverageAdapterGoCoverProfileV1,
		Report:           "coverage.out",
		Operator:         CoverageOperatorGreaterThan,
		ThresholdPercent: &badThreshold,
	}
	if err := coverage.Validate(); err == nil {
		t.Fatal("expected NaN coverage threshold rejection")
	}

	for _, value := range []string{"", "C:/tmp", "a/../b"} {
		if err := ValidateRepoRelativePath(value); err == nil {
			t.Fatalf("expected invalid path rejection for %q", value)
		}
	}
}
