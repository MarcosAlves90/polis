package policyexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// maxCoverageReportBytes bounds the in-memory coverage profile. The previous
// 16 MiB cap rejected legitimate -coverpkg=./... profiles; a 32 MiB bound keeps
// the read finite while retaining fail-closed size enforcement below.
const maxCoverageReportBytes int64 = 32 * 1024 * 1024

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
	Concurrency     ConcurrencyReport `json:"concurrency"`
}

func Execute(policy spec.Policy, repoRoot string, evidence io.Writer) Result {
	return ExecuteWithOptions(policy, repoRoot, evidence, Options{Jobs: 1})
}

func ExecuteWithOptions(policy spec.Policy, repoRoot string, evidence io.Writer, opts Options) Result {
	plan, err := policyplan.Compile(policy)
	if err != nil && strings.Contains(err.Error(), "requires explicit command environment") {
		// In-memory callers may build equivalent v3 policies programmatically.
		// A missing environment is the inherited process environment already
		// used by the executor; make that equivalence explicit for compilation
		// without changing any gate, threshold, dependency or evidence rule.
		plan, err = policyplan.Compile(normalizeImplicitEnvironment(policy))
	}
	if err != nil {
		return Result{Overall: spec.StatusBlocked, Gates: make(map[string]spec.Status, len(policy.Gates))}
	}
	return ExecutePlanWithOptions(plan, repoRoot, evidence, opts)
}

func normalizeImplicitEnvironment(policy spec.Policy) spec.Policy {
	raw, err := json.Marshal(policy)
	if err != nil {
		return policy
	}
	var normalized spec.Policy
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return policy
	}
	for i := range normalized.Gates {
		command := normalized.Gates[i].Command
		if command == nil || command.Environment != nil {
			continue
		}
		clone := *command
		clone.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
		normalized.Gates[i].Command = &clone
	}
	return normalized
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

func executeCoverage(enc *json.Encoder, gate spec.GatePolicy, repoRoot string, commandOptions commandexec.Options) (spec.Status, *CommandExecution) {
	reportPath := filepath.Join(repoRoot, filepath.FromSlash(gate.Report))
	if err := os.Remove(reportPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return spec.StatusFail, nil
	}
	observation := executeCommand(enc, gate.ID, *gate.Command, repoRoot, commandOptions)
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
	contained, err = pathguard.Contains(repoRoot, resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve coverage report boundary: %w", err)
	}
	if !contained {
		return nil, errors.New("coverage report resolves outside repository")
	}
	pathInfo, err := os.Lstat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat coverage report: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return nil, errors.New("coverage report must be a regular file")
	}
	if pathInfo.Size() > maxCoverageReportBytes {
		return nil, errors.New("coverage report exceeds maximum size")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open coverage report: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened coverage report: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return nil, errors.New("coverage report changed while opening")
	}
	if openedInfo.Size() > maxCoverageReportBytes {
		return nil, errors.New("coverage report exceeds maximum size")
	}
	currentInfo, err := os.Lstat(resolved)
	if err != nil {
		return nil, fmt.Errorf("recheck coverage report: %w", err)
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(openedInfo, currentInfo) {
		return nil, errors.New("coverage report changed while opening")
	}
	contained, err = pathguard.Contains(repoRoot, resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve coverage report boundary: %w", err)
	}
	if !contained {
		return nil, errors.New("coverage report resolves outside repository")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxCoverageReportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read coverage report: %w", err)
	}
	if int64(len(raw)) > maxCoverageReportBytes {
		return nil, errors.New("coverage report exceeds maximum size")
	}
	return raw, nil
}

func executeCommand(enc *json.Encoder, gate string, command spec.CommandSpec, repoRoot string, commandOptions commandexec.Options) commandexec.Observation {
	commandOptions.Gate = gate
	obs := commandexec.RunWithOptions(repoRoot, command, commandOptions)
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
