package policyinit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v5/spec"
)

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func goRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/initfixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "go.mod")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func TestInitAutoCreatesCanonicalGoPolicyWithoutTouchingIndex(t *testing.T) {
	repo := goRepo(t)
	beforeHead := runGit(t, repo, "rev-parse", "HEAD")
	beforeIndex := runGit(t, repo, "write-tree")
	result, err := Init(context.Background(), Options{Repo: repo, Profile: "auto"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Profile != "go" {
		t.Fatalf("profile=%q", result.Profile)
	}
	raw, err := os.ReadFile(result.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatalf("generated policy invalid: %v", err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		t.Fatalf("schema_version=%d want %d", policy.SchemaVersion, spec.PolicySchemaVersion)
	}
	for _, gate := range policy.Gates {
		if gate.Command != nil && gate.Command.Environment == nil {
			t.Fatalf("gate %q command missing explicit environment", gate.ID)
		}
	}
	if policy.Gates[1].Mode != spec.GateModeCoverage || policy.Gates[1].Adapter != spec.CoverageAdapterGoCoverProfileV1 {
		t.Fatalf("coverage gate=%+v", policy.Gates[1])
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed")
	}
	if got := runGit(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed")
	}
	status := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if status != "?? .polis/policy.json" {
		t.Fatalf("status=%q", status)
	}
}

func TestInitRefusesOverwrite(t *testing.T) {
	repo := goRepo(t)
	first, err := Init(context.Background(), Options{Repo: repo, Profile: "go"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(context.Background(), Options{Repo: repo, Profile: "go"}); err == nil {
		t.Fatal("expected overwrite rejection")
	}
	after, _ := os.ReadFile(first.PolicyPath)
	if string(after) != string(before) {
		t.Fatal("policy bytes changed")
	}
}

func TestInitRejectsUnsupportedAndNonGit(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if _, err := Init(context.Background(), Options{Repo: repo, Profile: "auto"}); err == nil {
		t.Fatal("expected unsupported repository rejection")
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis", "policy.json")); !os.IsNotExist(err) {
		t.Fatalf("policy unexpectedly exists: %v", err)
	}
	if _, err := Init(context.Background(), Options{Repo: t.TempDir(), Profile: "auto"}); err == nil {
		t.Fatal("expected non-git rejection")
	}
}

func TestInitRejectsUnknownProfile(t *testing.T) {
	repo := goRepo(t)
	if _, err := Init(context.Background(), Options{Repo: repo, Profile: "rust"}); err == nil {
		t.Fatal("expected unknown profile rejection")
	}
}

func TestInitDefaultsOptionsInsideGoRepository(t *testing.T) {
	repo := goRepo(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	result, err := Init(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Init defaults: %v", err)
	}
	if result.Profile != "go" {
		t.Fatalf("profile=%q", result.Profile)
	}
}

func TestInitExplicitGoRequiresGoMod(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if _, err := Init(context.Background(), Options{Repo: repo, Profile: "go"}); err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("expected go.mod error, got %v", err)
	}
}

func TestResolveProfileCustomSupportsNonGoRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")

	got, err := resolveProfile(repo, "custom")
	if err != nil {
		t.Fatalf("CUSTOM_INIT_RED: resolveProfile() error = %v", err)
	}
	if got != "custom" {
		t.Fatalf("profile=%q want custom", got)
	}
}

func nonGoRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func customOptions(repo string) Options {
	return Options{
		Repo:            repo,
		Profile:         ProfileCustom,
		TestArgv:        []string{"runner", "test with spaces", "&&"},
		CoverageArgv:    []string{"runner", "coverage with spaces", "&&"},
		CoverageAdapter: spec.CoverageAdapterLCOVV1,
		CoverageReport:  "coverage/lcov.info",
	}
}

func TestInitCustomCreatesCanonicalPolicyAndPreservesLiteralArgv(t *testing.T) {
	repo := nonGoRepo(t)
	result, err := Init(context.Background(), customOptions(repo))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Profile != ProfileCustom {
		t.Fatalf("profile=%q", result.Profile)
	}
	raw, err := os.ReadFile(result.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatalf("generated policy invalid: %v", err)
	}
	if policy.SchemaVersion != 3 || len(policy.Gates) != 11 {
		t.Fatalf("schema=%d gates=%d", policy.SchemaVersion, len(policy.Gates))
	}
	wantTest := []string{"runner", "test with spaces", "&&"}
	if got := policy.Gates[0].Command.Argv; strings.Join(got, "\x00") != strings.Join(wantTest, "\x00") {
		t.Fatalf("test argv=%q want %q", got, wantTest)
	}
	coverage := policy.Gates[1]
	wantCoverage := []string{"runner", "coverage with spaces", "&&"}
	if got := coverage.Command.Argv; strings.Join(got, "\x00") != strings.Join(wantCoverage, "\x00") {
		t.Fatalf("coverage argv=%q want %q", got, wantCoverage)
	}
	if coverage.Adapter != spec.CoverageAdapterLCOVV1 || coverage.Report != "coverage/lcov.info" || coverage.Operator != spec.CoverageOperatorGreaterThan {
		t.Fatalf("coverage metadata=%+v", coverage)
	}
	if coverage.ThresholdPercent == nil || *coverage.ThresholdPercent != 80.0 {
		t.Fatalf("threshold=%v want 80", coverage.ThresholdPercent)
	}
	for _, gate := range policy.Gates[2:] {
		if gate.Mode != spec.GateModeNotApplicable || gate.Reason == nil || strings.TrimSpace(*gate.Reason) == "" {
			t.Fatalf("gate %q = %+v", gate.ID, gate)
		}
	}
}

func TestInitCustomAcceptsExplicitCoverageThreshold(t *testing.T) {
	repo := nonGoRepo(t)
	threshold := 87.5
	opts := customOptions(repo)
	opts.CoverageThreshold = &threshold
	result, err := Init(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(result.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := *policy.Gates[1].ThresholdPercent; got != threshold {
		t.Fatalf("threshold=%v want %v", got, threshold)
	}
}

func TestInitCustomRejectsInvalidInputsWithoutWritingPolicy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{name: "missing test argv", mutate: func(o *Options) { o.TestArgv = nil }},
		{name: "empty test argv element", mutate: func(o *Options) { o.TestArgv = []string{"runner", ""} }},
		{name: "missing coverage argv", mutate: func(o *Options) { o.CoverageArgv = nil }},
		{name: "empty coverage argv element", mutate: func(o *Options) { o.CoverageArgv = []string{"runner", ""} }},
		{name: "invalid adapter", mutate: func(o *Options) { o.CoverageAdapter = "unknown-v1" }},
		{name: "absolute report", mutate: func(o *Options) { o.CoverageReport = "/tmp/lcov.info" }},
		{name: "unnormalized report", mutate: func(o *Options) { o.CoverageReport = "coverage/../lcov.info" }},
		{name: "threshold below minimum", mutate: func(o *Options) { v := 79.9; o.CoverageThreshold = &v }},
		{name: "threshold above maximum", mutate: func(o *Options) { v := 100.1; o.CoverageThreshold = &v }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := nonGoRepo(t)
			opts := customOptions(repo)
			tt.mutate(&opts)
			if _, err := Init(context.Background(), opts); err == nil {
				t.Fatal("expected failure")
			}
			if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
				t.Fatalf(".polis unexpectedly exists: %v", err)
			}
		})
	}
}

func TestInitRejectsCustomOptionsOutsideCustomProfile(t *testing.T) {
	repo := goRepo(t)
	threshold := 90.0
	tests := []Options{
		{Repo: repo, Profile: ProfileGo, TestArgv: []string{"go", "test", "./..."}},
		{Repo: repo, Profile: ProfileGo, CoverageArgv: []string{"go", "test", "./..."}},
		{Repo: repo, Profile: ProfileGo, CoverageAdapter: spec.CoverageAdapterGoCoverProfileV1},
		{Repo: repo, Profile: ProfileGo, CoverageReport: ".polis/coverage.out"},
		{Repo: repo, Profile: ProfileGo, CoverageThreshold: &threshold},
		{Repo: repo, Profile: ProfileAuto, TestArgv: []string{"go", "test", "./..."}},
	}
	for i, opts := range tests {
		if _, err := Init(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--profile custom") {
			t.Fatalf("case %d error=%v", i, err)
		}
	}
}

func TestInitAutoUnsupportedRecommendsCustomProfile(t *testing.T) {
	repo := nonGoRepo(t)
	if _, err := Init(context.Background(), Options{Repo: repo, Profile: ProfileAuto}); err == nil || !strings.Contains(err.Error(), "--profile custom") {
		t.Fatalf("error=%v", err)
	}
}

func TestInitDryRunReturnsValidPolicyWithoutMutation(t *testing.T) {
	repo := nonGoRepo(t)
	beforeHead := runGit(t, repo, "rev-parse", "HEAD")
	beforeIndex := runGit(t, repo, "write-tree")
	beforeStatus := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	opts := customOptions(repo)
	opts.DryRun = true
	result, err := Init(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.DecodePolicy(result.Policy); err != nil {
		t.Fatalf("dry-run policy invalid: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .polis: %v", err)
	}
	if got := runGit(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s", got)
	}
	if got := runGit(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s", got)
	}
	if got := runGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("worktree changed: %q", got)
	}
}

func TestInitDryRunDoesNotOverwriteExistingPolicy(t *testing.T) {
	repo := nonGoRepo(t)
	policyDir := filepath.Join(repo, ".polis")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(policyDir, "policy.json")
	original := []byte("existing-policy-bytes\n")
	if err := os.WriteFile(policyPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := customOptions(repo)
	opts.DryRun = true
	result, err := Init(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.DecodePolicy(result.Policy); err != nil {
		t.Fatalf("dry-run policy invalid: %v", err)
	}
	after, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("existing policy changed: %q", after)
	}
}
