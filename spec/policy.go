package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
)

const (
	PolicySchemaVersion       = 3
	LegacyPolicySchemaVersion = 2
)

const (
	GateModeCommand        = "command"
	GateModeCoverage       = "coverage"
	GateModeNotApplicable  = "not_applicable"
	EnvironmentModeClean   = "clean"
	EnvironmentModeInherit = "inherit"
	gateStringErrorFormat  = "%s must be a string"
)

const (
	ValidationLevelStrict   = "strict"
	ValidationLevelStandard = "standard"
	ValidationLevelMinimal  = "minimal"
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

func IsProjectGate(id string) bool {
	return projectGateIndex(id) >= 0
}

func projectGateIndex(id string) int {
	for i, known := range ProjectGateOrder {
		if known == id {
			return i
		}
	}
	return -1
}

var evidenceGateSet = func() map[string]struct{} {
	m := map[string]struct{}{
		"behavior":    {},
		"regression":  {},
		"affected":    {},
		"integrity":   {},
		"target-tree": {},
		"policy":      {},
	}
	for _, gate := range ProjectGateOrder {
		m[gate] = struct{}{}
	}
	return m
}()

type CommandSpec struct {
	Argv           []string         `json:"argv"`
	Cwd            string           `json:"cwd"`
	TimeoutSeconds int              `json:"timeout_seconds"`
	Environment    *EnvironmentSpec `json:"environment,omitempty"`
}

type EnvironmentSpec struct {
	Mode string   `json:"mode"`
	Pass []string `json:"pass,omitempty"`
}

type GatePolicy struct {
	ID               string       `json:"id"`
	Mode             string       `json:"mode"`
	Command          *CommandSpec `json:"command,omitempty"`
	Reason           *string      `json:"reason,omitempty"`
	DependsOn        []string     `json:"depends_on,omitempty"`
	Adapter          string       `json:"adapter,omitempty"`
	Report           string       `json:"report,omitempty"`
	Operator         string       `json:"operator,omitempty"`
	ThresholdPercent *float64     `json:"threshold_percent,omitempty"`
}

type Policy struct {
	SchemaVersion   int          `json:"schema_version"`
	ValidationLevel string       `json:"validation_level,omitempty"`
	Gates           []GatePolicy `json:"gates"`
}

type PolicyDependency struct {
	Gate      string `json:"gate"`
	DependsOn string `json:"depends_on"`
}

type PolicyLintIssue struct {
	Code      string `json:"code"`
	Gate      string `json:"gate,omitempty"`
	DependsOn string `json:"depends_on,omitempty"`
	Message   string `json:"message"`
}

type PolicyLintReport struct {
	Dependencies   []PolicyDependency `json:"dependencies"`
	ExecutionOrder []string           `json:"execution_order"`
	Issues         []PolicyLintIssue  `json:"issues,omitempty"`
}

func (r PolicyLintReport) Valid() bool {
	return len(r.Issues) == 0
}

func (r PolicyLintReport) Err() error {
	if r.Valid() {
		return nil
	}
	messages := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		messages = append(messages, issue.Message)
	}
	return errors.New("policy dependency lint: " + strings.Join(messages, "; "))
}

type rawPolicy struct {
	SchemaVersion   int               `json:"schema_version"`
	ValidationLevel json.RawMessage   `json:"validation_level"`
	Gates           []json.RawMessage `json:"gates"`
}

func DecodePolicy(raw []byte) (Policy, error) {
	var rp rawPolicy
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rp); err != nil {
		return Policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := ensureDecoderEOF(dec, "policy"); err != nil {
		return Policy{}, err
	}
	validationLevel, err := decodeValidationLevel(rp.ValidationLevel)
	if err != nil {
		return Policy{}, err
	}
	p := Policy{SchemaVersion: rp.SchemaVersion, ValidationLevel: validationLevel, Gates: make([]GatePolicy, 0, len(rp.Gates))}
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

func decodeValidationLevel(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var level string
	if err := json.Unmarshal(raw, &level); err != nil || level == "" {
		return "", errors.New("validation_level must be a non-empty string")
	}
	return level, nil
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
		"report": {}, "operator": {}, "threshold_percent": {}, "depends_on": {},
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
		return "", fmt.Errorf(gateStringErrorFormat, name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf(gateStringErrorFormat, name)
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
	if len(fields) != 3+dependencyFieldCount(fields) {
		return errors.New("command gate must contain exactly id, mode, command")
	}
	cmd, err := requiredCommand(fields)
	if err != nil {
		return err
	}
	gate.Command = &cmd
	return decodeDependencies(gate, fields)
}

func decodeCoverageGate(gate *GatePolicy, fields map[string]json.RawMessage) error {
	if len(fields) != 7+dependencyFieldCount(fields) {
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
	if err := decodeCoverageThreshold(gate, fields); err != nil {
		return err
	}
	return decodeDependencies(gate, fields)
}

func decodeCoverageStrings(gate *GatePolicy, fields map[string]json.RawMessage) error {
	for key, target := range map[string]*string{"adapter": &gate.Adapter, "report": &gate.Report, "operator": &gate.Operator} {
		rawValue, ok := fields[key]
		if !ok || json.Unmarshal(rawValue, target) != nil {
			return fmt.Errorf(gateStringErrorFormat, key)
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
	if len(fields) != 3+dependencyFieldCount(fields) {
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
	return decodeDependencies(gate, fields)
}

func dependencyFieldCount(fields map[string]json.RawMessage) int {
	if _, ok := fields["depends_on"]; ok {
		return 1
	}
	return 0
}

func decodeDependencies(gate *GatePolicy, fields map[string]json.RawMessage) error {
	raw, ok := fields["depends_on"]
	if !ok {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("depends_on must be an array of strings")
	}
	var dependencies []string
	if err := json.Unmarshal(raw, &dependencies); err != nil {
		return errors.New("depends_on must be an array of strings")
	}
	gate.DependsOn = dependencies
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
	if err := ensureDecoderEOF(dec, "policy"); err != nil {
		return CommandSpec{}, err
	}
	if err := cmd.Validate(); err != nil {
		return CommandSpec{}, err
	}
	return cmd, nil
}

func (p Policy) Validate() error {
	if p.SchemaVersion != PolicySchemaVersion && p.SchemaVersion != LegacyPolicySchemaVersion {
		return fmt.Errorf("unsupported policy schema_version %d", p.SchemaVersion)
	}
	if p.SchemaVersion < PolicySchemaVersion && p.ValidationLevel != "" {
		return fmt.Errorf("policy schema v%d does not support validation_level", p.SchemaVersion)
	}
	level := p.EffectiveValidationLevel()
	if err := ValidateValidationLevel(level); err != nil {
		return err
	}
	if len(p.Gates) != len(ProjectGateOrder) {
		return fmt.Errorf("policy must contain exactly %d project gates", len(ProjectGateOrder))
	}
	for i, expectedID := range ProjectGateOrder {
		if err := validatePolicyGateAt(i, expectedID, p.Gates[i], p.SchemaVersion, level); err != nil {
			return err
		}
	}
	return LintPolicy(p).Err()
}

func (p Policy) EffectiveValidationLevel() string {
	if p.ValidationLevel == "" {
		return ValidationLevelStrict
	}
	return p.ValidationLevel
}

func ValidateValidationLevel(level string) error {
	switch level {
	case ValidationLevelStrict, ValidationLevelStandard, ValidationLevelMinimal:
		return nil
	default:
		return fmt.Errorf("unsupported validation_level %q", level)
	}
}

func LintPolicy(policy Policy) PolicyLintReport {
	report := PolicyLintReport{
		Dependencies:   make([]PolicyDependency, 0),
		ExecutionOrder: make([]string, 0, len(policy.Gates)),
		Issues:         make([]PolicyLintIssue, 0),
	}
	gates := make(map[string]GatePolicy, len(policy.Gates))
	for _, gate := range policy.Gates {
		gates[gate.ID] = gate
	}
	indegree, dependents := buildPolicyDependencyGraph(gates, &report)
	report.ExecutionOrder = topologicalPolicyOrder(gates, indegree, dependents)
	if len(report.ExecutionOrder) != len(gates) {
		appendPolicyLintIssue(&report, "dependency_cycle", "", "", "policy dependency graph contains a cycle")
	}
	return report
}

func buildPolicyDependencyGraph(gates map[string]GatePolicy, report *PolicyLintReport) (map[string]int, map[string][]string) {
	indegree := make(map[string]int, len(gates))
	dependents := make(map[string][]string, len(gates))
	for _, gateID := range ProjectGateOrder {
		gate, ok := gates[gateID]
		if !ok {
			continue
		}
		indegree[gateID] = 0
		lintGateDependencies(gate, gates, report, indegree, dependents)
	}
	return indegree, dependents
}

func lintGateDependencies(gate GatePolicy, gates map[string]GatePolicy, report *PolicyLintReport, indegree map[string]int, dependents map[string][]string) {
	dependencies := policyDependencies(gate)
	lintDeclaredDependencyDuplicates(gate, report)
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		if _, duplicate := seen[dependency]; duplicate {
			continue
		}
		seen[dependency] = struct{}{}
		if !lintPolicyDependency(gate, dependency, gates, report) {
			continue
		}
		indegree[gate.ID]++
		dependents[dependency] = append(dependents[dependency], gate.ID)
	}
}

func lintDeclaredDependencyDuplicates(gate GatePolicy, report *PolicyLintReport) {
	seen := make(map[string]struct{}, len(gate.DependsOn))
	for _, dependency := range gate.DependsOn {
		if _, duplicate := seen[dependency]; duplicate {
			appendPolicyLintIssue(report, "duplicate_dependency", gate.ID, dependency, fmt.Sprintf("gate %q declares dependency %q more than once", gate.ID, dependency))
			continue
		}
		seen[dependency] = struct{}{}
	}
}

func policyDependencies(gate GatePolicy) []string {
	return append(builtInPolicyDependencies(gate.ID), gate.DependsOn...)
}

func lintPolicyDependency(gate GatePolicy, dependency string, gates map[string]GatePolicy, report *PolicyLintReport) bool {
	if !IsProjectGate(dependency) {
		appendPolicyLintIssue(report, "unknown_dependency", gate.ID, dependency, fmt.Sprintf("gate %q depends on unknown gate %q", gate.ID, dependency))
		return false
	}
	if dependency == gate.ID {
		appendPolicyLintIssue(report, "self_dependency", gate.ID, dependency, fmt.Sprintf("gate %q cannot depend on itself", gate.ID))
		return false
	}
	dependencyGate, exists := gates[dependency]
	if !exists {
		appendPolicyLintIssue(report, "unknown_dependency", gate.ID, dependency, fmt.Sprintf("gate %q depends on gate %q which is missing from the policy", gate.ID, dependency))
		return false
	}
	report.Dependencies = append(report.Dependencies, PolicyDependency{Gate: gate.ID, DependsOn: dependency})
	if gate.Mode != GateModeNotApplicable && dependencyGate.Mode == GateModeNotApplicable {
		appendPolicyLintIssue(report, "disabled_dependency", gate.ID, dependency, fmt.Sprintf("enabled gate %q depends on disabled gate %q", gate.ID, dependency))
	}
	return true
}

func builtInPolicyDependencies(gateID string) []string {
	if gateID == "coverage" {
		return []string{"test.complete"}
	}
	return nil
}

func appendPolicyLintIssue(report *PolicyLintReport, code, gate, dependency, message string) {
	report.Issues = append(report.Issues, PolicyLintIssue{Code: code, Gate: gate, DependsOn: dependency, Message: message})
}

func topologicalPolicyOrder(gates map[string]GatePolicy, indegree map[string]int, dependents map[string][]string) []string {
	order := make([]string, 0, len(gates))
	emitted := make(map[string]struct{}, len(gates))
	for len(order) < len(gates) {
		var next string
		for _, gateID := range ProjectGateOrder {
			if _, exists := gates[gateID]; exists && indegree[gateID] == 0 {
				if _, alreadyEmitted := emitted[gateID]; !alreadyEmitted {
					next = gateID
					break
				}
			}
		}
		if next == "" {
			break
		}
		emitted[next] = struct{}{}
		order = append(order, next)
		for _, dependent := range dependents[next] {
			indegree[dependent]--
		}
	}
	return order
}

type ValidationSummary struct {
	Level         string
	EnabledGates  []string
	DisabledGates []string
}

func (p Policy) ValidationSummary() ValidationSummary {
	summary := ValidationSummary{
		Level:         p.EffectiveValidationLevel(),
		EnabledGates:  make([]string, 0, len(p.Gates)),
		DisabledGates: make([]string, 0, len(p.Gates)),
	}
	for _, gate := range p.Gates {
		if gate.Mode == GateModeNotApplicable {
			summary.DisabledGates = append(summary.DisabledGates, gate.ID)
			continue
		}
		summary.EnabledGates = append(summary.EnabledGates, gate.ID)
	}
	return summary
}

func validatePolicyGateAt(index int, expectedID string, gate GatePolicy, schemaVersion int, validationLevel string) error {
	if gate.ID != expectedID {
		return fmt.Errorf("gate %d must be %q, got %q", index, expectedID, gate.ID)
	}
	if schemaVersion < PolicySchemaVersion && gate.DependsOn != nil {
		return fmt.Errorf("gate %q: policy schema v%d does not support depends_on", gate.ID, schemaVersion)
	}
	if err := gate.Validate(); err != nil {
		return fmt.Errorf("gate %q: %w", gate.ID, err)
	}
	if schemaVersion >= PolicySchemaVersion && gate.Command != nil && gate.Command.Environment == nil {
		return fmt.Errorf("gate %q: policy schema v3 requires explicit command environment", gate.ID)
	}
	if gate.ID == "test.complete" && validationLevel != ValidationLevelMinimal && gate.Mode != GateModeCommand {
		return fmt.Errorf("gate %q must use command mode", gate.ID)
	}
	if gate.ID == "coverage" && validationLevel == ValidationLevelStrict && gate.Mode != GateModeCoverage {
		return fmt.Errorf("gate %q must use coverage mode at validation level %q", gate.ID, validationLevel)
	}
	if gate.ID == "coverage" && gate.Mode != GateModeCoverage && gate.Mode != GateModeNotApplicable {
		return fmt.Errorf("gate %q must use coverage or not_applicable mode", gate.ID)
	}
	if gate.ID != "coverage" && gate.Mode == GateModeCoverage {
		return fmt.Errorf("gate %q must not use coverage mode", gate.ID)
	}
	return nil
}

func (g GatePolicy) Validate() error {
	if err := g.validateDependencies(); err != nil {
		return err
	}
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

func (g GatePolicy) validateDependencies() error {
	seen := make(map[string]struct{}, len(g.DependsOn))
	for i, dependency := range g.DependsOn {
		if strings.TrimSpace(dependency) == "" {
			return fmt.Errorf("depends_on[%d] must be a non-empty gate id", i)
		}
		if _, duplicate := seen[dependency]; duplicate {
			return fmt.Errorf("depends_on contains duplicate gate %q", dependency)
		}
		seen[dependency] = struct{}{}
	}
	return nil
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
	switch g.Adapter {
	case CoverageAdapterGoCoverProfileV1, CoverageAdapterLCOVV1, CoverageAdapterCoberturaV1:
	default:
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
	if c.Environment != nil {
		if err := c.Environment.Validate(); err != nil {
			return fmt.Errorf("command environment: %w", err)
		}
	}
	return nil
}

func (e EnvironmentSpec) Validate() error {
	switch e.Mode {
	case EnvironmentModeInherit:
		if len(e.Pass) != 0 {
			return errors.New("inherit environment must not declare pass variables")
		}
		return nil
	case EnvironmentModeClean:
		seen := map[string]struct{}{}
		for i, name := range e.Pass {
			if !validEnvironmentName(name) {
				return fmt.Errorf("pass[%d] contains invalid environment variable name %q", i, name)
			}
			if _, ok := seen[name]; ok {
				return fmt.Errorf("pass contains duplicate environment variable %q", name)
			}
			seen[name] = struct{}{}
		}
		return nil
	default:
		return fmt.Errorf("unsupported environment mode %q", e.Mode)
	}
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if !validEnvironmentRune(r, i == 0) {
			return false
		}
	}
	return true
}

func validEnvironmentRune(r rune, first bool) bool {
	if r == '=' || r == 0 {
		return false
	}
	if first {
		return isASCIILetter(r) || r == '_'
	}
	return isASCIILetter(r) || isASCIIDigit(r) || r == '_'
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
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
