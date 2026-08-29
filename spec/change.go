package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ChangeContractSchemaVersion = 1

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
	Behavior      CommandSpec        `json:"behavior"`
	Affected      CommandSpec        `json:"affected"`
	Regression    RegressionContract `json:"regression"`
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
	if c.SchemaVersion != ChangeContractSchemaVersion {
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
