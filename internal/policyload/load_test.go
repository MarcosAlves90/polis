package policyload

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
