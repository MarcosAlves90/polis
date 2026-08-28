package packagebuild

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/internal/changeexec"
	"github.com/MarcosAlves90/polis/internal/packageverify"
	"github.com/MarcosAlves90/polis/internal/pathguard"
	"github.com/MarcosAlves90/polis/internal/policyexec"
	"github.com/MarcosAlves90/polis/spec"
)

type Options struct {
	Repo            string
	Project         string
	Change          string
	Out             string
	Contract        string
	RegressionPatch string
}

type Result struct {
	Path       string
	SHA256     string
	BaseCommit string
	TargetTree string
}

const (
	gitRevParse = "rev-parse"
	gitCached   = "--cached"
)

type isolatedValidation struct {
	repo            string
	baseCommit      string
	targetTree      string
	patch           []byte
	regressionPatch []byte
	change          spec.ChangeContract
	policy          spec.Policy
	evidence        io.Writer
}

func Build(ctx context.Context, opts Options) (Result, error) {
	if opts.Repo == "" || opts.Project == "" || opts.Change == "" || opts.Out == "" || opts.Contract == "" {
		return Result{}, errors.New("repo, project, change, out, and contract are required")
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	changeRaw, err := readExternalInput(repo, opts.Contract, 1<<20)
	if err != nil {
		return Result{}, fmt.Errorf("load change contract: %w", err)
	}
	changeContract, err := spec.DecodeChangeContract(changeRaw)
	if err != nil {
		return Result{}, fmt.Errorf("invalid change contract: %w", err)
	}
	var regressionPatch []byte
	if changeContract.Kind == spec.ChangeKindDefect {
		if opts.RegressionPatch == "" {
			return Result{}, errors.New("defect build requires regression-patch")
		}
		regressionPatch, err = readExternalInput(repo, opts.RegressionPatch, 16<<20)
		if err != nil {
			return Result{}, fmt.Errorf("load regression patch: %w", err)
		}
		if len(regressionPatch) == 0 {
			return Result{}, errors.New("regression patch is empty")
		}
	} else if opts.RegressionPatch != "" {
		return Result{}, errors.New("non-defect build must not provide regression-patch")
	}
	objectFormat, err := gitOutput(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return Result{}, fmt.Errorf("detect Git object format: %w", err)
	}
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return Result{}, fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	baseCommit, err := gitOutput(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	if err := requireCleanIndex(ctx, repo); err != nil {
		return Result{}, err
	}
	status, err := gitOutput(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Result{}, fmt.Errorf("inspect working tree: %w", err)
	}
	if status == "" {
		return Result{}, errors.New("working tree has no non-ignored changes")
	}

	policyRaw, policy, err := loadCommittedPolicy(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	targetTree, patch, err := buildTargetWithTemporaryIndex(ctx, repo, baseCommit)
	if err != nil {
		return Result{}, err
	}
	if len(patch) == 0 {
		return Result{}, errors.New("generated patch is empty")
	}

	var evidence bytes.Buffer
	validation := isolatedValidation{
		repo:            repo,
		baseCommit:      baseCommit,
		targetTree:      targetTree,
		patch:           patch,
		regressionPatch: regressionPatch,
		change:          changeContract,
		policy:          policy,
		evidence:        &evidence,
	}
	if err := validateIsolated(ctx, validation); err != nil {
		return Result{}, err
	}

	policySum := sha256.Sum256(policyRaw)
	changeSum := sha256.Sum256(changeRaw)
	regressionSum := sha256.Sum256(regressionPatch)
	payloadSum := sha256.Sum256(patch)
	manifest := spec.Manifest{
		FormatVersion:         spec.FormatVersion,
		Project:               opts.Project,
		Change:                opts.Change,
		GitObjectFormat:       objectFormat,
		BaseCommit:            baseCommit,
		TargetTree:            targetTree,
		PolicySHA256:          hex.EncodeToString(policySum[:]),
		ChangeContractSHA256:  hex.EncodeToString(changeSum[:]),
		RegressionPatchSHA256: hex.EncodeToString(regressionSum[:]),
		PayloadSHA256:         hex.EncodeToString(payloadSum[:]),
	}
	if err := manifest.Validate(); err != nil {
		return Result{}, fmt.Errorf("invalid build identity: %w", err)
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return Result{}, fmt.Errorf("encode manifest: %w", err)
	}

	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}
	candidate, err := writeCandidateArchive(opts.Out, manifestRaw, policyRaw, changeRaw, regressionPatch, patch, evidence.Bytes())
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(candidate)
	if _, err := packageverify.Verify(candidate); err != nil {
		return Result{}, fmt.Errorf("verify candidate POLIS package: %w", err)
	}
	archiveHash, err := fileSHA256(candidate)
	if err != nil {
		return Result{}, err
	}
	finalName := fmt.Sprintf("polis-%s-%s-%s.polis", opts.Project, opts.Change, archiveHash[:12])
	finalPath := filepath.Join(opts.Out, finalName)
	if err := copyExclusive(candidate, finalPath); err != nil {
		return Result{}, err
	}
	return Result{Path: finalPath, SHA256: archiveHash, BaseCommit: baseCommit, TargetTree: targetTree}, nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	root, err := gitOutput(ctx, abs, nil, nil, gitRevParse, "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a Git worktree: %w", err)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve Git root: %w", err)
	}
	return rootAbs, nil
}

func requireCleanIndex(ctx context.Context, repo string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "diff", gitCached, "--quiet", "HEAD", "--")
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return errors.New("source index contains staged changes; POLIS build requires index == HEAD")
	}
	return fmt.Errorf("inspect source index: %w", err)
}

func loadCommittedPolicy(ctx context.Context, repo string) ([]byte, spec.Policy, error) {
	workingPath := filepath.Join(repo, ".polis", "policy.json")
	working, err := os.ReadFile(workingPath)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("read .polis/policy.json: %w", err)
	}
	committed, err := gitBytes(ctx, repo, nil, nil, "show", "HEAD:.polis/policy.json")
	if err != nil {
		return nil, spec.Policy{}, errors.New(".polis/policy.json must exist in HEAD")
	}
	if !bytes.Equal(working, committed) {
		return nil, spec.Policy{}, errors.New(".polis/policy.json working copy differs from HEAD")
	}
	policy, err := spec.DecodePolicy(working)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("invalid .polis/policy.json: %w", err)
	}
	return working, policy, nil
}

func buildTargetWithTemporaryIndex(ctx context.Context, repo, baseCommit string) (string, []byte, error) {
	f, err := os.CreateTemp("", "polis-index-*")
	if err != nil {
		return "", nil, fmt.Errorf("allocate temporary index path: %w", err)
	}
	indexPath := f.Name()
	_ = f.Close()
	_ = os.Remove(indexPath)
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := gitBytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", nil, fmt.Errorf("initialize temporary index: %w", err)
	}
	if _, err := gitBytes(ctx, repo, env, nil, "add", "-A", "--", "."); err != nil {
		return "", nil, fmt.Errorf("capture working tree in temporary index: %w", err)
	}
	targetTree, err := gitOutput(ctx, repo, env, nil, "write-tree")
	if err != nil {
		return "", nil, fmt.Errorf("write target tree: %w", err)
	}
	patch, err := gitBytes(ctx, repo, env, nil, "diff", gitCached, "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--find-renames", baseCommit, "--")
	if err != nil {
		return "", nil, fmt.Errorf("generate patch: %w", err)
	}
	return targetTree, patch, nil
}

func validateIsolated(ctx context.Context, validation isolatedValidation) error {
	repo := validation.repo
	baseCommit := validation.baseCommit
	targetTree := validation.targetTree
	patch := validation.patch
	regressionPatch := validation.regressionPatch
	change := validation.change
	policy := validation.policy
	evidence := validation.evidence

	var redPaths map[string]struct{}
	if change.Kind == spec.ChangeKindDefect {
		redWorktree, cleanup, err := detachedWorktree(ctx, repo, baseCommit, "polis-red-worktree-*")
		if err != nil {
			return err
		}
		defer cleanup()
		if _, err := gitBytes(ctx, redWorktree, nil, bytes.NewReader(regressionPatch), "apply", "--check", "-"); err != nil {
			return fmt.Errorf("regression probe apply check failed: %w", err)
		}
		if _, err := gitBytes(ctx, redWorktree, nil, bytes.NewReader(regressionPatch), "apply", "--index", "-"); err != nil {
			return fmt.Errorf("regression probe apply failed: %w", err)
		}
		redPaths, err = changedIndexPaths(ctx, redWorktree)
		if err != nil {
			return err
		}
		if err := changeexec.ExecuteBaseline(change, redWorktree, evidence); err != nil {
			return fmt.Errorf("regression baseline validation: %w", err)
		}
	}

	worktree, cleanup, err := detachedWorktree(ctx, repo, baseCommit, "polis-worktree-*")
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("isolated git apply --check failed: %w", err)
	}
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("isolated git apply --index failed: %w", err)
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
	policyValidation := policyexec.Execute(policy, worktree, evidence)
	if policyValidation.Overall != spec.StatusPass {
		return fmt.Errorf("project policy validation %s", policyValidation.Overall)
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
		return "", nil, fmt.Errorf("create isolated worktree: %w", err)
	}
	cleanup := func() {
		_, _ = gitBytes(context.Background(), repo, nil, nil, "worktree", "remove", "--force", worktree)
		_ = os.RemoveAll(parent)
	}
	return worktree, cleanup, nil
}

func changedIndexPaths(ctx context.Context, worktree string) (map[string]struct{}, error) {
	b, err := gitBytes(ctx, worktree, nil, nil, "diff", gitCached, "--name-only", "-z", "HEAD", "--")
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

func writeCandidateArchive(out string, manifest, policy, change, regression, payload, evidence []byte) (string, error) {
	members := map[string][]byte{
		"polis/polis-manifest.json":    manifest,
		"polis/polis-policy.json":      policy,
		"polis/polis-change.json":      change,
		"polis/polis-regression.patch": regression,
		"polis/polis-payload.patch":    payload,
		"polis/polis-evidence.ndjson":  evidence,
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		sum := sha256.Sum256(members[name])
		checksums.WriteString(hex.EncodeToString(sum[:]))
		checksums.WriteString("  ")
		checksums.WriteString(name)
		checksums.WriteByte('\n')
	}
	members["polis/polis-checksums.sha256"] = []byte(checksums.String())

	f, err := os.CreateTemp(out, ".polis-candidate-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create candidate archive: %w", err)
	}
	candidate := f.Name()
	zw := zip.NewWriter(f)
	names = names[:0]
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", fmt.Errorf("create archive member %s: %w", name, err)
		}
		if _, err := w.Write(members[name]); err != nil {
			_ = zw.Close()
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", fmt.Errorf("write archive member %s: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(candidate)
		return "", fmt.Errorf("finalize candidate archive: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(candidate)
		return "", fmt.Errorf("close candidate archive: %w", err)
	}
	return candidate, nil
}

func readExternalInput(repo, filename string, max int64) ([]byte, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	contained, err := pathguard.Contains(repo, abs)
	if err != nil {
		return nil, fmt.Errorf("resolve input boundary: %w", err)
	}
	if contained {
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
		return nil, fmt.Errorf("input exceeds maximum size %d", max)
	}
	return os.ReadFile(abs)
}

func fileSHA256(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("open archive for hashing: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash archive: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyExclusive(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open verified archive: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output artifact already exists: %s", target)
		}
		return fmt.Errorf("create final artifact: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(target)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy final artifact: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close final artifact: %w", err)
	}
	ok = true
	return nil
}

func gitOutput(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) (string, error) {
	b, err := gitBytes(ctx, repo, env, stdin, args...)
	return strings.TrimSpace(string(b)), err
}

func gitBytes(ctx context.Context, repo string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
	allArgs := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, "git", allArgs...)
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
