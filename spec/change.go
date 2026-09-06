package spec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	LegacyChangeContractSchemaVersion = 1
	ChangeContractSchemaVersion       = 2
	StrictChangeContractSchemaVersion = 3
	LockedChangeContractSchemaVersion = 4
)

const (
	ChangeKindFeature            = "feature"
	ChangeKindDefect             = "defect"
	ChangeKindBehaviorPreserving = "behavior_preserving"

	RegressionModeRedGreen      = "red_green"
	RegressionModeGreenGreen    = "green_green"
	RegressionModeNotApplicable = "not_applicable"
	RegressionReasonNotDefect   = "not-a-defect"

	DevelopmentMethodStrictSDDTDDV1 = "strict_sdd_tdd_v1"
	DevelopmentMethodStrictSDDTDDV2 = "strict_sdd_tdd_v2"
)

type RegressionContract struct {
	Mode                   string       `json:"mode"`
	Command                *CommandSpec `json:"command,omitempty"`
	BaselineExitCode       *int         `json:"baseline_exit_code,omitempty"`
	BaselineOutputContains []string     `json:"baseline_output_contains,omitempty"`
	ReasonCode             string       `json:"reason_code,omitempty"`
}

type SpecificationClause struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

const ProofGateRegression = "regression"

type AcceptanceCriterion struct {
	ID           string   `json:"id"`
	Statement    string   `json:"statement"`
	Requirements []string `json:"requirements"`
	Proof        string   `json:"proof"`
}

type TraceabilityLink struct {
	RequirementID         string `json:"requirement_id"`
	AcceptanceCriterionID string `json:"acceptance_criterion_id"`
	Proof                 string `json:"proof"`
}

type DevelopmentSpecification struct {
	Objective          string                `json:"objective"`
	Requirements       []SpecificationClause `json:"requirements"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`
	Invariants         []SpecificationClause `json:"invariants"`
	ForbiddenStates    []SpecificationClause `json:"forbidden_states"`
	Inputs             []SpecificationClause `json:"inputs"`
	Outputs            []SpecificationClause `json:"outputs"`
	FailureSemantics   []SpecificationClause `json:"failure_semantics"`
}

type BaselineLock struct {
	GitObjectFormat     string `json:"git_object_format"`
	BaseCommit          string `json:"base_commit"`
	BaseTree            string `json:"base_tree"`
	PolicySHA256        string `json:"policy_sha256"`
	SpecificationSHA256 string `json:"specification_sha256"`
}

type ChangeContract struct {
	SchemaVersion     int                       `json:"schema_version"`
	Kind              string                    `json:"kind"`
	Scope             *ChangeScope              `json:"scope,omitempty"`
	TestScope         *ChangeScope              `json:"test_scope,omitempty"`
	DevelopmentMethod string                    `json:"development_method,omitempty"`
	Specification     *DevelopmentSpecification `json:"specification,omitempty"`
	BaselineLock      *BaselineLock             `json:"baseline_lock,omitempty"`
	Behavior          CommandSpec               `json:"behavior"`
	Affected          CommandSpec               `json:"affected"`
	Regression        RegressionContract        `json:"regression"`
}

type ChangeScope struct {
	AllowedPaths []string `json:"allowed_paths"`
}

func DecodeChangeContract(raw []byte) (ChangeContract, error) {
	var c ChangeContract
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return ChangeContract{}, fmt.Errorf("decode change contract: %w", err)
	}
	if err := ensureDecoderEOF(dec, "change contract"); err != nil {
		return ChangeContract{}, err
	}
	if err := c.Validate(); err != nil {
		return ChangeContract{}, err
	}
	return c, nil
}

func (c ChangeContract) Validate() error {
	if c.SchemaVersion != LegacyChangeContractSchemaVersion && c.SchemaVersion != ChangeContractSchemaVersion && c.SchemaVersion != StrictChangeContractSchemaVersion && c.SchemaVersion != LockedChangeContractSchemaVersion {
		return fmt.Errorf("unsupported change contract schema_version %d", c.SchemaVersion)
	}
	switch c.Kind {
	case ChangeKindFeature, ChangeKindDefect, ChangeKindBehaviorPreserving:
	default:
		return fmt.Errorf("unsupported change kind %q", c.Kind)
	}
	if err := c.Behavior.Validate(); err != nil {
		return fmt.Errorf("behavior: %w", err)
	}
	if err := c.Affected.Validate(); err != nil {
		return fmt.Errorf("affected: %w", err)
	}
	if c.IsStrictDevelopment() {
		if err := c.validateStrictDevelopment(); err != nil {
			return err
		}
		if err := c.Regression.validateStrict(c.Kind); err != nil {
			return fmt.Errorf("regression: %w", err)
		}
	} else {
		if err := c.Regression.Validate(c.Kind); err != nil {
			return fmt.Errorf("regression: %w", err)
		}
	}
	if c.SchemaVersion == ChangeContractSchemaVersion || c.IsStrictDevelopment() {
		if c.Scope == nil {
			return fmt.Errorf("change contract schema v%d requires scope", c.SchemaVersion)
		}
		if err := c.Scope.Validate(); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		if err := requireExplicitEnvironment(c); err != nil {
			return err
		}
	} else {
		if c.Scope != nil {
			return errors.New("change contract schema v1 must not contain scope")
		}
		if c.TestScope != nil || c.DevelopmentMethod != "" || c.Specification != nil || c.BaselineLock != nil {
			return errors.New("change contract schema v1 must not contain strict development fields")
		}
	}
	if c.SchemaVersion == ChangeContractSchemaVersion && (c.TestScope != nil || c.DevelopmentMethod != "" || c.Specification != nil || c.BaselineLock != nil) {
		return errors.New("change contract schema v2 must not contain strict development fields")
	}
	return nil
}

func (c ChangeContract) validateStrictDevelopment() error {
	wantMethod := DevelopmentMethodStrictSDDTDDV1
	if c.SchemaVersion == LockedChangeContractSchemaVersion {
		wantMethod = DevelopmentMethodStrictSDDTDDV2
	}
	if c.DevelopmentMethod != wantMethod {
		return fmt.Errorf("development_method must be %q", wantMethod)
	}
	if c.Scope == nil {
		return fmt.Errorf("change contract schema v%d requires scope", c.SchemaVersion)
	}
	if c.TestScope == nil {
		return fmt.Errorf("change contract schema v%d requires test_scope", c.SchemaVersion)
	}
	if err := c.TestScope.Validate(); err != nil {
		return fmt.Errorf("test_scope: %w", err)
	}
	for _, entry := range c.TestScope.AllowedPaths {
		if !c.Scope.AllowsScopeEntry(entry) {
			return fmt.Errorf("test_scope entry %q is outside change scope", entry)
		}
	}
	if c.Specification == nil {
		return fmt.Errorf("change contract schema v%d requires specification", c.SchemaVersion)
	}
	if err := c.Specification.Validate(); err != nil {
		return fmt.Errorf("specification: %w", err)
	}
	if c.SchemaVersion == StrictChangeContractSchemaVersion {
		if c.BaselineLock != nil {
			return errors.New("change contract schema v3 must not contain baseline_lock")
		}
		return nil
	}
	if c.BaselineLock == nil {
		return errors.New("change contract schema v4 requires baseline_lock")
	}
	if err := c.BaselineLock.Validate(); err != nil {
		return fmt.Errorf("baseline_lock: %w", err)
	}
	digest, err := c.Specification.SHA256()
	if err != nil {
		return fmt.Errorf("specification digest: %w", err)
	}
	if c.BaselineLock.SpecificationSHA256 != digest {
		return errors.New("baseline_lock specification_sha256 does not match specification")
	}
	return nil
}

func (b BaselineLock) Validate() error {
	if b.GitObjectFormat != "sha1" && b.GitObjectFormat != "sha256" {
		return fmt.Errorf("unsupported git_object_format %q", b.GitObjectFormat)
	}
	objectLen := 40
	if b.GitObjectFormat == "sha256" {
		objectLen = 64
	}
	if !isLowerHexLen(b.BaseCommit, objectLen) {
		return errors.New("base_commit is not a canonical object id")
	}
	if !isLowerHexLen(b.BaseTree, objectLen) {
		return errors.New("base_tree is not a canonical object id")
	}
	if !isLowerHexLen(b.PolicySHA256, 64) {
		return errors.New("policy_sha256 is not a lowercase SHA-256")
	}
	if !isLowerHexLen(b.SpecificationSHA256, 64) {
		return errors.New("specification_sha256 is not a lowercase SHA-256")
	}
	return nil
}

func isLowerHexLen(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (s DevelopmentSpecification) SHA256() (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s DevelopmentSpecification) Validate() error {
	if strings.TrimSpace(s.Objective) == "" {
		return errors.New("objective must be non-empty")
	}
	seen := map[string]struct{}{}
	requirementIDs := map[string]struct{}{}
	coveredRequirements := map[string]struct{}{}
	if err := validateSpecificationClauses("requirements", s.Requirements, seen); err != nil {
		return err
	}
	for _, requirement := range s.Requirements {
		requirementIDs[requirement.ID] = struct{}{}
	}
	if len(s.AcceptanceCriteria) == 0 {
		return errors.New("acceptance_criteria must contain at least one clause")
	}
	for i, criterion := range s.AcceptanceCriteria {
		id := strings.TrimSpace(criterion.ID)
		if id == "" {
			return fmt.Errorf("acceptance_criteria[%d].id must be non-empty", i)
		}
		if strings.TrimSpace(criterion.Statement) == "" {
			return fmt.Errorf("acceptance_criteria[%d].statement must be non-empty", i)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate specification clause id %q", criterion.ID)
		}
		seen[id] = struct{}{}
		if len(criterion.Requirements) == 0 {
			return fmt.Errorf("acceptance_criteria[%d].requirements must contain at least one requirement", i)
		}
		refs := map[string]struct{}{}
		for j, requirementID := range criterion.Requirements {
			if strings.TrimSpace(requirementID) == "" {
				return fmt.Errorf("acceptance_criteria[%d].requirements[%d] must be non-empty", i, j)
			}
			if _, ok := refs[requirementID]; ok {
				return fmt.Errorf("acceptance_criteria[%d].requirements contains duplicate requirement %q", i, requirementID)
			}
			refs[requirementID] = struct{}{}
			if _, ok := requirementIDs[requirementID]; !ok {
				return fmt.Errorf("acceptance_criteria[%d] references unknown requirement %q", i, requirementID)
			}
			coveredRequirements[requirementID] = struct{}{}
		}
		if criterion.Proof != ProofGateRegression {
			return fmt.Errorf("acceptance_criteria[%d].proof must be %q", i, ProofGateRegression)
		}
	}
	for _, requirement := range s.Requirements {
		if _, ok := coveredRequirements[requirement.ID]; !ok {
			return fmt.Errorf("requirement %q is not covered by any acceptance criterion", requirement.ID)
		}
	}
	sections := []struct {
		name    string
		clauses []SpecificationClause
	}{
		{name: "invariants", clauses: s.Invariants},
		{name: "forbidden_states", clauses: s.ForbiddenStates},
		{name: "inputs", clauses: s.Inputs},
		{name: "outputs", clauses: s.Outputs},
		{name: "failure_semantics", clauses: s.FailureSemantics},
	}
	for _, section := range sections {
		if err := validateSpecificationClauses(section.name, section.clauses, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateSpecificationClauses(name string, clauses []SpecificationClause, seen map[string]struct{}) error {
	if len(clauses) == 0 {
		return fmt.Errorf("%s must contain at least one clause", name)
	}
	for i, clause := range clauses {
		id := strings.TrimSpace(clause.ID)
		if id == "" {
			return fmt.Errorf("%s[%d].id must be non-empty", name, i)
		}
		if strings.TrimSpace(clause.Statement) == "" {
			return fmt.Errorf("%s[%d].statement must be non-empty", name, i)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate specification clause id %q", clause.ID)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s DevelopmentSpecification) TraceabilityLinks() []TraceabilityLink {
	links := make([]TraceabilityLink, 0)
	for _, criterion := range s.AcceptanceCriteria {
		for _, requirementID := range criterion.Requirements {
			links = append(links, TraceabilityLink{RequirementID: requirementID, AcceptanceCriterionID: criterion.ID, Proof: criterion.Proof})
		}
	}
	return links
}

func (c ChangeContract) IsStrictDevelopment() bool {
	return c.SchemaVersion == StrictChangeContractSchemaVersion || c.SchemaVersion == LockedChangeContractSchemaVersion
}

func (c ChangeContract) RequiresRedGreen() bool {
	if c.Kind == ChangeKindDefect {
		return true
	}
	return c.IsStrictDevelopment() && c.Kind == ChangeKindFeature
}

func (c ChangeContract) RequiresGreenGreen() bool {
	return c.IsStrictDevelopment() && c.Kind == ChangeKindBehaviorPreserving
}

func (c ChangeContract) RequiresBaselineProof() bool {
	return c.RequiresRedGreen() || c.RequiresGreenGreen()
}

func (c ChangeContract) RequiresRegressionPatch() bool {
	return c.RequiresRedGreen()
}

func requireExplicitEnvironment(c ChangeContract) error {
	commands := []struct {
		name string
		cmd  *CommandSpec
	}{
		{name: "behavior", cmd: &c.Behavior},
		{name: "affected", cmd: &c.Affected},
	}
	if c.RequiresBaselineProof() {
		commands = append(commands, struct {
			name string
			cmd  *CommandSpec
		}{name: "regression", cmd: c.Regression.Command})
	}
	for _, item := range commands {
		if item.cmd == nil || item.cmd.Environment == nil {
			return fmt.Errorf("%s: change contract schema v%d requires explicit command environment", item.name, c.SchemaVersion)
		}
	}
	return nil
}

func (s ChangeScope) AllowsScopeEntry(entry string) bool {
	for _, allowed := range s.AllowedPaths {
		if allowed == "." || allowed == entry {
			return true
		}
		if strings.HasSuffix(allowed, "/") {
			if strings.HasSuffix(entry, "/") {
				if strings.HasPrefix(entry, allowed) {
					return true
				}
			} else if strings.HasPrefix(entry, allowed) {
				return true
			}
		}
	}
	return false
}

func (s ChangeScope) Validate() error {
	if len(s.AllowedPaths) == 0 {
		return errors.New("allowed_paths must contain at least one path")
	}
	seen := map[string]struct{}{}
	for i, value := range s.AllowedPaths {
		if err := validateScopePath(value); err != nil {
			return fmt.Errorf("allowed_paths[%d]: %w", i, err)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("allowed_paths contains duplicate path %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateScopePath(value string) error {
	if value == "." {
		return nil
	}
	if strings.HasSuffix(value, "/") {
		base := strings.TrimSuffix(value, "/")
		if base == "" {
			return errors.New("directory prefix must not be root slash")
		}
		return ValidateRepoRelativePath(base)
	}
	return ValidateRepoRelativePath(value)
}

func (c ChangeContract) AllowsPath(repoPath string) bool {
	if c.SchemaVersion == LegacyChangeContractSchemaVersion || c.Scope == nil {
		return true
	}
	for _, allowed := range c.Scope.AllowedPaths {
		if allowed == "." || allowed == repoPath {
			return true
		}
		if strings.HasSuffix(allowed, "/") && strings.HasPrefix(repoPath, allowed) {
			return true
		}
	}
	return false
}

func (c ChangeContract) ValidateChangedPaths(paths []string) error {
	for _, repoPath := range paths {
		if err := ValidateRepoRelativePath(repoPath); err != nil {
			return fmt.Errorf("changed path %q is invalid: %w", repoPath, err)
		}
		if !c.AllowsPath(repoPath) {
			return fmt.Errorf("changed path %q is outside change scope", repoPath)
		}
	}
	return nil
}

func (c ChangeContract) ValidateTestPaths(paths []string) error {
	if !c.IsStrictDevelopment() || c.TestScope == nil {
		return nil
	}
	for _, repoPath := range paths {
		if err := ValidateRepoRelativePath(repoPath); err != nil {
			return fmt.Errorf("test path %q is invalid: %w", repoPath, err)
		}
		if !c.TestScopeAllowsPath(repoPath) {
			return fmt.Errorf("Red probe path %q is outside test scope", repoPath)
		}
	}
	return nil
}

func (c ChangeContract) TestScopeAllowsPath(repoPath string) bool {
	if c.TestScope == nil {
		return false
	}
	for _, allowed := range c.TestScope.AllowedPaths {
		if allowed == "." || allowed == repoPath {
			return true
		}
		if strings.HasSuffix(allowed, "/") && strings.HasPrefix(repoPath, allowed) {
			return true
		}
	}
	return false
}

func (r RegressionContract) Validate(kind string) error {
	if kind == ChangeKindDefect {
		return r.validateRedGreen("defect")
	}
	return r.validateNonDefect()
}

func (r RegressionContract) validateStrict(kind string) error {
	switch kind {
	case ChangeKindFeature, ChangeKindDefect:
		return r.validateRedGreen(kind)
	case ChangeKindBehaviorPreserving:
		return r.validateGreenGreen()
	default:
		return fmt.Errorf("unsupported strict change kind %q", kind)
	}
}

func (r RegressionContract) validateGreenGreen() error {
	if r.Mode != RegressionModeGreenGreen {
		return errors.New("behavior_preserving requires green_green regression mode")
	}
	if r.Command == nil {
		return errors.New("green_green regression requires command")
	}
	if r.BaselineExitCode != nil || len(r.BaselineOutputContains) != 0 || r.ReasonCode != "" {
		return errors.New("green_green regression must not contain Red-only oracle fields")
	}
	if err := r.Command.Validate(); err != nil {
		return fmt.Errorf("command: %w", err)
	}
	return nil
}

func (r RegressionContract) validateRedGreen(kind string) error {
	if r.Mode != RegressionModeRedGreen {
		return fmt.Errorf("%s requires red_green regression mode", kind)
	}
	if r.Command == nil || r.BaselineExitCode == nil || r.ReasonCode != "" {
		return errors.New("red_green regression requires command and baseline_exit_code only")
	}
	if err := r.Command.Validate(); err != nil {
		return fmt.Errorf("command: %w", err)
	}
	if *r.BaselineExitCode < 1 || *r.BaselineExitCode > 255 {
		return errors.New("baseline_exit_code must be between 1 and 255")
	}
	return validateBaselineOutputTokens(r.BaselineOutputContains)
}

func validateBaselineOutputTokens(tokens []string) error {
	if len(tokens) == 0 {
		return errors.New("baseline_output_contains must contain at least one token")
	}
	seen := map[string]struct{}{}
	for i, token := range tokens {
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("baseline_output_contains[%d] must be non-empty", i)
		}
		if _, ok := seen[token]; ok {
			return fmt.Errorf("baseline_output_contains contains duplicate token %q", token)
		}
		seen[token] = struct{}{}
	}
	return nil
}

func (r RegressionContract) validateNonDefect() error {
	if r.Mode != RegressionModeNotApplicable {
		return errors.New("non-defect change requires not_applicable regression mode")
	}
	if r.Command != nil || r.BaselineExitCode != nil || len(r.BaselineOutputContains) != 0 {
		return errors.New("not_applicable regression must not contain command or baseline oracle")
	}
	if r.ReasonCode != RegressionReasonNotDefect {
		return fmt.Errorf("not_applicable regression reason_code must be %q", RegressionReasonNotDefect)
	}
	return nil
}
