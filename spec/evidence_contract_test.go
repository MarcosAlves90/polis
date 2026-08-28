package spec

import (
	"encoding/json"
	"testing"
)

func contractForEvidence(t *testing.T, defect bool) ChangeContract {
	t.Helper()
	base := CommandSpec{Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60}
	c := ChangeContract{SchemaVersion: 1, Kind: ChangeKindFeature, Behavior: base, Affected: base, Regression: RegressionContract{Mode: RegressionModeNotApplicable, ReasonCode: RegressionReasonNotDefect}}
	if defect {
		exit := 1
		reg := CommandSpec{Argv: []string{"go", "test", "./...", "-run", "TestBug"}, Cwd: ".", TimeoutSeconds: 60}
		c.Kind = ChangeKindDefect
		c.Regression = RegressionContract{Mode: RegressionModeRedGreen, Command: &reg, BaselineExitCode: &exit, BaselineOutputContains: []string{"BUG-RED"}}
	}
	return c
}

func policyForEvidence(t *testing.T) Policy {
	t.Helper()
	p, err := DecodePolicy([]byte(canonicalPolicyJSON()))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func passCommandEvent(gate string, cmd CommandSpec, status Status, exit int, stdout string) EvidenceEvent {
	d := int64(1)
	stderr := ""
	return EvidenceEvent{Event: "command_finished", Gate: gate, Status: status, Argv: append([]string(nil), cmd.Argv...), Cwd: cmd.Cwd, ExitCode: &exit, DurationMS: &d, Stdout: &stdout, Stderr: &stderr}
}

func validPassEvidence(t *testing.T, defect bool) ([]EvidenceEvent, ChangeContract, Policy) {
	t.Helper()
	c := contractForEvidence(t, defect)
	p := policyForEvidence(t)
	events := []EvidenceEvent{}
	if defect {
		events = append(events, EvidenceEvent{Event: "gate_started", Gate: "regression"}, passCommandEvent("regression", *c.Regression.Command, StatusFail, *c.Regression.BaselineExitCode, "BUG-RED"), EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: StatusPass})
		events = append(events, EvidenceEvent{Event: "gate_started", Gate: "regression"}, passCommandEvent("regression", *c.Regression.Command, StatusPass, 0, ""), EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: StatusPass})
	} else {
		reason := RegressionReasonNotDefect
		events = append(events, EvidenceEvent{Event: "gate_started", Gate: "regression"}, EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: StatusNotApplicable, Reason: &reason})
	}
	for _, x := range []struct {
		g string
		c CommandSpec
	}{{"behavior", c.Behavior}, {"affected", c.Affected}} {
		events = append(events, EvidenceEvent{Event: "gate_started", Gate: x.g}, passCommandEvent(x.g, x.c, StatusPass, 0, ""), EvidenceEvent{Event: "gate_finished", Gate: x.g, Status: StatusPass})
	}
	for _, g := range p.Gates {
		events = append(events, EvidenceEvent{Event: "gate_started", Gate: g.ID})
		switch g.Mode {
		case GateModeNotApplicable:
			events = append(events, EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: StatusNotApplicable, Reason: g.Reason})
		case GateModeCommand:
			events = append(events, passCommandEvent(g.ID, *g.Command, StatusPass, 0, ""), EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: StatusPass})
		case GateModeCoverage:
			events = append(events, passCommandEvent(g.ID, *g.Command, StatusPass, 0, ""))
			covered, total := 81, 100
			value := 81.0
			events = append(events, EvidenceEvent{Event: "coverage_measured", Gate: "coverage", Status: StatusPass, Adapter: g.Adapter, Report: g.Report, Metric: CoverageMetricLinePercent, CoveredLines: &covered, TotalLines: &total, ValuePercent: &value, Operator: g.Operator, ThresholdPercent: g.ThresholdPercent}, EvidenceEvent{Event: "gate_finished", Gate: g.ID, Status: StatusPass})
		}
	}
	return events, c, p
}

func TestValidatePassEvidenceAcceptsFeatureAndDefect(t *testing.T) {
	for _, defect := range []bool{false, true} {
		events, c, p := validPassEvidence(t, defect)
		if err := ValidatePassEvidence(events, c, p); err != nil {
			t.Fatalf("defect=%v: %v", defect, err)
		}
	}
}

func TestValidatePassEvidenceRejectsTampering(t *testing.T) {
	base, c, p := validPassEvidence(t, true)
	mutations := []func([]EvidenceEvent){
		func(e []EvidenceEvent) { e[0].Gate = "behavior" },
		func(e []EvidenceEvent) { *e[1].ExitCode = 2 },
		func(e []EvidenceEvent) { s := "other"; e[1].Stdout = &s },
		func(e []EvidenceEvent) { e[len(e)-1].Status = StatusFail },
	}
	for i, mut := range mutations {
		raw, _ := json.Marshal(base)
		var copyEvents []EvidenceEvent
		_ = json.Unmarshal(raw, &copyEvents)
		mut(copyEvents)
		if err := ValidatePassEvidence(copyEvents, c, p); err == nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
	if err := ValidatePassEvidence(base[:len(base)-1], c, p); err == nil {
		t.Fatal("truncated evidence accepted")
	}
	extra := append(append([]EvidenceEvent{}, base...), EvidenceEvent{Event: "gate_started", Gate: "behavior"})
	if err := ValidatePassEvidence(extra, c, p); err == nil {
		t.Fatal("extra evidence accepted")
	}
}

func TestValidatePassEvidenceRejectsCoverageArithmetic(t *testing.T) {
	events, c, p := validPassEvidence(t, false)
	for i := range events {
		if events[i].Event == "coverage_measured" {
			v := 82.0
			events[i].ValuePercent = &v
			break
		}
	}
	if err := ValidatePassEvidence(events, c, p); err == nil {
		t.Fatal("arithmetic mismatch accepted")
	}
}

func cloneEvidence(t *testing.T, in []EvidenceEvent) []EvidenceEvent {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []EvidenceEvent
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func eventIndex(events []EvidenceEvent, event, gate string, occurrence int) int {
	seen := 0
	for i, e := range events {
		if e.Event == event && e.Gate == gate {
			if seen == occurrence {
				return i
			}
			seen++
		}
	}
	return -1
}

func TestValidatePassEvidenceRejectsContractAndPolicyErrors(t *testing.T) {
	events, c, p := validPassEvidence(t, false)
	badChange := c
	badChange.SchemaVersion = 999
	if err := ValidatePassEvidence(events, badChange, p); err == nil {
		t.Fatal("invalid change contract accepted")
	}
	badPolicy := p
	badPolicy.SchemaVersion = 999
	if err := ValidatePassEvidence(events, c, badPolicy); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := ValidatePassEvidence(nil, c, p); err == nil {
		t.Fatal("empty evidence accepted")
	}
}

func TestValidatePassEvidenceRejectsFeatureTraceVariants(t *testing.T) {
	base, c, p := validPassEvidence(t, false)
	cases := []struct {
		name   string
		mutate func([]EvidenceEvent) []EvidenceEvent
	}{
		{"regression start event", func(e []EvidenceEvent) []EvidenceEvent { e[0].Event = "command_finished"; return e }},
		{"unexpected regression reason", func(e []EvidenceEvent) []EvidenceEvent { s := "wrong"; e[1].Reason = &s; return e }},
		{"behavior start missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_started", "behavior", 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"behavior command mismatch", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "command_finished", "behavior", 0)
			e[i].Argv = []string{"false"}
			return e
		}},
		{"behavior finish missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", "behavior", 0)
			return append(e[:i], e[i+1:]...)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.mutate(cloneEvidence(t, base))
			if err := ValidatePassEvidence(e, c, p); err == nil {
				t.Fatal("tampered evidence accepted")
			}
		})
	}
}

func TestValidatePassEvidenceRejectsProjectPolicyTraceVariants(t *testing.T) {
	base, c, p := validPassEvidence(t, false)
	naGate := "lint"
	commandGate := "test.complete"
	coverageGate := "coverage"
	cases := []struct {
		name   string
		mutate func([]EvidenceEvent) []EvidenceEvent
	}{
		{"project start missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_started", commandGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"na finish missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", naGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"na reason mismatch", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", naGate, 0)
			s := "wrong"
			e[i].Reason = &s
			return e
		}},
		{"command event missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "command_finished", commandGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"command finish missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", commandGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"coverage command missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "command_finished", coverageGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"coverage command mismatch", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "command_finished", coverageGate, 0)
			e[i].Cwd = "subdir"
			return e
		}},
		{"coverage measurement missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "coverage_measured", coverageGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"coverage metadata mismatch", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "coverage_measured", coverageGate, 0)
			e[i].Adapter = "other"
			return e
		}},
		{"coverage threshold failure", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "coverage_measured", coverageGate, 0)
			covered, total := 80, 100
			value := 80.0
			e[i].CoveredLines = &covered
			e[i].TotalLines = &total
			e[i].ValuePercent = &value
			return e
		}},
		{"coverage finish missing", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", coverageGate, 0)
			return append(e[:i], e[i+1:]...)
		}},
		{"coverage finish reason", func(e []EvidenceEvent) []EvidenceEvent {
			i := eventIndex(e, "gate_finished", coverageGate, 0)
			s := "unexpected"
			e[i].Reason = &s
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.mutate(cloneEvidence(t, base))
			if err := ValidatePassEvidence(e, c, p); err == nil {
				t.Fatal("tampered project evidence accepted")
			}
		})
	}
}
