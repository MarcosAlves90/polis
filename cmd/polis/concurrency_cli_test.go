package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func concurrencyCLIRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/concurrency\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cliGit(t, repo, "add", "go.mod")
	cliGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func externalParallelCLIPolicy(t *testing.T) string {
	t.Helper()
	var policy spec.Policy
	if err := json.Unmarshal(canonicalPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	for i := range policy.Gates {
		if policy.Gates[i].ID != "lint" && policy.Gates[i].ID != "dependency" {
			continue
		}
		policy.Gates[i].Mode = spec.GateModeCommand
		policy.Gates[i].Reason = nil
		policy.Gates[i].ParallelSafe = true
		policy.Gates[i].Command = &spec.CommandSpec{Argv: cliFixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "parallel-policy-v3.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParallelSafeInitCLIRequiresExplicitReviewedGates(t *testing.T) {
	repo := concurrencyCLIRepo(t)
	var out, errOut bytes.Buffer
	args := []string{"init", "--repo", repo, "--profile", "go", "--dry-run", "--parallel-safe", "lint", "--parallel-safe", "dependency"}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	var policy spec.Policy
	if err := json.Unmarshal(out.Bytes(), &policy); err != nil {
		t.Fatalf("init policy JSON: %v\n%s", err, out.String())
	}
	for _, gate := range policy.Gates {
		want := gate.ID == "lint" || gate.ID == "dependency"
		if gate.ParallelSafe != want {
			t.Fatalf("gate %s parallel_safe=%t want %t", gate.ID, gate.ParallelSafe, want)
		}
	}

	for name, extra := range map[string][]string{
		"unknown":   {"not-a-gate"},
		"coverage":  {"coverage"},
		"duplicate": {"lint", "lint"},
		"disabled":  {"lint"},
	} {
		t.Run(name, func(t *testing.T) {
			args := []string{"init", "--repo", repo, "--profile", "go", "--dry-run", "--parallel-safe"}
			if name == "disabled" {
				args = append([]string{"init", "--repo", repo, "--profile", "go", "--dry-run", "--disable-gate", "lint", "--parallel-safe"}, extra...)
			} else {
				args = append(args, extra...)
			}
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "parallel-safe") {
				t.Fatalf("args=%v code=%d stderr=%s", args, code, errOut.String())
			}
		})
	}
}

func TestConcurrencyPlanCLITextAndJSONAgree(t *testing.T) {
	repo := makeBuildRepo(t)
	policy := externalParallelCLIPolicy(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"plan", "--repo", repo, "--policy", policy, "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("json code=%d stderr=%s", code, errOut.String())
	}
	var plan policyplan.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	eligible := map[string]string{}
	for _, gate := range plan.Concurrency.EligibleGates {
		eligible[gate.Gate] = gate.Reason
	}
	for _, id := range []string{"lint", "dependency"} {
		if eligible[id] != spec.ConcurrencyDeclaredParallelSafe {
			t.Fatalf("eligible=%+v", plan.Concurrency.EligibleGates)
		}
	}
	foundGroup := false
	for _, group := range plan.Concurrency.OverlapGroups {
		if strings.Join(group.Gates, ",") == "dependency,lint" {
			foundGroup = true
		}
	}
	if !foundGroup {
		t.Fatalf("overlap groups=%+v", plan.Concurrency.OverlapGroups)
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"plan", "--repo", repo, "--policy", policy}, &out, &errOut); code != exitPass {
		t.Fatalf("text code=%d stderr=%s", code, errOut.String())
	}
	for _, fragment := range []string{
		"Concurrency (declared only; no gates were run):",
		"lint (declared_parallel_safe)", "dependency (declared_parallel_safe)",
		"Overlap group: dependency, lint reason=declared_parallel_safe",
		"coverage (coverage_report_exclusive)",
		"test.complete (parallel_safe_not_declared)",
	} {
		if !strings.Contains(out.String(), fragment) {
			t.Fatalf("text plan missing %q: %s", fragment, out.String())
		}
	}
}

func TestParallelGatesCLIReportsConcurrencyWithoutChangingOutcomes(t *testing.T) {
	repo := makeBuildRepo(t)
	policy := externalParallelCLIPolicy(t)
	baseArgs := []string{"gates", "--repo", repo, "--policy", policy, "--gate", "lint", "--gate", "dependency", "--jobs", "2"}

	var jsonOut, errOut bytes.Buffer
	if code := run(append(append([]string{}, baseArgs...), "--format", "json"), &jsonOut, &errOut); code != exitPass {
		t.Fatalf("json code=%d stderr=%s stdout=%s", code, errOut.String(), jsonOut.String())
	}
	var report gatesCLIResult
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != spec.StatusPass || report.Concurrency.Jobs != 2 || !report.Concurrency.Diagnosis.CanOverlap {
		t.Fatalf("gates report status=%s concurrency=%+v", report.Status, report.Concurrency)
	}
	if got := strings.Join(report.Concurrency.Diagnosis.EligibleGates, ","); got != "lint,dependency" {
		t.Fatalf("eligible gates=%q", got)
	}
	if report.Concurrency.Observation.OverlapObserved && report.Concurrency.Observation.MaxConcurrent < 2 {
		t.Fatalf("inconsistent observation=%+v", report.Concurrency.Observation)
	}

	var textOut, textErr bytes.Buffer
	if code := run(baseArgs, &textOut, &textErr); code != exitPass {
		t.Fatalf("text code=%d stderr=%s", code, textErr.String())
	}
	for _, fragment := range []string{
		"Concurrency: jobs=2", "overlap_observed=", "max_concurrent=",
		"Concurrency eligible gates: lint, dependency", "Concurrency blockers: none",
	} {
		if !strings.Contains(textOut.String(), fragment) {
			t.Fatalf("gates text missing %q: %s", fragment, textOut.String())
		}
	}
}
