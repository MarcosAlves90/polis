package gitutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const wrappedErrorFormat = "%s: %w"

func Output(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) (string, error) {
	b, err := Bytes(ctx, repo, env, stdin, args...)
	return strings.TrimSpace(string(b)), err
}

func Bytes(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
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

func DetachedWorktree(ctx context.Context, repo, baseCommit, pattern, stagingError, createError string) (string, func(), error) {
	parent, err := os.MkdirTemp("", pattern)
	if err != nil {
		if stagingError == "" {
			return "", nil, err
		}
		return "", nil, fmt.Errorf(wrappedErrorFormat, stagingError, err)
	}
	worktree := filepath.Join(parent, "repo")
	if _, err := Bytes(ctx, repo, nil, nil, "worktree", "add", "--detach", worktree, baseCommit); err != nil {
		_ = os.RemoveAll(parent)
		if createError == "" {
			return "", nil, err
		}
		return "", nil, fmt.Errorf(wrappedErrorFormat, createError, err)
	}
	cleanup := func() {
		_, _ = Bytes(context.Background(), repo, nil, nil, "worktree", "remove", "--force", worktree)
		_ = os.RemoveAll(parent)
	}
	return worktree, cleanup, nil
}

func ChangedIndexPaths(ctx context.Context, worktree, cachedFlag string) (map[string]struct{}, error) {
	b, err := Bytes(ctx, worktree, nil, nil, "diff", cachedFlag, "--name-only", "-z", "HEAD", "--")
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

func RequireTargetTree(ctx context.Context, worktree, targetTree string) error {
	gotTree, err := Output(ctx, worktree, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("read isolated target tree: %w", err)
	}
	if gotTree != targetTree {
		return fmt.Errorf("isolated target_tree mismatch: got %s want %s", gotTree, targetTree)
	}
	return nil
}

func TemporaryIndex(prefix string) (string, func(), error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", nil, err
	}
	indexPath := f.Name()
	_ = f.Close()
	_ = os.Remove(indexPath)
	return indexPath, func() { _ = os.Remove(indexPath) }, nil
}

type ResolveRootOptions struct {
	EmptyAsDot bool
	PathError  string
	GitError   string
	RootError  string
}

func ResolveRoot(ctx context.Context, repo string, opts ResolveRootOptions) (string, error) {
	if opts.EmptyAsDot && repo == "" {
		repo = "."
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", wrapOptional(opts.PathError, err)
	}
	root, err := Output(ctx, abs, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", wrapOptional(opts.GitError, err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", wrapOptional(opts.RootError, err)
	}
	return root, nil
}

func Wrap(label string, err error) error {
	return fmt.Errorf(wrappedErrorFormat, label, err)
}

func wrapOptional(label string, err error) error {
	if label == "" {
		return err
	}
	return Wrap(label, err)
}
