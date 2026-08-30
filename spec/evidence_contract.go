package spec

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
)

type evidenceValidator struct {
	events []EvidenceEvent
	index  int
}

func ValidatePassEvidence(events []EvidenceEvent, change ChangeContract, policy Policy) error {
	if err := change.Validate(); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	validator := evidenceValidator{events: events}
	if err := validator.validateRegression(change); err != nil {
		return err
	}
	if err := validator.expectPassCommandGate("behavior", change.Behavior); err != nil {
		return err
	}
	if err := validator.expectPassCommandGate("affected", change.Affected); err != nil {
		return err
	}
	if err := validator.validateProjectGates(policy); err != nil {
		return err
	}
	if validator.index != len(events) {
		return fmt.Errorf("unexpected extra evidence events: %d", len(events)-validator.index)
	}
	return nil
}

func (v *evidenceValidator) next() (EvidenceEvent, error) {
	if v.index >= len(v.events) {
		return EvidenceEvent{}, fmt.Errorf("evidence ended at event %d", v.index)
	}
	e := v.events[v.index]
	v.index++
	return e, nil
}

func (v *evidenceValidator) expectStart(gate string) error {
	e, err := v.next()
	if err != nil {
		return err
	}
	if e.Event != "gate_started" || e.Gate != gate {
		return fmt.Errorf("event %d: want gate_started %s, got %s %s", v.index-1, gate, e.Event, e.Gate)
	}
	return nil
}

func (v *evidenceValidator) expectCommand(gate string, cmd CommandSpec, wantStatus Status, wantExit *int) (EvidenceEvent, error) {
	e, err := v.next()
	if err != nil {
		return EvidenceEvent{}, err
	}
	if e.Event != "command_finished" || e.Gate != gate {
		return EvidenceEvent{}, fmt.Errorf("event %d: want command_finished %s", v.index-1, gate)
	}
	if e.Status != wantStatus || !reflect.DeepEqual(e.Argv, cmd.Argv) || e.Cwd != cmd.Cwd {
		return EvidenceEvent{}, fmt.Errorf("event %d: command evidence does not match contract for %s", v.index-1, gate)
	}
	if wantExit != nil && (e.ExitCode == nil || *e.ExitCode != *wantExit) {
		return EvidenceEvent{}, fmt.Errorf("event %d: exit code mismatch for %s", v.index-1, gate)
	}
	if err := validateCommandEnvironmentEvidence(e, cmd); err != nil {
		return EvidenceEvent{}, fmt.Errorf("event %d: %w", v.index-1, err)
	}
	return e, nil
}

func validateCommandEnvironmentEvidence(event EvidenceEvent, command CommandSpec) error {
	if command.Environment == nil {
		return nil
	}
	if event.EnvironmentMode == "" && event.Stdout != nil {
		// Legacy evidence is accepted only for migration contracts.
		return nil
	}
	if event.EnvironmentMode != command.Environment.Mode || !reflect.DeepEqual(event.EnvironmentPass, command.Environment.Pass) {
		return errors.New("command environment evidence does not match contract")
	}
	return nil
}

func (v *evidenceValidator) expectFinish(gate string, status Status, reason *string) error {
	e, err := v.next()
	if err != nil {
		return err
	}
	if e.Event != "gate_finished" || e.Gate != gate || e.Status != status {
		return fmt.Errorf("event %d: gate_finished mismatch for %s", v.index-1, gate)
	}
	return validateFinishReason(e, reason, v.index-1, gate)
}

func validateFinishReason(e EvidenceEvent, reason *string, index int, gate string) error {
	if reason == nil {
		if e.Reason != nil {
			return fmt.Errorf("event %d: unexpected reason for %s", index, gate)
		}
		return nil
	}
	if e.Reason == nil || *e.Reason != *reason {
		return fmt.Errorf("event %d: reason mismatch for %s", index, gate)
	}
	return nil
}

func (v *evidenceValidator) validateRegression(change ChangeContract) error {
	if change.Kind != ChangeKindDefect {
		return v.validateNonDefectRegression()
	}
	if err := v.expectStart("regression"); err != nil {
		return err
	}
	commandEvent, err := v.expectCommand("regression", *change.Regression.Command, StatusFail, change.Regression.BaselineExitCode)
	if err != nil {
		return err
	}
	if commandEvent.Stdout != nil || commandEvent.Stderr != nil {
		if err := validateRegressionOutput(commandEvent, change.Regression.BaselineOutputContains); err != nil {
			return err
		}
	} else {
		for i := range change.Regression.BaselineOutputContains {
			if err := v.expectOracle(i); err != nil {
				return err
			}
		}
	}
	if err := v.expectFinish("regression", StatusPass, nil); err != nil {
		return err
	}
	return v.expectPassCommandGate("regression", *change.Regression.Command)
}

func (v *evidenceValidator) expectOracle(index int) error {
	e, err := v.next()
	if err != nil {
		return err
	}
	if e.Event != "oracle_checked" || e.Gate != "regression" || e.Status != StatusPass || e.Oracle != "baseline_output_contains" || e.OracleIndex == nil || *e.OracleIndex != index {
		return fmt.Errorf("event %d: regression oracle evidence mismatch", v.index-1)
	}
	return nil
}

func (v *evidenceValidator) validateNonDefectRegression() error {
	if err := v.expectStart("regression"); err != nil {
		return err
	}
	reason := RegressionReasonNotDefect
	return v.expectFinish("regression", StatusNotApplicable, &reason)
}

func validateRegressionOutput(event EvidenceEvent, tokens []string) error {
	combined := ""
	if event.Stdout != nil {
		combined += *event.Stdout
	}
	if event.Stderr != nil {
		combined += "\n" + *event.Stderr
	}
	for _, token := range tokens {
		if !strings.Contains(combined, token) {
			return fmt.Errorf("stored regression baseline output missing token %q", token)
		}
	}
	return nil
}

func (v *evidenceValidator) expectPassCommandGate(gate string, cmd CommandSpec) error {
	if err := v.expectStart(gate); err != nil {
		return err
	}
	if err := v.expectPassCommand(gate, cmd); err != nil {
		return err
	}
	return v.expectPassFinish(gate)
}

func (v *evidenceValidator) expectPassCommand(gate string, cmd CommandSpec) error {
	if v.index >= len(v.events) {
		return fmt.Errorf("evidence ended before command %s", gate)
	}
	e := v.events[v.index]
	v.index++
	if e.Event != "command_finished" || e.Gate != gate || e.Status != StatusPass || e.ExitCode == nil || *e.ExitCode != 0 || !reflect.DeepEqual(e.Argv, cmd.Argv) || e.Cwd != cmd.Cwd {
		return fmt.Errorf("command evidence mismatch for %s", gate)
	}
	if err := validateCommandEnvironmentEvidence(e, cmd); err != nil {
		return fmt.Errorf("command evidence mismatch for %s: %w", gate, err)
	}
	return nil
}

func (v *evidenceValidator) expectPassFinish(gate string) error {
	if v.index >= len(v.events) {
		return fmt.Errorf("evidence ended before gate_finished %s", gate)
	}
	finish := v.events[v.index]
	v.index++
	if finish.Event != "gate_finished" || finish.Gate != gate || finish.Status != StatusPass || finish.Reason != nil {
		return fmt.Errorf("gate_finished mismatch for %s", gate)
	}
	return nil
}

func (v *evidenceValidator) validateProjectGates(policy Policy) error {
	for _, gate := range policy.Gates {
		if err := v.expectProjectGateStart(gate.ID); err != nil {
			return err
		}
		if err := v.validateProjectGate(gate); err != nil {
			return err
		}
	}
	return nil
}

func (v *evidenceValidator) expectProjectGateStart(gate string) error {
	if v.index >= len(v.events) || v.events[v.index].Event != "gate_started" || v.events[v.index].Gate != gate {
		return fmt.Errorf("event %d: missing project gate start %s", v.index, gate)
	}
	v.index++
	return nil
}

func (v *evidenceValidator) validateProjectGate(gate GatePolicy) error {
	switch gate.Mode {
	case GateModeNotApplicable:
		return v.validateNotApplicableGate(gate)
	case GateModeCommand:
		if err := v.expectPassCommand(gate.ID, *gate.Command); err != nil {
			return err
		}
		return v.expectPassFinish(gate.ID)
	case GateModeCoverage:
		return v.validateCoverageGate(gate)
	default:
		return fmt.Errorf("unsupported policy mode %s", gate.Mode)
	}
}

func (v *evidenceValidator) validateNotApplicableGate(gate GatePolicy) error {
	if v.index >= len(v.events) {
		return fmt.Errorf("evidence ended at project gate %s", gate.ID)
	}
	e := v.events[v.index]
	v.index++
	if e.Event != "gate_finished" || e.Gate != gate.ID || e.Status != StatusNotApplicable || e.Reason == nil || gate.Reason == nil || *e.Reason != *gate.Reason {
		return fmt.Errorf("project gate %s NOT_APPLICABLE evidence mismatch", gate.ID)
	}
	return nil
}

func (v *evidenceValidator) validateCoverageGate(gate GatePolicy) error {
	if err := v.validateCoverageCommand(gate); err != nil {
		return err
	}
	if err := v.validateCoverageMeasurement(gate); err != nil {
		return err
	}
	return v.validateCoverageFinish()
}

func (v *evidenceValidator) validateCoverageCommand(gate GatePolicy) error {
	if v.index >= len(v.events) {
		return fmt.Errorf("coverage command evidence missing")
	}
	e := v.events[v.index]
	v.index++
	if e.Event != "command_finished" || e.Gate != gate.ID || e.Status != StatusPass || e.ExitCode == nil || *e.ExitCode != 0 || !reflect.DeepEqual(e.Argv, gate.Command.Argv) || e.Cwd != gate.Command.Cwd {
		return fmt.Errorf("coverage command evidence mismatch")
	}
	if err := validateCommandEnvironmentEvidence(e, *gate.Command); err != nil {
		return fmt.Errorf("coverage command evidence mismatch: %w", err)
	}
	return nil
}

func (v *evidenceValidator) validateCoverageMeasurement(gate GatePolicy) error {
	if v.index >= len(v.events) {
		return fmt.Errorf("coverage evidence missing")
	}
	e := v.events[v.index]
	v.index++
	if !coverageEvidenceMatchesPolicy(e, gate) {
		return fmt.Errorf("coverage evidence does not match policy")
	}
	computed := float64(*e.CoveredLines) / float64(*e.TotalLines) * 100
	if math.Abs(computed-*e.ValuePercent) > 1e-9 {
		return fmt.Errorf("coverage evidence arithmetic mismatch: got %.12f calculated %.12f", *e.ValuePercent, computed)
	}
	if !CoveragePass(*e.ValuePercent, *gate.ThresholdPercent) {
		return fmt.Errorf("coverage evidence does not pass policy")
	}
	return nil
}

func coverageEvidenceMatchesPolicy(e EvidenceEvent, gate GatePolicy) bool {
	return e.Event == "coverage_measured" &&
		e.Gate == "coverage" &&
		e.Status == StatusPass &&
		e.Adapter == gate.Adapter &&
		e.Report == gate.Report &&
		e.Metric == CoverageMetricLinePercent &&
		e.Operator == gate.Operator &&
		e.ThresholdPercent != nil && gate.ThresholdPercent != nil &&
		*e.ThresholdPercent == *gate.ThresholdPercent &&
		e.CoveredLines != nil && e.TotalLines != nil && e.ValuePercent != nil
}

func (v *evidenceValidator) validateCoverageFinish() error {
	if v.index >= len(v.events) {
		return fmt.Errorf("coverage gate_finished missing")
	}
	finish := v.events[v.index]
	v.index++
	if finish.Event != "gate_finished" || finish.Gate != "coverage" || finish.Status != StatusPass || finish.Reason != nil {
		return fmt.Errorf("coverage gate_finished mismatch")
	}
	return nil
}
