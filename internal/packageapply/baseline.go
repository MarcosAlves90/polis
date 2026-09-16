package packageapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
)

type BaselineMode string

const (
	BaselineModeStrict     BaselineMode = "strict"
	BaselineModeCompatible BaselineMode = "compatible"
	BaselineModePermissive BaselineMode = "permissive"
)

type Options struct {
	BaselineMode BaselineMode
}

type baselineAssessment struct {
	Mode         BaselineMode
	ConsumerHead string
	TargetTree   string
	Exact        bool
	Ancestor     bool
	Warnings     []string
}

func ParseBaselineMode(value string) (BaselineMode, error) {
	switch BaselineMode(strings.TrimSpace(value)) {
	case "", BaselineModeStrict:
		return BaselineModeStrict, nil
	case BaselineModeCompatible:
		return BaselineModeCompatible, nil
	case BaselineModePermissive:
		return BaselineModePermissive, nil
	default:
		return "", fmt.Errorf("invalid baseline mode %q: expected strict, compatible, or permissive", value)
	}
}

func normalizedOptions(opts Options) (Options, error) {
	mode, err := ParseBaselineMode(string(opts.BaselineMode))
	if err != nil {
		return Options{}, err
	}
	opts.BaselineMode = mode
	return opts, nil
}

func assessBaseline(ctx context.Context, repo string, pkg packageverify.Package, opts Options) (baselineAssessment, error) {
	opts, err := normalizedOptions(opts)
	if err != nil {
		return baselineAssessment{}, err
	}
	head, err := verifyConsumerState(ctx, repo, pkg.Manifest.GitObjectFormat)
	if err != nil {
		return baselineAssessment{}, err
	}
	assessment := baselineAssessment{
		Mode:         opts.BaselineMode,
		ConsumerHead: head,
		Exact:        head == pkg.Manifest.BaseCommit,
	}

	if assessment.Exact {
		assessment.Ancestor = true
		if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
			return baselineAssessment{}, err
		}
	} else {
		if opts.BaselineMode == BaselineModeStrict {
			return baselineAssessment{}, fmt.Errorf("%w: base_commit got %s want %s", ErrBaselineMismatch, head, pkg.Manifest.BaseCommit)
		}
		if err := devlock.ValidateRepositoryBase(ctx, repo, pkg.Change); err != nil {
			return baselineAssessment{}, fmt.Errorf("%w: locked development baseline: %v", ErrBaselineMismatch, err)
		}
		ancestor, err := isAncestor(ctx, repo, pkg.Manifest.BaseCommit, head)
		if err != nil {
			return baselineAssessment{}, fmt.Errorf("%w: validate base ancestry: %v", ErrBaselineMismatch, err)
		}
		assessment.Ancestor = ancestor
		switch opts.BaselineMode {
		case BaselineModeCompatible:
			if !ancestor {
				return baselineAssessment{}, fmt.Errorf("%w: consumer HEAD %s is not a descendant of artifact base %s", ErrBaselineMismatch, head, pkg.Manifest.BaseCommit)
			}
		case BaselineModePermissive:
			if !ancestor {
				assessment.Warnings = append(assessment.Warnings, "artifact-base ancestry is not proven; permissive mode relies on payload context and complete isolated validation")
			}
		}
	}

	if _, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", "--check", "-"); err != nil {
		return baselineAssessment{}, fmt.Errorf("%w: payload is incompatible with consumer HEAD %s: %v", ErrBaselineMismatch, head, err)
	}
	targetTree, err := targetTreeForPatch(ctx, repo, head, pkg.Patch)
	if err != nil {
		return baselineAssessment{}, fmt.Errorf("%w: compute consumer target tree: %v", ErrBaselineMismatch, err)
	}
	assessment.TargetTree = targetTree
	if assessment.Exact && targetTree != pkg.Manifest.TargetTree {
		return baselineAssessment{}, fmt.Errorf("%w: exact-baseline target tree got %s want %s", ErrBaselineMismatch, targetTree, pkg.Manifest.TargetTree)
	}
	return assessment, nil
}

func verifyConsumerState(ctx context.Context, repo, objectFormat string) (string, error) {
	format, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return "", fmt.Errorf("detect Git object format: %w", err)
	}
	if format != objectFormat {
		return "", fmt.Errorf("%w: git object format got %s want %s", ErrBaselineMismatch, format, objectFormat)
	}
	head, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	status, err := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("inspect consumer status: %w", err)
	}
	if status != "" {
		return "", fmt.Errorf("%w: consumer working tree/index is not clean", ErrBaselineMismatch)
	}
	return head, nil
}

func verifyAssessmentStable(ctx context.Context, repo string, pkg packageverify.Package, assessment baselineAssessment) error {
	head, err := verifyConsumerState(ctx, repo, pkg.Manifest.GitObjectFormat)
	if err != nil {
		return err
	}
	if head != assessment.ConsumerHead {
		return fmt.Errorf("%w: consumer HEAD changed after isolated validation: got %s want %s", ErrBaselineMismatch, head, assessment.ConsumerHead)
	}
	if assessment.Mode == BaselineModeStrict {
		if err := verifyLockedBaseline(ctx, repo, pkg.Change); err != nil {
			return err
		}
		return nil
	}
	if err := devlock.ValidateRepositoryBase(ctx, repo, pkg.Change); err != nil {
		return fmt.Errorf("%w: locked development baseline: %v", ErrBaselineMismatch, err)
	}
	return nil
}

func isAncestor(ctx context.Context, repo, base, head string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", base, head)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func targetTreeForPatch(ctx context.Context, repo, baseCommit string, patch []byte) (string, error) {
	indexPath, cleanupIndex, err := gitutil.TemporaryIndex("polis-consumer-target-index-*")
	if err != nil {
		return "", err
	}
	defer cleanupIndex()
	objectEnv, cleanupObjects, err := gitutil.TemporaryObjectEnv(ctx, repo, "polis-consumer-target-objects-*")
	if err != nil {
		return "", err
	}
	defer cleanupObjects()
	env := append(objectEnv, "GIT_INDEX_FILE="+indexPath)
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", err
	}
	if _, err := gitutil.Bytes(ctx, repo, env, bytes.NewReader(patch), "apply", "--cached", "--check", "-"); err != nil {
		return "", err
	}
	if _, err := gitutil.Bytes(ctx, repo, env, bytes.NewReader(patch), "apply", "--cached", "-"); err != nil {
		return "", err
	}
	return gitutil.Output(ctx, repo, env, nil, "write-tree")
}
