package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/MarcosAlves90/polis/v6/internal/changeexec"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Validation struct {
	BaselineRepo          string
	BaselineCommit        string
	TargetRepo            string
	TargetBaseCommit      string
	SkipBaselineProof     bool
	TargetTree            string
	Patch                 []byte
	RegressionPatch       []byte
	Change                spec.ChangeContract
	Policy                spec.Policy
	ExecutionPlan         *policyplan.Plan
	PolicyResult          *policyexec.Result
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
	if validation.SkipBaselineProof || !validation.Change.RequiresBaselineProof() {
		return nil, nil
	}
	worktree, cleanup, err := gitutil.DetachedWorktree(
		ctx,
		validation.BaselineRepo,
		validation.BaselineCommit,
		validation.RedWorktreePattern,
		"create isolated worktree staging",
		validation.CreateWorktreeError,
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	redProof, err := prepareRegressionProof(ctx, validation, worktree)
	if err != nil {
		return nil, err
	}
	if err := changeexec.ExecuteBaseline(validation.Change, worktree, validation.Evidence); err != nil {
		return nil, fmt.Errorf("regression baseline validation: %w", err)
	}
	return redProof, nil
}

func prepareRegressionProof(ctx context.Context, validation Validation, worktree string) (map[string]string, error) {
	if !validation.Change.RequiresRegressionPatch() {
		return nil, nil
	}
	if err := applyRegressionPatch(ctx, worktree, validation.RegressionPatch); err != nil {
		return nil, err
	}
	return captureRegressionProof(ctx, worktree, validation.Change.IsStrictDevelopment())
}

func applyRegressionPatch(ctx context.Context, worktree string, patch []byte) error {
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("regression probe apply failed: %w", err)
	}
	return nil
}

func captureRegressionProof(ctx context.Context, worktree string, strict bool) (map[string]string, error) {
	changedPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return nil, err
	}
	redProof := make(map[string]string, len(changedPaths))
	for path := range changedPaths {
		blob, err := regressionBlobID(ctx, worktree, path, strict)
		if err != nil {
			return nil, err
		}
		redProof[path] = blob
	}
	return redProof, nil
}

func regressionBlobID(ctx context.Context, worktree, path string, strict bool) (string, error) {
	if !strict {
		return "", nil
	}
	blob, err := indexBlobID(ctx, worktree, path)
	if err != nil {
		return "", fmt.Errorf("capture strict Red proof path %q: %w", path, err)
	}
	return blob, nil
}

func validateTarget(ctx context.Context, validation Validation, redProof map[string]string) error {
	targetBaseCommit := validation.TargetBaseCommit
	if targetBaseCommit == "" {
		targetBaseCommit = validation.BaselineCommit
	}
	worktree, cleanup, err := gitutil.DetachedWorktree(
		ctx,
		validation.TargetRepo,
		targetBaseCommit,
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
	changedPaths, err := gitutil.ChangedTreePaths(ctx, worktree, targetBaseCommit, validation.TargetTree)
	if err != nil {
		return err
	}
	if err := validation.Change.ValidateChangedPaths(changedPaths); err != nil {
		return fmt.Errorf("consumer change scope validation: %w", err)
	}
	if err := changeexec.ExecuteTarget(validation.Change, worktree, validation.Evidence); err != nil {
		return err
	}
	var result policyexec.Result
	if validation.ExecutionPlan != nil {
		result = policyexec.ExecutePlan(*validation.ExecutionPlan, worktree, validation.Evidence)
	} else {
		result = policyexec.Execute(validation.Policy, worktree, validation.Evidence)
	}
	if validation.PolicyResult != nil {
		*validation.PolicyResult = result
	}
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
