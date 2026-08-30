package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/MarcosAlves90/polis/v5/internal/changeexec"
	"github.com/MarcosAlves90/polis/v5/internal/gitutil"
	"github.com/MarcosAlves90/polis/v5/internal/policyexec"
	"github.com/MarcosAlves90/polis/v5/spec"
)

type Validation struct {
	Repo                  string
	BaseCommit            string
	TargetTree            string
	Patch                 []byte
	RegressionPatch       []byte
	Change                spec.ChangeContract
	Policy                spec.Policy
	Evidence              io.Writer
	RedWorktreePattern    string
	TargetWorktreePattern string
	CreateWorktreeError   string
	TargetApplyCheckError string
	TargetApplyError      string
	PolicyFailureLabel    string
}

func Validate(ctx context.Context, validation Validation) error {
	redPaths, err := validateRegression(ctx, validation)
	if err != nil {
		return err
	}
	return validateTarget(ctx, validation, redPaths)
}

func validateRegression(ctx context.Context, validation Validation) (map[string]struct{}, error) {
	if validation.Change.Kind != spec.ChangeKindDefect {
		return nil, nil
	}
	worktree, cleanup, err := gitutil.DetachedWorktree(
		ctx,
		validation.Repo,
		validation.BaseCommit,
		validation.RedWorktreePattern,
		"create isolated worktree staging",
		validation.CreateWorktreeError,
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.RegressionPatch), "apply", "--check", "-"); err != nil {
		return nil, fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.RegressionPatch), "apply", "--index", "-"); err != nil {
		return nil, fmt.Errorf("regression probe apply failed: %w", err)
	}
	redPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return nil, err
	}
	if err := changeexec.ExecuteBaseline(validation.Change, worktree, validation.Evidence); err != nil {
		return nil, fmt.Errorf("regression baseline validation: %w", err)
	}
	return redPaths, nil
}

func validateTarget(ctx context.Context, validation Validation, redPaths map[string]struct{}) error {
	worktree, cleanup, err := gitutil.DetachedWorktree(
		ctx,
		validation.Repo,
		validation.BaseCommit,
		validation.TargetWorktreePattern,
		"create isolated worktree staging",
		validation.CreateWorktreeError,
	)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.Patch), "apply", "--check", "-"); err != nil {
		return gitutil.Wrap(validation.TargetApplyCheckError, err)
	}
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.Patch), "apply", "--index", "-"); err != nil {
		return gitutil.Wrap(validation.TargetApplyError, err)
	}
	if err := requireRegressionPaths(ctx, worktree, redPaths); err != nil {
		return err
	}
	if err := gitutil.RequireTargetTree(ctx, worktree, validation.TargetTree); err != nil {
		return err
	}
	changedPaths, err := gitutil.ChangedTreePaths(ctx, worktree, validation.BaseCommit, validation.TargetTree)
	if err != nil {
		return err
	}
	if err := validation.Change.ValidateChangedPaths(changedPaths); err != nil {
		return fmt.Errorf("consumer change scope validation: %w", err)
	}
	if err := changeexec.ExecuteTarget(validation.Change, worktree, validation.Evidence); err != nil {
		return err
	}
	result := policyexec.Execute(validation.Policy, worktree, validation.Evidence)
	if result.Overall != spec.StatusPass {
		return fmt.Errorf("%s %s", validation.PolicyFailureLabel, result.Overall)
	}
	return nil
}

func requireRegressionPaths(ctx context.Context, worktree string, redPaths map[string]struct{}) error {
	targetPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return err
	}
	for path := range redPaths {
		if _, ok := targetPaths[path]; !ok {
			return fmt.Errorf("regression probe path %q is absent from final payload", path)
		}
	}
	return nil
}
