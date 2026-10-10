// Package policyorigin preserves provenance recorded when polis init resolves a
// profile into concrete policy gate declarations. It never reconstructs the
// source of a declaration by comparing it with a current profile template.
package policyorigin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	Filename             = "policy-provenance.json"
	SchemaVersion        = 1
	SourceProfileDefault = "profile_default"
	SourcePolicyExplicit = "policy_explicit"
)

type Decision struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type GateRecord struct {
	ID         string `json:"id"`
	GateSHA256 string `json:"gate_sha256"`
	Decision
}

// LevelRecord captures the original field representation as well as the
// decision made by init. Strict remains omitted in policy v3 for compatibility.
type LevelRecord struct {
	PolicyValue string `json:"policy_value"`
	Decision
}

type Manifest struct {
	SchemaVersion   int          `json:"schema_version"`
	Profile         string       `json:"profile"`
	ValidationLevel *LevelRecord `json:"validation_level,omitempty"`
	Gates           []GateRecord `json:"gates"`
}

// Create records which resolver assigned each policy declaration. Decisions
// must be supplied by the generator at the same point that it chooses values.
func Create(profile string, policy spec.Policy, decisions map[string]Decision) (Manifest, error) {
	if profile != "go" && profile != "custom" {
		return Manifest{}, fmt.Errorf("unsupported provenance profile %q", profile)
	}
	if err := policy.Validate(); err != nil {
		return Manifest{}, err
	}
	if len(decisions) != len(policy.Gates) {
		return Manifest{}, errors.New("provenance must cover every generated gate")
	}
	m := Manifest{SchemaVersion: SchemaVersion, Profile: profile, Gates: make([]GateRecord, 0, len(policy.Gates))}
	for _, gate := range policy.Gates {
		decision, ok := decisions[gate.ID]
		if !ok || !validDecision(decision) {
			return Manifest{}, fmt.Errorf("missing or invalid provenance for gate %q", gate.ID)
		}
		hash, err := gateHash(gate)
		if err != nil {
			return Manifest{}, err
		}
		m.Gates = append(m.Gates, GateRecord{ID: gate.ID, GateSHA256: hash, Decision: decision})
	}
	return m, nil
}

func Encode(m Manifest) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func Decode(raw []byte) (Manifest, error) {
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return Manifest{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, errors.New("provenance must contain exactly one JSON value")
	}
	if err := m.validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) validate() error {
	if m.SchemaVersion != SchemaVersion || (m.Profile != "go" && m.Profile != "custom") {
		return errors.New("invalid provenance schema version or profile")
	}
	if len(m.Gates) != len(spec.ProjectGateOrder) {
		return errors.New("provenance requires the complete canonical gate inventory")
	}
	if record := m.ValidationLevel; record != nil {
		if record.PolicyValue != "" && record.PolicyValue != spec.ValidationLevelStandard && record.PolicyValue != spec.ValidationLevelMinimal && record.PolicyValue != spec.ValidationLevelStrict {
			return errors.New("invalid provenance validation level")
		}
		if record.Decision != (Decision{Source: SourceProfileDefault, Reason: "init_default_validation_level"}) &&
			record.Decision != (Decision{Source: SourcePolicyExplicit, Reason: "init_explicit_validation_level"}) {
			return errors.New("invalid validation level decision")
		}
		if record.Decision.Source == SourceProfileDefault && record.PolicyValue != "" {
			return errors.New("default validation level must be omitted in generated policy")
		}
	}
	for i, gate := range m.Gates {
		if gate.ID != spec.ProjectGateOrder[i] || len(gate.GateSHA256) != 64 || !validDecision(gate.Decision) {
			return fmt.Errorf("invalid provenance for gate %q", gate.ID)
		}
		b, err := hex.DecodeString(gate.GateSHA256)
		if err != nil || hex.EncodeToString(b) != gate.GateSHA256 {
			return fmt.Errorf("invalid gate digest for %q", gate.ID)
		}
	}
	return nil
}

// MatchValidationLevel returns the recorded choice only while the policy still
// has the same field representation. No history is inferred from its value.
func (m Manifest) MatchValidationLevel(policy spec.Policy) (Decision, bool) {
	if m.validate() != nil || m.ValidationLevel == nil || m.ValidationLevel.PolicyValue != policy.ValidationLevel {
		return Decision{}, false
	}
	return m.ValidationLevel.Decision, true
}

func validDecision(d Decision) bool {
	switch d.Source {
	case SourceProfileDefault:
		return d.Reason == "generated_by_profile" || d.Reason == "profile_validation_level_default"
	case SourcePolicyExplicit:
		return d.Reason == "init_explicit_command" || d.Reason == "init_explicit_disable_gate"
	}
	return false
}

// Match yields provenance only for declarations still identical to the actual
// generated gate. Stale, unrelated or incomplete evidence cannot be applied.
func (m Manifest) Match(policy spec.Policy) map[string]Decision {
	matches := make(map[string]Decision)
	if m.validate() != nil || len(policy.Gates) != len(m.Gates) {
		return matches
	}
	for i, gate := range policy.Gates {
		record := m.Gates[i]
		hash, err := gateHash(gate)
		if err == nil && gate.ID == record.ID && hash == record.GateSHA256 {
			matches[gate.ID] = record.Decision
		}
	}
	return matches
}

func gateHash(gate spec.GatePolicy) (string, error) {
	canonical, err := json.Marshal(gate)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
