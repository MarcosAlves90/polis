package policyload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
		return spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return spec.Policy{}, fmt.Errorf("Project Policy schema v%d required", spec.PolicySchemaVersion)
	}
	return policy, nil
}
