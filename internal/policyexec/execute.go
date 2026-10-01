package policyexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const maxCoverageReportBytes = 16 * 1024 * 1024

type CommandExecution struct {
	Argv        []string
	Cwd         string
	Observation commandexec.Observation
}

type Result struct {
	Overall         spec.Status
	Gates           map[string]spec.Status
	CommandFailures map[string]CommandExecution
	Outcomes        map[string]Outcome
}

func Execute(policy spec.Policy, repoRoot string, evidence io.Writer) Result {
	plan, err := policyplan.Compile(policy)
	if err != nil {
		return Result{Overall: spec.StatusBlocked, Gates: make(map[string]spec.Status, len(policy.Gates))}
	}
	return ExecutePlan(plan, repoRoot, evidence)
}

func ExecutePlan(plan policyplan.Plan, repoRoot string, evidence io.Writer) Result {
	return ExecutePlanWithOptions(plan, repoRoot, evidence, Options{Jobs: 1})
}

func planGateDeferred(plan policyplan.Plan, gateID string) bool {
	for _, deferred := range plan.DeferredGates {
		if deferred == gateID {
			return true
		}
	}
	return false
}

func executeCoverage(enc *json.Encoder, gate spec.GatePolicy, repoRoot string) (spec.Status, *CommandExecution) {
	reportPath := filepath.Join(repoRoot, filepath.FromSlash(gate.Report))
	if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return spec.StatusFail, nil
	}
	observation := executeCommand(enc, gate.ID, *gate.Command, repoRoot)
	execution := &CommandExecution{Argv: append([]string(nil), gate.Command.Argv...), Cwd: gate.Command.Cwd, Observation: observation}
	if observation.Status != spec.StatusPass {
		return observation.Status, execution
	}
	raw, err := readCoverageReport(repoRoot, reportPath)
	if err != nil {
		return spec.StatusFail, execution
	}
	metric, err := spec.ParseCoverage(gate.Adapter, raw)
	if err != nil {
		return spec.StatusFail, execution
	}
	status := spec.StatusFail
	if spec.CoveragePass(metric.Percent, *gate.ThresholdPercent) {
		status = spec.StatusPass
	}
	covered, total, value, threshold := metric.CoveredLines, metric.TotalLines, metric.Percent, *gate.ThresholdPercent
	_ = enc.Encode(spec.EvidenceEvent{
		Event:            "coverage_measured",
		Gate:             "coverage",
		Status:           status,
		Adapter:          gate.Adapter,
		Report:           gate.Report,
		Metric:           spec.CoverageMetricLinePercent,
		CoveredLines:     &covered,
		TotalLines:       &total,
		ValuePercent:     &value,
		Operator:         gate.Operator,
		ThresholdPercent: &threshold,
	})
	return status, execution
}

func readCoverageReport(repoRoot, reportPath string) ([]byte, error) {
	contained, err := pathguard.Contains(repoRoot, reportPath)
	if err != nil {
		return nil, fmt.Errorf("resolve coverage report boundary: %w", err)
	}
	if !contained {
		return nil, errors.New("coverage report resolves outside repository")
	}
	resolved, err := filepath.EvalSymlinks(reportPath)
	if err != nil {
		return nil, fmt.Errorf("resolve coverage report: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat coverage report: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("coverage report must be a regular file")
	}
	if info.Size() > maxCoverageReportBytes {
		return nil, errors.New("coverage report exceeds maximum size")
	}
	return os.ReadFile(resolved)
}

func executeCommand(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string) commandexec.Observation {
	obs := commandexec.Run(repoRoot, command)
	exitCode, duration := obs.ExitCode, obs.DurationMS
	stdoutBytes, stderrBytes := obs.StdoutBytes, obs.StderrBytes
	stdoutTruncated, stderrTruncated := obs.StdoutTruncated, obs.StderrTruncated
	event := spec.EvidenceEvent{
		Event:           "command_finished",
		Gate:            gate,
		Status:          obs.Status,
		Argv:            append([]string(nil), command.Argv...),
		Cwd:             command.Cwd,
		ExitCode:        &exitCode,
		DurationMS:      &duration,
		StdoutBytes:     &stdoutBytes,
		StderrBytes:     &stderrBytes,
		StdoutSHA256:    obs.StdoutSHA256,
		StderrSHA256:    obs.StderrSHA256,
		StdoutTruncated: &stdoutTruncated,
		StderrTruncated: &stderrTruncated,
	}
	if command.Environment != nil {
		event.EnvironmentMode = command.Environment.Mode
		event.EnvironmentPass = append([]string(nil), command.Environment.Pass...)
	}
	_ = enc.Encode(event)
	return obs
}

func combine(current, next spec.Status) spec.Status {
	if current == spec.StatusFail || next == spec.StatusFail {
		return spec.StatusFail
	}
	if current == spec.StatusBlocked || next == spec.StatusBlocked {
		return spec.StatusBlocked
	}
	return spec.StatusPass
}
