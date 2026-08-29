package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
)

const PolicySchemaVersion = 2

const (
	GateModeCommand       = "command"
	GateModeCoverage      = "coverage"
	GateModeNotApplicable = "not_applicable"
)

var ProjectGateOrder = []string{
	"test.complete",
	"coverage",
	"lint",
	"typecheck",
	"build",
	"smoke",
	"compatibility",
	"dependency",
	"migration",
	"security",
	"platform",
}

var evidenceGateSet = func() map[string]struct{} {
	m := map[string]struct{}{
		"behavior":    {},
		"regression":  {},
		"affected":    {},
		"integrity":   {},
		"target-tree": {},
	}
	for _, gate := range ProjectGateOrder {
		m[gate] = struct{}{}
	}
	return m
}()

type CommandSpec struct {
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type GatePolicy struct {
	ID               string       `json:"id"`
	Mode             string       `json:"mode"`
	Command          *CommandSpec `json:"command,omitempty"`
	Reason           *string      `json:"reason,omitempty"`
	Adapter          string       `json:"adapter,omitempty"`
	Report           string       `json:"report,omitempty"`
	Operator         string       `json:"operator,omitempty"`
	ThresholdPercent *float64     `json:"threshold_percent,omitempty"`
}

type Policy struct {
	SchemaVersion int          `json:"schema_version"`
	Gates         []GatePolicy `json:"gates"`
}

type rawPolicy struct {
	SchemaVersion int               `json:"schema_version"`
	Gates         []json.RawMessage `json:"gates"`
}

func DecodePolicy(raw []byte) (Policy, error) {
	var rp rawPolicy
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rp); err != nil {
		return Policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := ensurePolicyEOF(dec); err != nil {
		return Policy{}, err
	}
	p := Policy{SchemaVersion: rp.SchemaVersion, Gates: make([]GatePolicy, 0, len(rp.Gates))}
	for i, rawGate := range rp.Gates {
		gate, err := decodeGatePolicy(rawGate)
		if err != nil {
			return Policy{}, fmt.Errorf("gate %d: %w", i, err)
		}
		p.Gates = append(p.Gates, gate)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func ensurePolicyEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("policy contains trailing JSON value")
	}
	return fmt.Errorf("policy trailing data: %w", err)
}

func decodeGatePolicy(raw json.RawMessage) (GatePolicy, error) {
	fields, err := decodeGateFields(raw)
	if err != nil {
		return GatePolicy{}, err
	}
	id, err := requiredGateString(fields, "id")
	if err != nil {
		return GatePolicy{}, err
	}
	mode, err := requiredGateString(fields, "mode")
	if err != nil {
		return GatePolicy{}, err
	}
	gate := GatePolicy{ID: id, Mode: mode}
	if err := decodeGateModeFields(&gate, fields); err != nil {
		return GatePolicy{}, err
	}
	return gate, nil
}

func decodeGateFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode gate object: %w", err)
	}
	allowed := map[string]struct{}{
		"id": {}, "mode": {}, "command": {}, "reason": {}, "adapter": {},
		"report": {}, "operator": {}, "threshold_percent": {},
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("unknown field %q", key)
		}
	}
	return fields, nil
}

func requiredGateString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}

func decodeGateModeFields(gate *GatePolicy, fields map[string]json.RawMessage) error {
	switch gate.Mode {
	case GateModeCommand:
		return decodeCommandGate(gate, fields)
	case GateModeCoverage:
		return decodeCoverageGate(gate, fields)
	case GateModeNotApplicable:
		return decodeNotApplicableGate(gate, fields)
	default:
		return fmt.Errorf("unknown gate mode %q", gate.Mode)
	}
}

func decodeCommandGate(gate *GatePolicy, fields map[string]json.RawMessage) error {
	if len(fields) != 3 {
		return errors.New("command gate must contain exactly id, mode, command")
	}
	cmd, err := requiredCommand(fields)
	if err != nil {
		return err
	}
	gate.Command = &cmd
	return nil
}

func decodeCoverageGate(gate *GatePolicy, fields map[string]json.RawMessage) error {
	if len(fields) != 7 {
		return errors.New("coverage gate must contain exactly id, mode, command, adapter, report, operator, threshold_percent")
	}
	cmd, err := requiredCommand(fields)
	if err != nil {
		return err
	}
	gate.Command = &cmd
	if err := decodeCoverageStrings(gate, fields); err != nil {
		return err
	}
	return decodeCoverageThreshold(gate, fields)
}

func decodeCoverageStrings(gate *GatePolicy, fields map[string]json.RawMessage) error {
	for key, target := range map[string]*string{"adapter": &gate.Adapter, "report": &gate.Report, "operator": &gate.Operator} {
		rawValue, ok := fields[key]
		if !ok || json.Unmarshal(rawValue, target) != nil {
			return fmt.Errorf("%s must be a string", key)
		}
	}
	return nil
}

func decodeCoverageThreshold(gate *GatePolicy, fields map[string]json.RawMessage) error {
	var threshold float64
	rawThreshold, ok := fields["threshold_percent"]
	if !ok || json.Unmarshal(rawThreshold, &threshold) != nil {
		return errors.New("threshold_percent must be a number")
	}
	gate.ThresholdPercent = &threshold
	return nil
}

func decodeNotApplicableGate(gate *GatePolicy, fields map[string]json.RawMessage) error {
	if len(fields) != 3 {
		return errors.New("not_applicable gate must contain exactly id, mode, reason")
	}
	rawReason, ok := fields["reason"]
	if !ok {
		return errors.New("not_applicable gate missing reason")
	}
	var reason string
	if err := json.Unmarshal(rawReason, &reason); err != nil {
		return errors.New("reason must be a string")
	}
	gate.Reason = &reason
	return nil
}

func requiredCommand(fields map[string]json.RawMessage) (CommandSpec, error) {
	rawCommand, ok := fields["command"]
	if !ok {
		return CommandSpec{}, errors.New("gate missing command")
	}
	return decodeCommand(rawCommand)
}

func decodeCommand(raw json.RawMessage) (CommandSpec, error) {
	var cmd CommandSpec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cmd); err != nil {
		return CommandSpec{}, fmt.Errorf("decode command: %w", err)
	}
	if err := ensurePolicyEOF(dec); err != nil {
		return CommandSpec{}, err
	}
	if err := cmd.Validate(); err != nil {
		return CommandSpec{}, err
	}
	return cmd, nil
}

func (p Policy) Validate() error {
	if p.SchemaVersion != PolicySchemaVersion {
		return fmt.Errorf("unsupported policy schema_version %d", p.SchemaVersion)
	}
	if len(p.Gates) != len(ProjectGateOrder) {
		return fmt.Errorf("policy must contain exactly %d project gates", len(ProjectGateOrder))
	}
	for i, expectedID := range ProjectGateOrder {
		if err := validatePolicyGateAt(i, expectedID, p.Gates[i]); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicyGateAt(index int, expectedID string, gate GatePolicy) error {
	if gate.ID != expectedID {
		return fmt.Errorf("gate %d must be %q, got %q", index, expectedID, gate.ID)
	}
	if err := gate.Validate(); err != nil {
		return fmt.Errorf("gate %q: %w", gate.ID, err)
	}
	if gate.ID == "test.complete" && gate.Mode != GateModeCommand {
		return fmt.Errorf("gate %q must use command mode", gate.ID)
	}
	if gate.ID == "coverage" && gate.Mode != GateModeCoverage {
		return fmt.Errorf("gate %q must use coverage mode", gate.ID)
	}
	if gate.ID != "coverage" && gate.Mode == GateModeCoverage {
		return fmt.Errorf("gate %q must not use coverage mode", gate.ID)
	}
	return nil
}

func (g GatePolicy) Validate() error {
	switch g.Mode {
	case GateModeCommand:
		return g.validateCommandMode()
	case GateModeCoverage:
		return g.validateCoverageMode()
	case GateModeNotApplicable:
		return g.validateNotApplicableMode()
	default:
		return fmt.Errorf("unknown mode %q", g.Mode)
	}
}

func (g GatePolicy) validateCommandMode() error {
	if g.Command == nil || g.Reason != nil || g.Adapter != "" || g.Report != "" || g.Operator != "" || g.ThresholdPercent != nil {
		return errors.New("command mode has invalid field combination")
	}
	return g.Command.Validate()
}

func (g GatePolicy) validateCoverageMode() error {
	if g.Command == nil || g.Reason != nil || g.ThresholdPercent == nil {
		return errors.New("coverage mode has invalid field combination")
	}
	if err := g.Command.Validate(); err != nil {
		return err
	}
	if err := g.validateCoverageMetadata(); err != nil {
		return err
	}
	return g.validateCoverageThreshold()
}

func (g GatePolicy) validateCoverageMetadata() error {
	if g.Adapter != CoverageAdapterGoCoverProfileV1 {
		return fmt.Errorf("unsupported coverage adapter %q", g.Adapter)
	}
	if err := ValidateRepoRelativePath(g.Report); err != nil {
		return fmt.Errorf("invalid coverage report: %w", err)
	}
	if g.Operator != CoverageOperatorGreaterThan {
		return fmt.Errorf("coverage operator must be %q", CoverageOperatorGreaterThan)
	}
	return nil
}

func (g GatePolicy) validateCoverageThreshold() error {
	value := *g.ThresholdPercent
	if math.IsNaN(value) || math.IsInf(value, 0) || value < MinimumCoverageThreshold || value > 100.0 {
		return fmt.Errorf("coverage threshold_percent must be between %.1f and 100.0", MinimumCoverageThreshold)
	}
	return nil
}

func (g GatePolicy) validateNotApplicableMode() error {
	if g.Command != nil || g.Reason == nil || strings.TrimSpace(*g.Reason) == "" || g.Adapter != "" || g.Report != "" || g.Operator != "" || g.ThresholdPercent != nil {
		return errors.New("not_applicable mode requires only a non-empty reason")
	}
	return nil
}

func (c CommandSpec) Validate() error {
	if len(c.Argv) == 0 {
		return errors.New("command argv must not be empty")
	}
	for i, arg := range c.Argv {
		if arg == "" {
			return fmt.Errorf("command argv[%d] must not be empty", i)
		}
	}
	if err := ValidateRepoRelativePath(c.Cwd); err != nil {
		return fmt.Errorf("invalid command cwd: %w", err)
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 3600 {
		return errors.New("command timeout_seconds must be between 1 and 3600")
	}
	return nil
}

func ValidateRepoRelativePath(value string) error {
	if value == "." {
		return nil
	}
	if value == "" {
		return errors.New("path must not be empty")
	}
	if strings.Contains(value, "\\") {
		return errors.New("path must use forward slashes")
	}
	if strings.HasPrefix(value, "/") {
		return errors.New("path must be relative")
	}
	if len(value) >= 2 && value[1] == ':' {
		return errors.New("path must not use a drive-letter prefix")
	}
	if path.Clean(value) != value {
		return errors.New("path must be normalized")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path contains prohibited segment")
		}
	}
	return nil
}

func IsEvidenceGate(id string) bool {
	_, ok := evidenceGateSet[id]
	return ok
}
