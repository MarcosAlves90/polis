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

	"github.com/MarcosAlves90/polis/v4/internal/fileutil"
	"github.com/MarcosAlves90/polis/v4/internal/gitutil"
	"github.com/MarcosAlves90/polis/v4/internal/isolation"
	"github.com/MarcosAlves90/polis/v4/internal/packageverify"
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
	validation := isolation.Validation{
		Repo:                  repo,
		BaseCommit:            baseCommit,
		TargetTree:            targetTree,
		Patch:                 patch,
		RegressionPatch:       regressionPatch,
		Change:                changeContract,
		Policy:                policy,
		Evidence:              &evidence,
		RedWorktreePattern:    "polis-red-worktree-*",
		TargetWorktreePattern: "polis-worktree-*",
		CreateWorktreeError:   "create isolated worktree",
		TargetApplyCheckError: "isolated git apply --check failed",
		TargetApplyError:      "isolated git apply --index failed",
		PolicyFailureLabel:    "project policy validation",
	}
	if err := isolation.Validate(ctx, validation); err != nil {
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
	objectFormat, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "--show-object-format")
	if err != nil {
		return "", "", fmt.Errorf("detect Git object format: %w", err)
	}
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return "", "", fmt.Errorf("unsupported Git object format %q", objectFormat)
	}
	baseCommit, err := gitutil.Output(ctx, repo, nil, nil, gitRevParse, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("resolve HEAD: %w", err)
	}
	return objectFormat, baseCommit, nil
}

func requireBuildSourceState(ctx context.Context, repo string) error {
	if err := requireCleanIndex(ctx, repo); err != nil {
		return err
	}
	status, err := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
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
	return gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{PathError: "resolve repo path", GitError: "not a Git worktree", RootError: "resolve Git root"})
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
	committed, err := gitutil.Bytes(ctx, repo, nil, nil, "show", "HEAD:.polis/policy.json")
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
	indexPath, cleanup, err := gitutil.TemporaryIndex("polis-index-*")
	if err != nil {
		return "", nil, fmt.Errorf("allocate temporary index path: %w", err)
	}
	defer cleanup()
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", nil, fmt.Errorf("initialize temporary index: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "add", "-A", "--", "."); err != nil {
		return "", nil, fmt.Errorf("capture working tree in temporary index: %w", err)
	}
	targetTree, err := gitutil.Output(ctx, repo, env, nil, "write-tree")
	if err != nil {
		return "", nil, fmt.Errorf("write target tree: %w", err)
	}
	patch, err := gitutil.Bytes(ctx, repo, env, nil, "diff", gitCached, "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--find-renames", baseCommit, "--")
	if err != nil {
		return "", nil, fmt.Errorf("generate patch: %w", err)
	}
	return targetTree, patch, nil
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
	return fileutil.ReadOutside(repo, filename, fileutil.OutsideReadOptions{Max: max, OversizeMessage: "input exceeds maximum size %d"})
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
