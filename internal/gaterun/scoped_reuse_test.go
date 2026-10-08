package gaterun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Scoped reuse is an explicit policy assertion, not implied by input_paths.
func TestScopedReuseUnrelatedChangesAndFailClosed(t *testing.T) {
	repo, policy, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(repo, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "backend", "main.go"), "backend v1")
	mustWrite(t, filepath.Join(repo, "frontend.txt"), "frontend v1")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "inputs")
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"backend"}
			policy.Gates[i].InputPathsComplete = true
		}
	}
	plan := compiled(t, policy)
	prior := filepath.Join(t.TempDir(), "prior.json")
	options := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: prior}
	first := runOK(t, repo, plan, options)
	if priorGate(&first, "lint").Action != "executed" {
		t.Fatal("first run did not execute")
	}
	options.Out = ""
	options.Reuse = prior
	mustWrite(t, filepath.Join(repo, "frontend.txt"), "frontend v2")
	next := runOK(t, repo, plan, options)
	if next.Inputs.SourceSHA256 == first.Inputs.SourceSHA256 || priorGate(&next, "lint").Action != "reused" {
		t.Fatalf("disjoint edit must reuse: %+v", priorGate(&next, "lint"))
	}
	git(t, repo, "add", "frontend.txt")
	git(t, repo, "commit", "-qm", "frontend only")
	committed := runOK(t, repo, plan, options)
	if got := priorGate(&committed, "lint"); got.Action != "reused" {
		t.Fatalf("disjoint commit must reuse: %+v", got)
	}
	mustWrite(t, filepath.Join(repo, "backend", "main.go"), "backend v2")
	invalid := runOK(t, repo, plan, options)
	if got := priorGate(&invalid, "lint"); got.Action != "executed" || !slices.Contains(got.StaleCategories, "source") {
		t.Fatalf("in-scope mutation reused: %+v", got)
	}
}

func TestScopedReuseRequiresExplicitCompleteness(t *testing.T) {
	repo, policy, _ := fixture(t)
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"source.txt"}
		}
	}
	plan := compiled(t, policy)
	prior := filepath.Join(t.TempDir(), "prior.json")
	options := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: prior}
	runOK(t, repo, plan, options)
	options.Out = ""
	options.Reuse = prior
	mustWrite(t, filepath.Join(repo, "unrelated.txt"), "unrelated")
	second := runOK(t, repo, plan, options)
	if gate := priorGate(&second, "lint"); gate.Action != "executed" || !slices.Contains(gate.StaleCategories, "source") {
		t.Fatalf("implicit scoped reuse: %+v", gate)
	}
}

func TestScopedReusePolicyRequiresInputs(t *testing.T) {
	_, policy, _ := fixture(t)
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPathsComplete = true
		}
	}
	if err := policy.Validate(); err == nil {
		t.Fatal("scope completeness accepted without input paths")
	}
}

func TestScopedReuseInputMetadataAndEntries(t *testing.T) {
	repo, policy, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(repo, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "backend", "a.txt"), "same")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "backend")
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"backend"}
			policy.Gates[i].InputPathsComplete = true
		}
	}
	plan := compiled(t, policy)
	prior := filepath.Join(t.TempDir(), "prior.json")
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: prior}
	runOK(t, repo, plan, opts)
	opts.Out = ""
	opts.Reuse = prior
	assertions := []struct {
		name   string
		change func()
	}{
		{"chmod", func() {
			if err := os.Chmod(filepath.Join(repo, "backend", "a.txt"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"new file", func() { mustWrite(t, filepath.Join(repo, "backend", "new.txt"), "new") }},
		{"staged", func() {
			mustWrite(t, filepath.Join(repo, "backend", "a.txt"), "staged bytes")
			git(t, repo, "add", "backend/a.txt")
			mustWrite(t, filepath.Join(repo, "backend", "a.txt"), "same")
		}},
	}
	for _, tc := range assertions {
		t.Run(tc.name, func(t *testing.T) {
			// Every scenario starts from original Git state, but not from a new baseline.
			git(t, repo, "reset", "-q", "--hard", "HEAD")
			_ = os.Remove(filepath.Join(repo, "backend", "new.txt"))
			tc.change()
			next := runOK(t, repo, plan, opts)
			got := priorGate(&next, "lint")
			if got.Action != "executed" || !slices.Contains(got.StaleCategories, "source") {
				t.Fatalf("scoped metadata change incorrectly reused: %+v", got)
			}
		})
	}
}

func TestScopedReuseDoesNotEscapeInputDirectory(t *testing.T) {
	repo, policy, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(repo, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	mustWrite(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(repo, "backend", "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"backend"}
			policy.Gates[i].InputPathsComplete = true
		}
	}
	plan := compiled(t, policy)
	_, _, err := Run(context.Background(), repo, plan, Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}})
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escaping input should be rejected: %v", err)
	}
}

// Property-style deterministic variation: a disjoint path has no influence on
// the scoped checksum, while every change to a scoped file does.
func TestScopedSnapshotDisjointPathProperty(t *testing.T) {
	repo, _, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(repo, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	scoped := filepath.Join(repo, "backend", "file.txt")
	outside := filepath.Join(repo, "frontend.txt")
	mustWrite(t, scoped, "original")
	first, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		mustWrite(t, outside, fmt.Sprintf("frontend value %d", i))
		unchanged, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
		if err != nil || unchanged != first {
			t.Fatalf("unrelated value %d changed hash: %v", i, err)
		}
		mustWrite(t, scoped, fmt.Sprintf("backend value %d", i))
		changed, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
		if err != nil || changed == first {
			t.Fatalf("input value %d reused hash: %v", i, err)
		}
		mustWrite(t, scoped, "original")
		restored, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
		if err != nil || restored != first {
			t.Fatalf("restoration %d did not restore hash: %v", i, err)
		}
	}
}

func TestScopedSnapshotIncludesIgnoredInputs(t *testing.T) {
	repo, _, _ := fixture(t)
	if err := os.Mkdir(filepath.Join(repo, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, ".gitignore"), "backend/cache.txt\n")
	mustWrite(t, filepath.Join(repo, "backend", "cache.txt"), "v1")
	first, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "backend", "cache.txt"), "v2")
	after, err := scopedSnapshot(context.Background(), repo, []string{"backend"})
	if err != nil || first == after {
		t.Fatalf("ignored input mutation did not invalidate scope: %v", err)
	}
}

func TestScopedReuseRejectsLegacyGlobalIdentityAndEnvironmentDrift(t *testing.T) {
	repo, policy, _ := fixture(t)
	policy.Gates[2].InputPaths = []string{"source.txt"}
	plan := compiled(t, policy)
	oldPath := filepath.Join(t.TempDir(), "old.json")
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: oldPath}
	runOK(t, repo, plan, opts)
	policy.Gates[2].InputPathsComplete = true
	plan = compiled(t, policy)
	opts.Out = ""
	opts.Reuse = oldPath
	current := runOK(t, repo, plan, opts)
	if g := priorGate(&current, "lint"); g.Action != "executed" || !slices.Contains(g.StaleCategories, "identity_missing_or_invalid") && !slices.Contains(g.StaleCategories, "policy") {
		t.Fatalf("legacy/global identity was incorrectly reused: %+v", g)
	}
	opts.Reuse = ""
	opts.Out = filepath.Join(t.TempDir(), "scoped.json")
	runOK(t, repo, plan, opts)
	opts.Reuse = opts.Out
	opts.Out = ""
	opts.EnvironmentID = "env-v2"
	drift := runOK(t, repo, plan, opts)
	if g := priorGate(&drift, "lint"); g.Action != "executed" || !slices.Contains(g.StaleCategories, "environment") {
		t.Fatalf("environment drift reused: %+v", g)
	}
}

func TestScopedReuseRootInputFallsBackToGlobal(t *testing.T) {
	repo, policy, _ := fixture(t)
	for i := range policy.Gates {
		if policy.Gates[i].ID == "lint" {
			policy.Gates[i].InputPaths = []string{"."}
			policy.Gates[i].InputPathsComplete = true
		}
	}
	plan := compiled(t, policy)
	prior := filepath.Join(t.TempDir(), "prior.json")
	opts := Options{Version: "test", EnvironmentID: "env-v1", Selected: []string{"lint"}, Out: prior}
	runOK(t, repo, plan, opts)
	opts.Out = ""
	opts.Reuse = prior
	mustWrite(t, filepath.Join(repo, "unrelated.txt"), "changed")
	second := runOK(t, repo, plan, opts)
	gate := priorGate(&second, "lint")
	if gate.Action != "executed" || !slices.Contains(gate.StaleCategories, "source") {
		t.Fatalf("root input must retain global fallback: %+v", gate)
	}
}
