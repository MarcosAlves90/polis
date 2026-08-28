package packageapply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/polis-dev/polis-v4/internal/changeexec"
	"github.com/polis-dev/polis-v4/internal/packageverify"
	"github.com/polis-dev/polis-v4/internal/policyexec"
	"github.com/polis-dev/polis-v4/spec"
)

type Result struct {
	Project      string
	Change       string
	TargetTree   string
	EvidencePath string
}

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

	evidencePath, evidenceFile, err := createEvidenceFile(ctx, repo, artifact)
	if err != nil {
		return Result{}, err
	}
	defer evidenceFile.Close()

	if err := validateIsolated(ctx, repo, pkg.Manifest.BaseCommit, pkg.Manifest.TargetTree, pkg.Patch, pkg.RegressionPatch, pkg.Change, pkg.Policy, evidenceFile); err != nil {
		return Result{}, err
	}
	if err := evidenceFile.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync evidence: %w", err)
	}

	// Close the TOCTOU window as much as possible before touching consumer files.
	if err := verifyBaseline(ctx, repo, pkg.Manifest); err != nil {
		return Result{}, fmt.Errorf("baseline changed after isolated validation: %w", err)
	}
	if _, err := gitBytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", "--check", "-"); err != nil {
		return Result{}, fmt.Errorf("real git apply --check failed: %w", err)
	}
	if _, err := gitBytes(ctx, repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		return Result{}, fmt.Errorf("real git apply failed: %w", err)
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
	return Result{Project: pkg.Manifest.Project, Change: pkg.Manifest.Change, TargetTree: gotTree, EvidencePath: evidencePath}, nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	if repo == "" {
		repo = "."
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	root, err := gitOutput(ctx, abs, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a Git worktree: %w", err)
	}
	return filepath.Abs(root)
}

func verifyBaseline(ctx context.Context, repo string, manifest spec.Manifest) error {
	format, err := gitOutput(ctx, repo, nil, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return fmt.Errorf("detect Git object format: %w", err)
	}
	if format != manifest.GitObjectFormat {
		return fmt.Errorf("git object format mismatch: got %s want %s", format, manifest.GitObjectFormat)
	}
	head, err := gitOutput(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	if head != manifest.BaseCommit {
		return fmt.Errorf("base_commit mismatch: got %s want %s", head, manifest.BaseCommit)
	}
	status, err := gitOutput(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect consumer status: %w", err)
	}
	if status != "" {
		return errors.New("consumer working tree/index is not clean")
	}
	return nil
}

func createEvidenceFile(ctx context.Context, repo, artifact string) (string, *os.File, error) {
	gitPath, err := gitOutput(ctx, repo, nil, nil, "rev-parse", "--git-path", "polis/results")
	if err != nil {
		return "", nil, fmt.Errorf("resolve evidence directory: %w", err)
	}
	if !filepath.IsAbs(gitPath) {
		gitPath = filepath.Join(repo, gitPath)
	}
	if err := os.MkdirAll(gitPath, 0o755); err != nil {
		return "", nil, fmt.Errorf("create evidence directory: %w", err)
	}
	digest, err := fileSHA256(artifact)
	if err != nil {
		return "", nil, err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	filename := filepath.Join(gitPath, fmt.Sprintf("polis-apply-%s-%s.ndjson", digest[:12], stamp))
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("create evidence file: %w", err)
	}
	return filename, f, nil
}

func validateIsolated(ctx context.Context, repo, baseCommit, targetTree string, patch, regressionPatch []byte, change spec.ChangeContract, policy spec.Policy, evidence io.Writer) error {
	var redPaths map[string]struct{}
	if change.Kind == spec.ChangeKindDefect {
		red, cleanup, err := detachedWorktree(ctx, repo, baseCommit, "polis-apply-red-*")
		if err != nil {
			return err
		}
		defer cleanup()
		if _, err := gitBytes(ctx, red, nil, bytes.NewReader(regressionPatch), "apply", "--check", "-"); err != nil {
			return fmt.Errorf("regression probe apply check failed: %w", err)
		}
		if _, err := gitBytes(ctx, red, nil, bytes.NewReader(regressionPatch), "apply", "--index", "-"); err != nil {
			return fmt.Errorf("regression probe apply failed: %w", err)
		}
		redPaths, err = changedIndexPaths(ctx, red)
		if err != nil {
			return err
		}
		if err := changeexec.ExecuteBaseline(change, red, evidence); err != nil {
			return fmt.Errorf("regression baseline validation: %w", err)
		}
	}

	worktree, cleanup, err := detachedWorktree(ctx, repo, baseCommit, "polis-apply-worktree-*")
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("isolated apply check failed: %w", err)
	}
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("isolated apply failed: %w", err)
	}
	targetPaths, err := changedIndexPaths(ctx, worktree)
	if err != nil {
		return err
	}
	for p := range redPaths {
		if _, ok := targetPaths[p]; !ok {
			return fmt.Errorf("regression probe path %q is absent from final payload", p)
		}
	}
	gotTree, err := gitOutput(ctx, worktree, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("read isolated target tree: %w", err)
	}
	if gotTree != targetTree {
		return fmt.Errorf("isolated target_tree mismatch: got %s want %s", gotTree, targetTree)
	}
	if err := changeexec.ExecuteTarget(change, worktree, evidence); err != nil {
		return err
	}
	result := policyexec.Execute(policy, worktree, evidence)
	if result.Overall != spec.StatusPass {
		return fmt.Errorf("consumer policy validation %s", result.Overall)
	}
	return nil
}

func detachedWorktree(ctx context.Context, repo, baseCommit, pattern string) (string, func(), error) {
	parent, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create isolated worktree staging: %w", err)
	}
	worktree := filepath.Join(parent, "repo")
	if _, err := gitBytes(ctx, repo, nil, nil, "worktree", "add", "--detach", worktree, baseCommit); err != nil {
		os.RemoveAll(parent)
		return "", nil, fmt.Errorf("create isolated consumer worktree: %w", err)
	}
	cleanup := func() {
		_, _ = gitBytes(context.Background(), repo, nil, nil, "worktree", "remove", "--force", worktree)
		_ = os.RemoveAll(parent)
	}
	return worktree, cleanup, nil
}

func changedIndexPaths(ctx context.Context, worktree string) (map[string]struct{}, error) {
	b, err := gitBytes(ctx, worktree, nil, nil, "diff", "--cached", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("list changed paths: %w", err)
	}
	result := map[string]struct{}{}
	for _, raw := range bytes.Split(b, []byte{0}) {
		if len(raw) > 0 {
			result[string(raw)] = struct{}{}
		}
	}
	return result, nil
}

func workingTreeID(ctx context.Context, repo, baseCommit string) (string, error) {
	f, err := os.CreateTemp("", "polis-apply-index-*")
	if err != nil {
		return "", err
	}
	indexPath := f.Name()
	_ = f.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := gitBytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", err
	}
	if _, err := gitBytes(ctx, repo, env, nil, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	return gitOutput(ctx, repo, env, nil, "write-tree")
}

func reversePatch(ctx context.Context, repo string, patch []byte) error {
	_, err := gitBytes(ctx, repo, nil, bytes.NewReader(patch), "apply", "--reverse", "-")
	return err
}

func fileSHA256(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func gitOutput(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) (string, error) {
	b, err := gitBytes(ctx, repo, env, stdin, args...)
	return strings.TrimSpace(string(b)), err
}

func gitBytes(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = stdin
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return b, nil
}
