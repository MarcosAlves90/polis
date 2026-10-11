package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestRunApplyAppliesBuiltPackage(t *testing.T) {
	repo := makeBuildRepo(t)
	draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, nil)
	contract := filepath.Join(t.TempDir(), "cli-locked-v6.json")
	var startOut, startErr bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--contract", draft, "--out", contract}, &startOut, &startErr); code != exitPass {
		t.Fatalf("start code=%d stderr=%s", code, startErr.String())
	}
	lockedRaw, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	lockedContract, err := spec.DecodeChangeContract(lockedRaw)
	if err != nil {
		t.Fatalf("decode locked schema-v6 contract: %v", err)
	}
	if lockedContract.SchemaVersion != spec.CommitIntentLockedChangeContractSchemaVersion || lockedContract.Commit != nil {
		t.Fatalf("locked contract=%+v; want schema v6 without commit metadata", lockedContract)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var buildOut, buildErr bytes.Buffer
	if code := run([]string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-apply", "--contract", contract, "--defer-gate", "coverage", "--out", outDir}, &buildOut, &buildErr); code != 0 {
		t.Fatalf("build code=%d stderr=%s", code, buildErr.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	artifact := filepath.Join(outDir, entries[0].Name())
	cmd := exec.Command("git", "-C", repo, "restore", "--", "app.txt")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore: %v\n%s", err, b)
	}
	verified := runCLIJSON(t, "verify", "--format", "json", artifact)
	if verified["status"] != "PASS" {
		t.Fatalf("verify result=%v", verified)
	}
	inspection := runCLIJSON(t, "inspect", "--format", "json", artifact)
	if commit, exists := inspection["commit"]; exists && commit != nil {
		t.Fatalf("inspect synthesized commit metadata for schema v6 contract without it: %v", commit)
	}
	head := cliGit(t, repo, "rev-parse", "HEAD")
	apply := runCLIJSON(t, "apply", "--repo", repo, "--format", "json", artifact)
	if apply["committed"] != false || apply["commit_sha"] != nil || apply["commit_message"] != nil {
		t.Fatalf("default apply synthesized commit intent: %v", apply)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("default apply moved HEAD: got %s want %s", got, head)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
}

func TestRunApplyAutoCommitsArtifactIntentEndToEnd(t *testing.T) {
	repo := makeBuildRepo(t)
	message := "feat(apply): commit the validated artifact tree\n\nsecond line with preserved spaces  \n"
	draft := cliDraftContract(t, repo, spec.CommitIntentDraftChangeContractSchemaVersion, &message)
	locked := filepath.Join(t.TempDir(), "cli-locked-v6.json")
	var startOut, startErr bytes.Buffer
	if code := run([]string{"start", "--repo", repo, "--contract", draft, "--out", locked}, &startOut, &startErr); code != exitPass {
		t.Fatalf("start code=%d stderr=%s", code, startErr.String())
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	built := runCLIJSON(t, "build", "--repo", repo, "--project", "polis", "--change", "artifact-commit-e2e", "--contract", locked, "--defer-gate", "coverage", "--out", outDir, "--format", "json")
	artifact, ok := built["artifact"].(string)
	if !ok || artifact == "" {
		t.Fatalf("build output has no artifact path: %v", built)
	}
	if output, err := exec.Command("git", "-C", repo, "restore", "--", "app.txt").CombinedOutput(); err != nil {
		t.Fatalf("restore producer worktree: %v\n%s", err, output)
	}
	verified := runCLIJSON(t, "verify", "--format", "json", artifact)
	if verified["status"] != "PASS" {
		t.Fatalf("verify result=%v", verified)
	}
	inspection := runCLIJSON(t, "inspect", "--format", "json", artifact)
	commitIntent, ok := inspection["commit"].(map[string]any)
	if !ok || commitIntent["message"] != message {
		t.Fatalf("inspect commit intent=%v want exact message %q", inspection["commit"], message)
	}
	if output, err := exec.Command("git", "-C", repo, "config", "user.name", "POLIS CLI Consumer").CombinedOutput(); err != nil {
		t.Fatalf("configure consumer name: %v\n%s", err, output)
	}
	if output, err := exec.Command("git", "-C", repo, "config", "user.email", "polis-cli@example.invalid").CombinedOutput(); err != nil {
		t.Fatalf("configure consumer email: %v\n%s", err, output)
	}
	parent := cliGit(t, repo, "rev-parse", "HEAD")
	targetTree, ok := built["target_tree"].(string)
	if !ok || targetTree == "" {
		t.Fatalf("build output has no target tree: %v", built)
	}
	apply := runCLIJSON(t, "apply", "--repo", repo, "--commit-mode", "auto", "--format", "json", artifact)
	commitSHA, ok := apply["commit_sha"].(string)
	if !ok || commitSHA == "" || apply["committed"] != true || apply["commit_message"] != message {
		t.Fatalf("apply commit result=%v", apply)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD"); got != commitSHA {
		t.Fatalf("HEAD=%s want commit_sha %s", got, commitSHA)
	}
	if got := cliGit(t, repo, "show", "-s", "--format=%P", "HEAD"); got != parent {
		t.Fatalf("commit parent=%s want validated consumer HEAD %s", got, parent)
	}
	if got := cliGit(t, repo, "rev-parse", "HEAD^{tree}"); got != targetTree {
		t.Fatalf("commit tree=%s want validated target tree %s", got, targetTree)
	}
	commitObject, err := exec.Command("git", "-C", repo, "cat-file", "commit", "HEAD").Output()
	if err != nil {
		t.Fatalf("read created commit: %v", err)
	}
	separator := bytes.Index(commitObject, []byte("\n\n"))
	if separator < 0 || !bytes.Equal(commitObject[separator+2:], []byte(message)) {
		t.Fatalf("commit message bytes=%q want exact %q", commitObject[separator+2:], message)
	}
	if status := cliGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("successful auto commit left worktree changes: %q", status)
	}
}

func TestRunApplyRequiresArtifact(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"apply"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestRunConsumerBaselineModes(t *testing.T) {
	repo, built := buildV6CLIArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "consumer.txt"), []byte("consumer-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "consumer.txt"},
		{"-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "consumer unrelated change"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	var strictOut, strictErr bytes.Buffer
	if code := run([]string{"preflight", "--repo", repo, built.Path}, &strictOut, &strictErr); code != exitBaselineMismatch {
		t.Fatalf("strict code=%d stdout=%s stderr=%s", code, strictOut.String(), strictErr.String())
	}

	payload := runCLIJSON(t, "preflight", "--repo", repo, "--baseline-mode", "compatible", "--format", "json", built.Path)
	assertJSONFields(t, payload, map[string]string{"status": "PASS", "baseline_mode": "compatible", "baseline_source": "local", "baseline_ancestry": "proven_descendant"})
	if payload["consumer_base_commit"] == "" || payload["baseline_compatibility"] == "" {
		t.Fatalf("missing compatibility details: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || active {
		t.Fatalf("compatible preflight override_active=%v", payload["override_active"])
	}

	permissivePayload := runCLIJSON(t, "preflight", "--repo", repo, "--baseline-mode", "permissive", "--allow-missing-baseline-proof", "--format", "json", built.Path)
	assertJSONFields(t, permissivePayload, map[string]string{"status": "PASS", "baseline_mode": "permissive", "baseline_source": "local", "baseline_ancestry": "proven_descendant"})
	if active, ok := permissivePayload["override_active"].(bool); !ok || active {
		t.Fatalf("override should not activate while local proof exists: %v", permissivePayload)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"apply", "--repo", repo, "--baseline-mode", "compatible", built.Path}, &out, &errOut); code != exitPass {
		t.Fatalf("compatible apply code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	for _, fragment := range []string{"POLIS APPLY: PASS", "Baseline mode: compatible", "Consumer base:", "Baseline compatibility:"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("compatible output missing %q: %s", fragment, out.String())
		}
	}
	assertFileContents(t, filepath.Join(repo, "app.txt"), "changed\n")
}

func TestRunRejectsMissingBaselineOverrideOutsidePermissiveMode(t *testing.T) {
	for _, command := range []string{"preflight", "apply"} {
		for _, mode := range []string{"strict", "compatible"} {
			var out, errOut bytes.Buffer
			code := run([]string{command, "--baseline-mode", mode, "--allow-missing-baseline-proof", "x.polis"}, &out, &errOut)
			if code != exitUsage {
				t.Fatalf("command=%s mode=%s code=%d stdout=%s stderr=%s", command, mode, code, out.String(), errOut.String())
			}
			if !strings.Contains(errOut.String(), "requires --baseline-mode permissive") {
				t.Fatalf("command=%s mode=%s stderr=%q", command, mode, errOut.String())
			}
		}
	}
}

func TestBaselineReportingIncludesStrictMode(t *testing.T) {
	result := packageapply.Result{
		BaselineMode:          packageapply.BaselineModeStrict,
		BaselineSource:        packageapply.BaselineSourceLocal,
		BaselineAncestry:      packageapply.BaselineAncestryExact,
		ConsumerBaseCommit:    "consumer",
		BaselineCompatibility: "accepted exact artifact baseline",
	}
	payload := map[string]any{}
	addBaselineResultFields(payload, result)
	if payload["baseline_mode"] != packageapply.BaselineModeStrict || payload["baseline_source"] != packageapply.BaselineSourceLocal || payload["baseline_ancestry"] != packageapply.BaselineAncestryExact {
		t.Fatalf("strict baseline fields missing or incorrect: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || active {
		t.Fatalf("strict override_active=%v", payload["override_active"])
	}

	var out bytes.Buffer
	writeBaselineResultText(&out, result)
	for _, fragment := range []string{"Baseline mode: strict", "Baseline source: local", "Baseline ancestry: exact"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("strict text output missing %q: %s", fragment, out.String())
		}
	}
}

func TestBaselineOverrideReportingIsExplicit(t *testing.T) {
	result := packageapply.Result{
		BaselineMode:          packageapply.BaselineModePermissive,
		BaselineSource:        packageapply.BaselineSourceOverridden,
		BaselineAncestry:      packageapply.BaselineAncestryUnproven,
		ConsumerBaseCommit:    "consumer",
		BaselineCompatibility: "accepted with reduced baseline proof",
		OverrideActive:        true,
		BypassedGuarantees:    []string{"locked baseline behavior replay", "Red proof reconstruction"},
		Warnings:              []string{"locked producer baseline proof is unavailable and explicitly waived"},
	}
	payload := map[string]any{}
	addBaselineResultFields(payload, result)
	if payload["baseline_mode"] != packageapply.BaselineModePermissive || payload["baseline_source"] != packageapply.BaselineSourceOverridden || payload["baseline_ancestry"] != packageapply.BaselineAncestryUnproven {
		t.Fatalf("unexpected baseline fields: %v", payload)
	}
	if active, ok := payload["override_active"].(bool); !ok || !active {
		t.Fatalf("override_active=%v", payload["override_active"])
	}
	bypassed, ok := payload["bypassed_guarantees"].([]string)
	if !ok || len(bypassed) != 2 {
		t.Fatalf("bypassed_guarantees=%T %v", payload["bypassed_guarantees"], payload["bypassed_guarantees"])
	}

	var out bytes.Buffer
	writeBaselineResultText(&out, result)
	for _, fragment := range []string{"Baseline source: overridden", "Baseline ancestry: unproven", "WARNING: USER SAFETY OVERRIDE ACTIVE", "Bypassed guarantee: locked baseline behavior replay", "Warning: locked producer baseline proof is unavailable"} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("text output missing %q: %s", fragment, out.String())
		}
	}
}

func TestRunRejectsInvalidBaselineMode(t *testing.T) {
	for _, command := range []string{"preflight", "apply"} {
		var out, errOut bytes.Buffer
		if code := run([]string{command, "--baseline-mode", "force", "x.polis"}, &out, &errOut); code != exitUsage {
			t.Fatalf("command=%s code=%d stdout=%s stderr=%s", command, code, out.String(), errOut.String())
		}
		if !strings.Contains(errOut.String(), "strict, compatible, or permissive") {
			t.Fatalf("command=%s stderr=%q", command, errOut.String())
		}
	}
}
