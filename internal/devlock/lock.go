package devlock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func Snapshot(ctx context.Context, repo string, specification *spec.DevelopmentSpecification) (spec.BaselineLock, error) {
	if specification == nil {
		return spec.BaselineLock{}, errors.New("specification is required")
	}
	if err := specification.Validate(); err != nil {
		return spec.BaselineLock{}, fmt.Errorf("specification: %w", err)
	}
	root, err := gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return spec.BaselineLock{}, err
	}
	objectFormat, err := gitutil.Output(ctx, root, nil, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return spec.BaselineLock{}, fmt.Errorf("detect Git object format: %w", err)
	}
	baseCommit, err := gitutil.Output(ctx, root, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return spec.BaselineLock{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	baseTree, err := gitutil.Output(ctx, root, nil, nil, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return spec.BaselineLock{}, fmt.Errorf("resolve HEAD tree: %w", err)
	}
	working, err := os.ReadFile(filepath.Join(root, ".polis", "policy.json"))
	if err != nil {
		return spec.BaselineLock{}, fmt.Errorf("read .polis/policy.json: %w", err)
	}
	committed, err := gitutil.Bytes(ctx, root, nil, nil, "show", "HEAD:.polis/policy.json")
	if err != nil {
		return spec.BaselineLock{}, errors.New(".polis/policy.json must exist in HEAD")
	}
	if !bytes.Equal(working, committed) {
		return spec.BaselineLock{}, errors.New(".polis/policy.json working copy differs from HEAD")
	}
	policySum := sha256.Sum256(committed)
	specificationSum, err := specification.SHA256()
	if err != nil {
		return spec.BaselineLock{}, fmt.Errorf("hash specification: %w", err)
	}
	lock := spec.BaselineLock{
		GitObjectFormat:     objectFormat,
		BaseCommit:          baseCommit,
		BaseTree:            baseTree,
		PolicySHA256:        hex.EncodeToString(policySum[:]),
		SpecificationSHA256: specificationSum,
	}
	if err := lock.Validate(); err != nil {
		return spec.BaselineLock{}, fmt.Errorf("invalid baseline lock: %w", err)
	}
	return lock, nil
}

func Validate(ctx context.Context, repo string, change spec.ChangeContract) error {
	if change.SchemaVersion != spec.LockedChangeContractSchemaVersion {
		return nil
	}
	if change.BaselineLock == nil || change.Specification == nil {
		return errors.New("locked change contract requires baseline_lock and specification")
	}
	got, err := Snapshot(ctx, repo, change.Specification)
	if err != nil {
		return err
	}
	want := *change.BaselineLock
	if got.GitObjectFormat != want.GitObjectFormat {
		return fmt.Errorf("baseline git_object_format mismatch: got %s want %s", got.GitObjectFormat, want.GitObjectFormat)
	}
	if got.BaseCommit != want.BaseCommit {
		return fmt.Errorf("baseline base_commit mismatch: got %s want %s", got.BaseCommit, want.BaseCommit)
	}
	if got.BaseTree != want.BaseTree {
		return fmt.Errorf("baseline base_tree mismatch: got %s want %s", got.BaseTree, want.BaseTree)
	}
	if got.PolicySHA256 != want.PolicySHA256 {
		return fmt.Errorf("baseline policy_sha256 mismatch: got %s want %s", got.PolicySHA256, want.PolicySHA256)
	}
	if got.SpecificationSHA256 != want.SpecificationSHA256 {
		return fmt.Errorf("baseline specification_sha256 mismatch: got %s want %s", got.SpecificationSHA256, want.SpecificationSHA256)
	}
	return nil
}
