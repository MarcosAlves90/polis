package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestRunVerifyPassesCanonicalPackage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"verify", makeValidPackage(t)}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS VERIFY: PASS") {
		t.Fatalf("stdout=%q", out.String())
	}
}

func TestRunVerifyRejectsInvalidPackage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.polis")
	if err := os.WriteFile(p, []byte("not zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"verify", p}, &out, &errOut)
	if code != exitInvalidArtifact {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "POLIS VERIFY: FAIL") {
		t.Fatalf("stderr=%q", errOut.String())
	}
}

func TestRunBuildCreatesPackage(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded spec.Policy
	if err := json.Unmarshal(policyRaw, &decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	if err := os.WriteFile(policyPath, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	contract := lockedCLIContract(t, repo, policyPath)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	code := run([]string{"build", "--repo", repo, "--policy", policyPath, "--project", "gitrex", "--change", "cli-build", "--contract", contract, "--defer-gate", "coverage", "--jobs", "2", "--out", outDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "POLIS BUILD: PASS") || !strings.Contains(out.String(), "Deferred gates: coverage") || !strings.Contains(out.String(), "Consumer validation required: true") || !strings.Contains(out.String(), "DEFERRED") || !strings.Contains(out.String(), "Retained artifacts: .polis/artifacts/") {
		t.Fatalf("stdout=%q", out.String())
	}
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".polis") {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}

func TestRunBuildDeferralTextAndJSONReporting(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	args := []string{"build", "--repo", repo, "--project", "gitrex", "--change", "cli-defer", "--contract", contract, "--defer-gate", "coverage", "--format", "json", "--out", outDir}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("json code=%d stderr=%s", code, errOut.String())
	}
	var result struct {
		DeferredGates              []string          `json:"deferred_gates"`
		ConsumerValidationRequired bool              `json:"consumer_validation_required"`
		ProducerGateStatuses       map[string]string `json:"producer_gate_statuses"`
		RetainedPaths              []string          `json:"retained_paths"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid build JSON: %v\n%s", err, out.String())
	}
	if !result.ConsumerValidationRequired || len(result.DeferredGates) != 1 || result.DeferredGates[0] != "coverage" || result.ProducerGateStatuses["coverage"] != string(spec.StatusDeferred) || len(result.RetainedPaths) != 2 {
		t.Fatalf("build output=%+v", result)
	}
	for _, path := range result.RetainedPaths {
		if strings.HasPrefix(path, ".polis/artifacts/packages/") {
			t.Errorf("delivery package was retained in repository: %q", path)
		}
		if !strings.HasPrefix(path, ".polis/artifacts/") {
			t.Errorf("unexpected retained path %q", path)
		}
	}
}

func TestRunBuildRejectsInvalidJobs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"build", "--jobs", "17"}, &out, &errOut); code != exitUsage {
		t.Fatalf("invalid jobs code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "jobs must be between 1 and 16") {
		t.Fatalf("invalid jobs diagnostic missing: %q", errOut.String())
	}
}

func TestRunBuildRequiresFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"build"}, &out, &errOut); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
}

func TestInspectionTextIncludesTraceability(t *testing.T) {
	inspection := packageverify.Inspection{
		Project: "polis", Change: "strict", FormatVersion: 3, PolicySchemaVersion: 3, ValidationLevel: spec.ValidationLevelStrict, ChangeContractSchemaVersion: 3,
		Kind: spec.ChangeKindFeature, BaseCommit: "base", TargetTree: "target", AllowedPaths: []string{"spec/"}, EvidenceEvents: 1,
		Traceability: []spec.TraceabilityLink{{RequirementID: "REQ-001", AcceptanceCriterionID: "AC-001", Proof: spec.ProofGateRegression}},
	}
	var out bytes.Buffer
	writeInspectionText(&out, inspection)
	if !strings.Contains(out.String(), "Validation level: strict") || !strings.Contains(out.String(), "Trace: REQ-001 -> AC-001 -> regression") {
		t.Fatalf("stdout=%q", out.String())
	}
}
