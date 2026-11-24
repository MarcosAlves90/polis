package changeexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// Hooks are presentation-only. They never change command execution, evidence
// or the resulting gate status.
type Hooks struct {
	Context  context.Context
	OnOutput commandexec.OutputObserver
	OnStart  func(gate string, command spec.CommandSpec)
	OnResult func(gate string, command spec.CommandSpec, status spec.Status, durationMS int64)
}

func ExecuteBaseline(contract spec.ChangeContract, repoRoot string, evidence io.Writer) error {
	return ExecuteBaselineWithHooks(contract, repoRoot, evidence, Hooks{})
}

func ExecuteBaselineWithHooks(contract spec.ChangeContract, repoRoot string, evidence io.Writer, hooks Hooks) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	if !contract.RequiresBaselineProof() {
		return errors.New("baseline regression execution requires a baseline proof contract")
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	if contract.RequiresGreenGreen() {
		return runPassGate(enc, "regression", *contract.Regression.Command, repoRoot, hooks, "target regression", "behavior", "affected", "project gate validation")
	}
	command := *contract.Regression.Command
	hooks.start("regression", command)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
	obs := commandexec.RunWithOptions(repoRoot, command, commandexec.Options{Context: hooks.Context, Gate: "regression", OnOutput: hooks.OnOutput})
	writeObservation(enc, "regression", command, obs)
	hooks.result("regression", command, obs.Status, obs.DurationMS)
	if obs.Status == spec.StatusBlocked {
		reason := safeBlockedReason(obs)
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusBlocked, Reason: &reason})
		return fmt.Errorf("regression prerequisite blocked: %s", reason)
	}
	combined := obs.Stdout + "\n" + obs.Stderr
	if obs.ExitCode != *contract.Regression.BaselineExitCode {
		return fmt.Errorf("regression baseline exit code %d, want %d", obs.ExitCode, *contract.Regression.BaselineExitCode)
	}
	for _, token := range contract.Regression.BaselineOutputContains {
		if obs.StdoutTruncated || obs.StderrTruncated {
			return errors.New("regression baseline output exceeded bounded capture; oracle cannot be proven")
		}
		if !strings.Contains(combined, token) {
			return fmt.Errorf("regression baseline output missing token %q", token)
		}
		index := 0
		for i, candidate := range contract.Regression.BaselineOutputContains {
			if candidate == token {
				index = i
				break
			}
		}
		_ = enc.Encode(spec.EvidenceEvent{Event: "oracle_checked", Gate: "regression", Status: spec.StatusPass, Oracle: "baseline_output_contains", OracleIndex: &index})
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusPass})
	return nil
}

func ExecuteTarget(contract spec.ChangeContract, repoRoot string, evidence io.Writer) error {
	return ExecuteTargetWithHooks(contract, repoRoot, evidence, Hooks{})
}

func ExecuteTargetWithHooks(contract spec.ChangeContract, repoRoot string, evidence io.Writer, hooks Hooks) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	if contract.RequiresRedGreen() || contract.RequiresGreenGreen() {
		if err := runPassGate(enc, "regression", *contract.Regression.Command, repoRoot, hooks, "behavior", "affected", "project gate validation"); err != nil {
			return err
		}
	} else {
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
		reason := spec.RegressionReasonNotDefect
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusNotApplicable, Reason: &reason})
	}
	if err := runPassGate(enc, "behavior", contract.Behavior, repoRoot, hooks, "affected", "project gate validation"); err != nil {
		return err
	}
	if err := runPassGate(enc, "affected", contract.Affected, repoRoot, hooks, "project gate validation"); err != nil {
		return err
	}
	return nil
}

func runPassGate(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string, hooks Hooks, notRun ...string) error {
	hooks.start(gate, command)
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: gate})
	obs := commandexec.RunWithOptions(repoRoot, command, commandexec.Options{Context: hooks.Context, Gate: gate, OnOutput: hooks.OnOutput})
	writeObservation(enc, gate, command, obs)
	reason := commandexec.BlockedReason(obs)
	if obs.Status == spec.StatusBlocked {
		safeReason := safeBlockedReason(obs)
		reason = &safeReason
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate, Status: obs.Status, Reason: reason})
	hooks.result(gate, command, obs.Status, obs.DurationMS)
	if obs.Status != spec.StatusPass {
		stdout, stdoutPreviewTruncated := diagnostic.BoundProcessOutput(obs.Stdout)
		stderr, stderrPreviewTruncated := diagnostic.BoundProcessOutput(obs.Stderr)
		if obs.Status == spec.StatusBlocked {
			// Blocked output can echo secret-bearing prerequisite details; keep metadata only.
			stdout, stderr = "", ""
			stdoutPreviewTruncated, stderrPreviewTruncated = false, false
		}
		commandReport := diagnostic.Command{
			Gate: gate, Argv: append([]string(nil), command.Argv...), Cwd: command.Cwd,
			Status: string(obs.Status), ExitCode: obs.ExitCode, DurationMS: obs.DurationMS, TimedOut: obs.TimedOut,
			StdoutContext: stdout, StderrContext: stderr, StdoutBytes: obs.StdoutBytes, StderrBytes: obs.StderrBytes,
			StdoutSHA256: obs.StdoutSHA256, StderrSHA256: obs.StderrSHA256,
			StdoutTruncated: obs.StdoutTruncated || stdoutPreviewTruncated,
			StderrTruncated: obs.StderrTruncated || stderrPreviewTruncated,
		}
		if obs.Status == spec.StatusBlocked {
			commandReport.Prerequisite = safeBlockedCategory(obs)
		}
		report := diagnostic.Report{
			Stage: "change contract validation", Condition: "configured change gate did not pass",
			GateStatuses: map[string]string{gate: string(obs.Status)}, Commands: []diagnostic.Command{commandReport}, NotRun: notRun,
		}
		if obs.Status == spec.StatusBlocked {
			return &diagnostic.Error{Summary: fmt.Sprintf("change gate %s prerequisite blocked: %s", gate, safeBlockedReason(obs)), Report: diagnostic.ClassifyGateFailure(report)}
		}
		return &diagnostic.Error{Summary: fmt.Sprintf("change gate %s %s", gate, obs.Status), Report: diagnostic.ClassifyGateFailure(report)}
	}
	return nil
}

func (h Hooks) start(gate string, command spec.CommandSpec) {
	if h.OnStart != nil {
		h.OnStart(gate, command)
	}
}

func (h Hooks) result(gate string, command spec.CommandSpec, status spec.Status, durationMS int64) {
	if h.OnResult != nil {
		h.OnResult(gate, command, status, durationMS)
	}
}

func safeBlockedReason(obs commandexec.Observation) string {
	category := safeBlockedCategory(obs)
	if category == "" {
		return ""
	}
	return category + "; intended checks did not run"
}

func safeBlockedCategory(obs commandexec.Observation) string {
	if obs.Status != spec.StatusBlocked {
		return ""
	}
	category := "command prerequisite unavailable"
	for _, prefix := range []string{"missing executable", "missing dependency", "missing environment condition"} {
		if strings.HasPrefix(obs.Prerequisite, prefix+" ") {
			category = prefix
			break
		}
	}
	return category + " (details omitted)"
}

func writeObservation(enc *json.Encoder, gate string, command spec.CommandSpec, obs commandexec.Observation) {
	exit, duration := obs.ExitCode, obs.DurationMS
	stdoutBytes, stderrBytes := obs.StdoutBytes, obs.StderrBytes
	stdoutTruncated, stderrTruncated := obs.StdoutTruncated, obs.StderrTruncated
	event := spec.EvidenceEvent{
		Event: "command_finished", Gate: gate, Status: obs.Status, Argv: append([]string(nil), command.Argv...), Cwd: command.Cwd,
		ExitCode: &exit, DurationMS: &duration,
		StdoutBytes: &stdoutBytes, StderrBytes: &stderrBytes, StdoutSHA256: obs.StdoutSHA256, StderrSHA256: obs.StderrSHA256,
		StdoutTruncated: &stdoutTruncated, StderrTruncated: &stderrTruncated,
	}
	if command.Environment != nil {
		event.EnvironmentMode = command.Environment.Mode
		event.EnvironmentPass = append([]string(nil), command.Environment.Pass...)
	}
	_ = enc.Encode(event)
}
