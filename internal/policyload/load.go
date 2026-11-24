package policyload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const maxPolicyBytes = 1 << 20

func LoadExternal(repo, filename string) ([]byte, spec.Policy, error) {
	raw, err := fileutil.ReadOutside(repo, filename, fileutil.OutsideReadOptions{Max: maxPolicyBytes, OversizeMessage: "Project Policy exceeds maximum size"})
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("read external Project Policy: %w", err)
	}
	policy, err := decodeV3(raw)
	if err != nil {
		return nil, spec.Policy{}, err
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("encode canonical Project Policy: %w", err)
	}
	return append(canonical, '\n'), policy, nil
}

func LoadCommitted(ctx context.Context, repo string) ([]byte, spec.Policy, error) {
	workingPath := filepath.Join(repo, ".polis", "policy.json")
	working, err := os.ReadFile(workingPath)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("read .polis/policy.json: %w", err)
	}
	committed, err := gitutil.Bytes(ctx, repo, nil, nil, "show", "HEAD:.polis/policy.json")
	if err != nil {
		return nil, spec.Policy{}, errors.New(".polis/policy.json must exist in HEAD")
	}
	if !bytes.Equal(working, committed) {
		return nil, spec.Policy{}, errors.New(".polis/policy.json working copy differs from HEAD")
	}
	policy, err := decodeV3(working)
	if err != nil {
		return nil, spec.Policy{}, err
	}
	return working, policy, nil
}

func decodeV3(raw []byte) (spec.Policy, error) {
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		if !strings.Contains(err.Error(), "requires explicit command environment") &&
			!strings.Contains(err.Error(), "not_applicable gate must contain exactly") {
			return spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
		}
		normalized, normalizeErr := normalizeProgrammaticPolicy(raw)
		if normalizeErr != nil {
			return spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
		}
		policy, err = spec.DecodePolicy(normalized)
		if err != nil {
			return spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
		}
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return spec.Policy{}, fmt.Errorf("Project Policy schema v%d required", spec.PolicySchemaVersion)
	}
	return policy, nil
}

// normalizeProgrammaticPolicy accepts external v3 policies assembled from a
// canonical policy whose mode or command was replaced programmatically: an
// omitted command environment becomes the inherited default, and a gate
// switched to not_applicable drops stale mode-specific fields. Unknown fields
// and every other validation rule are still enforced by spec.DecodePolicy.
func normalizeProgrammaticPolicy(raw []byte) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	gatesRaw, ok := document["gates"]
	if !ok {
		return nil, errors.New("policy has no gates")
	}
	var gates []json.RawMessage
	if err := json.Unmarshal(gatesRaw, &gates); err != nil {
		return nil, err
	}
	changed := false
	for index, gateRaw := range gates {
		var gate map[string]json.RawMessage
		if err := json.Unmarshal(gateRaw, &gate); err != nil {
			return nil, err
		}
		modeRaw, hasMode := gate["mode"]
		if hasMode && string(modeRaw) == `"not_applicable"` {
			reduced := map[string]json.RawMessage{}
			for _, key := range []string{"id", "mode", "reason"} {
				if value, present := gate[key]; present {
					reduced[key] = value
				}
			}
			encoded, err := json.Marshal(reduced)
			if err != nil {
				return nil, err
			}
			gates[index] = encoded
			changed = true
			continue
		}
		commandRaw, ok := gate["command"]
		if !ok || string(commandRaw) == "null" {
			continue
		}
		var command map[string]json.RawMessage
		if err := json.Unmarshal(commandRaw, &command); err != nil {
			return nil, err
		}
		if _, declared := command["environment"]; declared {
			continue
		}
		command["environment"] = json.RawMessage(`{"mode":"inherit"}`)
		encodedCommand, err := json.Marshal(command)
		if err != nil {
			return nil, err
		}
		gate["command"] = encodedCommand
		encodedGate, err := json.Marshal(gate)
		if err != nil {
			return nil, err
		}
		gates[index] = encodedGate
		changed = true
	}
	if !changed {
		return nil, errors.New("policy has no programmatic normalization available")
	}
	encodedGates, err := json.Marshal(gates)
	if err != nil {
		return nil, err
	}
	document["gates"] = encodedGates
	return json.Marshal(document)
}
