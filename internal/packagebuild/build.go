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

	"github.com/MarcosAlves90/polis/v4/internal/changeexec"
	"github.com/MarcosAlves90/polis/v4/internal/packageverify"
	"github.com/MarcosAlves90/polis/v4/internal/pathguard"
	"github.com/MarcosAlves90/polis/v4/internal/policyexec"
	"github.com/MarcosAlves90/polis/v4/spec"
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

type buildArtifact struct {
	opts            Options
	objectFormat    string
	baseCommit      string
	targetTree      string
	policyRaw       []byte
	changeRaw       []byte
	regressionPatch []byte
	patch           []byte
	evidence        []byte
}

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
	if err := validateBuildOptions(opts); err != nil {
		return Result{}, err
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	changeRaw, changeContract, regressionPatch, err := loadBuildInputs(repo, opts)
	if err != nil {
		return Result{}, err
	}
	objectFormat, baseCommit, err := resolveSourceIdentity(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	if err := requireBuildSourceState(ctx, repo); err != nil {
		return Result{}, err
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
		repo: repo, baseCommit: baseCommit, targetTree: targetTree, patch: patch,
		regressionPatch: regressionPatch, change: changeContract, policy: policy, evidence: &evidence,
	}
	if err := validateIsolated(ctx, validation); err != nil {
		return Result{}, err
	}

	artifact := buildArtifact{
		opts: opts, objectFormat: objectFormat, baseCommit: baseCommit, targetTree: targetTree,
		policyRaw: policyRaw, changeRaw: changeRaw, regressionPatch: regressionPatch, patch: patch, evidence: evidence.Bytes(),
	}
	manifestRaw, err := encodeManifest(artifact)
	if err != nil {
		return Result{}, err
	}
	return finalizeArtifact(artifact, manifestRaw)
}

func validateBuildOptions(opts Options) error {
	if opts.Repo == "" || opts.Project == "" || opts.Change == "" || opts.Out == "" || opts.Contract == "" {
		return errors.New("repo, project, change, out, and contract are required")
	}
	return nil
}

func loadBuildInputs(repo string, opts Options) ([]byte, spec.ChangeContract, []byte, error) {
	changeRaw, err := readExternalInput(repo, opts.Contract, 1<<20)
	if err != nil {
		return nil, spec.ChangeContract{}, nil, fmt.Errorf("load change contract: %w", err)
	}
	changeContract, err := spec.DecodeChangeContract(changeRaw)
	if err != nil {
		return nil, spec.ChangeContract{}, nil, fmt.Errorf("invalid change contract: %w", err)
	}
	regressionPatch, err := loadRegressionPatch(repo, opts.RegressionPatch, changeContract.Kind)
	if err != nil {
		return nil, spec.ChangeContract{}, nil, err
	}
	return changeRaw, changeContract, regressionPatch, nil
}

func loadRegressionPatch(repo, filename, changeKind string) ([]byte, error) {
	if changeKind != spec.ChangeKindDefect {
		if filename != "" {
			return nil, errors.New("non-defect build must not provide regression-patch")
		}
		return nil, nil
	}
	if filename == "" {
		return nil, errors.New("defect build requires regression-patch")
	}
	patch, err := readExternalInput(repo, filename, 16<<20)
	if err != nil {
		return nil, fmt.Errorf("load regression patch: %w", err)
	}
	if len(patch) == 0 {
		return nil, errors.New("regression patch is empty")
	}
	return patch, nil
}

func resolveSourceIdentity(ctx context.Context, repo string) (string, string, error) {
	objectFormat, err := gitOutput(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return "", "", fmt.Errorf("detect Git object format: %w", err)
	}
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return "", "", fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	baseCommit, err := gitOutput(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolve HEAD: %w", err)
	}
	return objectFormat, baseCommit, nil
}

func requireBuildSourceState(ctx context.Context, repo string) error {
	if err := requireCleanIndex(ctx, repo); err != nil {
		return err
	}
	status, err := gitOutput(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect working tree: %w", err)
	}
	if status == "" {
		return errors.New("working tree has no non-ignored changes")
	}
	return nil
}

func encodeManifest(artifact buildArtifact) ([]byte, error) {
	manifest := spec.Manifest{
		FormatVersion: spec.FormatVersion, Project: artifact.opts.Project, Change: artifact.opts.Change,
		GitObjectFormat: artifact.objectFormat, BaseCommit: artifact.baseCommit, TargetTree: artifact.targetTree,
		PolicySHA256: sha256Hex(artifact.policyRaw), ChangeContractSHA256: sha256Hex(artifact.changeRaw),
		RegressionPatchSHA256: sha256Hex(artifact.regressionPatch), PayloadSHA256: sha256Hex(artifact.patch),
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid build identity: %w", err)
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return manifestRaw, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func finalizeArtifact(artifact buildArtifact, manifestRaw []byte) (Result, error) {
	if err := os.MkdirAll(artifact.opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}
	candidate, err := writeCandidateArchive(
		artifact.opts.Out, manifestRaw, artifact.policyRaw, artifact.changeRaw,
		artifact.regressionPatch, artifact.patch, artifact.evidence,
	)
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
	finalPath := filepath.Join(artifact.opts.Out, fmt.Sprintf("polis-%s-%s-%s.polis", artifact.opts.Project, artifact.opts.Change, archiveHash[:12]))
	if err := copyExclusive(candidate, finalPath); err != nil {
		return Result{}, err
	}
	return Result{Path: finalPath, SHA256: archiveHash, BaseCommit: artifact.baseCommit, TargetTree: artifact.targetTree}, nil
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
	redPaths, err := validateRegressionIsolation(ctx, validation)
	if err != nil {
		return err
	}
	return validateTargetIsolation(ctx, validation, redPaths)
}

func validateRegressionIsolation(ctx context.Context, validation isolatedValidation) (map[string]struct{}, error) {
	if validation.change.Kind != spec.ChangeKindDefect {
		return nil, nil
	}
	worktree, cleanup, err := detachedWorktree(ctx, validation.repo, validation.baseCommit, "polis-red-worktree-*")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(validation.regressionPatch), "apply", "--check", "-"); err != nil {
		return nil, fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(validation.regressionPatch), "apply", "--index", "-"); err != nil {
		return nil, fmt.Errorf("regression probe apply failed: %w", err)
	}
	redPaths, err := changedIndexPaths(ctx, worktree)
	if err != nil {
		return nil, err
	}
	if err := changeexec.ExecuteBaseline(validation.change, worktree, validation.evidence); err != nil {
		return nil, fmt.Errorf("regression baseline validation: %w", err)
	}
	return redPaths, nil
}

func validateTargetIsolation(ctx context.Context, validation isolatedValidation, redPaths map[string]struct{}) error {
	worktree, cleanup, err := detachedWorktree(ctx, validation.repo, validation.baseCommit, "polis-worktree-*")
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(validation.patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("isolated git apply --check failed: %w", err)
	}
	if _, err := gitBytes(ctx, worktree, nil, bytes.NewReader(validation.patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("isolated git apply --index failed: %w", err)
	}
	if err := requireRegressionPaths(ctx, worktree, redPaths); err != nil {
		return err
	}
	if err := requireTargetTree(ctx, worktree, validation.targetTree); err != nil {
		return err
	}
	if err := changeexec.ExecuteTarget(validation.change, worktree, validation.evidence); err != nil {
		return err
	}
	result := policyexec.Execute(validation.policy, worktree, validation.evidence)
	if result.Overall != spec.StatusPass {
		return fmt.Errorf("project policy validation %s", result.Overall)
	}
	return nil
}

func requireRegressionPaths(ctx context.Context, worktree string, redPaths map[string]struct{}) error {
	targetPaths, err := changedIndexPaths(ctx, worktree)
	if err != nil {
		return err
	}
	for path := range redPaths {
		if _, ok := targetPaths[path]; !ok {
			return fmt.Errorf("regression probe path %q is absent from final payload", path)
		}
	}
	return nil
}

func requireTargetTree(ctx context.Context, worktree, targetTree string) error {
	gotTree, err := gitOutput(ctx, worktree, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("read isolated target tree: %w", err)
	}
	if gotTree != targetTree {
		return fmt.Errorf("isolated target_tree mismatch: got %s want %s", gotTree, targetTree)
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
