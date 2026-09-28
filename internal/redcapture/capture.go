package redcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/changeexec"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Options struct {
	Repo               string
	Contract           string
	ImplementationPlan string
	Out                string
}

type Result struct {
	Path          string
	SHA256        string
	RetainedPaths []string `json:"retained_paths,omitempty"`
}

const gitRevParse = "rev-parse"

type sourceSnapshot struct {
	head      string
	indexTree string
	status    string
}

func Capture(ctx context.Context, opts Options) (Result, error) {
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	contract, contractRaw, err := loadRedGreenContract(repo, opts.Contract, retention)
	if err != nil {
		return Result{}, err
	}
	if opts.ImplementationPlan != "" {
		if _, _, err := implementationplan.LoadWithRetention(repo, opts.ImplementationPlan, contract, contractRaw, retention); err != nil {
			return Result{}, fmt.Errorf("invalid implementation plan: %w", err)
		}
	}
	if err := devlock.ValidateRepository(ctx, repo, contract); err != nil {
		return Result{}, fmt.Errorf("locked development baseline: %w", err)
	}
	outAbs, err := resolveOutputPath(repo, opts.Out)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := snapshotSource(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	patch, err := capturePatch(ctx, repo, snapshot.head)
	if err != nil {
		return Result{}, err
	}
	if len(patch) == 0 {
		return Result{}, errors.New("captured regression patch is empty")
	}
	if err := validateProbe(ctx, repo, snapshot.head, patch, contract); err != nil {
		return Result{}, err
	}
	if err := writeCapturedPatch(outAbs, patch); err != nil {
		return Result{}, err
	}
	if err := verifySourceSnapshot(ctx, repo, snapshot); err != nil {
		_ = os.Remove(outAbs)
		return Result{}, err
	}
	retainedPath, err := retention.Publish(repo, "proofs", patch)
	if err != nil {
		_ = os.Remove(outAbs)
		return Result{}, fmt.Errorf("retain regression proof: %w", err)
	}
	sum := sha256.Sum256(patch)
	return Result{Path: outAbs, SHA256: hex.EncodeToString(sum[:]), RetainedPaths: retainedResult(retainedPath)}, nil
}

func validateOptions(opts Options) error {
	if opts.Repo == "" || opts.Contract == "" || opts.Out == "" {
		return errors.New("repo, contract, and out are required")
	}
	return nil
}

func loadRedGreenContract(repo, filename string, retention artifactretention.State) (spec.ChangeContract, []byte, error) {
	contractRaw, err := retention.ReadInput(repo, filename, "contracts", 1<<20, "input exceeds maximum size")
	if err != nil {
		return spec.ChangeContract{}, nil, fmt.Errorf("load change contract: %w", err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		return spec.ChangeContract{}, nil, fmt.Errorf("invalid change contract: %w", err)
	}
	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 || contract.BaselineLock == nil {
		return spec.ChangeContract{}, nil, errors.New("POLIS V6 capture-red requires locked Change Contract schema v4 or v6 produced by polis start")
	}
	if !contract.RequiresRedGreen() {
		return spec.ChangeContract{}, nil, errors.New("capture-red requires a red_green change contract")
	}
	return contract, contractRaw, nil
}

func resolveOutputPath(repo, output string) (string, error) {
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	contained, err := pathguard.Contains(repo, outAbs)
	if err != nil {
		return "", fmt.Errorf("resolve output boundary: %w", err)
	}
	if contained {
		return "", errors.New("output must be outside target worktree")
	}
	if err := requireAbsentOutput(outAbs); err != nil {
		return "", err
	}
	return outAbs, nil
}

func requireAbsentOutput(filename string) error {
	_, err := os.Lstat(filename)
	if err == nil {
		return fmt.Errorf("output already exists: %s", filename)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func snapshotSource(ctx context.Context, repo string) (sourceSnapshot, error) {
	if err := requireCleanIndex(ctx, repo); err != nil {
		return sourceSnapshot{}, err
	}
	head, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return sourceSnapshot{}, err
	}
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return sourceSnapshot{}, err
	}
	status, err := artifactretention.WorktreeStatus(ctx, repo)
	if err != nil {
		return sourceSnapshot{}, err
	}
	if len(status) == 0 {
		return sourceSnapshot{}, errors.New("working tree has no non-ignored changes")
	}
	return sourceSnapshot{head: head, indexTree: indexTree, status: string(status)}, nil
}

func writeCapturedPatch(filename string, patch []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	if err := writeAndClose(f, patch); err != nil {
		_ = os.Remove(filename)
		return err
	}
	raw, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(raw, patch) {
		_ = os.Remove(filename)
		return errors.New("written regression patch does not match validated bytes")
	}
	return nil
}

func writeAndClose(f *os.File, patch []byte) error {
	if _, err := f.Write(patch); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func verifySourceSnapshot(ctx context.Context, repo string, want sourceSnapshot) error {
	if got, _ := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "HEAD"); got != want.head {
		return errors.New("source HEAD changed during capture")
	}
	if got, _ := gitutil.Output(ctx, repo, nil, nil, "write-tree"); got != want.indexTree {
		return errors.New("source index changed during capture")
	}
	gotStatus, err := artifactretention.WorktreeStatus(ctx, repo)
	if err != nil {
		return err
	}
	if string(gotStatus) != want.status {
		return errors.New("source working tree changed during capture")
	}
	return nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	return gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{GitError: "not a Git worktree"})
}

func requireCleanIndex(ctx context.Context, repo string) error {
	staged, err := artifactretention.StagedChanges(ctx, repo)
	if err != nil {
		return err
	}
	if !staged {
		return nil
	}
	return errors.New("source index contains staged changes outside .polis/artifacts/; capture-red requires index == HEAD")
}

func capturePatch(ctx context.Context, repo, head string) ([]byte, error) {
	indexPath, cleanupIndex, err := gitutil.TemporaryIndex("polis-red-index-*")
	if err != nil {
		return nil, err
	}
	defer cleanupIndex()
	objectEnv, cleanupObjects, err := gitutil.TemporaryObjectEnv(ctx, repo, "polis-red-objects-*")
	if err != nil {
		return nil, err
	}
	defer cleanupObjects()
	env := append(objectEnv, "GIT_INDEX_FILE="+indexPath)
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "read-tree", head); err != nil {
		return nil, fmt.Errorf("initialize temporary capture index: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "add", "-A", "--", ".", ":(exclude).polis/artifacts/**"); err != nil {
		return nil, fmt.Errorf("capture working tree in temporary index: %w", err)
	}
	patch, err := gitutil.Bytes(ctx, repo, env, nil, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--find-renames", head, "--")
	if err != nil {
		return nil, fmt.Errorf("generate regression patch: %w", err)
	}
	return patch, nil
}

func sortedPathKeys(paths map[string]struct{}) []string {
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	return keys
}

func retainedResult(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}

func readExternal(repo, filename string, max int64) ([]byte, error) {
	return fileutil.ReadOutside(repo, filename, fileutil.OutsideReadOptions{Max: max, OversizeMessage: "input exceeds maximum size"})
}

// ValidateProof verifies retained Red-proof bytes against one exact locked
// contract and its baseline. It performs the same isolated apply, test-scope,
// and baseline-oracle checks used by Capture without mutating workflow state.
func ValidateProof(ctx context.Context, repo string, patch []byte, contract spec.ChangeContract) error {
	_, err := ValidateProofCandidate(ctx, repo, patch, contract)
	return err
}

// ValidateProofCandidate also reports whether the patch applies within the
// locked test scope. That lets status distinguish a failed Red oracle from an
// unrelated retained patch without treating unrelated artifacts as evidence.
func ValidateProofCandidate(ctx context.Context, repo string, patch []byte, contract spec.ChangeContract) (bool, error) {
	if len(patch) == 0 {
		return false, errors.New("captured regression patch is empty")
	}
	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 || contract.BaselineLock == nil {
		return false, errors.New("Red proof requires locked Change Contract schema v4 or v6 produced by polis start")
	}
	if !contract.RequiresRedGreen() {
		return false, errors.New("Red proof requires a red_green change contract")
	}
	if err := devlock.ValidateRepositoryBase(ctx, repo, contract); err != nil {
		return false, fmt.Errorf("locked development baseline: %w", err)
	}
	return validateProbeCandidate(ctx, repo, contract.BaselineLock.BaseCommit, patch, contract)
}

func validateProbe(ctx context.Context, repo, head string, patch []byte, contract spec.ChangeContract) error {
	_, err := validateProbeCandidate(ctx, repo, head, patch, contract)
	return err
}

func validateProbeCandidate(ctx context.Context, repo, head string, patch []byte, contract spec.ChangeContract) (bool, error) {
	worktree, cleanup, err := gitutil.DetachedWorktree(ctx, repo, head, "polis-capture-red-*", "", "")
	if err != nil {
		return false, err
	}
	defer cleanup()
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return false, fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return false, fmt.Errorf("regression probe apply failed: %w", err)
	}
	changed, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return false, err
	}
	paths := sortedPathKeys(changed)
	scope := assessScopePaths(contract, paths)
	if scope.Status == spec.StatusFail {
		offending := make([]string, 0, len(scope.RejectedPaths))
		for _, rejected := range scope.RejectedPaths {
			offending = append(offending, rejected.Path)
		}
		cause := contract.ValidateTestPaths(paths)
		if cause == nil {
			cause = errors.New(scope.RejectedPaths[0].Reason)
		}
		return false, &diagnostic.Error{
			Summary: fmt.Sprintf("Red probe scope validation: %v", cause),
			Report: diagnostic.Report{
				Stage:     "Red probe scope validation",
				Condition: "captured Red probe paths must match the declared Change Contract test_scope",
				Expected:  map[string]any{"allowed_test_paths": scope.AllowedTestPaths},
				Actual:    map[string]any{"offending_paths": offending, "rejected_paths": scope.RejectedPaths},
				Paths:     offending,
			},
			Cause: cause,
		}
	}
	if err := changeexec.ExecuteBaseline(contract, worktree, io.Discard); err != nil {
		return true, fmt.Errorf("regression Red oracle not satisfied: %w", err)
	}
	return true, nil
}
