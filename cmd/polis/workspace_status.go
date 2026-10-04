package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type workspaceStatusResult struct {
	SchemaVersion                int      `json:"schema_version"`
	Status                       string   `json:"status"`
	CheckpointState              string   `json:"checkpoint_state"`
	RecordedValidationStatus     string   `json:"recorded_validation_status"`
	ReportAuthenticated          bool     `json:"report_authenticated"`
	CurrentValidationEstablished bool     `json:"current_validation_established"`
	ProofInputDigestsBound       bool     `json:"proof_input_digests_bound"`
	DeliveryArtifactVerified     bool     `json:"delivery_artifact_verified"`
	LockedBaseCommit             string   `json:"locked_base_commit"`
	RecordedBaseCommit           string   `json:"recorded_base_commit"`
	RecordedTargetTree           string   `json:"recorded_target_tree"`
	CurrentTargetTree            *string  `json:"current_target_tree"`
	RecordedContractSHA256       string   `json:"recorded_contract_sha256"`
	ContractSHA256               string   `json:"contract_sha256"`
	RecordedPolicySHA256         string   `json:"recorded_policy_sha256"`
	PolicySHA256                 string   `json:"policy_sha256"`
	Differences                  []string `json:"differences"`
	NextAction                   string   `json:"next_action"`
}

func runWorkspaceStatus(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("workspace status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", repoHelp)
	policyPath := fs.String("policy", "", externalPolicyHelp)
	contractPath := fs.String("contract", "", "external locked strict Change Contract JSON")
	reportPath := fs.String("report", "", "external workspace-validation-v1 report to inspect")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) || *contractPath == "" || *reportPath == "" {
		fmt.Fprintln(errOut, "usage: polis workspace status --repo <path> [--policy <policy-v3.json>] --contract <external-locked-v4-or-v6.json> --report <external-workspace-validation-v1.json> [--format text|json]")
		return exitUsage
	}
	result, err := inspectWorkspaceCheckpoint(context.Background(), *repo, *policyPath, *contractPath, *reportPath)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE STATUS", exitValidationFailed, err)
	}
	if *format == "json" {
		writeJSON(out, result)
		return exitPass
	}
	writeWorkspaceStatusText(out, result)
	return exitPass
}

func inspectWorkspaceCheckpoint(ctx context.Context, repo, policyPath, contractPath, reportPath string) (workspaceStatusResult, error) {
	root, err := gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{PathError: "resolve repo path", GitError: "not a Git worktree", RootError: "resolve Git root"})
	if err != nil {
		return workspaceStatusResult{}, err
	}
	reportRaw, err := readWorkspaceValidationReport(root, reportPath)
	if err != nil {
		return workspaceStatusResult{}, fmt.Errorf("read workspace report: %w", err)
	}
	report, err := spec.DecodeWorkspaceValidationReport(reportRaw)
	if err != nil {
		return workspaceStatusResult{}, err
	}

	contractRaw, err := readWorkspaceExternalInput(root, contractPath, int64(spec.MaxContractMemberBytes))
	if err != nil {
		return workspaceStatusResult{}, fmt.Errorf("load external locked Change Contract: %w", err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		return workspaceStatusResult{}, fmt.Errorf("decode locked Change Contract: %w", err)
	}
	if !contract.IsLockedStrictDevelopment() || contract.BaselineLock == nil || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 {
		return workspaceStatusResult{}, fmt.Errorf("workspace status requires a locked strict-development Change Contract")
	}
	var policyRaw []byte
	if policyPath != "" {
		policyRaw, _, err = loadWorkspaceExternalPolicy(root, policyPath)
	} else {
		policyRaw, _, err = loadWorkspaceCommittedPolicy(ctx, root)
	}
	if err != nil {
		return workspaceStatusResult{}, fmt.Errorf("load effective Project Policy: %w", err)
	}
	if err := devlock.ValidatePolicy(contract, policyRaw); err != nil {
		return workspaceStatusResult{}, fmt.Errorf("locked baseline policy: %w", err)
	}

	contractSum := sha256.Sum256(contractRaw)
	policySum := sha256.Sum256(policyRaw)
	result := workspaceStatusResult{
		SchemaVersion: 1, Status: "PASS", CheckpointState: "source_snapshot_matches",
		RecordedValidationStatus: report.Status, ReportAuthenticated: false,
		CurrentValidationEstablished: false, ProofInputDigestsBound: false,
		DeliveryArtifactVerified: false, LockedBaseCommit: contract.BaselineLock.BaseCommit,
		RecordedBaseCommit: report.BaseCommit, RecordedTargetTree: report.TargetTree,
		RecordedContractSHA256: report.ContractSHA256, ContractSHA256: hex.EncodeToString(contractSum[:]),
		RecordedPolicySHA256: report.PolicySHA256, PolicySHA256: hex.EncodeToString(policySum[:]),
		Differences: make([]string, 0), NextAction: "polis workspace validate",
	}
	if report.BaseCommit != contract.BaselineLock.BaseCommit {
		result.Differences = append(result.Differences, "base_commit")
	}
	if report.ContractSHA256 != result.ContractSHA256 {
		result.Differences = append(result.Differences, "contract_sha256")
	}
	if report.PolicySHA256 != result.PolicySHA256 {
		result.Differences = append(result.Differences, "policy_sha256")
	}

	if err := devlock.ValidateBuildRepository(ctx, root, contract); err != nil {
		result.CheckpointState = "checkpoint_unavailable"
		result.Differences = append(result.Differences, "current_source_snapshot_unavailable")
		return result, nil
	}
	currentTree, err := packagebuild.WorkspaceTargetTree(ctx, root, contract.BaselineLock.BaseCommit)
	if err != nil {
		result.CheckpointState = "checkpoint_unavailable"
		result.Differences = append(result.Differences, "current_source_snapshot_unavailable")
		return result, nil
	}
	result.CurrentTargetTree = &currentTree
	if report.TargetTree != currentTree {
		result.Differences = append(result.Differences, "target_tree")
	}
	if len(result.Differences) != 0 {
		result.CheckpointState = "source_snapshot_differs"
	}
	return result, nil
}

func writeWorkspaceStatusText(out io.Writer, result workspaceStatusResult) {
	fmt.Fprintf(out, "POLIS WORKSPACE STATUS: %s\nCheckpoint: %s\nRecorded validation claim: %s (unsigned)\nLocked base commit: %s\nRecorded base commit: %s\nRecorded target tree: %s\n",
		result.Status, result.CheckpointState, result.RecordedValidationStatus, result.LockedBaseCommit, result.RecordedBaseCommit, result.RecordedTargetTree)
	if result.CurrentTargetTree == nil {
		fmt.Fprintln(out, "Current target tree: unavailable")
	} else {
		fmt.Fprintf(out, "Current target tree: %s\n", *result.CurrentTargetTree)
	}
	fmt.Fprintf(out, "Recorded contract SHA256: %s\nCurrent contract SHA256: %s\nRecorded policy SHA256: %s\nCurrent policy SHA256: %s\n",
		result.RecordedContractSHA256, result.ContractSHA256, result.RecordedPolicySHA256, result.PolicySHA256)
	fmt.Fprintf(out, "Report authenticated: no\nCurrent validation established: no\nProof input digests bound: no\nDelivery artifact verified: no\nNext action: %s\n",
		result.NextAction)
	if len(result.Differences) != 0 {
		fmt.Fprintf(out, "Differences: %s\n", strings.Join(result.Differences, ", "))
	}
}

const workspaceStatusMaxPolicyBytes int64 = 1 << 20

func readWorkspaceValidationReport(repo, filename string) ([]byte, error) {
	return readWorkspaceExternalInput(repo, filename, spec.MaxWorkspaceValidationReportBytes)
}

func readWorkspaceExternalInput(repo, filename string, maximum int64) ([]byte, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("resolve external input path: %w", err)
	}
	if err := requireWorkspaceExternalPath(repo, abs); err != nil {
		return nil, err
	}
	return readWorkspaceBoundedFile(repo, abs, maximum, true)
}

func requireWorkspaceExternalPath(repo, filename string) error {
	contained, err := pathguard.Contains(repo, filename)
	if err != nil {
		return fmt.Errorf("resolve input boundary: %w", err)
	}
	if contained {
		return fmt.Errorf("input must be outside target worktree")
	}
	return nil
}

func readWorkspaceBoundedFile(repo, filename string, maximum int64, requireExternal bool) ([]byte, error) {
	if maximum < 0 {
		return nil, fmt.Errorf("maximum input size must not be negative")
	}
	pathInfo, err := os.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("inspect input file: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular non-symlink file")
	}
	if pathInfo.Size() > maximum {
		return nil, fmt.Errorf("input exceeds maximum size")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened input file: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return nil, fmt.Errorf("input path changed while opening")
	}
	if openedInfo.Size() > maximum {
		return nil, fmt.Errorf("input exceeds maximum size")
	}
	if requireExternal {
		if err := requireWorkspaceExternalPath(repo, filename); err != nil {
			return nil, err
		}
		currentInfo, err := os.Lstat(filename)
		if err != nil {
			return nil, fmt.Errorf("recheck input path: %w", err)
		}
		if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() || !os.SameFile(openedInfo, currentInfo) {
			return nil, fmt.Errorf("input path changed while opening")
		}
		if err := requireWorkspaceExternalPath(repo, filename); err != nil {
			return nil, err
		}
	}
	return readWorkspaceBoundedBytes(file, maximum)
}

func loadWorkspaceExternalPolicy(repo, filename string) ([]byte, spec.Policy, error) {
	raw, err := readWorkspaceExternalInput(repo, filename, workspaceStatusMaxPolicyBytes)
	if err != nil {
		return nil, spec.Policy{}, err
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return nil, spec.Policy{}, fmt.Errorf("Project Policy schema v%d required", spec.PolicySchemaVersion)
	}
	canonical, err := json.Marshal(policy)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("encode canonical Project Policy: %w", err)
	}
	return append(canonical, '\n'), policy, nil
}

func loadWorkspaceCommittedPolicy(ctx context.Context, repo string) ([]byte, spec.Policy, error) {
	const member = ".polis/policy.json"
	working, err := readWorkspaceBoundedFile(repo, filepath.Join(repo, filepath.FromSlash(member)), workspaceStatusMaxPolicyBytes, false)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("read %s: %w", member, err)
	}
	committed, err := readPinnedWorkspaceBlob(workspaceStatusMaxPolicyBytes, func(args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "--no-replace-objects" && args[1] == "cat-file" && args[2] == "blob" {
			if len(args) != 4 {
				return nil, fmt.Errorf("invalid bounded Git blob command")
			}
			return readWorkspaceGitBlobBounded(ctx, repo, args[3], workspaceStatusMaxPolicyBytes)
		}
		return gitutil.Bytes(ctx, repo, nil, nil, args...)
	})
	if err != nil {
		return nil, spec.Policy{}, err
	}
	if !bytes.Equal(working, committed) {
		return nil, spec.Policy{}, fmt.Errorf("%s working copy differs from HEAD", member)
	}
	policy, err := spec.DecodePolicy(working)
	if err != nil {
		return nil, spec.Policy{}, fmt.Errorf("invalid Project Policy: %w", err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return nil, spec.Policy{}, fmt.Errorf("Project Policy schema v%d required", spec.PolicySchemaVersion)
	}
	return working, policy, nil
}

func readWorkspaceGitBlobBounded(ctx context.Context, repo, object string, maximum int64) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", "-C", repo, "--no-replace-objects", "cat-file", "blob", object)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Git blob output: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Git blob read: %w", err)
	}
	data, readErr := readWorkspaceBoundedBytes(stdout, maximum)
	if readErr != nil {
		_ = stdout.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
		return nil, readErr
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("finish Git blob read: %w", err)
	}
	return data, nil
}

func readPinnedWorkspaceBlob(maximum int64, gitBytes func(args ...string) ([]byte, error)) ([]byte, error) {
	if maximum < 1 {
		return nil, fmt.Errorf("maximum Git input size must be positive")
	}
	headRaw, err := gitBytes("--no-replace-objects", "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD for committed Project Policy: %w", err)
	}
	head := strings.TrimSpace(string(headRaw))
	if (len(head) != 40 && len(head) != 64) || head != strings.ToLower(head) {
		return nil, fmt.Errorf("invalid resolved HEAD object ID")
	}
	if _, err := hex.DecodeString(head); err != nil {
		return nil, fmt.Errorf("invalid resolved HEAD object ID: %w", err)
	}
	const member = ".polis/policy.json"
	object := head + ":" + member
	sizeRaw, err := gitBytes("--no-replace-objects", "cat-file", "-s", object)
	if err != nil {
		return nil, fmt.Errorf("%s must exist in pinned HEAD", member)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeRaw)), 10, 64)
	if err != nil || size < 1 || size > maximum {
		return nil, fmt.Errorf("committed %s size must be between 1 and %d bytes", member, maximum)
	}
	data, err := gitBytes("--no-replace-objects", "cat-file", "blob", object)
	if err != nil {
		return nil, fmt.Errorf("read committed %s: %w", member, err)
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("committed %s exceeds maximum size", member)
	}
	headAfterRaw, err := gitBytes("--no-replace-objects", "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("recheck HEAD after committed Project Policy read: %w", err)
	}
	if strings.TrimSpace(string(headAfterRaw)) != head {
		return nil, fmt.Errorf("HEAD changed while reading committed Project Policy")
	}
	return data, nil
}

func readWorkspaceBoundedBytes(reader io.Reader, maximum int64) ([]byte, error) {
	if maximum < 0 {
		return nil, fmt.Errorf("maximum input size must not be negative")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read bounded input: %w", err)
	}
	if int64(len(raw)) > maximum {
		return nil, fmt.Errorf("input exceeds maximum size")
	}
	return raw, nil
}
