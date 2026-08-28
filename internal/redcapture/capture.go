package redcapture

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

	"github.com/MarcosAlves90/polis/internal/changeexec"
	"github.com/MarcosAlves90/polis/spec"
)

type Options struct {
	Repo     string
	Contract string
	Out      string
}

type Result struct {
	Path   string
	SHA256 string
}

func Capture(ctx context.Context, opts Options) (Result, error) {
	if opts.Repo == "" || opts.Contract == "" || opts.Out == "" {
		return Result{}, errors.New("repo, contract, and out are required")
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	contractRaw, err := readExternal(repo, opts.Contract, 1<<20)
	if err != nil {
		return Result{}, fmt.Errorf("load change contract: %w", err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		return Result{}, fmt.Errorf("invalid change contract: %w", err)
	}
	if contract.Kind != spec.ChangeKindDefect {
		return Result{}, errors.New("capture-red requires a defect change contract")
	}
	outAbs, err := filepath.Abs(opts.Out)
	if err != nil {
		return Result{}, err
	}
	if inside(repo, outAbs) {
		return Result{}, errors.New("output must be outside target worktree")
	}
	if _, err := os.Lstat(outAbs); err == nil {
		return Result{}, fmt.Errorf("output already exists: %s", outAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	if err := requireCleanIndex(ctx, repo); err != nil {
		return Result{}, err
	}
	head, err := gitOutput(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	indexTree, err := gitOutput(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return Result{}, err
	}
	status, err := gitOutput(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Result{}, err
	}
	if status == "" {
		return Result{}, errors.New("working tree has no non-ignored changes")
	}
	patch, err := capturePatch(ctx, repo, head)
	if err != nil {
		return Result{}, err
	}
	if len(patch) == 0 {
		return Result{}, errors.New("captured regression patch is empty")
	}
	if err := validateProbe(ctx, repo, head, patch, contract); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return Result{}, err
	}
	f, err := os.OpenFile(outAbs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create output: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(outAbs)
		}
	}()
	if _, err := f.Write(patch); err != nil {
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		return Result{}, err
	}
	raw, err := os.ReadFile(outAbs)
	if err != nil || !bytes.Equal(raw, patch) {
		return Result{}, errors.New("written regression patch does not match validated bytes")
	}
	if got, _ := gitOutput(ctx, repo, nil, nil, "rev-parse", "HEAD"); got != head {
		return Result{}, errors.New("source HEAD changed during capture")
	}
	if got, _ := gitOutput(ctx, repo, nil, nil, "write-tree"); got != indexTree {
		return Result{}, errors.New("source index changed during capture")
	}
	if got, _ := gitOutput(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all"); got != status {
		return Result{}, errors.New("source working tree changed during capture")
	}
	sum := sha256.Sum256(patch)
	ok = true
	return Result{Path: outAbs, SHA256: hex.EncodeToString(sum[:])}, nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	root, err := gitOutput(ctx, abs, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a Git worktree: %w", err)
	}
	return filepath.Abs(root)
}

func readExternal(repo, filename string, max int64) ([]byte, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if inside(repo, abs) {
		return nil, errors.New("input must be outside target worktree")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	if info.Size() > max {
		return nil, errors.New("input exceeds maximum size")
	}
	return os.ReadFile(abs)
}

func inside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func requireCleanIndex(ctx context.Context, repo string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "diff", "--cached", "--quiet", "HEAD", "--")
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return errors.New("source index contains staged changes; capture-red requires index == HEAD")
	}
	return fmt.Errorf("inspect source index: %w", err)
}

func capturePatch(ctx context.Context, repo, head string) ([]byte, error) {
	f, err := os.CreateTemp("", "polis-red-index-*")
	if err != nil {
		return nil, err
	}
	indexPath := f.Name()
	_ = f.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := gitBytes(ctx, repo, env, nil, "read-tree", head); err != nil {
		return nil, err
	}
	if _, err := gitBytes(ctx, repo, env, nil, "add", "-A", "--", "."); err != nil {
		return nil, err
	}
	return gitBytes(ctx, repo, env, nil, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--find-renames", head, "--")
}

func validateProbe(ctx context.Context, repo, head string, patch []byte, contract spec.ChangeContract) error {
	parent, err := os.MkdirTemp("", "polis-capture-red-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(parent)
	worktree := filepath.Join(parent, "repo")
	if _, err := gitBytes(ctx, repo, nil, nil, "worktree", "add", "--detach", worktree, head); err != nil {
		return err
	}
	defer func() {
		_, _ = gitBytes(context.Background(), repo, nil, nil, "worktree", "remove", "--force", worktree)
	}()
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("regression probe apply failed: %w", err)
	}
	if err := changeexec.ExecuteBaseline(contract, worktree, io.Discard); err != nil {
		return fmt.Errorf("regression Red oracle not satisfied: %w", err)
	}
	return nil
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
