package packageapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/isolation"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

var (
	ErrBaselineMismatch = errors.New("consumer baseline mismatch")
	ErrValidationFailed = errors.New("consumer validation failed")
	ErrApplyFailed      = errors.New("consumer apply failed")
)

type Result struct {
	Project      string
	Change       string
	TargetTree   string
	EvidencePath string
}

const (
	gitApplyCheck = "--check"
	gitRevParse   = "rev-parse"
)

func Apply(ctx context.Context, artifact, repoPath string) (Result, error) {
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		return Result{}, fmt.Errorf("verify package: %w", err)
	}
	repo, err := resolveRepo(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	if err := verifyBaseline(ctx, repo, pkg.Manifest); err != nil {
		return Result{}, err
	}
	if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
		return Result{}, err
	}

	evidenceFile, err := os.CreateTemp("", "polis-apply-evidence-*.ndjson")
	if err != nil {
		return Result{}, fmt.Errorf("create temporary evidence: %w", err)
	}
	evidencePath := evidenceFile.Name()

	if err := isolation.Validate(ctx, isolation.Validation{
		Repo:                  repo,
		BaseCommit:            pkg.Manifest.BaseCommit,
		TargetTree:            pkg.Manifest.TargetTree,
		Patch:                 pkg.Patch,
		RegressionPatch:       pkg.RegressionPatch,
		Change:                pkg.Change,
		Policy:                pkg.Policy,
		Evidence:              evidenceFile,
		RedWorktreePattern:    "polis-apply-red-*",
		TargetWorktreePattern: "polis-apply-worktree-*",
		CreateWorktreeError:   "create isolated consumer worktree",
		TargetApplyCheckError: "isolated apply check failed",
		TargetApplyError:      "isolated apply failed",
		PolicyFailureLabel:    "consumer policy validation",
	}); err != nil {
		cleanupErr := discardTemporaryEvidence(evidenceFile, evidencePath, false)
		if cleanupErr != nil {
			return Result{}, fmt.Errorf("%w: %v; cleanup temporary evidence: %v", ErrValidationFailed, err, cleanupErr)
		}
		return Result{}, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	if err := discardTemporaryEvidence(evidenceFile, evidencePath, true); err != nil {
		return Result{}, fmt.Errorf("cleanup temporary evidence: %w", err)
	}

	// Close the TOCTOU window as much as possible before touching consumer files.
	if err := verifyBaseline(ctx, repo, pkg.Manifest); err != nil {
		return Result{}, fmt.Errorf("baseline changed after isolated validation: %w", err)
	}
	if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
		return Result{}, fmt.Errorf("baseline changed after isolated validation: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", gitApplyCheck, "-"); err != nil {
		return Result{}, fmt.Errorf("%w: real git apply --check failed: %v", ErrApplyFailed, err)
	}
	if _, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		return Result{}, fmt.Errorf("%w: real git apply failed: %v", ErrApplyFailed, err)
	}
	gotTree, err := workingTreeID(ctx, repo, pkg.Manifest.BaseCommit)
	if err != nil {
		rollbackErr := reversePatch(ctx, repo, pkg.Patch)
		if rollbackErr != nil {
			return Result{}, fmt.Errorf("compute post-apply tree: %v; rollback failed: %v", err, rollbackErr)
		}
		return Result{}, fmt.Errorf("compute post-apply tree: %w; patch reversed", err)
	}
	if gotTree != pkg.Manifest.TargetTree {
		rollbackErr := reversePatch(ctx, repo, pkg.Patch)
		if rollbackErr != nil {
			return Result{}, fmt.Errorf("post-apply target_tree mismatch: got %s want %s; rollback failed: %v", gotTree, pkg.Manifest.TargetTree, rollbackErr)
		}
		return Result{}, fmt.Errorf("post-apply target_tree mismatch: got %s want %s; patch reversed", gotTree, pkg.Manifest.TargetTree)
	}
	return Result{Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, TargetTree: gotTree}, nil
}

func Preflight(ctx context.Context, artifact, repoPath string) (Result, error) {
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		return Result{}, fmt.Errorf("verify package: %w", err)
	}
	repo, err := resolveRepo(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	if err := verifyBaseline(ctx, repo, pkg.Manifest); err != nil {
		return Result{}, err
	}
	if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
		return Result{}, err
	}
	if err := isolation.Validate(ctx, isolation.Validation{
		Repo:                  repo,
		BaseCommit:            pkg.Manifest.BaseCommit,
		TargetTree:            pkg.Manifest.TargetTree,
		Patch:                 pkg.Patch,
		RegressionPatch:       pkg.RegressionPatch,
		Change:                pkg.Change,
		Policy:                pkg.Policy,
		Evidence:              io.Discard,
		RedWorktreePattern:    "polis-preflight-red-*",
		TargetWorktreePattern: "polis-preflight-worktree-*",
		CreateWorktreeError:   "create isolated preflight worktree",
		TargetApplyCheckError: "preflight isolated apply check failed",
		TargetApplyError:      "preflight isolated apply failed",
		PolicyFailureLabel:    "preflight policy validation",
	}); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	if err := verifyBaseline(ctx, repo, pkg.Manifest); err != nil {
		return Result{}, fmt.Errorf("baseline changed after preflight validation: %w", err)
	}
	if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
		return Result{}, fmt.Errorf("baseline changed after preflight validation: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", gitApplyCheck, "-"); err != nil {
		return Result{}, fmt.Errorf("%w: real git apply --check failed: %v", ErrValidationFailed, err)
	}
	return Result{Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, TargetTree: pkg.Manifest.TargetTree}, nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	return gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{EmptyAsDot: true, PathError: "resolve repo path", GitError: "not a Git worktree"})
}

func verifyBaseline(ctx context.Context, repo string, manifest spec.Manifest) error {
	format, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return fmt.Errorf("detect Git object format: %w", err)
	}
	if format != manifest.GitObjectFormat {
		return fmt.Errorf("%w: git object format got %s want %s", ErrBaselineMismatch, format, manifest.GitObjectFormat)
	}
	head, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	if head != manifest.BaseCommit {
		return fmt.Errorf("%w: base_commit got %s want %s", ErrBaselineMismatch, head, manifest.BaseCommit)
	}
	status, err := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect consumer status: %w", err)
	}
	if status != "" {
		return fmt.Errorf("%w: consumer working tree/index is not clean", ErrBaselineMismatch)
	}
	return nil
}

func verifyLockedBaseline(ctx context.Context, repo string, change spec.ChangeContract) error {
	if err := devlock.ValidateRepository(ctx, repo, change); err != nil {
		return fmt.Errorf("%w: locked development baseline: %v", ErrBaselineMismatch, err)
	}
	return nil
}

func discardTemporaryEvidence(file *os.File, path string, sync bool) error {
	var cleanupErrs []error
	if sync {
		if err := file.Sync(); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("sync: %w", err))
		}
	}
	if err := file.Close(); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("close: %w", err))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("remove: %w", err))
	}
	return errors.Join(cleanupErrs...)
}
func workingTreeID(ctx context.Context, repo, baseCommit string) (string, error) {
	indexPath, cleanupIndex, err := gitutil.TemporaryIndex("polis-apply-index-*")
	if err != nil {
		return "", err
	}
	defer cleanupIndex()
	objectEnv, cleanupObjects, err := gitutil.TemporaryObjectEnv(ctx, repo, "polis-apply-objects-*")
	if err != nil {
		return "", err
	}
	defer cleanupObjects()
	env := append(objectEnv, "GIT_INDEX_FILE="+indexPath)
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", err
	}
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	return gitutil.Output(ctx, repo, env, nil, "write-tree")
}

func reversePatch(ctx context.Context, repo string, patch []byte) error {
	_, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(patch), "apply", "--reverse", "-")
	return err
}
