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
