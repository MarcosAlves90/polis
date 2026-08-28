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
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fields); err != nil {
		return EvidenceEvent{}, fmt.Errorf("decode event: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return EvidenceEvent{}, errors.New("event contains trailing JSON value")
	}

	getString := func(name string) (string, error) {
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
	eventName, err := getString("event")
	if err != nil {
		return EvidenceEvent{}, err
	}
	gate, err := getString("gate")
	if err != nil {
		return EvidenceEvent{}, err
	}
	if !IsEvidenceGate(gate) {
		return EvidenceEvent{}, fmt.Errorf("unknown gate %q", gate)
	}

	allowed := map[string]bool{"event": true, "gate": true}
	e := EvidenceEvent{Event: eventName, Gate: gate}
	switch eventName {
	case "gate_started":
		// no additional fields
	case "gate_finished":
		allowed["status"] = true
		status, err := decodeStatus(fields["status"])
		if err != nil {
			return EvidenceEvent{}, err
		}
		e.Status = status
		if status == StatusBlocked || status == StatusNotApplicable {
			allowed["reason"] = true
			reason, err := getString("reason")
			if err != nil || strings.TrimSpace(reason) == "" {
				return EvidenceEvent{}, errors.New("BLOCKED/NOT_APPLICABLE gate_finished requires non-empty reason")
			}
			e.Reason = &reason
		} else if _, ok := fields["reason"]; ok {
			return EvidenceEvent{}, errors.New("PASS/FAIL gate_finished must not contain reason")
		}
	case "command_finished":
		for _, k := range []string{"status", "argv", "cwd", "exit_code", "duration_ms", "stdout", "stderr"} {
			allowed[k] = true
			if _, ok := fields[k]; !ok {
				return EvidenceEvent{}, fmt.Errorf("command_finished missing %s", k)
			}
		}
		status, err := decodeStatus(fields["status"])
		if err != nil {
			return EvidenceEvent{}, err
		}
		if status != StatusPass && status != StatusFail && status != StatusBlocked {
			return EvidenceEvent{}, errors.New("command_finished status must be PASS, FAIL, or BLOCKED")
		}
		e.Status = status
		if err := json.Unmarshal(fields["argv"], &e.Argv); err != nil || len(e.Argv) == 0 {
			return EvidenceEvent{}, errors.New("command_finished argv must be a non-empty string array")
		}
		for _, arg := range e.Argv {
			if arg == "" {
				return EvidenceEvent{}, errors.New("command_finished argv values must not be empty")
			}
		}
		e.Cwd, err = getString("cwd")
		if err != nil {
			return EvidenceEvent{}, err
		}
		if err := ValidateRepoRelativePath(e.Cwd); err != nil {
			return EvidenceEvent{}, fmt.Errorf("invalid command_finished cwd: %w", err)
		}
		var exitCode int
		if err := json.Unmarshal(fields["exit_code"], &exitCode); err != nil || exitCode < -1 {
			return EvidenceEvent{}, errors.New("command_finished exit_code must be an integer >= -1")
		}
		e.ExitCode = &exitCode
		var duration int64
		if err := json.Unmarshal(fields["duration_ms"], &duration); err != nil || duration < 0 {
			return EvidenceEvent{}, errors.New("command_finished duration_ms must be an integer >= 0")
		}
		e.DurationMS = &duration
		var stdout, stderr string
		if err := json.Unmarshal(fields["stdout"], &stdout); err != nil {
			return EvidenceEvent{}, errors.New("command_finished stdout must be a string")
		}
		if err := json.Unmarshal(fields["stderr"], &stderr); err != nil {
			return EvidenceEvent{}, errors.New("command_finished stderr must be a string")
		}
		e.Stdout, e.Stderr = &stdout, &stderr
	case "coverage_measured":
		if gate != "coverage" {
			return EvidenceEvent{}, errors.New("coverage_measured gate must be coverage")
		}
		for _, k := range []string{"status", "adapter", "report", "metric", "covered_lines", "total_lines", "value_percent", "operator", "threshold_percent"} {
			allowed[k] = true
			if _, ok := fields[k]; !ok {
				return EvidenceEvent{}, fmt.Errorf("coverage_measured missing %s", k)
			}
		}
		status, err := decodeStatus(fields["status"])
		if err != nil || (status != StatusPass && status != StatusFail) {
			return EvidenceEvent{}, errors.New("coverage_measured status must be PASS or FAIL")
		}
		e.Status = status
		if e.Adapter, err = getString("adapter"); err != nil || e.Adapter != CoverageAdapterGoCoverProfileV1 {
			return EvidenceEvent{}, errors.New("coverage_measured adapter is unsupported")
		}
		if e.Report, err = getString("report"); err != nil {
			return EvidenceEvent{}, err
		}
		if err := ValidateRepoRelativePath(e.Report); err != nil {
			return EvidenceEvent{}, fmt.Errorf("invalid coverage_measured report: %w", err)
		}
		if e.Metric, err = getString("metric"); err != nil || e.Metric != CoverageMetricLinePercent {
			return EvidenceEvent{}, errors.New("coverage_measured metric must be line_coverage_percent")
		}
		if e.Operator, err = getString("operator"); err != nil || e.Operator != CoverageOperatorGreaterThan {
			return EvidenceEvent{}, errors.New("coverage_measured operator must be >")
		}
		var covered, total int
		if err := json.Unmarshal(fields["covered_lines"], &covered); err != nil || covered < 0 {
			return EvidenceEvent{}, errors.New("coverage_measured covered_lines must be an integer >= 0")
		}
		if err := json.Unmarshal(fields["total_lines"], &total); err != nil || total < 1 || covered > total {
			return EvidenceEvent{}, errors.New("coverage_measured total_lines must be >= 1 and >= covered_lines")
		}
		e.CoveredLines, e.TotalLines = &covered, &total
		var value, threshold float64
		if err := json.Unmarshal(fields["value_percent"], &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
			return EvidenceEvent{}, errors.New("coverage_measured value_percent must be between 0 and 100")
		}
		if err := json.Unmarshal(fields["threshold_percent"], &threshold); err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < MinimumCoverageThreshold || threshold > 100 {
			return EvidenceEvent{}, errors.New("coverage_measured threshold_percent is invalid")
		}
		e.ValuePercent, e.ThresholdPercent = &value, &threshold
		computed := float64(covered) / float64(total) * 100
		if math.Abs(computed-value) > 1e-9 {
			return EvidenceEvent{}, errors.New("coverage_measured value_percent does not match covered_lines/total_lines")
		}
		if CoveragePass(value, threshold) != (status == StatusPass) {
			return EvidenceEvent{}, errors.New("coverage_measured status does not match strict threshold calculation")
		}
	default:
		return EvidenceEvent{}, fmt.Errorf("unknown event %q", eventName)
	}

	for key := range fields {
		if !allowed[key] {
			return EvidenceEvent{}, fmt.Errorf("event %q contains forbidden field %q", eventName, key)
		}
	}
	return e, nil
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
