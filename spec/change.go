package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ChangeContractSchemaVersion       = 2
	LegacyChangeContractSchemaVersion = 1
)

const (
	ChangeKindFeature            = "feature"
	ChangeKindDefect             = "defect"
	ChangeKindBehaviorPreserving = "behavior_preserving"

	RegressionModeRedGreen      = "red_green"
	RegressionModeNotApplicable = "not_applicable"
	RegressionReasonNotDefect   = "not-a-defect"
)

type RegressionContract struct {
	Mode                   string       `json:"mode"`
	Command                *CommandSpec `json:"command,omitempty"`
	BaselineExitCode       *int         `json:"baseline_exit_code,omitempty"`
	BaselineOutputContains []string     `json:"baseline_output_contains,omitempty"`
	ReasonCode             string       `json:"reason_code,omitempty"`
}

type ChangeContract struct {
	SchemaVersion int                `json:"schema_version"`
	Kind          string             `json:"kind"`
	Scope         *ChangeScope       `json:"scope,omitempty"`
	Behavior      CommandSpec        `json:"behavior"`
	Affected      CommandSpec        `json:"affected"`
	Regression    RegressionContract `json:"regression"`
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
	if c.SchemaVersion != ChangeContractSchemaVersion && c.SchemaVersion != LegacyChangeContractSchemaVersion {
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
	if err := c.Regression.Validate(c.Kind); err != nil {
		return fmt.Errorf("regression: %w", err)
	}
	if c.SchemaVersion == ChangeContractSchemaVersion {
		if c.Scope == nil {
			return errors.New("change contract schema v2 requires scope")
		}
		if err := c.Scope.Validate(); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		if err := requireExplicitEnvironment(c); err != nil {
			return err
		}
	} else if c.Scope != nil {
		return errors.New("change contract schema v1 must not contain scope")
	}
	return nil
}

func requireExplicitEnvironment(c ChangeContract) error {
	commands := []struct {
		name string
		cmd  *CommandSpec
	}{
		{name: "behavior", cmd: &c.Behavior},
		{name: "affected", cmd: &c.Affected},
	}
	if c.Kind == ChangeKindDefect {
		commands = append(commands, struct {
			name string
			cmd  *CommandSpec
		}{name: "regression", cmd: c.Regression.Command})
	}
	for _, item := range commands {
		if item.cmd == nil || item.cmd.Environment == nil {
			return fmt.Errorf("%s: change contract schema v2 requires explicit command environment", item.name)
		}
	}
	return nil
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

func (r RegressionContract) Validate(kind string) error {
	if kind == ChangeKindDefect {
		return r.validateDefect()
	}
	return r.validateNonDefect()
}

func (r RegressionContract) validateDefect() error {
	if r.Mode != RegressionModeRedGreen {
		return errors.New("defect requires red_green regression mode")
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
