package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ImplementationPlanSchemaVersion = 1
	MaxImplementationPlanBytes      = 1 << 20
	MaxImplementationPlanSteps      = 4096
	MaxImplementationPlanJSONDepth  = 64

	ImplementationPlanStrategyRedGreen   = "red_green"
	ImplementationPlanStrategyGreenGreen = "green_green"

	ImplementationPlanStepTest           = "test"
	ImplementationPlanStepProof          = "proof"
	ImplementationPlanStepImplementation = "implementation"
	ImplementationPlanStepValidation     = "validation"
)

var planStepIDPattern = regexp.MustCompile(`^PLAN-[0-9]{3,}$`)

// ImplementationPlan is a subordinate, deterministic sequence for one exact
// locked Change Contract. It carries references, not executable commands.
type ImplementationPlan struct {
	SchemaVersion        int                      `json:"schema_version"`
	ChangeContractSHA256 string                   `json:"change_contract_sha256"`
	GitObjectFormat      string                   `json:"git_object_format"`
	BaseCommit           string                   `json:"base_commit"`
	BaseTree             string                   `json:"base_tree"`
	Strategy             string                   `json:"strategy"`
	Steps                []ImplementationPlanStep `json:"steps"`
}

// ImplementationPlanStep describes intended work and references authoritative
// requirements, acceptance criteria, proofs, and policy gates.
type ImplementationPlanStep struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Objective          string   `json:"objective"`
	Requirements       []string `json:"requirements,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	AllowedPaths       []string `json:"allowed_paths,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	ContractProofs     []string `json:"contract_proofs,omitempty"`
	ProjectGates       []string `json:"project_gates,omitempty"`
}

// DecodeImplementationPlan strictly decodes a bounded v1 plan.
func DecodeImplementationPlan(raw []byte) (ImplementationPlan, error) {
	if len(raw) > MaxImplementationPlanBytes {
		return ImplementationPlan{}, fmt.Errorf("implementation plan exceeds maximum size of %d bytes", MaxImplementationPlanBytes)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return ImplementationPlan{}, fmt.Errorf("decode implementation plan: %w", err)
	}
	if err := validateImplementationPlanJSONShape(raw); err != nil {
		return ImplementationPlan{}, fmt.Errorf("decode implementation plan: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var plan ImplementationPlan
	if err := dec.Decode(&plan); err != nil {
		return ImplementationPlan{}, fmt.Errorf("decode implementation plan: %w", err)
	}
	if err := ensureDecoderEOF(dec, "implementation plan"); err != nil {
		return ImplementationPlan{}, err
	}
	if err := plan.Validate(); err != nil {
		return ImplementationPlan{}, err
	}
	return plan, nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	return scanJSONValue(dec, 0)
}

func validateImplementationPlanJSONShape(raw []byte) error {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil || document == nil {
		return nil
	}
	planFields := map[string]struct{}{
		"schema_version": {}, "change_contract_sha256": {}, "git_object_format": {},
		"base_commit": {}, "base_tree": {}, "strategy": {}, "steps": {},
	}
	for field := range document {
		if _, ok := planFields[field]; !ok {
			return fmt.Errorf("unknown implementation plan property %q", field)
		}
	}
	stepsRaw, hasSteps := document["steps"]
	if hasSteps && isJSONNull(stepsRaw) {
		return errors.New("steps must be an array, not null")
	}
	var steps []json.RawMessage
	if err := json.Unmarshal(stepsRaw, &steps); err != nil {
		return nil // The strict struct decoder reports non-array values.
	}
	arrayFields := []string{"requirements", "acceptance_criteria", "allowed_paths", "depends_on", "contract_proofs", "project_gates"}
	stepFields := map[string]struct{}{
		"id": {}, "kind": {}, "objective": {}, "requirements": {}, "acceptance_criteria": {},
		"allowed_paths": {}, "depends_on": {}, "contract_proofs": {}, "project_gates": {},
	}
	for i, stepRaw := range steps {
		var step map[string]json.RawMessage
		if err := json.Unmarshal(stepRaw, &step); err != nil || step == nil {
			continue // The strict struct decoder reports non-object steps.
		}
		for field := range step {
			if _, ok := stepFields[field]; !ok {
				return fmt.Errorf("unknown implementation plan property steps[%d].%q", i, field)
			}
		}
		for _, field := range arrayFields {
			if value, ok := step[field]; ok && isJSONNull(value) {
				return fmt.Errorf("steps[%d].%s must be an array, not null", i, field)
			}
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func scanJSONValue(dec *json.Decoder, depth int) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if depth >= MaxImplementationPlanJSONDepth {
		return fmt.Errorf("implementation plan JSON nesting depth exceeds %d", MaxImplementationPlanJSONDepth)
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		closeToken, err := dec.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim('}') {
			return errors.New("malformed JSON object")
		}
	case '[':
		for dec.More() {
			if err := scanJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		closeToken, err := dec.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim(']') {
			return errors.New("malformed JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

// Validate checks the closed structural contract and dependency graph.
func (p ImplementationPlan) Validate() error {
	if p.SchemaVersion != ImplementationPlanSchemaVersion {
		return fmt.Errorf("unsupported implementation plan schema_version %d", p.SchemaVersion)
	}
	if !sha256Pattern.MatchString(p.ChangeContractSHA256) {
		return errors.New("change_contract_sha256 must be 64 lowercase hexadecimal characters")
	}
	objectLength := 0
	switch p.GitObjectFormat {
	case "sha1":
		objectLength = 40
	case "sha256":
		objectLength = 64
	default:
		return fmt.Errorf("unsupported git_object_format %q", p.GitObjectFormat)
	}
	if !isLowerHex(p.BaseCommit, objectLength) {
		return fmt.Errorf("base_commit must be %d lowercase hexadecimal characters for %s", objectLength, p.GitObjectFormat)
	}
	if !isLowerHex(p.BaseTree, objectLength) {
		return fmt.Errorf("base_tree must be %d lowercase hexadecimal characters for %s", objectLength, p.GitObjectFormat)
	}
	switch p.Strategy {
	case ImplementationPlanStrategyRedGreen, ImplementationPlanStrategyGreenGreen:
	default:
		return fmt.Errorf("unsupported implementation plan strategy %q", p.Strategy)
	}
	if len(p.Steps) == 0 {
		return errors.New("implementation plan steps must not be empty")
	}
	if len(p.Steps) > MaxImplementationPlanSteps {
		return fmt.Errorf("implementation plan contains more than %d steps", MaxImplementationPlanSteps)
	}

	stepIndexes := make(map[string]int, len(p.Steps))
	implementationCount := 0
	for i, step := range p.Steps {
		if !planStepIDPattern.MatchString(step.ID) {
			return fmt.Errorf("steps[%d].id must use PLAN- followed by at least three digits", i)
		}
		ordinal, err := strconv.ParseUint(strings.TrimPrefix(step.ID, "PLAN-"), 10, 64)
		if err != nil || ordinal == 0 || fmt.Sprintf("PLAN-%03d", ordinal) != step.ID {
			return fmt.Errorf("steps[%d].id %q is not a canonical plan step ID", i, step.ID)
		}
		if _, exists := stepIndexes[step.ID]; exists {
			return fmt.Errorf("duplicate implementation plan step ID %q", step.ID)
		}
		stepIndexes[step.ID] = i
		if strings.TrimSpace(step.Objective) == "" {
			return fmt.Errorf("steps[%d].objective must be non-empty", i)
		}
		switch step.Kind {
		case ImplementationPlanStepTest, ImplementationPlanStepProof, ImplementationPlanStepImplementation, ImplementationPlanStepValidation:
		default:
			return fmt.Errorf("steps[%d].kind %q is unsupported", i, step.Kind)
		}
		if step.Kind == ImplementationPlanStepImplementation {
			implementationCount++
		}
		if err := validatePlanReferences(i, "requirements", step.Requirements); err != nil {
			return err
		}
		if err := validatePlanReferences(i, "acceptance_criteria", step.AcceptanceCriteria); err != nil {
			return err
		}
		if err := validatePlanReferences(i, "depends_on", step.DependsOn); err != nil {
			return err
		}
		if len(step.AllowedPaths) > 0 {
			if err := (ChangeScope{AllowedPaths: step.AllowedPaths}).Validate(); err != nil {
				return fmt.Errorf("steps[%d].allowed_paths: %w", i, err)
			}
		}
		if err := validatePlanReferences(i, "contract_proofs", step.ContractProofs); err != nil {
			return err
		}
		for _, proof := range step.ContractProofs {
			switch proof {
			case "regression", "behavior", "affected":
			default:
				return fmt.Errorf("steps[%d].contract_proofs contains unsupported proof %q", i, proof)
			}
		}
		if err := validatePlanReferences(i, "project_gates", step.ProjectGates); err != nil {
			return err
		}
		if len(step.ProjectGates) > 0 && step.Kind != ImplementationPlanStepValidation {
			return fmt.Errorf("steps[%d].project_gates is only valid on validation steps", i)
		}
		for _, gateID := range step.ProjectGates {
			if !IsProjectGate(gateID) {
				return fmt.Errorf("steps[%d].project_gates references unknown project gate %q", i, gateID)
			}
		}
	}
	if implementationCount == 0 {
		return errors.New("implementation plan must contain an implementation step")
	}
	for i, step := range p.Steps {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			dependencyIndex, ok := stepIndexes[dependency]
			if !ok {
				return fmt.Errorf("steps[%d].depends_on references unknown step %q", i, dependency)
			}
			if dependency == step.ID {
				return fmt.Errorf("steps[%d].depends_on contains self-dependency %q", i, dependency)
			}
			if _, ok := seen[dependency]; ok {
				return fmt.Errorf("steps[%d].depends_on contains duplicate step %q", i, dependency)
			}
			seen[dependency] = struct{}{}
			if dependencyIndex >= i {
				return fmt.Errorf("steps[%d].depends_on must precede the dependent step", i)
			}
		}
	}
	topologicalOrder, err := p.TopologicalOrder()
	if err != nil {
		return err
	}
	for i, step := range p.Steps {
		if topologicalOrder[i] != step.ID {
			return errors.New("deterministic topological order must match serialized step order")
		}
	}
	return nil
}

// ValidateProjectGates checks plan gate references against the effective
// policyplan execution order supplied by the producer or package verifier.
func (p ImplementationPlan) ValidateProjectGates(executionOrder []string) error {
	positions := make(map[string]int, len(executionOrder))
	for i, gateID := range executionOrder {
		if !IsProjectGate(gateID) {
			return fmt.Errorf("effective policy execution order contains unknown project gate %q", gateID)
		}
		if _, exists := positions[gateID]; exists {
			return fmt.Errorf("effective policy execution order contains duplicate project gate %q", gateID)
		}
		positions[gateID] = i
	}
	lastPosition := -1
	for stepIndex, step := range p.Steps {
		for _, gateID := range step.ProjectGates {
			position, ok := positions[gateID]
			if !ok {
				return fmt.Errorf("steps[%d].project_gates references gate %q outside the effective policy", stepIndex, gateID)
			}
			if position < lastPosition {
				return fmt.Errorf("steps[%d].project_gates do not follow effective policy execution order", stepIndex)
			}
			lastPosition = position
		}
	}
	return nil
}

func validatePlanReferences(stepIndex int, name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("steps[%d].%s contains an empty reference", stepIndex, name)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("steps[%d].%s contains duplicate reference %q", stepIndex, name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

// ValidateAgainst binds the plan to the exact locked contract bytes and checks
// traceability, baseline identity, scope, strategy, and proof ordering.
func (p ImplementationPlan) ValidateAgainst(contract ChangeContract, contractRaw []byte) error {
	if err := p.Validate(); err != nil {
		return err
	}
	decoded, err := DecodeChangeContract(contractRaw)
	if err != nil {
		return fmt.Errorf("invalid bound Change Contract: %w", err)
	}
	if !reflect.DeepEqual(contract, decoded) {
		return errors.New("implementation plan Change Contract value does not match exact input bytes")
	}
	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != DevelopmentMethodStrictSDDTDDV2 || contract.BaselineLock == nil || contract.Specification == nil {
		return errors.New("implementation plan requires a locked strict Change Contract schema v4 or v6")
	}
	if err := contract.Validate(); err != nil {
		return fmt.Errorf("invalid bound Change Contract: %w", err)
	}
	sum := sha256.Sum256(contractRaw)
	if got := hex.EncodeToString(sum[:]); got != p.ChangeContractSHA256 {
		return errors.New("implementation plan is bound to a different Change Contract")
	}
	lock := contract.BaselineLock
	if p.GitObjectFormat != lock.GitObjectFormat || p.BaseCommit != lock.BaseCommit || p.BaseTree != lock.BaseTree {
		return errors.New("implementation plan baseline does not match Change Contract baseline_lock")
	}
	wantStrategy := ""
	switch {
	case contract.RequiresRedGreen():
		wantStrategy = ImplementationPlanStrategyRedGreen
	case contract.RequiresGreenGreen():
		wantStrategy = ImplementationPlanStrategyGreenGreen
	default:
		return errors.New("locked Change Contract has no supported development proof strategy")
	}
	if p.Strategy != wantStrategy {
		return fmt.Errorf("implementation plan strategy %q does not match Change Contract strategy %q", p.Strategy, wantStrategy)
	}
	if err := p.validateTraceability(contract); err != nil {
		return err
	}
	if err := p.validateStrategyOrder(contract); err != nil {
		return err
	}
	return nil
}

func (p ImplementationPlan) validateTraceability(contract ChangeContract) error {
	specification := contract.Specification
	requirements := make(map[string]struct{}, len(specification.Requirements))
	for _, requirement := range specification.Requirements {
		requirements[requirement.ID] = struct{}{}
	}
	criteria := make(map[string]struct{}, len(specification.AcceptanceCriteria))
	validPairs := make(map[string]struct{})
	for _, criterion := range specification.AcceptanceCriteria {
		criteria[criterion.ID] = struct{}{}
		for _, requirement := range criterion.Requirements {
			validPairs[traceabilityPair(requirement, criterion.ID)] = struct{}{}
		}
	}
	implemented := make(map[string]struct{}, len(requirements))
	validated := make(map[string]struct{}, len(criteria))
	for i, step := range p.Steps {
		for _, requirement := range step.Requirements {
			if _, ok := requirements[requirement]; !ok {
				return fmt.Errorf("steps[%d] references unknown requirement %q", i, requirement)
			}
			if step.Kind == ImplementationPlanStepImplementation {
				implemented[requirement] = struct{}{}
			}
		}
		for _, criterion := range step.AcceptanceCriteria {
			if _, ok := criteria[criterion]; !ok {
				return fmt.Errorf("steps[%d] references unknown acceptance criterion %q", i, criterion)
			}
			if step.Kind != ImplementationPlanStepImplementation {
				validated[criterion] = struct{}{}
			}
		}
		if len(step.Requirements) > 0 && len(step.AcceptanceCriteria) > 0 {
			for _, requirement := range step.Requirements {
				for _, criterion := range step.AcceptanceCriteria {
					if _, ok := validPairs[traceabilityPair(requirement, criterion)]; !ok {
						return fmt.Errorf("steps[%d] links requirement %q to unrelated acceptance criterion %q", i, requirement, criterion)
					}
				}
			}
		}
		for _, path := range step.AllowedPaths {
			if step.Kind == ImplementationPlanStepTest {
				if !contract.TestScope.AllowsScopeEntry(path) {
					return fmt.Errorf("steps[%d] path %q is outside test_scope", i, path)
				}
			} else if !contract.Scope.AllowsScopeEntry(path) {
				return fmt.Errorf("steps[%d] path %q is outside Change Contract scope", i, path)
			}
		}
	}
	for requirement := range requirements {
		if _, ok := implemented[requirement]; !ok {
			return fmt.Errorf("implementation plan does not cover requirement %q with an implementation step", requirement)
		}
	}
	for _, criterion := range specification.AcceptanceCriteria {
		if _, ok := validated[criterion.ID]; !ok {
			return fmt.Errorf("implementation plan does not cover acceptance criterion %q with test, proof, or validation", criterion.ID)
		}
		linked := false
		for _, step := range p.Steps {
			if step.Kind == ImplementationPlanStepImplementation {
				continue
			}
			for _, stepCriterion := range step.AcceptanceCriteria {
				if stepCriterion != criterion.ID {
					continue
				}
				for _, requirement := range step.Requirements {
					if _, ok := validPairs[traceabilityPair(requirement, criterion.ID)]; ok {
						linked = true
					}
				}
			}
		}
		if !linked {
			return fmt.Errorf("implementation plan acceptance criterion %q has no linked requirement proof path", criterion.ID)
		}
	}
	return nil
}

func traceabilityPair(requirementID, criterionID string) string {
	return requirementID + "\x00" + criterionID
}

func (p ImplementationPlan) validateStrategyOrder(contract ChangeContract) error {
	implementationIndexes := make([]int, 0)
	for i, step := range p.Steps {
		if step.Kind == ImplementationPlanStepImplementation {
			implementationIndexes = append(implementationIndexes, i)
		}
	}
	firstImplementation := implementationIndexes[0]
	lastImplementation := implementationIndexes[len(implementationIndexes)-1]
	if contract.RequiresRedGreen() {
		redProofIndex := -1
		redProofCount := 0
		validationCount := 0
		var finalValidation *ImplementationPlanStep
		for i, step := range p.Steps {
			if step.Kind == ImplementationPlanStepProof && containsPlanValue(step.ContractProofs, "regression") {
				redProofIndex = i
				redProofCount++
			}
		}
		if redProofCount != 1 {
			return errors.New("red_green plan must contain exactly one regression proof step")
		}
		testBeforeProof := false
		for i, step := range p.Steps {
			if step.Kind == ImplementationPlanStepTest {
				if i >= redProofIndex {
					return errors.New("red_green test steps must precede the Red proof step")
				}
				testBeforeProof = true
			}
			if step.Kind == ImplementationPlanStepValidation && i <= lastImplementation {
				return errors.New("red_green final validation must follow implementation steps")
			}
			if step.Kind == ImplementationPlanStepValidation {
				validationCount++
				finalValidation = &p.Steps[i]
			}
		}
		if !testBeforeProof || redProofIndex >= firstImplementation {
			return errors.New("red_green plan must order test, Red proof, and implementation")
		}
		if validationCount == 0 || finalValidation == nil {
			return errors.New("red_green plan must contain final validation after implementation")
		}
		if !hasPlanValues(finalValidation.ContractProofs, "regression", "behavior", "affected") {
			return errors.New("red_green final validation must reference regression, behavior, and affected proofs")
		}
		return nil
	}
	if contract.RequiresGreenGreen() {
		if len(implementationIndexes) == 0 {
			return errors.New("green_green plan requires implementation steps")
		}
		baselineCharacterization := false
		targetCharacterization := false
		lastTargetCharacterization := -1
		lastValidation := -1
		for i, step := range p.Steps {
			if step.Kind == ImplementationPlanStepProof && containsPlanValue(step.ContractProofs, "regression") {
				return errors.New("green_green plan must not introduce a Red proof step")
			}
			if step.Kind == ImplementationPlanStepTest && containsPlanValue(step.ContractProofs, "regression") {
				if i < firstImplementation {
					baselineCharacterization = true
				}
				if i > lastImplementation {
					targetCharacterization = true
					lastTargetCharacterization = i
				}
			}
			if step.Kind == ImplementationPlanStepValidation && i <= lastImplementation {
				return errors.New("green_green final validation must follow implementation steps")
			}
			if step.Kind == ImplementationPlanStepValidation {
				lastValidation = i
			}
		}
		if !baselineCharacterization || !targetCharacterization {
			return errors.New("green_green plan must order baseline characterization, implementation, and target characterization")
		}
		if lastValidation <= lastTargetCharacterization {
			return errors.New("green_green plan must contain final validation after target characterization")
		}
		finalValidation := p.Steps[lastValidation]
		if !hasPlanValues(finalValidation.ContractProofs, "behavior", "affected") {
			return errors.New("green_green final validation must reference behavior and affected proofs")
		}
		return nil
	}
	return errors.New("implementation plan strategy is unsupported for Change Contract")
}

func hasPlanValues(values []string, required ...string) bool {
	for _, target := range required {
		if !containsPlanValue(values, target) {
			return false
		}
	}
	return true
}

func containsPlanValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// TopologicalOrder returns a deterministic order, breaking ties by step ID.
func (p ImplementationPlan) TopologicalOrder() ([]string, error) {
	steps := make(map[string]ImplementationPlanStep, len(p.Steps))
	indegree := make(map[string]int, len(p.Steps))
	dependents := make(map[string][]string, len(p.Steps))
	for _, step := range p.Steps {
		if _, exists := steps[step.ID]; exists {
			return nil, fmt.Errorf("duplicate implementation plan step ID %q", step.ID)
		}
		steps[step.ID] = step
		indegree[step.ID] = 0
	}
	for _, step := range p.Steps {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			if _, ok := steps[dependency]; !ok {
				return nil, fmt.Errorf("step %q depends on unknown step %q", step.ID, dependency)
			}
			if dependency == step.ID {
				return nil, fmt.Errorf("step %q depends on itself", step.ID)
			}
			if _, ok := seen[dependency]; ok {
				return nil, fmt.Errorf("step %q has duplicate dependency %q", step.ID, dependency)
			}
			seen[dependency] = struct{}{}
			indegree[step.ID]++
			dependents[dependency] = append(dependents[dependency], step.ID)
		}
	}
	ready := make([]string, 0, len(steps))
	for id, degree := range indegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(steps))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(steps) {
		return nil, errors.New("implementation plan contains dependency cycle")
	}
	return order, nil
}
