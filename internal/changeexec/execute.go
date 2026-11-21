package changeexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func ExecuteBaseline(contract spec.ChangeContract, repoRoot string, evidence io.Writer) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	if !contract.RequiresBaselineProof() {
		return errors.New("baseline regression execution requires a baseline proof contract")
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	if contract.RequiresGreenGreen() {
		return runPassGate(enc, "regression", *contract.Regression.Command, repoRoot)
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
	obs := commandexec.Run(repoRoot, *contract.Regression.Command)
	writeObservation(enc, "regression", *contract.Regression.Command, obs)
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
	if err := contract.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	if contract.RequiresRedGreen() || contract.RequiresGreenGreen() {
		if err := runPassGate(enc, "regression", *contract.Regression.Command, repoRoot, "behavior", "affected", "project gate validation"); err != nil {
			return err
		}
	} else {
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: "regression"})
		reason := spec.RegressionReasonNotDefect
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: "regression", Status: spec.StatusNotApplicable, Reason: &reason})
	}
	if err := runPassGate(enc, "behavior", contract.Behavior, repoRoot, "affected", "project gate validation"); err != nil {
		return err
	}
	if err := runPassGate(enc, "affected", contract.Affected, repoRoot, "project gate validation"); err != nil {
		return err
	}
	return nil
}

func runPassGate(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string, notRun ...string) error {
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: gate})
	obs := commandexec.Run(repoRoot, command)
	writeObservation(enc, gate, command, obs)
	reason := commandexec.BlockedReason(obs)
	if obs.Status == spec.StatusBlocked {
		safeReason := safeBlockedReason(obs)
		reason = &safeReason
	}
	_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate, Status: obs.Status, Reason: reason})
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
			Status: string(obs.Status), ExitCode: obs.ExitCode, DurationMS: obs.DurationMS,
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
