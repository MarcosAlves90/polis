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
	"path/filepath"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/baselineproof"
	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/isolation"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Options struct {
	Repo               string
	Policy             string
	Project            string
	Change             string
	Out                string
	Contract           string
	RegressionPatch    string
	ImplementationPlan string
	DeferredGates      []string
}

type WorkspaceOptions struct {
	Repo               string
	Policy             string
	Contract           string
	RegressionPatch    string
	ImplementationPlan string
}

type Result struct {
	Path                       string
	SHA256                     string
	RetainedPaths              []string `json:"retained_paths,omitempty"`
	BaseCommit                 string
	TargetTree                 string
	ContractSHA256             string `json:"contract_sha256,omitempty"`
	PolicySHA256               string `json:"policy_sha256,omitempty"`
	ValidationLevel            string
	EnabledGates               []string
	DisabledGates              []string
	DeferredGates              []string
	ConsumerValidationRequired bool
	ProducerGateStatuses       map[string]spec.Status
}

const (
	gitRevParse = "rev-parse"
	gitCached   = "--cached"
)

type buildArtifact struct {
	opts                  Options
	objectFormat          string
	baseCommit            string
	targetTree            string
	policyRaw             []byte
	policy                spec.Policy
	plan                  policyplan.Plan
	producerGateStatuses  map[string]spec.Status
	changeRaw             []byte
	regressionPatch       []byte
	patch                 []byte
	evidence              []byte
	baseline              []byte
	implementationPlanRaw []byte
}

func Build(ctx context.Context, opts Options) (Result, error) {
	return executeBuild(ctx, opts, false)
}

func ValidateWorkspace(ctx context.Context, opts WorkspaceOptions) (Result, error) {
	return executeBuild(ctx, Options{
		Repo: opts.Repo, Policy: opts.Policy, Contract: opts.Contract,
		RegressionPatch: opts.RegressionPatch, ImplementationPlan: opts.ImplementationPlan,
	}, true)
}

func executeBuild(ctx context.Context, opts Options, workspaceValidation bool) (Result, error) {
	if workspaceValidation {
		if opts.Repo == "" || opts.Contract == "" {
			return Result{}, errors.New("repo and contract are required")
		}
	} else if err := validateBuildOptions(opts); err != nil {
		return Result{}, err
	}
	repo, err := resolveRepo(ctx, opts.Repo)
	if err != nil {
		return Result{}, err
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	changeRaw, changeContract, regressionPatch, err := loadBuildInputs(repo, opts, retention)
	if err != nil {
		return Result{}, err
	}
	implementationPlanRaw, implementationPlan, err := implementationplan.LoadWithRetention(repo, opts.ImplementationPlan, changeContract, changeRaw, retention)
	if err != nil {
		return Result{}, fmt.Errorf("invalid implementation plan: %w", err)
	}
	objectFormat, _, err := resolveSourceIdentity(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	if err := requireBuildSourceState(ctx, repo); err != nil {
		return Result{}, err
	}
	if err := devlock.ValidateBuildRepository(ctx, repo, changeContract); err != nil {
		return Result{}, fmt.Errorf("locked development baseline: %w", err)
	}
	var policyRaw []byte
	var policy spec.Policy
	if opts.Policy != "" {
		policyRaw, policy, err = policyload.LoadExternal(repo, opts.Policy)
	} else {
		policyRaw, policy, err = policyload.LoadCommitted(ctx, repo)
	}
	if err != nil {
		return Result{}, err
	}
	if err := devlock.ValidatePolicy(changeContract, policyRaw); err != nil {
		return Result{}, fmt.Errorf("locked development baseline: %w", err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return Result{}, errors.New("POLIS V6 producer validation requires Project Policy schema v3")
	}
	if err := requireV6ProducerContract(changeContract); err != nil {
		return Result{}, err
	}
	plan, err := policyplan.CompileWithOptions(policy, policyplan.CompileOptions{DeferredGates: opts.DeferredGates})
	if err != nil {
		return Result{}, err
	}
	if implementationPlan != nil {
		gateOrder, err := implementationplan.EffectiveExecutionOrder(plan)
		if err != nil {
			return Result{}, err
		}
		if err := implementationPlan.ValidateProjectGates(gateOrder); err != nil {
			return Result{}, fmt.Errorf("implementation plan Project Policy gates: %w", err)
		}
	}
	baseCommit := changeContract.BaselineLock.BaseCommit
	targetTree, patch, changedPaths, err := buildTargetWithTemporaryIndex(ctx, repo, baseCommit)
	if err != nil {
		return Result{}, err
	}
	if len(patch) == 0 {
		return Result{}, errors.New("generated patch is empty")
	}
	if err := changeContract.ValidateChangedPaths(changedPaths); err != nil {
		return Result{}, fmt.Errorf("change scope validation: %w", err)
	}
	baseline, err := baselineproof.Build(ctx, repo, baseCommit, spec.MaxBaselineMemberBytes)
	if err != nil {
		return Result{}, baselineConstraintError(fmt.Errorf("build embedded baseline: %w", err), plan, workspaceValidation)
	}
	baselineRepo, cleanupBaseline, err := baselineproof.Materialize(ctx, baseline, objectFormat, baseCommit, changeContract.BaselineLock.BaseTree)
	if err != nil {
		return Result{}, baselineConstraintError(fmt.Errorf("materialize embedded baseline for producer replay: %w", err), plan, workspaceValidation)
	}
	defer cleanupBaseline()

	var evidence bytes.Buffer
	var producerResult policyexec.Result
	notRun := []string{"artifact packaging"}
	if workspaceValidation {
		notRun = nil
	}
	validation := isolation.Validation{
		BaselineRepo:          baselineRepo,
		BaselineCommit:        baseCommit,
		TargetRepo:            repo,
		TargetTree:            targetTree,
		Patch:                 patch,
		RegressionPatch:       regressionPatch,
		Change:                changeContract,
		Policy:                policy,
		ExecutionPlan:         &plan,
		PolicyResult:          &producerResult,
		Evidence:              &evidence,
		RedWorktreePattern:    "polis-red-worktree-*",
		TargetWorktreePattern: "polis-worktree-*",
		CreateWorktreeError:   "create isolated worktree",
		TargetApplyCheckError: "isolated git apply --check failed",
		TargetApplyError:      "isolated git apply --index failed",
		PolicyFailureLabel:    "project policy validation",
		PolicyFailureStage:    "project gate validation",
		PolicyFailureNotRun:   notRun,
	}
	if err := isolation.Validate(ctx, validation); err != nil {
		return Result{}, err
	}
	if workspaceValidation {
		if err := requireBuildSourceState(ctx, repo); err != nil {
			return Result{}, err
		}
		currentTree, _, _, err := buildTargetWithTemporaryIndex(ctx, repo, baseCommit)
		if err != nil {
			return Result{}, fmt.Errorf("recheck workspace target after validation: %w", err)
		}
		if currentTree != targetTree {
			return Result{}, errors.New("workspace changed during validation; no current validation result can be reported")
		}
		summary := policy.ValidationSummary()
		return Result{
			BaseCommit: baseCommit, TargetTree: targetTree,
			ContractSHA256: sha256Hex(changeRaw), PolicySHA256: sha256Hex(policyRaw),
			ValidationLevel:      summary.Level,
			EnabledGates:         append([]string(nil), summary.EnabledGates...),
			DisabledGates:        append([]string(nil), summary.DisabledGates...),
			ProducerGateStatuses: cloneGateStatuses(producerResult.Gates),
		}, nil
	}

	artifact := buildArtifact{
		opts: opts, objectFormat: objectFormat, baseCommit: baseCommit, targetTree: targetTree,
		policyRaw: policyRaw, policy: policy, plan: plan, producerGateStatuses: producerResult.Gates,
		changeRaw: changeRaw, regressionPatch: regressionPatch, patch: patch, evidence: evidence.Bytes(), baseline: baseline, implementationPlanRaw: implementationPlanRaw,
	}
	manifestRaw, err := encodeManifest(artifact)
	if err != nil {
		return Result{}, err
	}
	result, err := finalizeArtifact(artifact, manifestRaw)
	if err != nil || !retention.RepositoryEnabled() {
		return result, err
	}
	packageBytes, err := os.ReadFile(result.Path)
	if err != nil {
		return Result{}, fmt.Errorf("read generated package for retention: %w", err)
	}
	retained := []artifactretention.Artifact{
		{Class: "contracts", Data: artifact.changeRaw},
		{Class: "evidence", Data: artifact.evidence},
		{Class: "packages", Data: packageBytes},
	}
	if len(artifact.implementationPlanRaw) > 0 {
		retained = append(retained, artifactretention.Artifact{Class: "plans", Data: artifact.implementationPlanRaw})
	}
	if len(artifact.regressionPatch) > 0 {
		retained = append(retained, artifactretention.Artifact{Class: "proofs", Data: artifact.regressionPatch})
	}
	paths, err := retention.PublishMany(repo, retained)
	if err != nil {
		return Result{}, fmt.Errorf("retain generated delivery artifacts: %w", err)
	}
	result.RetainedPaths = paths
	return result, nil
}

func baselineConstraintError(err error, plan policyplan.Plan, workspaceValidation bool) error {
	notRun := make([]string, 0, len(plan.EnabledGates)+1)
	for _, gate := range plan.Gates {
		if gate.ProducerAction == policyplan.ProducerActionExecute {
			notRun = append(notRun, gate.ID)
		}
	}
	if !workspaceValidation {
		notRun = append(notRun, "artifact packaging")
	}
	report := diagnostic.Report{
		Stage:     "locked baseline constraint",
		Condition: "complete identity-checked embedded baseline could not be processed",
		NotRun:    notRun,
	}
	summary := err.Error()
	if detail, ok := diagnostic.As(err); ok {
		report = detail.Report
		report.Stage = "locked baseline constraint"
		report.NotRun = notRun
		if detail.Summary != "" {
			summary = detail.Summary
		}
	}
	return &diagnostic.Error{
		Summary: "locked baseline constraint: " + summary,
		Report:  report,
		Cause:   err,
	}
}

func validateBuildOptions(opts Options) error {
	if opts.Repo == "" || opts.Project == "" || opts.Change == "" || opts.Out == "" || opts.Contract == "" {
		return errors.New("repo, project, change, out, and contract are required")
	}
	return nil
}

func loadBuildInputs(repo string, opts Options, retention artifactretention.State) ([]byte, spec.ChangeContract, []byte, error) {
	changeRaw, err := retention.ReadInput(repo, opts.Contract, "contracts", 1<<20, "input exceeds maximum size %d")
	if err != nil {
		return nil, spec.ChangeContract{}, nil, fmt.Errorf("load change contract: %w", err)
	}
	changeContract, err := spec.DecodeChangeContract(changeRaw)
	if err != nil {
		return nil, spec.ChangeContract{}, nil, fmt.Errorf("invalid change contract: %w", err)
	}
	regressionPatch, err := loadRegressionPatch(repo, opts.RegressionPatch, changeContract, retention)
	if err != nil {
		return nil, spec.ChangeContract{}, nil, err
	}
	return changeRaw, changeContract, regressionPatch, nil
}

func requireV6ProducerContract(change spec.ChangeContract) error {
	if !change.IsLockedStrictDevelopment() || change.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 || change.BaselineLock == nil {
		return errors.New("POLIS V6 producer validation requires locked Change Contract schema v4 or v6 produced by polis start")
	}
	return nil
}

func loadRegressionPatch(repo, filename string, change spec.ChangeContract, retention artifactretention.State) ([]byte, error) {
	if !change.RequiresRedGreen() {
		if filename != "" {
			return nil, errors.New("change contract without red_green must not provide regression-patch")
		}
		return nil, nil
	}
	if filename == "" {
		if change.Kind == spec.ChangeKindDefect {
			return nil, errors.New("Red-to-Green defect requires regression-patch")
		}
		return nil, errors.New("Red-to-Green feature requires regression-patch")
	}
	patch, err := retention.ReadInput(repo, filename, "proofs", 16<<20, "input exceeds maximum size %d")
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
	return requireCleanIndex(ctx, repo)
}

func encodeManifest(artifact buildArtifact) ([]byte, error) {
	formatVersion := spec.FormatVersion
	planSHA256 := ""
	if len(artifact.implementationPlanRaw) > 0 {
		formatVersion = spec.ImplementationPlanFormatVersion
		planSHA256 = sha256Hex(artifact.implementationPlanRaw)
	}
	manifest := spec.Manifest{
		FormatVersion: formatVersion, Project: artifact.opts.Project, Change: artifact.opts.Change,
		GitObjectFormat: artifact.objectFormat, BaseCommit: artifact.baseCommit, TargetTree: artifact.targetTree,
		PolicySHA256: sha256Hex(artifact.policyRaw), ChangeContractSHA256: sha256Hex(artifact.changeRaw),
		RegressionPatchSHA256: sha256Hex(artifact.regressionPatch), PayloadSHA256: sha256Hex(artifact.patch), BaselineSHA256: sha256Hex(artifact.baseline),
		ImplementationPlanSHA256: planSHA256,
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
	candidate, err := writeCandidateArchive(artifact.opts.Out, candidateArchiveContents{
		Manifest: manifestRaw, Policy: artifact.policyRaw, Change: artifact.changeRaw,
		Regression: artifact.regressionPatch, Payload: artifact.patch, Evidence: artifact.evidence, Baseline: artifact.baseline, ImplementationPlan: artifact.implementationPlanRaw,
	})
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
	summary := artifact.policy.ValidationSummary()
	return Result{
		Path:                       finalPath,
		SHA256:                     archiveHash,
		BaseCommit:                 artifact.baseCommit,
		TargetTree:                 artifact.targetTree,
		ValidationLevel:            summary.Level,
		EnabledGates:               append([]string{}, summary.EnabledGates...),
		DisabledGates:              append([]string{}, summary.DisabledGates...),
		DeferredGates:              append([]string{}, artifact.plan.DeferredGates...),
		ConsumerValidationRequired: artifact.plan.ConsumerValidationRequired,
		ProducerGateStatuses:       cloneGateStatuses(artifact.producerGateStatuses),
	}, nil
}

func cloneGateStatuses(gates map[string]spec.Status) map[string]spec.Status {
	cloned := make(map[string]spec.Status, len(gates))
	for gate, status := range gates {
		cloned[gate] = status
	}
	return cloned
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	return gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{PathError: "resolve repo path", GitError: "not a Git worktree", RootError: "resolve Git root"})
}

func requireCleanIndex(ctx context.Context, repo string) error {
	staged, err := artifactretention.StagedChanges(ctx, repo)
	if err != nil {
		return err
	}
	if !staged {
		return nil
	}
	return errors.New("source index contains staged changes outside .polis/artifacts/; POLIS producer validation requires index == HEAD")
}

func buildTargetWithTemporaryIndex(ctx context.Context, repo, baseCommit string) (string, []byte, []string, error) {
	indexPath, cleanupIndex, err := gitutil.TemporaryIndex("polis-index-*")
	if err != nil {
		return "", nil, nil, fmt.Errorf("allocate temporary index path: %w", err)
	}
	defer cleanupIndex()
	objectEnv, cleanupObjects, err := gitutil.TemporaryObjectEnv(ctx, repo, "polis-build-objects-*")
	if err != nil {
		return "", nil, nil, fmt.Errorf("allocate temporary object directory: %w", err)
	}
	defer cleanupObjects()
	env := append(objectEnv, "GIT_INDEX_FILE="+indexPath)
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "read-tree", baseCommit); err != nil {
		return "", nil, nil, fmt.Errorf("initialize temporary index: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, repo, env, nil, "add", "-A", "--", ".", ":(exclude).polis/artifacts/**"); err != nil {
		return "", nil, nil, fmt.Errorf("capture working tree in temporary index: %w", err)
	}
	targetTree, err := gitutil.Output(ctx, repo, env, nil, "write-tree")
	if err != nil {
		return "", nil, nil, fmt.Errorf("write target tree: %w", err)
	}
	patch, err := gitutil.Bytes(ctx, repo, env, nil, "diff", gitCached, "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--find-renames", baseCommit, "--")
	if err != nil {
		return "", nil, nil, fmt.Errorf("generate patch: %w", err)
	}
	changedRaw, err := gitutil.Bytes(ctx, repo, env, nil, "diff", "--no-renames", "--name-only", "-z", baseCommit, targetTree, "--")
	if err != nil {
		return "", nil, nil, fmt.Errorf("list base-to-target changed paths: %w", err)
	}
	var changedPaths []string
	for _, raw := range bytes.Split(changedRaw, []byte{0}) {
		if len(raw) > 0 {
			changedPaths = append(changedPaths, string(raw))
		}
	}
	return targetTree, patch, changedPaths, nil
}

type candidateArchiveContents struct {
	Manifest           []byte
	Policy             []byte
	Change             []byte
	Regression         []byte
	Payload            []byte
	Evidence           []byte
	Baseline           []byte
	ImplementationPlan []byte
}

func writeCandidateArchive(out string, contents candidateArchiveContents) (string, error) {
	members := map[string][]byte{
		spec.MemberManifest:   contents.Manifest,
		spec.MemberPolicy:     contents.Policy,
		spec.MemberChange:     contents.Change,
		spec.MemberRegression: contents.Regression,
		spec.MemberPayload:    contents.Payload,
		spec.MemberEvidence:   contents.Evidence,
		spec.MemberBaseline:   contents.Baseline,
	}
	if len(contents.ImplementationPlan) > 0 {
		members[spec.MemberImplementationPlan] = contents.ImplementationPlan
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
	members[spec.MemberChecksums] = []byte(checksums.String())

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
