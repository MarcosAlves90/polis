package spec

import (
	"fmt"
	"math"
	"reflect"
	"strings"
)

func ValidatePassEvidence(events []EvidenceEvent, change ChangeContract, policy Policy) error {
	if err := change.Validate(); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	i := 0
	next := func() (EvidenceEvent, error) {
		if i >= len(events) {
			return EvidenceEvent{}, fmt.Errorf("evidence ended at event %d", i)
		}
		e := events[i]
		i++
		return e, nil
	}
	expectStart := func(gate string) error {
		e, err := next()
		if err != nil {
			return err
		}
		if e.Event != "gate_started" || e.Gate != gate {
			return fmt.Errorf("event %d: want gate_started %s, got %s %s", i-1, gate, e.Event, e.Gate)
		}
		return nil
	}
	expectCommand := func(gate string, cmd CommandSpec, wantStatus Status, wantExit *int) (EvidenceEvent, error) {
		e, err := next()
		if err != nil {
			return EvidenceEvent{}, err
		}
		if e.Event != "command_finished" || e.Gate != gate {
			return EvidenceEvent{}, fmt.Errorf("event %d: want command_finished %s", i-1, gate)
		}
		if e.Status != wantStatus || !reflect.DeepEqual(e.Argv, cmd.Argv) || e.Cwd != cmd.Cwd {
			return EvidenceEvent{}, fmt.Errorf("event %d: command evidence does not match contract for %s", i-1, gate)
		}
		if wantExit != nil && (e.ExitCode == nil || *e.ExitCode != *wantExit) {
			return EvidenceEvent{}, fmt.Errorf("event %d: exit code mismatch for %s", i-1, gate)
		}
		return e, nil
	}
	expectFinish := func(gate string, status Status, reason *string) error {
		e, err := next()
		if err != nil {
			return err
		}
		if e.Event != "gate_finished" || e.Gate != gate || e.Status != status {
			return fmt.Errorf("event %d: gate_finished mismatch for %s", i-1, gate)
		}
		if reason == nil {
			if e.Reason != nil {
				return fmt.Errorf("event %d: unexpected reason for %s", i-1, gate)
			}
		} else if e.Reason == nil || *e.Reason != *reason {
			return fmt.Errorf("event %d: reason mismatch for %s", i-1, gate)
		}
		return nil
	}

	if change.Kind == ChangeKindDefect {
		if err := expectStart("regression"); err != nil {
			return err
		}
		ce, err := expectCommand("regression", *change.Regression.Command, StatusFail, change.Regression.BaselineExitCode)
		if err != nil {
			return err
		}
		combined := ""
		if ce.Stdout != nil {
			combined += *ce.Stdout
		}
		if ce.Stderr != nil {
			combined += "\n" + *ce.Stderr
		}
		for _, token := range change.Regression.BaselineOutputContains {
			if !strings.Contains(combined, token) {
				return fmt.Errorf("stored regression baseline output missing token %q", token)
			}
		}
		if err := expectFinish("regression", StatusPass, nil); err != nil {
			return err
		}
		if err := expectPassCommandGate(&i, events, "regression", *change.Regression.Command); err != nil {
			return err
		}
	} else {
		if err := expectStart("regression"); err != nil {
			return err
		}
		reason := RegressionReasonNotDefect
		if err := expectFinish("regression", StatusNotApplicable, &reason); err != nil {
			return err
		}
	}
	if err := expectPassCommandGate(&i, events, "behavior", change.Behavior); err != nil {
		return err
	}
	if err := expectPassCommandGate(&i, events, "affected", change.Affected); err != nil {
		return err
	}

	for _, gate := range policy.Gates {
		if i >= len(events) || events[i].Event != "gate_started" || events[i].Gate != gate.ID {
			return fmt.Errorf("event %d: missing project gate start %s", i, gate.ID)
		}
		i++
		switch gate.Mode {
		case GateModeNotApplicable:
			if i >= len(events) {
				return fmt.Errorf("evidence ended at project gate %s", gate.ID)
			}
			e := events[i]
			i++
			if e.Event != "gate_finished" || e.Gate != gate.ID || e.Status != StatusNotApplicable || e.Reason == nil || gate.Reason == nil || *e.Reason != *gate.Reason {
				return fmt.Errorf("project gate %s NOT_APPLICABLE evidence mismatch", gate.ID)
			}
		case GateModeCommand:
			var err error
			i, err = validatePassCommandAt(events, i, gate.ID, *gate.Command)
			if err != nil {
				return err
			}
		case GateModeCoverage:
			if i >= len(events) {
				return fmt.Errorf("coverage command evidence missing")
			}
			ce := events[i]
			i++
			if ce.Event != "command_finished" || ce.Gate != gate.ID || ce.Status != StatusPass || ce.ExitCode == nil || *ce.ExitCode != 0 || !reflect.DeepEqual(ce.Argv, gate.Command.Argv) || ce.Cwd != gate.Command.Cwd {
				return fmt.Errorf("coverage command evidence mismatch")
			}
			if i >= len(events) {
				return fmt.Errorf("coverage evidence missing")
			}
			e := events[i]
			i++
			if e.Event != "coverage_measured" || e.Gate != "coverage" || e.Status != StatusPass || e.Adapter != gate.Adapter || e.Report != gate.Report || e.Metric != CoverageMetricLinePercent || e.Operator != gate.Operator || e.ThresholdPercent == nil || gate.ThresholdPercent == nil || *e.ThresholdPercent != *gate.ThresholdPercent || e.CoveredLines == nil || e.TotalLines == nil || e.ValuePercent == nil {
				return fmt.Errorf("coverage evidence does not match policy")
			}
			computed := float64(*e.CoveredLines) / float64(*e.TotalLines) * 100
			if math.Abs(computed-*e.ValuePercent) > 1e-9 {
				return fmt.Errorf("coverage evidence arithmetic mismatch: got %.12f calculated %.12f", *e.ValuePercent, computed)
			}
			if !CoveragePass(*e.ValuePercent, *gate.ThresholdPercent) {
				return fmt.Errorf("coverage evidence does not pass policy")
			}
			if i >= len(events) {
				return fmt.Errorf("coverage gate_finished missing")
			}
			finish := events[i]
			i++
			if finish.Event != "gate_finished" || finish.Gate != "coverage" || finish.Status != StatusPass || finish.Reason != nil {
				return fmt.Errorf("coverage gate_finished mismatch")
			}
		default:
			return fmt.Errorf("unsupported policy mode %s", gate.Mode)
		}
	}
	if i != len(events) {
		return fmt.Errorf("unexpected extra evidence events: %d", len(events)-i)
	}
	return nil
}

func expectPassCommandGate(index *int, events []EvidenceEvent, gate string, cmd CommandSpec) error {
	i := *index
	if i >= len(events) || events[i].Event != "gate_started" || events[i].Gate != gate {
		return fmt.Errorf("event %d: missing gate_started %s", i, gate)
	}
	i++
	var err error
	i, err = validatePassCommandAt(events, i, gate, cmd)
	if err != nil {
		return err
	}
	*index = i
	return nil
}

func validatePassCommandAt(events []EvidenceEvent, i int, gate string, cmd CommandSpec) (int, error) {
	if i >= len(events) {
		return i, fmt.Errorf("evidence ended before command %s", gate)
	}
	e := events[i]
	i++
	if e.Event != "command_finished" || e.Gate != gate || e.Status != StatusPass || e.ExitCode == nil || *e.ExitCode != 0 || !reflect.DeepEqual(e.Argv, cmd.Argv) || e.Cwd != cmd.Cwd {
		return i, fmt.Errorf("command evidence mismatch for %s", gate)
	}
	if i >= len(events) {
		return i, fmt.Errorf("evidence ended before gate_finished %s", gate)
	}
	f := events[i]
	i++
	if f.Event != "gate_finished" || f.Gate != gate || f.Status != StatusPass || f.Reason != nil {
		return i, fmt.Errorf("gate_finished mismatch for %s", gate)
	}
	return i, nil
}
