package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
)

var agentHelpCommands = []string{
	"help", "doctor", "init", "plan", "gates", "workspace", "start", "implementation-plan", "status",
	"check-red-scope", "capture-red", "build", "verify", "inspect", "preflight", "apply", "sign", "export",
}

func TestAgentHelpEveryCommand(t *testing.T) {
	for _, name := range agentHelpCommands {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run([]string{"help", name}, &out, &errOut); code != exitPass || errOut.Len() != 0 {
				t.Fatalf("help code=%d stderr=%q", code, errOut.String())
			}
			for _, section := range []string{
				"Usage:", "Purpose:", "When to use:", "Prerequisites:", "Required inputs:",
				"Workflow:", "Reads/writes:", "Options/defaults:", "Outcomes:", "Examples:", "Do not use:",
			} {
				if !strings.Contains(out.String(), section+"\n  ") {
					t.Errorf("agent help missing %s for %s", section, name)
				}
			}
			for _, alias := range []string{"-h", "--help"} {
				var aliasOut, aliasErr bytes.Buffer
				if code := run([]string{name, alias}, &aliasOut, &aliasErr); code != exitPass || aliasErr.Len() != 0 || aliasOut.String() != out.String() {
					t.Errorf("agent help missing identical %s %s: code=%d stdout=%q stderr=%q", name, alias, code, aliasOut.String(), aliasErr.String())
				}
			}
		})
	}
}

func TestAgentHelpCanonicalUsage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "usage.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got := strings.Count(guide, "<!-- command-help: "); got != len(agentHelpCommands) {
		t.Fatalf("agent help missing canonical command coverage: got=%d want=%d", got, len(agentHelpCommands))
	}
	for _, name := range agentHelpCommands {
		start := "<!-- command-help: " + name + " -->\n```text\n"
		_, rest, found := strings.Cut(guide, start)
		if !found {
			t.Fatalf("agent help missing canonical instructions for %s", name)
		}
		instructions, _, found := strings.Cut(rest, "```\n<!-- /command-help -->")
		if !found {
			t.Fatalf("unterminated canonical instructions for %s", name)
		}
		var out, errOut bytes.Buffer
		if code := run([]string{"help", name}, &out, &errOut); code != exitPass {
			t.Fatalf("help %s code=%d stderr=%s", name, code, errOut.String())
		}
		want := fmt.Sprintf("POLIS V6 %s\n\n%s", version, instructions)
		if out.String() != want {
			t.Errorf("canonical help drift for %s: got=%q want=%q", name, out.String(), want)
		}
	}
}

func TestAgentHelpSafetyAndOptions(t *testing.T) {
	fragments := map[string][]string{
		"help":                {"-h", "--help", "does not execute"},
		"doctor":              {"--format", "project dependencies", "4"},
		"init":                {"--repo", "--profile", "--validation-level", "--disable-gate", "--test-argv", "--coverage-argv", "--coverage-adapter", "--coverage-report", "--coverage-threshold", "80", "--dry-run", "no --format"},
		"plan":                {"--repo", "--policy", "--defer-gate", "--format", "does not execute"},
		"gates":               {"--repo", "--policy", "--contract", "--gate", "--affected", "--jobs", "--environment-id", "--reuse", "--replay", "--inspect-run", "--out-run", "--format", "not built or verified", "BLOCKED", "parallel_safe"},
		"workspace":           {"validate", "status", "--contract", "--out-report", "--jobs", "--report", "external-locked", "source_snapshot_matches", "report_authenticated=false", "current_validation_established=false", "proof_input_digests_bound=false", "delivery_artifact_built/verified=false", "without a package"},
		"start":               {"--repo", "--policy", "--contract", "--out", "clean", "schema-v3", "schema-v5", "no --format"},
		"implementation-plan": {"--repo", "--policy", "--contract", "--out", "--format", "same plan bytes", "optional"},
		"status":              {"--repo", "--policy", "--contract", "--implementation-plan", "--regression-patch", "--report", "--package", "--format", "proven", "stale_or_unproven", "missing", "unsigned workspace checkpoint", "not implementation completion"},
		"check-red-scope":     {"--repo", "--contract", "--path", "--format", "actual patch", "no regression command"},
		"capture-red":         {"--repo", "--contract", "--implementation-plan", "--out", "--format", "immutable", "BLOCKED", "behavior_preserving"},
		"build":               {"--repo", "--policy", "--project", "--change", "--contract", "--regression-patch", "--implementation-plan", "--defer-gate", "--jobs", "--format", "--out", "consumer", "polis verify"},
		"verify":              {"--format", "--signature", "--trusted-key", "does not execute", "consumer"},
		"inspect":             {"--format", "--signature", "--trusted-key", "does not execute", "traceability"},
		"preflight":           {"--repo", "--baseline-mode", "--allow-missing-baseline-proof", "--format", "--signature", "--trusted-key", "strict", "permissive", "authorization"},
		"apply":               {"--repo", "--baseline-mode", "--allow-missing-baseline-proof", "--commit-mode", "--format", "--signature", "--trusted-key", "none", "prompt", "auto", "authorization", "rollback"},
		"sign":                {"--key", "--out", "--format", "does not validate", "private key"},
		"export":              {"--out", "--format", "--executable", "--runtime", "current executable", "polis verify"},
	}
	for _, name := range agentHelpCommands {
		var out, errOut bytes.Buffer
		if code := run([]string{"help", name}, &out, &errOut); code != exitPass {
			t.Fatalf("help %s code=%d stderr=%s", name, code, errOut.String())
		}
		for _, fragment := range fragments[name] {
			if !strings.Contains(out.String(), fragment) {
				t.Errorf("agent help missing safety/option %q for %s", fragment, name)
			}
		}
	}
}

func TestAgentHelpInvalidRequests(t *testing.T) {
	for _, args := range [][]string{{"help", "unknown"}, {"unknown", "--help"}, {"help", "apply", "extra"}, {"apply", "--help", "extra"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestWorkspaceStatusRejectsParentSymlinkIntoWorktree(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	report := writeWorkspaceStatusFixture(t, repo, contract)
	reportBytes, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	insidePath := filepath.Join(repo, "workspace-report.json")
	if err := os.WriteFile(insidePath, reportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(repo, aliasDir); err != nil {
		t.Fatal(err)
	}
	aliasReport := filepath.Join(aliasDir, filepath.Base(insidePath))
	if code, _, stderr := invokeWorkspaceStatus(repo, contract, aliasReport); code != exitValidationFailed || !strings.Contains(stderr, "input must be outside target worktree") {
		t.Fatalf("parent symlink into worktree was accepted: code=%d stderr=%q", code, stderr)
	}
}

func TestWorkspaceCommittedPolicyPinsHeadDuringBlobRead(t *testing.T) {
	original := strings.Repeat("a", 40)
	updated := strings.Repeat("b", 40)
	revParseCalls := 0
	var sizeChecked, contentRead bool
	gitBytes := func(args ...string) ([]byte, error) {
		if len(args) < 2 || args[0] != "--no-replace-objects" {
			t.Fatalf("Git command did not disable replacement objects: %v", args)
		}
		switch args[1] {
		case "rev-parse":
			revParseCalls++
			if revParseCalls == 1 {
				return []byte(original + "\n"), nil
			}
			return []byte(updated + "\n"), nil
		case "cat-file":
			if len(args) == 4 && args[2] == "-s" && args[3] == original+":.polis/policy.json" {
				sizeChecked = true
				return []byte("4\n"), nil
			}
			if len(args) == 4 && args[2] == "blob" && args[3] == original+":.polis/policy.json" {
				contentRead = true
				return []byte("data"), nil
			}
			t.Fatalf("Git blob query was not pinned to initial HEAD: %v", args)
		default:
			t.Fatalf("unexpected Git command: %v", args)
			return nil, nil
		}
		return nil, nil
	}
	if _, err := readPinnedWorkspaceBlob(8, gitBytes); err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("concurrent HEAD change should fail closed, got %v", err)
	}
	if !sizeChecked || !contentRead || revParseCalls != 2 {
		t.Fatalf("pinned read sequence incomplete: size=%t content=%t rev-parse=%d", sizeChecked, contentRead, revParseCalls)
	}
}

func TestWorkspaceStatusRejectsMissingReport(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	missing := filepath.Join(t.TempDir(), "missing-report.json")
	if code, _, stderr := invokeWorkspaceStatus(repo, contract, missing); code != exitValidationFailed || !strings.Contains(stderr, "inspect input file") {
		t.Fatalf("missing report was not rejected: code=%d stderr=%q", code, stderr)
	}
}

func TestWorkspaceStatusTextOutput(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	report := writeWorkspaceStatusFixture(t, repo, contract)
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", "staged.txt")

	code, stdout, stderr := invokeWorkspaceStatusWithOptions(repo, "", contract, report, "text")
	if code != exitPass {
		t.Fatalf("text status should inspect unavailable checkpoint: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{
		"POLIS WORKSPACE STATUS: PASS",
		"Checkpoint: checkpoint_unavailable",
		"Current target tree: unavailable",
		"Report authenticated: no",
		"Current validation established: no",
		"Proof input digests bound: no",
		"Delivery artifact verified: no",
		"Differences: current_source_snapshot_unavailable",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("text status missing %q: %s", want, stdout)
		}
	}
}

func TestWorkspaceStatusExplicitExternalPolicy(t *testing.T) {
	repo := makeBuildRepo(t)
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	externalInput := append([]byte(" \n"), policyRaw...)
	if err := os.WriteFile(policyPath, externalInput, 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalPolicy := append(append([]byte(nil), policyRaw...), '\n')
	contract := lockedCLIContract(t, repo, policyPath)
	report := writeWorkspaceStatusFixture(t, repo, contract)
	setWorkspaceStatusReportPolicyDigest(t, report, canonicalPolicy)

	code, stdout, stderr := invokeWorkspaceStatusWithOptions(repo, policyPath, contract, report, "json")
	if code != exitPass {
		t.Fatalf("matching external policy should be accepted: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"checkpoint_state":"source_snapshot_matches"`) {
		t.Fatalf("external policy did not produce matching status: %s", stdout)
	}
}

func TestWorkspaceStatusRejectsInvalidExternalPolicy(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	report := writeWorkspaceStatusFixture(t, repo, contract)
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policyPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := invokeWorkspaceStatusWithOptions(repo, policyPath, contract, report, "json")
	if code != exitValidationFailed || !strings.Contains(stderr, "invalid Project Policy") {
		t.Fatalf("invalid external policy was not rejected: code=%d stderr=%q", code, stderr)
	}
}

func TestWorkspaceStatusRejectsModifiedCommittedPolicy(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	report := writeWorkspaceStatusFixture(t, repo, contract)
	policyPath := filepath.Join(repo, ".polis", "policy.json")
	if err := os.WriteFile(policyPath, []byte("changed policy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := invokeWorkspaceStatus(repo, contract, report)
	if code != exitValidationFailed || !strings.Contains(stderr, "working copy differs from HEAD") {
		t.Fatalf("modified committed policy was not rejected: code=%d stderr=%q", code, stderr)
	}
}

func TestRunWorkspaceRejectsInvalidOperations(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}} {
		var out, errOut bytes.Buffer
		if code := runWorkspace(args, &out, &errOut); code != exitUsage || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("workspace args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestReadPinnedWorkspaceBlobRejectsInvalidGitResults(t *testing.T) {
	validHead := strings.Repeat("a", 40)
	tests := []struct {
		name           string
		maximum        int64
		head           string
		size           string
		data           []byte
		resolveErr     bool
		catFileErr     bool
		showErr        bool
		recheckHeadErr bool
		wantError      string
	}{
		{name: "non-positive limit", maximum: 0, head: validHead, size: "4", data: []byte("data"), wantError: "must be positive"},
		{name: "HEAD resolution fails", maximum: 8, head: validHead, size: "4", data: []byte("data"), resolveErr: true, wantError: "resolve HEAD"},
		{name: "malformed object ID", maximum: 8, head: "not-an-object-id", size: "4", data: []byte("data"), wantError: "invalid resolved HEAD object ID"},
		{name: "non-hex object ID", maximum: 8, head: strings.Repeat("g", 40), size: "4", data: []byte("data"), wantError: "invalid resolved HEAD object ID:"},
		{name: "policy blob missing", maximum: 8, head: validHead, size: "4", data: []byte("data"), catFileErr: true, wantError: "must exist in pinned HEAD"},
		{name: "invalid blob size", maximum: 8, head: validHead, size: "unknown", data: []byte("data"), wantError: "size must be"},
		{name: "zero blob size", maximum: 8, head: validHead, size: "0", data: []byte("data"), wantError: "size must be"},
		{name: "oversized blob size", maximum: 3, head: validHead, size: "4", data: []byte("data"), wantError: "size must be"},
		{name: "blob read fails", maximum: 8, head: validHead, size: "4", data: []byte("data"), showErr: true, wantError: "read committed"},
		{name: "returned blob exceeds limit", maximum: 3, head: validHead, size: "3", data: []byte("four"), wantError: "exceeds maximum"},
		{name: "HEAD recheck fails", maximum: 8, head: validHead, size: "4", data: []byte("data"), recheckHeadErr: true, wantError: "recheck HEAD"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			revParseCalls := 0
			gitBytes := func(args ...string) ([]byte, error) {
				if len(args) < 2 || args[0] != "--no-replace-objects" {
					return nil, errors.New("replacement objects were not disabled")
				}
				switch args[1] {
				case "rev-parse":
					revParseCalls++
					if test.resolveErr && revParseCalls == 1 || test.recheckHeadErr && revParseCalls == 2 {
						return nil, errors.New("injected Git failure")
					}
					return []byte(test.head + "\n"), nil
				case "cat-file":
					if len(args) < 3 {
						return nil, errors.New("incomplete cat-file command")
					}
					switch args[2] {
					case "-s":
						if test.catFileErr {
							return nil, errors.New("injected Git failure")
						}
						return []byte(test.size + "\n"), nil
					case "blob":
						if test.showErr {
							return nil, errors.New("injected Git failure")
						}
						return test.data, nil
					default:
						return nil, errors.New("unexpected cat-file operation")
					}
				default:
					return nil, errors.New("unexpected Git command")
				}
			}
			if _, err := readPinnedWorkspaceBlob(test.maximum, gitBytes); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected error containing %q, got %v", test.wantError, err)
			}
		})
	}
}

func TestWorkspaceGitBlobReadIsBounded(t *testing.T) {
	repo := makeBuildRepo(t)
	ctx := context.Background()
	payload := bytes.Repeat([]byte("x"), 1<<20)
	objectRaw, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader(payload), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatalf("write test blob: %v", err)
	}
	object := strings.TrimSpace(string(objectRaw))
	if _, err := readWorkspaceGitBlobBounded(ctx, repo, object, 64); err == nil || !strings.Contains(err.Error(), "input exceeds maximum size") {
		t.Fatalf("oversized Git blob was not bounded: err=%v", err)
	}
}

func TestWorkspaceCommittedPolicyIgnoresReplacementRefs(t *testing.T) {
	repo := makeBuildRepo(t)
	head := cliGit(t, repo, "rev-parse", "HEAD")
	policyPath := filepath.Join(repo, ".polis", "policy.json")
	originalPolicy, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	smallReplacement := createWorkspacePolicyReplacement(t, repo, head, []byte("small replacement\n"))
	largeReplacement := createWorkspacePolicyReplacement(t, repo, head, bytes.Repeat([]byte("x"), int(workspaceStatusMaxPolicyBytes)+4096))
	replacementRef := "refs/replace/" + head
	ctx := context.Background()

	t.Run("existing replacement cannot change size check", func(t *testing.T) {
		cliGit(t, repo, "update-ref", replacementRef, largeReplacement)
		got, err := readPinnedWorkspaceBlob(workspaceStatusMaxPolicyBytes, func(args ...string) ([]byte, error) {
			return gitutil.Bytes(ctx, repo, nil, nil, args...)
		})
		if err != nil || !bytes.Equal(got, originalPolicy) {
			t.Fatalf("replacement ref changed committed policy read: bytes=%d err=%v", len(got), err)
		}
	})

	t.Run("replacement update cannot change content after size check", func(t *testing.T) {
		cliGit(t, repo, "update-ref", replacementRef, smallReplacement)
		replaced := false
		gitBytes := func(args ...string) ([]byte, error) {
			result, err := gitutil.Bytes(ctx, repo, nil, nil, args...)
			if err == nil && !replaced && isWorkspaceGitSizeQuery(args) {
				cliGit(t, repo, "update-ref", replacementRef, largeReplacement)
				replaced = true
			}
			return result, err
		}
		got, err := readPinnedWorkspaceBlob(workspaceStatusMaxPolicyBytes, gitBytes)
		if err != nil || !bytes.Equal(got, originalPolicy) {
			t.Fatalf("replacement ref changed committed policy read: bytes=%d err=%v", len(got), err)
		}
		if !replaced {
			t.Fatal("test did not update replacement ref between size and content reads")
		}
	})
}

func createWorkspacePolicyReplacement(t *testing.T, repo, parent string, policy []byte) string {
	t.Helper()
	policyPath := filepath.Join(repo, ".polis", "policy.json")
	if err := os.WriteFile(policyPath, policy, 0o600); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", ".polis/policy.json")
	tree := cliGit(t, repo, "write-tree")
	commit, err := gitutil.Bytes(context.Background(), repo, nil, nil,
		"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid",
		"commit-tree", tree, "-p", parent, "-m", "replacement policy")
	if err != nil {
		t.Fatalf("create replacement commit: %v", err)
	}
	cliGit(t, repo, "reset", "--hard", parent)
	return strings.TrimSpace(string(commit))
}

func isWorkspaceGitSizeQuery(args []string) bool {
	return len(args) >= 3 && args[len(args)-3] == "cat-file" && args[len(args)-2] == "-s"
}

func invokeWorkspaceStatusWithOptions(repo, policy, contract, report, format string) (int, string, string) {
	var out, errOut bytes.Buffer
	args := []string{"workspace", "status", "--repo", repo}
	if policy != "" {
		args = append(args, "--policy", policy)
	}
	args = append(args, "--contract", contract, "--report", report, "--format", format)
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func setWorkspaceStatusReportPolicyDigest(t *testing.T, report string, policyRaw []byte) {
	t.Helper()
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(policyRaw)
	value["policy_sha256"] = hex.EncodeToString(digest[:])
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

type workspaceReportCountingReader struct {
	reader *bytes.Reader
	read   int
}

func (r *workspaceReportCountingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.read += n
	return n, err
}

func TestWorkspaceStatusReportReadIsBounded(t *testing.T) {
	reader := &workspaceReportCountingReader{reader: bytes.NewReader(bytes.Repeat([]byte("x"), 64))}
	if _, err := readWorkspaceBoundedBytes(reader, 8); err == nil {
		t.Fatal("oversized stream was accepted")
	}
	if reader.read > 9 {
		t.Fatalf("bounded report reader consumed %d bytes, want at most 9", reader.read)
	}
	if _, err := readWorkspaceBoundedBytes(bytes.NewReader(nil), -1); err == nil {
		t.Fatal("negative size limit was accepted")
	}
	got, err := readWorkspaceBoundedBytes(strings.NewReader("exact"), 5)
	if err != nil || string(got) != "exact" {
		t.Fatalf("exact-size input failed: bytes=%q err=%v", got, err)
	}
}
