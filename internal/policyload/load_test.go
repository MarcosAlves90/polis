package policyload

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func validPolicy(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		case "coverage":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	raw, err := json.MarshalIndent(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func TestLoadExternalCanonicalizesValidPolicyOutsideRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policyPath, validPolicy(t), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, policy, err := LoadExternal(repo, policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		t.Fatalf("schema=%d", policy.SchemaVersion)
	}
	var compact []byte
	compact, err = json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	compact = append(compact, '\n')
	if string(raw) != string(compact) {
		t.Fatalf("canonical bytes mismatch\ngot=%q\nwant=%q", raw, compact)
	}
}

func TestLoadExternalRejectsPolicyInsideRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(repo, "policy.json")
	if err := os.WriteFile(policyPath, validPolicy(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadExternal(repo, policyPath); err == nil {
		t.Fatal("inside-repo policy accepted")
	}
}

func TestLoadCommittedPreservesExactCommittedBytes(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := validPolicy(t)
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "init", "-q")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	cmd = exec.Command("git", "-C", repo, "add", ".")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, b)
	}
	cmd = exec.Command("git", "-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, b)
	}

	got, _, err := LoadCommitted(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("committed bytes changed")
	}
}

func pr48Policy(t *testing.T, mutate func(*spec.Policy)) []byte {
	t.Helper()
	var policy spec.Policy
	if err := json.Unmarshal(validPolicy(t), &policy); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&policy)
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func pr48InvalidPolicies(t *testing.T) map[string]struct {
	raw  []byte
	want string
} {
	t.Helper()
	missingEnvironment := pr48Policy(t, func(policy *spec.Policy) {
		for i := range policy.Gates {
			if policy.Gates[i].ID == "test.complete" {
				policy.Gates[i].Command.Environment = nil
			}
		}
	})
	staleNotApplicable := pr48Policy(t, func(policy *spec.Policy) {
		reason := "coverage disabled for PR #48 fixture"
		for i := range policy.Gates {
			if policy.Gates[i].ID == "coverage" {
				policy.Gates[i].Mode = spec.GateModeNotApplicable
				policy.Gates[i].Reason = &reason
			}
		}
	})
	return map[string]struct {
		raw  []byte
		want string
	}{
		"missing explicit environment": {
			raw:  missingEnvironment,
			want: "requires explicit command environment",
		},
		"not_applicable with stale fields": {
			raw:  staleNotApplicable,
			want: "not_applicable gate must contain exactly",
		},
	}
}

func pr48CommittedRepo(t *testing.T, raw []byte) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", ".polis/policy.json"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "invalid policy"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	return repo
}

func TestIssue44PR48LoadExternalRejectsInvalidPolicies(t *testing.T) {
	repo := t.TempDir()
	for name, testCase := range pr48InvalidPolicies(t) {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, testCase.raw, 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := LoadExternal(repo, path)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("invalid policy accepted: err=%v want=%q", err, testCase.want)
			}
		})
	}
}

func TestIssue44PR48LoadCommittedRejectsInvalidPolicies(t *testing.T) {
	for name, testCase := range pr48InvalidPolicies(t) {
		t.Run(name, func(t *testing.T) {
			repo := pr48CommittedRepo(t, testCase.raw)
			_, _, err := LoadCommitted(context.Background(), repo)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("invalid committed policy accepted: err=%v want=%q", err, testCase.want)
			}
		})
	}
}
