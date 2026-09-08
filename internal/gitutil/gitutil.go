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
	if _, err := Bytes(ctx, repo, nil, nil, "clone", "--quiet", "--no-checkout", "--shared", ".", worktree); err != nil {
		_ = os.RemoveAll(parent)
		if createError == "" {
			return "", nil, err
		}
		return "", nil, fmt.Errorf(wrappedErrorFormat, createError, err)
	}
	if _, err := Bytes(ctx, worktree, nil, nil, "checkout", "--quiet", "--detach", baseCommit); err != nil {
		_ = os.RemoveAll(parent)
		if createError == "" {
			return "", nil, err
		}
		return "", nil, fmt.Errorf(wrappedErrorFormat, createError, err)
	}
	cleanup := func() { _ = os.RemoveAll(parent) }
	return worktree, cleanup, nil
}

func ChangedIndexPaths(ctx context.Context, worktree, cachedFlag string) (map[string]struct{}, error) {
	b, err := Bytes(ctx, worktree, nil, nil, "diff", cachedFlag, "--no-renames", "--name-only", "-z", "HEAD", "--")
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

func ChangedTreePaths(ctx context.Context, repo, base, targetTree string) ([]string, error) {
	b, err := Bytes(ctx, repo, nil, nil, "diff", "--no-renames", "--name-only", "-z", base, targetTree, "--")
	if err != nil {
		return nil, fmt.Errorf("list base-to-target changed paths: %w", err)
	}
	var result []string
	for _, raw := range bytes.Split(b, []byte{0}) {
		if len(raw) > 0 {
			result = append(result, string(raw))
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

func TemporaryObjectEnv(ctx context.Context, repo, prefix string) ([]string, func(), error) {
	objectDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, nil, err
	}
	commonDir, err := Output(ctx, repo, nil, nil, "rev-parse", "--git-common-dir")
	if err != nil {
		_ = os.RemoveAll(objectDir)
		return nil, nil, fmt.Errorf("resolve Git common directory: %w", err)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repo, commonDir)
	}
	commonDir, err = filepath.Abs(commonDir)
	if err != nil {
		_ = os.RemoveAll(objectDir)
		return nil, nil, err
	}
	alternate := filepath.Join(commonDir, "objects")
	env := filteredEnvironment("GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES")
	env = append(env,
		"GIT_OBJECT_DIRECTORY="+objectDir,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES="+alternate,
	)
	return env, func() { _ = os.RemoveAll(objectDir) }, nil
}

func filteredEnvironment(keys ...string) []string {
	blocked := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		blocked[key] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, drop := blocked[key]; drop {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
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
