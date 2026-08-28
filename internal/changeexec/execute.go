package changeexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v4/internal/commandexec"
	"github.com/MarcosAlves90/polis/v4/spec"
)

func ExecuteBaseline(contract spec.ChangeContract, repoRoot string, evidence io.Writer) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	if contract.Kind != spec.ChangeKindDefect {
		return errors.New("baseline regression execution is only valid for defects")
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
	obs := commandexec.Run(repoRoot, *contract.Regression.Command)
	writeObservation(enc, "regression", *contract.Regression.Command, obs)
	combined := obs.Stdout + "\n" + obs.Stderr
	if obs.ExitCode != *contract.Regression.BaselineExitCode {
		return fmt.Errorf("regression baseline exit code %d, want %d", obs.ExitCode, *contract.Regression.BaselineExitCode)
	}
	for _, token := range contract.Regression.BaselineOutputContains {
		if !strings.Contains(combined, token) {
			return fmt.Errorf("regression baseline output missing token %q", token)
		}
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusPass})
	return nil
}

func ExecuteTarget(contract spec.ChangeContract, repoRoot string, evidence io.Writer) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	if contract.Kind == spec.ChangeKindDefect {
		if err := runPassGate(enc, "regression", *contract.Regression.Command, repoRoot); err != nil {
			return err
		}
	} else {
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
		reason := spec.RegressionReasonNotDefect
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusNotApplicable, Reason: &reason})
	}
	if err := runPassGate(enc, "behavior", contract.Behavior, repoRoot); err != nil {
		return err
	}
	if err := runPassGate(enc, "affected", contract.Affected, repoRoot); err != nil {
		return err
	}
	return nil
}

func runPassGate(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string) error {
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: gate})
	obs := commandexec.Run(repoRoot, command)
	writeObservation(enc, gate, command, obs)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate, Status: obs.Status, Reason: blockedReason(obs.Status)})
	if obs.Status != spec.StatusPass {
		return fmt.Errorf("change gate %s %s", gate, obs.Status)
	}
	return nil
}

func writeObservation(enc *json.Encoder, gate string, command spec.CommandSpec, obs commandexec.Observation) {
	exit, duration, stdout, stderr := obs.ExitCode, obs.DurationMS, obs.Stdout, obs.Stderr
	_ = enc.Encode(spec.EvidenceEvent{Event: "command_finished", Gate: gate, Status: obs.Status, Argv: append([]string(nil), command.Argv...), Cwd: command.Cwd, ExitCode: &exit, DurationMS: &duration, Stdout: &stdout, Stderr: &stderr})
}

func blockedReason(status spec.Status) *string {
	if status != spec.StatusBlocked {
		return nil
	}
	reason := "command could not be started in the declared environment"
	return &reason
}
