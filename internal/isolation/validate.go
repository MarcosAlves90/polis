package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/MarcosAlves90/polis/v6/internal/changeexec"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/spec"
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

func validateRegression(ctx context.Context, validation Validation) (map[string]string, error) {
	if !validation.Change.RequiresBaselineProof() {
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

	var redProof map[string]string
	if validation.Change.RequiresRegressionPatch() {
		if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.RegressionPatch), "apply", "--check", "-"); err != nil {
			return nil, fmt.Errorf("regression probe apply check failed: %w", err)
		}
		if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.RegressionPatch), "apply", "--index", "-"); err != nil {
			return nil, fmt.Errorf("regression probe apply failed: %w", err)
		}
		changedPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
		if err != nil {
			return nil, err
		}
		redProof = make(map[string]string, len(changedPaths))
		for path := range changedPaths {
			redProof[path] = ""
			if validation.Change.IsStrictDevelopment() {
				blob, err := indexBlobID(ctx, worktree, path)
				if err != nil {
					return nil, fmt.Errorf("capture strict Red proof path %q: %w", path, err)
				}
				redProof[path] = blob
			}
		}
	}
	if err := changeexec.ExecuteBaseline(validation.Change, worktree, validation.Evidence); err != nil {
		return nil, fmt.Errorf("regression baseline validation: %w", err)
	}
	return redProof, nil
}

func validateTarget(ctx context.Context, validation Validation, redProof map[string]string) error {
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
	if err := requireRegressionPaths(ctx, worktree, redProof); err != nil {
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

func requireRegressionPaths(ctx context.Context, worktree string, redProof map[string]string) error {
	targetPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return err
	}
	for path, capturedBlob := range redProof {
		if _, ok := targetPaths[path]; !ok {
			return fmt.Errorf("regression probe path %q is absent from final payload", path)
		}
		if capturedBlob == "" {
			continue
		}
		targetBlob, err := indexBlobID(ctx, worktree, path)
		if err != nil {
			return fmt.Errorf("read final Red proof path %q: %w", path, err)
		}
		if targetBlob != capturedBlob {
			return fmt.Errorf("regression probe path %q differs from captured Red proof", path)
		}
	}
	return nil
}

func indexBlobID(ctx context.Context, worktree, path string) (string, error) {
	blob, err := gitutil.Output(ctx, worktree, nil, nil, "rev-parse", "--verify", ":"+path)
	if err != nil {
		return "", err
	}
	if blob == "" {
		return "", fmt.Errorf("empty index blob id")
	}
	return blob, nil
}
