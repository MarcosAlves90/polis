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
	Path   string
	SHA256 string
}

func Start(ctx context.Context, opts Options) (Result, error) {
	if opts.Repo == "" || opts.Contract == "" || opts.Out == "" {
		return Result{}, errors.New("repo, contract, and out are required")
	}
	root, err := gitutil.ResolveRoot(ctx, opts.Repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return Result{}, err
	}
	status, err := gitutil.Output(ctx, root, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Result{}, fmt.Errorf("inspect source state: %w", err)
	}
	if status != "" {
		return Result{}, errors.New("polis start requires a clean worktree and index")
	}
	var policyRaw []byte
	if opts.Policy != "" {
		policyRaw, _, err = policyload.LoadExternal(root, opts.Policy)
	} else {
		policyRaw, _, err = policyload.LoadCommitted(ctx, root)
	}
	if err != nil {
		return Result{}, err
	}
	draftRaw, err := fileutil.ReadOutside(root, opts.Contract, fileutil.OutsideReadOptions{Max: 1 << 20, OversizeMessage: "input exceeds maximum size"})
	if err != nil {
		return Result{}, fmt.Errorf("load draft change contract: %w", err)
	}
	draft, err := spec.DecodeChangeContract(draftRaw)
	if err != nil {
		return Result{}, fmt.Errorf("invalid draft change contract: %w", err)
	}
	if draft.SchemaVersion != spec.StrictChangeContractSchemaVersion || draft.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV1 {
		return Result{}, errors.New("polis start requires a strict schema-v3 draft using strict_sdd_tdd_v1")
	}
	outAbs, err := filepath.Abs(opts.Out)
	if err != nil {
		return Result{}, err
	}
	contained, err := pathguard.Contains(root, outAbs)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output boundary: %w", err)
	}
	if contained {
		return Result{}, errors.New("output must be outside target worktree")
	}
	if _, err := os.Lstat(outAbs); err == nil {
		return Result{}, fmt.Errorf("output already exists: %s", outAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	lock, err := devlock.SnapshotWithPolicy(ctx, root, draft.Specification, policyRaw)
	if err != nil {
		return Result{}, fmt.Errorf("lock baseline: %w", err)
	}
	locked := draft
	locked.SchemaVersion = spec.LockedChangeContractSchemaVersion
	locked.DevelopmentMethod = spec.DevelopmentMethodStrictSDDTDDV2
	locked.BaselineLock = &lock
	if err := locked.Validate(); err != nil {
		return Result{}, fmt.Errorf("locked change contract failed self-validation: %w", err)
	}
	raw, err := json.MarshalIndent(locked, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode locked change contract: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return Result{}, err
	}
	f, err := os.OpenFile(outAbs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create output: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(outAbs)
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(outAbs)
		return Result{}, err
	}
	sum := sha256.Sum256(raw)
	return Result{Path: outAbs, SHA256: hex.EncodeToString(sum[:])}, nil
}
