package packageapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/baselineproof"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type BaselineMode string

type BaselineSource string

type BaselineAncestry string

const (
	BaselineModeStrict     BaselineMode = "strict"
	BaselineModeCompatible BaselineMode = "compatible"
	BaselineModePermissive BaselineMode = "permissive"

	BaselineSourceLocal      BaselineSource = "local"
	BaselineSourceEmbedded   BaselineSource = "embedded"
	BaselineSourceOverridden BaselineSource = "overridden"

	BaselineAncestryExact            BaselineAncestry = "exact"
	BaselineAncestryProvenDescendant BaselineAncestry = "proven_descendant"
	BaselineAncestryUnproven         BaselineAncestry = "unproven"
)

type Options struct {
	BaselineMode              BaselineMode
	AllowMissingBaselineProof bool
}

type baselineAssessment struct {
	Mode               BaselineMode
	Source             BaselineSource
	Ancestry           BaselineAncestry
	ConsumerHead       string
	TargetTree         string
	Exact              bool
	Ancestor           bool
	OverrideActive     bool
	SkipBaselineProof  bool
	BypassedGuarantees []string
	Warnings           []string
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
	if opts.AllowMissingBaselineProof && mode != BaselineModePermissive {
		return Options{}, errors.New("--allow-missing-baseline-proof requires --baseline-mode permissive")
	}
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
		Source:       BaselineSourceLocal,
		Ancestry:     BaselineAncestryUnproven,
		ConsumerHead: head,
		Exact:        head == pkg.Manifest.BaseCommit,
	}
	if err := assessBaselineAdmission(ctx, repo, pkg, opts, &assessment); err != nil {
		return baselineAssessment{}, err
	}
	if err := assessPayloadCompatibility(ctx, repo, pkg, &assessment); err != nil {
		return baselineAssessment{}, err
	}
	return assessment, nil
}

func assessBaselineAdmission(ctx context.Context, repo string, pkg packageverify.Package, opts Options, assessment *baselineAssessment) error {
	if assessment.Exact {
		assessment.Ancestor = true
		assessment.Ancestry = BaselineAncestryExact
		return verifyLockedBaseline(ctx, repo, pkg.Change)
	}
	if opts.BaselineMode == BaselineModeStrict {
		return fmt.Errorf("%w: base_commit got %s want %s", ErrBaselineMismatch, assessment.ConsumerHead, pkg.Manifest.BaseCommit)
	}
	baselineAvailable, err := inspectLocalBaseline(ctx, repo, pkg, assessment)
	if err != nil {
		return err
	}
	switch opts.BaselineMode {
	case BaselineModeCompatible:
		return requireCompatibleBaseline(pkg, assessment, baselineAvailable)
	case BaselineModePermissive:
		return admitPermissiveBaseline(pkg, opts, assessment, baselineAvailable)
	default:
		return nil
	}
}

func inspectLocalBaseline(ctx context.Context, repo string, pkg packageverify.Package, assessment *baselineAssessment) (bool, error) {
	available, err := localBaselineAvailable(ctx, repo, pkg.Manifest.BaseCommit)
	if err != nil {
		return false, fmt.Errorf("%w: inspect locked development baseline: %v", ErrBaselineMismatch, err)
	}
	if !available {
		assessment.Source = ""
		return false, nil
	}
	if err := devlock.ValidateRepositoryBase(ctx, repo, pkg.Change); err != nil {
		return false, fmt.Errorf("%w: locked development baseline: %v", ErrBaselineMismatch, err)
	}
	ancestor, err := isAncestor(ctx, repo, pkg.Manifest.BaseCommit, assessment.ConsumerHead)
	if err != nil {
		return false, fmt.Errorf("%w: validate base ancestry: %v", ErrBaselineMismatch, err)
	}
	assessment.Ancestor = ancestor
	if ancestor {
		assessment.Ancestry = BaselineAncestryProvenDescendant
	}
	return true, nil
}

func requireCompatibleBaseline(pkg packageverify.Package, assessment *baselineAssessment, baselineAvailable bool) error {
	if !baselineAvailable {
		return fmt.Errorf("%w: locked development baseline commit %s is unavailable in consumer repository", ErrBaselineMismatch, pkg.Manifest.BaseCommit)
	}
	if !assessment.Ancestor {
		return fmt.Errorf("%w: consumer HEAD %s is not a descendant of artifact base %s", ErrBaselineMismatch, assessment.ConsumerHead, pkg.Manifest.BaseCommit)
	}
	return nil
}

func admitPermissiveBaseline(pkg packageverify.Package, opts Options, assessment *baselineAssessment, baselineAvailable bool) error {
	if !baselineAvailable {
		if err := resolveMissingPermissiveBaseline(pkg, opts, assessment); err != nil {
			return err
		}
	}
	if !assessment.Ancestor {
		assessment.Warnings = append(assessment.Warnings, "artifact-base ancestry is not proven; permissive mode relies on payload context and complete isolated target validation")
	}
	return nil
}

func resolveMissingPermissiveBaseline(pkg packageverify.Package, opts Options, assessment *baselineAssessment) error {
	if spec.FormatHasEmbeddedBaseline(pkg.Manifest.FormatVersion) && len(pkg.Baseline) != 0 {
		assessment.Source = BaselineSourceEmbedded
		return nil
	}
	if !opts.AllowMissingBaselineProof {
		return fmt.Errorf("%w: locked development baseline commit %s is unavailable; use a self-contained artifact or explicitly authorize --allow-missing-baseline-proof", ErrBaselineMismatch, pkg.Manifest.BaseCommit)
	}
	assessment.Source = BaselineSourceOverridden
	assessment.OverrideActive = true
	assessment.SkipBaselineProof = true
	assessment.BypassedGuarantees = bypassedBaselineGuarantees(pkg.Change)
	assessment.Warnings = append(assessment.Warnings, "locked producer baseline proof is unavailable and explicitly waived")
	return nil
}

func assessPayloadCompatibility(ctx context.Context, repo string, pkg packageverify.Package, assessment *baselineAssessment) error {
	if _, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("%w: payload is incompatible with consumer HEAD %s: %v", ErrBaselineMismatch, assessment.ConsumerHead, err)
	}
	targetTree, err := targetTreeForPatch(ctx, repo, assessment.ConsumerHead, pkg.Patch)
	if err != nil {
		return fmt.Errorf("%w: compute consumer target tree: %v", ErrBaselineMismatch, err)
	}
	assessment.TargetTree = targetTree
	if assessment.Exact && targetTree != pkg.Manifest.TargetTree {
		return fmt.Errorf("%w: exact-baseline target tree got %s want %s", ErrBaselineMismatch, targetTree, pkg.Manifest.TargetTree)
	}
	return nil
}

func localBaselineAvailable(ctx context.Context, repo, baseCommit string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--quiet", baseCommit+"^{commit}")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func prepareBaselineRepo(ctx context.Context, consumerRepo string, pkg packageverify.Package, assessment baselineAssessment) (string, func(), error) {
	switch assessment.Source {
	case BaselineSourceLocal:
		return consumerRepo, noBaselineCleanup, nil
	case BaselineSourceEmbedded:
		if pkg.Change.BaselineLock == nil {
			return "", nil, errors.New("embedded baseline requires baseline_lock")
		}
		repo, cleanup, err := baselineproof.Materialize(ctx, pkg.Baseline, pkg.Manifest.GitObjectFormat, pkg.Manifest.BaseCommit, pkg.Change.BaselineLock.BaseTree)
		if err != nil {
			return "", nil, fmt.Errorf("materialize embedded baseline: %w", err)
		}
		return repo, cleanup, nil
	case BaselineSourceOverridden:
		return consumerRepo, noBaselineCleanup, nil
	default:
		return "", nil, errors.New("baseline source was not resolved")
	}
}

func noBaselineCleanup() {
	// Local and overridden baseline sources allocate no temporary baseline state.
}

func bypassedBaselineGuarantees(change spec.ChangeContract) []string {
	bypassed := []string{"locked producer baseline commit/tree resolution"}
	if !change.RequiresBaselineProof() {
		return bypassed
	}
	bypassed = append(bypassed, "locked baseline behavior replay")
	if change.RequiresRedGreen() {
		bypassed = append(bypassed, "Red proof reconstruction", "Red regression path reconstruction")
		if change.IsStrictDevelopment() {
			bypassed = append(bypassed, "strict Red blob identity reconstruction")
		}
	}
	return bypassed
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
	if assessment.Source == BaselineSourceLocal {
		if err := devlock.ValidateRepositoryBase(ctx, repo, pkg.Change); err != nil {
			return fmt.Errorf("%w: locked development baseline: %v", ErrBaselineMismatch, err)
		}
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
