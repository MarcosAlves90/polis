package devstart

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Options struct {
	Repo     string
	Policy   string
	Contract string
	Out      string
}

type Result struct {
	Path            string
	SHA256          string
	ValidationLevel string
	EnabledGates    []string
	DisabledGates   []string
}

func Start(ctx context.Context, opts Options) (Result, error) {
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	root, err := resolveCleanRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	policyRaw, policy, err := loadPolicy(ctx, root, opts.Policy)
	if err != nil {
		return Result{}, err
	}
	draft, err := loadDraft(root, opts.Contract)
	if err != nil {
		return Result{}, err
	}
	outAbs, err := validateOutputPath(root, opts.Out)
	if err != nil {
		return Result{}, err
	}
	locked, err := lockContract(ctx, root, draft, policyRaw)
	if err != nil {
		return Result{}, err
	}
	raw, err := encodeLockedContract(locked)
	if err != nil {
		return Result{}, err
	}
	if err := writeLockedContract(outAbs, raw); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(raw)
	summary := policy.ValidationSummary()
	return Result{
		Path:            outAbs,
		SHA256:          hex.EncodeToString(sum[:]),
		ValidationLevel: summary.Level,
		EnabledGates:    append([]string{}, summary.EnabledGates...),
		DisabledGates:   append([]string{}, summary.DisabledGates...),
	}, nil
}

func validateOptions(opts Options) error {
	if opts.Repo == "" || opts.Contract == "" || opts.Out == "" {
		return errors.New("repo, contract, and out are required")
	}
	return nil
}

func resolveCleanRepo(ctx context.Context, repo string) (string, error) {
	root, err := gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return "", err
	}
	status, err := gitutil.Output(ctx, root, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("inspect source state: %w", err)
	}
	if status != "" {
		return "", errors.New("polis start requires a clean worktree and index")
	}
	return root, nil
}

func loadPolicy(ctx context.Context, root, policyPath string) ([]byte, spec.Policy, error) {
	if policyPath != "" {
		return policyload.LoadExternal(root, policyPath)
	}
	return policyload.LoadCommitted(ctx, root)
}

func loadDraft(root, contractPath string) (spec.ChangeContract, error) {
	draftRaw, err := fileutil.ReadOutside(root, contractPath, fileutil.OutsideReadOptions{Max: 1 << 20, OversizeMessage: "input exceeds maximum size"})
	if err != nil {
		return spec.ChangeContract{}, fmt.Errorf("load draft change contract: %w", err)
	}
	draft, err := spec.DecodeChangeContract(draftRaw)
	if err != nil {
		return spec.ChangeContract{}, fmt.Errorf("invalid draft change contract: %w", err)
	}
	if draft.SchemaVersion != spec.StrictChangeContractSchemaVersion || draft.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV1 {
		return spec.ChangeContract{}, errors.New("polis start requires a strict schema-v3 draft using strict_sdd_tdd_v1")
	}
	return draft, nil
}

func validateOutputPath(root, outputPath string) (string, error) {
	outAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return "", err
	}
	contained, err := pathguard.Contains(root, outAbs)
	if err != nil {
		return "", fmt.Errorf("resolve output boundary: %w", err)
	}
	if contained {
		return "", errors.New("output must be outside target worktree")
	}
	if _, err := os.Lstat(outAbs); err == nil {
		return "", fmt.Errorf("output already exists: %s", outAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return outAbs, nil
}

func lockContract(ctx context.Context, root string, draft spec.ChangeContract, policyRaw []byte) (spec.ChangeContract, error) {
	lock, err := devlock.SnapshotWithPolicy(ctx, root, draft.Specification, policyRaw)
	if err != nil {
		return spec.ChangeContract{}, fmt.Errorf("lock baseline: %w", err)
	}
	locked := draft
	locked.SchemaVersion = spec.LockedChangeContractSchemaVersion
	locked.DevelopmentMethod = spec.DevelopmentMethodStrictSDDTDDV2
	locked.BaselineLock = &lock
	if err := locked.Validate(); err != nil {
		return spec.ChangeContract{}, fmt.Errorf("locked change contract failed self-validation: %w", err)
	}
	return locked, nil
}

func encodeLockedContract(locked spec.ChangeContract) ([]byte, error) {
	raw, err := json.MarshalIndent(locked, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode locked change contract: %w", err)
	}
	return append(raw, '\n'), nil
}

func writeLockedContract(outputPath string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(outputPath)
		return err
	}
	return nil
}
