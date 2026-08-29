package spec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Status string

const (
	StatusPass          Status = "PASS"
	StatusFail          Status = "FAIL"
	StatusBlocked       Status = "BLOCKED"
	StatusNotApplicable Status = "NOT_APPLICABLE"
)

type EvidenceEvent struct {
	Event            string   `json:"event"`
	Gate             string   `json:"gate"`
	Status           Status   `json:"status,omitempty"`
	Argv             []string `json:"argv,omitempty"`
	Cwd              string   `json:"cwd,omitempty"`
	ExitCode         *int     `json:"exit_code,omitempty"`
	DurationMS       *int64   `json:"duration_ms,omitempty"`
	Stdout           *string  `json:"stdout,omitempty"`
	Stderr           *string  `json:"stderr,omitempty"`
	Reason           *string  `json:"reason,omitempty"`
	Adapter          string   `json:"adapter,omitempty"`
	Report           string   `json:"report,omitempty"`
	Metric           string   `json:"metric,omitempty"`
	CoveredLines     *int     `json:"covered_lines,omitempty"`
	TotalLines       *int     `json:"total_lines,omitempty"`
	ValuePercent     *float64 `json:"value_percent,omitempty"`
	Operator         string   `json:"operator,omitempty"`
	ThresholdPercent *float64 `json:"threshold_percent,omitempty"`
}

func DecodeEvidence(raw []byte) ([]EvidenceEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var events []EvidenceEvent
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("evidence line %d is empty", lineNo)
		}
		event, err := decodeEvidenceEvent(line)
		if err != nil {
			return nil, fmt.Errorf("evidence line %d: %w", lineNo, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read evidence: %w", err)
	}
	return events, nil
}

func decodeEvidenceEvent(raw []byte) (EvidenceEvent, error) {
	fields, err := decodeEvidenceFields(raw)
	if err != nil {
		return EvidenceEvent{}, err
	}
	eventName, err := requiredStringField(fields, "event")
	if err != nil {
		return EvidenceEvent{}, err
	}
	gate, err := requiredStringField(fields, "gate")
	if err != nil {
		return EvidenceEvent{}, err
	}
	if !IsEvidenceGate(gate) {
		return EvidenceEvent{}, fmt.Errorf("unknown gate %q", gate)
	}

	e := EvidenceEvent{Event: eventName, Gate: gate}
	allowed := map[string]bool{"event": true, "gate": true}
	if err := decodeEventPayload(&e, fields, allowed); err != nil {
		return EvidenceEvent{}, err
	}
	if err := rejectForbiddenEvidenceFields(fields, allowed, eventName); err != nil {
		return EvidenceEvent{}, err
	}
	return e, nil
}

func decodeEvidenceFields(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errors.New("event contains trailing JSON value")
	}
	return fields, nil
}

func decodeEventPayload(e *EvidenceEvent, fields map[string]json.RawMessage, allowed map[string]bool) error {
	switch e.Event {
	case "gate_started":
		return nil
	case "gate_finished":
		return decodeGateFinished(e, fields, allowed)
	case "command_finished":
		return decodeCommandFinished(e, fields, allowed)
	case "coverage_measured":
		return decodeCoverageMeasured(e, fields, allowed)
	default:
		return fmt.Errorf("unknown event %q", e.Event)
	}
}

func decodeGateFinished(e *EvidenceEvent, fields map[string]json.RawMessage, allowed map[string]bool) error {
	allowed["status"] = true
	status, err := decodeStatus(fields["status"])
	if err != nil {
		return err
	}
	e.Status = status
	if status == StatusBlocked || status == StatusNotApplicable {
		return decodeGateFinishedReason(e, fields, allowed)
	}
	if _, ok := fields["reason"]; ok {
		return errors.New("PASS/FAIL gate_finished must not contain reason")
	}
	return nil
}

func decodeGateFinishedReason(e *EvidenceEvent, fields map[string]json.RawMessage, allowed map[string]bool) error {
	allowed["reason"] = true
	reason, err := requiredStringField(fields, "reason")
	if err != nil || strings.TrimSpace(reason) == "" {
		return errors.New("BLOCKED/NOT_APPLICABLE gate_finished requires non-empty reason")
	}
	e.Reason = &reason
	return nil
}

func decodeCommandFinished(e *EvidenceEvent, fields map[string]json.RawMessage, allowed map[string]bool) error {
	required := []string{"status", "argv", "cwd", "exit_code", "duration_ms", "stdout", "stderr"}
	if err := requireEvidenceFields(fields, allowed, required); err != nil {
		return err
	}
	status, err := decodeCommandStatus(fields["status"])
	if err != nil {
		return err
	}
	e.Status = status
	if err := decodeCommandArgv(e, fields["argv"]); err != nil {
		return err
	}
	if err := decodeCommandCwd(e, fields); err != nil {
		return err
	}
	if err := decodeCommandNumbers(e, fields); err != nil {
		return err
	}
	return decodeCommandOutput(e, fields)
}

func decodeCommandStatus(raw json.RawMessage) (Status, error) {
	status, err := decodeStatus(raw)
	if err != nil {
		return "", err
	}
	if status != StatusPass && status != StatusFail && status != StatusBlocked {
		return "", errors.New("command_finished status must be PASS, FAIL, or BLOCKED")
	}
	return status, nil
}

func decodeCommandArgv(e *EvidenceEvent, raw json.RawMessage) error {
	if err := json.Unmarshal(raw, &e.Argv); err != nil || len(e.Argv) == 0 {
		return errors.New("command_finished argv must be a non-empty string array")
	}
	for _, arg := range e.Argv {
		if arg == "" {
			return errors.New("command_finished argv values must not be empty")
		}
	}
	return nil
}

func decodeCommandCwd(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	cwd, err := requiredStringField(fields, "cwd")
	if err != nil {
		return err
	}
	if err := ValidateRepoRelativePath(cwd); err != nil {
		return fmt.Errorf("invalid command_finished cwd: %w", err)
	}
	e.Cwd = cwd
	return nil
}

func decodeCommandNumbers(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	var exitCode int
	if err := json.Unmarshal(fields["exit_code"], &exitCode); err != nil || exitCode < -1 {
		return errors.New("command_finished exit_code must be an integer >= -1")
	}
	e.ExitCode = &exitCode
	var duration int64
	if err := json.Unmarshal(fields["duration_ms"], &duration); err != nil || duration < 0 {
		return errors.New("command_finished duration_ms must be an integer >= 0")
	}
	e.DurationMS = &duration
	return nil
}

func decodeCommandOutput(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	var stdout, stderr string
	if err := json.Unmarshal(fields["stdout"], &stdout); err != nil {
		return errors.New("command_finished stdout must be a string")
	}
	if err := json.Unmarshal(fields["stderr"], &stderr); err != nil {
		return errors.New("command_finished stderr must be a string")
	}
	e.Stdout, e.Stderr = &stdout, &stderr
	return nil
}

func decodeCoverageMeasured(e *EvidenceEvent, fields map[string]json.RawMessage, allowed map[string]bool) error {
	if e.Gate != "coverage" {
		return errors.New("coverage_measured gate must be coverage")
	}
	required := []string{"status", "adapter", "report", "metric", "covered_lines", "total_lines", "value_percent", "operator", "threshold_percent"}
	if err := requireEvidenceFields(fields, allowed, required); err != nil {
		return err
	}
	if err := decodeCoverageIdentity(e, fields); err != nil {
		return err
	}
	if err := decodeCoverageCounts(e, fields); err != nil {
		return err
	}
	if err := decodeCoveragePercentages(e, fields); err != nil {
		return err
	}
	return validateCoverageMeasurement(e)
}

func decodeCoverageIdentity(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	status, err := decodeStatus(fields["status"])
	if err != nil || (status != StatusPass && status != StatusFail) {
		return errors.New("coverage_measured status must be PASS or FAIL")
	}
	e.Status = status
	adapter, err := requiredStringField(fields, "adapter")
	if err != nil || adapter != CoverageAdapterGoCoverProfileV1 {
		return errors.New("coverage_measured adapter is unsupported")
	}
	e.Adapter = adapter
	report, err := requiredStringField(fields, "report")
	if err != nil {
		return err
	}
	if err := ValidateRepoRelativePath(report); err != nil {
		return fmt.Errorf("invalid coverage_measured report: %w", err)
	}
	e.Report = report
	metric, err := requiredStringField(fields, "metric")
	if err != nil || metric != CoverageMetricLinePercent {
		return errors.New("coverage_measured metric must be line_coverage_percent")
	}
	e.Metric = metric
	operator, err := requiredStringField(fields, "operator")
	if err != nil || operator != CoverageOperatorGreaterThan {
		return errors.New("coverage_measured operator must be >")
	}
	e.Operator = operator
	return nil
}

func decodeCoverageCounts(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	var covered, total int
	if err := json.Unmarshal(fields["covered_lines"], &covered); err != nil || covered < 0 {
		return errors.New("coverage_measured covered_lines must be an integer >= 0")
	}
	if err := json.Unmarshal(fields["total_lines"], &total); err != nil || total < 1 || covered > total {
		return errors.New("coverage_measured total_lines must be >= 1 and >= covered_lines")
	}
	e.CoveredLines, e.TotalLines = &covered, &total
	return nil
}

func decodeCoveragePercentages(e *EvidenceEvent, fields map[string]json.RawMessage) error {
	var value, threshold float64
	if err := json.Unmarshal(fields["value_percent"], &value); err != nil || invalidPercent(value, 0) {
		return errors.New("coverage_measured value_percent must be between 0 and 100")
	}
	if err := json.Unmarshal(fields["threshold_percent"], &threshold); err != nil || invalidPercent(threshold, MinimumCoverageThreshold) {
		return errors.New("coverage_measured threshold_percent is invalid")
	}
	e.ValuePercent, e.ThresholdPercent = &value, &threshold
	return nil
}

func invalidPercent(value, minimum float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < minimum || value > 100
}

func validateCoverageMeasurement(e *EvidenceEvent) error {
	computed := float64(*e.CoveredLines) / float64(*e.TotalLines) * 100
	if math.Abs(computed-*e.ValuePercent) > 1e-9 {
		return errors.New("coverage_measured value_percent does not match covered_lines/total_lines")
	}
	if CoveragePass(*e.ValuePercent, *e.ThresholdPercent) != (e.Status == StatusPass) {
		return errors.New("coverage_measured status does not match strict threshold calculation")
	}
	return nil
}

func requireEvidenceFields(fields map[string]json.RawMessage, allowed map[string]bool, names []string) error {
	for _, name := range names {
		allowed[name] = true
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("%s missing %s", eventNameForMissingField(allowed), name)
		}
	}
	return nil
}

func eventNameForMissingField(allowed map[string]bool) string {
	if allowed["argv"] {
		return "command_finished"
	}
	return "coverage_measured"
}

func requiredStringField(fields map[string]json.RawMessage, name string) (string, error) {
	v, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("missing %s", name)
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return s, nil
}

func rejectForbiddenEvidenceFields(fields map[string]json.RawMessage, allowed map[string]bool, eventName string) error {
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("event %q contains forbidden field %q", eventName, key)
		}
	}
	return nil
}

func decodeStatus(raw json.RawMessage) (Status, error) {
	if len(raw) == 0 {
		return "", errors.New("missing status")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("status must be a string")
	}
	status := Status(s)
	switch status {
	case StatusPass, StatusFail, StatusBlocked, StatusNotApplicable:
		return status, nil
	default:
		return "", fmt.Errorf("unknown status %s", strconv.Quote(s))
	}
}
