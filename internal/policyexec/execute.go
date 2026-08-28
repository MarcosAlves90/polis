package policyexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/polis-dev/polis-v4/internal/commandexec"
	"github.com/polis-dev/polis-v4/spec"
)

const maxCoverageReportBytes = 16 * 1024 * 1024

type Result struct {
	Overall spec.Status
	Gates   map[string]spec.Status
}

func Execute(policy spec.Policy, repoRoot string, evidence io.Writer) Result {
	result := Result{Overall: spec.StatusPass, Gates: make(map[string]spec.Status, len(policy.Gates))}
	if err := policy.Validate(); err != nil {
		result.Overall = spec.StatusBlocked
		return result
	}
	enc := json.NewEncoder(evidence)
	enc.SetEscapeHTML(false)
	for _, gate := range policy.Gates {
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_started", Gate: gate.ID})
		status := spec.StatusPass
		switch gate.Mode {
		case spec.GateModeNotApplicable:
			status = spec.StatusNotApplicable
			_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate.ID, Status: status, Reason: gate.Reason})
			result.Gates[gate.ID] = status
			continue
		case spec.GateModeCoverage:
			status = executeCoverage(enc, gate, repoRoot)
		default:
			status = executeCommand(enc, gate.ID, *gate.Command, repoRoot)
		}
		_ = enc.Encode(spec.EvidenceEvent{Event: "gate_finished", Gate: gate.ID, Status: status, Reason: blockedReason(status)})
		result.Gates[gate.ID] = status
		result.Overall = combine(result.Overall, status)
	}
	return result
}

func executeCoverage(enc *json.Encoder, gate spec.GatePolicy, repoRoot string) spec.Status {
	reportPath := filepath.Join(repoRoot, filepath.FromSlash(gate.Report))
	if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return spec.StatusFail
	}
	status := executeCommand(enc, gate.ID, *gate.Command, repoRoot)
	if status != spec.StatusPass {
		return status
	}
	raw, err := readCoverageReport(repoRoot, reportPath)
	if err != nil {
		return spec.StatusFail
	}
	var metric spec.CoverageMetric
	switch gate.Adapter {
	case spec.CoverageAdapterGoCoverProfileV1:
		metric, err = spec.ParseGoCoverProfile(raw)
	default:
		err = fmt.Errorf("unsupported coverage adapter %q", gate.Adapter)
	}
	if err != nil {
		return spec.StatusFail
	}
	status = spec.StatusFail
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
	return status
}

func readCoverageReport(repoRoot, reportPath string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(reportPath)
	if err != nil {
		return nil, fmt.Errorf("resolve coverage report: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, errors.New("coverage report resolves outside repository")
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

func executeCommand(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string) spec.Status {
	obs := commandexec.Run(repoRoot, command)
	exitCode, duration := obs.ExitCode, obs.DurationMS
	stdout, stderr := obs.Stdout, obs.Stderr
	_ = enc.Encode(spec.EvidenceEvent{
		Event:      "command_finished",
		Gate:       gate,
		Status:     obs.Status,
		Argv:       append([]string(nil), command.Argv...),
		Cwd:        command.Cwd,
		ExitCode:   &exitCode,
		DurationMS: &duration,
		Stdout:     &stdout,
		Stderr:     &stderr,
	})
	return obs.Status
}

func blockedReason(status spec.Status) *string {
	if status != spec.StatusBlocked {
		return nil
	}
	reason := "command could not be started in the declared environment"
	return &reason
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
