package retentioncompleteness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type completionFixture struct {
	ctx         context.Context
	repo        string
	retention   artifactretention.State
	contractRaw []byte
	contract    spec.ChangeContract
	policyRaw   []byte
	policy      spec.Policy
	targetTree  string
	planRaw     []byte
	patch       []byte
	evidence    []byte
	statuses    map[string]spec.Status
	summary     spec.ValidationSummary
}

func newCompletionFixture(t *testing.T) completionFixture {
	t.Helper()
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompletionFile(t, filepath.Join(repo, artifactretention.ManifestPath), []byte("{\"schema_version\":1,\"mode\":\"repository\"}\n"))
	writeCompletionFile(t, filepath.Join(repo, ".polis", "policy.json"), completionPolicyBytes(t))
	writeCompletionFile(t, filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644)
	runCompletionGit(t, repo, "init", "-q")
	runCompletionGit(t, repo, "config", "user.name", "POLIS Test")
	runCompletionGit(t, repo, "config", "user.email", "polis@example.invalid")
	runCompletionGit(t, repo, "add", "-A")
	runCompletionGit(t, repo, "commit", "-q", "-m", "baseline")

	draft := filepath.Join(t.TempDir(), "draft-v5.json")
	writeCompletionFile(t, draft, completionDraftContractBytes(t))
	locked, err := devstart.Start(ctx, devstart.Options{
		Repo: repo, Contract: draft, Out: filepath.Join(t.TempDir(), "locked-v6.json"),
	})
	if err != nil {
		t.Fatalf("lock contract: %v", err)
	}
	contractRaw, err := os.ReadFile(locked.Path)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := spec.DecodePolicy(policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	retention, err := artifactretention.Load(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !retention.RepositoryEnabled() {
		t.Fatal("fixture retention mode is not repository")
	}
	targetTree, err := TargetTree(ctx, repo, contract.BaselineLock.BaseCommit)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := policyplan.Compile(policy)
	if err != nil {
		t.Fatal(err)
	}
	gateOrder, err := implementationplan.EffectiveExecutionOrder(compiled)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := implementationplan.Generate(contract, contractRaw, gateOrder)
	if err != nil {
		t.Fatal(err)
	}
	planRaw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	summary := policy.ValidationSummary()
	statuses := make(map[string]spec.Status, len(spec.ProjectGateOrder))
	for _, gate := range spec.ProjectGateOrder {
		status := spec.StatusNotApplicable
		if containsCompletionGate(summary.EnabledGates, gate) {
			status = spec.StatusPass
		}
		statuses[gate] = status
	}
	return completionFixture{
		ctx: ctx, repo: repo, retention: retention,
		contractRaw: contractRaw, contract: contract,
		policyRaw: policyRaw, policy: policy,
		targetTree: targetTree, planRaw: planRaw,
		patch:    []byte("diff --git a/app.txt b/app.txt\n"),
		evidence: completionEvidenceBytes(t, summary, statuses, nil),
		statuses: statuses, summary: summary,
	}
}

func (fixture completionFixture) inputs(operation string) Inputs {
	return Inputs{
		Operation:             operation,
		ContractRaw:           fixture.contractRaw,
		Contract:              fixture.contract,
		PolicyRaw:             fixture.policyRaw,
		Policy:                fixture.policy,
		ImplementationPlanRaw: fixture.planRaw,
		RegressionPatch:       fixture.patch,
		Evidence:              fixture.evidence,
		BaseCommit:            fixture.contract.BaselineLock.BaseCommit,
		TargetTree:            fixture.targetTree,
		ValidationLevel:       fixture.summary.Level,
		EnabledGates:          fixture.summary.EnabledGates,
		DisabledGates:         fixture.summary.DisabledGates,
		DeferredGates:         []string{},
		ProducerGateStatuses:  fixture.statuses,
	}
}

func TestCompletePublishesAndVerifiesRetainedCompletion(t *testing.T) {
	fixture := newCompletionFixture(t)
	headBefore := runCompletionGit(t, fixture.repo, "rev-parse", "HEAD")
	indexBefore := runCompletionGit(t, fixture.repo, "write-tree")
	refsBefore := runCompletionGit(t, fixture.repo, "for-each-ref", "--format=%(refname) %(objectname)")

	operation := spec.RetainedValidationOperationWorkspace
	outcome, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(operation), nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if outcome.Record.Operation != operation || outcome.Record.TargetTree != fixture.targetTree || outcome.Record.Status != spec.RetainedValidationStatusPASS {
		t.Fatalf("completion record = %+v", outcome.Record)
	}
	if outcome.RecordPath == "" || !strings.HasPrefix(outcome.RecordPath, ".polis/artifacts/evidence/sha256-") {
		t.Fatalf("record path = %q", outcome.RecordPath)
	}
	if len(outcome.PublicationPaths) == 0 {
		t.Fatal("completion published no retained inputs")
	}
	for _, path := range outcome.PublicationPaths {
		if strings.HasPrefix(path, ".polis/artifacts/packages/") {
			t.Fatalf("completion retained a delivery package: %s", path)
		}
		raw, err := os.ReadFile(filepath.Join(fixture.repo, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read retained %s: %v", path, err)
		}
		wantPath := ""
		switch {
		case strings.HasPrefix(path, ".polis/artifacts/contracts/"):
			if !bytes.Equal(raw, fixture.contractRaw) {
				t.Fatal("retained contract bytes differ")
			}
			wantPath = expectedPath("contracts", digestHex(raw))
		case strings.HasPrefix(path, ".polis/artifacts/evidence/"):
			if !bytes.Equal(raw, fixture.evidence) {
				t.Fatal("retained evidence bytes differ")
			}
			wantPath = expectedPath("evidence", digestHex(raw))
		case strings.HasPrefix(path, ".polis/artifacts/plans/"):
			if !bytes.Equal(raw, fixture.planRaw) {
				t.Fatal("retained plan bytes differ")
			}
			wantPath = expectedPath("plans", digestHex(raw))
		case strings.HasPrefix(path, ".polis/artifacts/proofs/"):
			if !bytes.Equal(raw, fixture.patch) {
				t.Fatal("retained proof bytes differ")
			}
			wantPath = expectedPath("proofs", digestHex(raw))
		default:
			t.Fatalf("unexpected retained class in %s", path)
		}
		if path != wantPath {
			t.Fatalf("retained path %s is not content-addressed as %s", path, wantPath)
		}
	}
	recordRaw, err := os.ReadFile(filepath.Join(fixture.repo, filepath.FromSlash(outcome.RecordPath)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spec.DecodeRetainedValidationRecord(recordRaw); err != nil {
		t.Fatalf("published record is not strictly decodable: %v", err)
	}
	if len(outcome.Verification.Paths) == 0 {
		t.Fatal("post-publication verification returned no artifacts")
	}
	if headBefore != runCompletionGit(t, fixture.repo, "rev-parse", "HEAD") ||
		indexBefore != runCompletionGit(t, fixture.repo, "write-tree") ||
		refsBefore != runCompletionGit(t, fixture.repo, "for-each-ref", "--format=%(refname) %(objectname)") {
		t.Fatal("completion mutated HEAD, the real index, or refs")
	}
}

func TestCompleteFailsClosedOnPublisherFailure(t *testing.T) {
	fixture := newCompletionFixture(t)
	wantErr := errors.New("publication unavailable")
	publisher := func(context.Context, artifactretention.State, string, []artifactretention.Artifact) ([]string, error) {
		return nil, wantErr
	}
	if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(spec.RetainedValidationOperationBuild), publisher); !errors.Is(err, wantErr) {
		t.Fatalf("Complete() error = %v, want publisher failure", err)
	}
	paths, err := fixture.retention.ListPaths(fixture.repo, "evidence")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, readErr := os.ReadFile(filepath.Join(fixture.repo, filepath.FromSlash(path)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(raw, []byte(`"kind":"repository_validation"`)) {
			t.Fatal("failed publication left a completeness record")
		}
	}
}

func TestCompleteRejectsPublisherPathMismatch(t *testing.T) {
	fixture := newCompletionFixture(t)
	publisher := func(ctx context.Context, state artifactretention.State, repo string, artifacts []artifactretention.Artifact) ([]string, error) {
		paths, err := DefaultPublisher(ctx, state, repo, artifacts)
		if err != nil {
			return nil, err
		}
		return paths[:1], nil
	}
	if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(spec.RetainedValidationOperationBuild), publisher); err == nil || !strings.Contains(err.Error(), "publisher returned") {
		t.Fatalf("Complete() error = %v, want publisher path mismatch", err)
	}
}

func TestCompleteRejectsInvalidInputs(t *testing.T) {
	fixture := newCompletionFixture(t)
	cases := []struct {
		name   string
		mutate func(*Inputs)
		want   string
	}{
		{"unsupported operation", func(in *Inputs) { in.Operation = "other" }, "unsupported retention completion operation"},
		{"missing contract", func(in *Inputs) { in.ContractRaw = nil }, "requires a locked Change Contract"},
		{"missing policy", func(in *Inputs) { in.PolicyRaw = nil }, "requires the exact policy bytes"},
		{"missing evidence", func(in *Inputs) { in.Evidence = nil }, "requires validation evidence"},
		{"missing target tree", func(in *Inputs) { in.TargetTree = "" }, "requires a target tree"},
		{"unlocked contract", func(in *Inputs) { in.Contract.DevelopmentMethod = spec.DevelopmentMethodStrictSDDTDDV1 }, "locked strict_sdd_tdd_v2"},
		{"malformed contract", func(in *Inputs) { in.ContractRaw = []byte("{") }, "decode retained validation contract"},
		{"missing gate outcomes", func(in *Inputs) { in.ProducerGateStatuses = nil }, "requires producer gate outcomes"},
		{"wrong base commit", func(in *Inputs) { in.BaseCommit = strings.Repeat("1", len(in.BaseCommit)) }, "base commit differs"},
		{"wrong policy bytes", func(in *Inputs) { in.PolicyRaw = append([]byte(nil), in.PolicyRaw...); in.PolicyRaw[0] = ' ' }, "policy differs"},
		{"missing red proof", func(in *Inputs) { in.RegressionPatch = nil }, "requires a captured Red proof"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputs := fixture.inputs(spec.RetainedValidationOperationWorkspace)
			tc.mutate(&inputs)
			published := false
			publisher := func(context.Context, artifactretention.State, string, []artifactretention.Artifact) ([]string, error) {
				published = true
				return nil, errors.New("publisher must not run for invalid inputs")
			}
			_, err := Complete(fixture.ctx, fixture.retention, fixture.repo, inputs, publisher)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Complete() error = %v, want %q", err, tc.want)
			}
			if published {
				t.Fatal("Complete published artifacts for invalid inputs")
			}
		})
	}
}

// The exact retained contract bytes are authoritative: a caller-supplied
// ChangeContract struct that diverges from those bytes must never produce or
// confirm a PASS.
func TestCompleteRejectsDivergentContractStruct(t *testing.T) {
	fixture := newCompletionFixture(t)
	inputs := fixture.inputs(spec.RetainedValidationOperationWorkspace)

	// ContractRaw is a defect that requires a Red proof. Claiming a
	// behavior-preserving change in the struct drops that requirement and lets
	// the completion omit the Red proof while retaining the defect contract.
	inputs.Contract.Kind = spec.ChangeKindBehaviorPreserving
	inputs.RegressionPatch = nil

	outcome, err := Complete(fixture.ctx, fixture.retention, fixture.repo, inputs, nil)
	if err == nil {
		t.Fatalf("Complete accepted a divergent contract struct and published %+v", outcome.Record)
	}
	if !strings.Contains(err.Error(), "contract") {
		t.Fatalf("Complete() error = %v, want a contract identity rejection", err)
	}
}

func TestCompleteRejectsDivergentPolicyStruct(t *testing.T) {
	fixture := newCompletionFixture(t)
	inputs := fixture.inputs(spec.RetainedValidationOperationWorkspace)

	// The validation summary is unchanged, so the divergence is invisible to
	// gate inventories but still contradicts the exact locked policy bytes.
	inputs.Policy.Gates[0].Command.TimeoutSeconds++

	published := false
	publisher := func(context.Context, artifactretention.State, string, []artifactretention.Artifact) ([]string, error) {
		published = true
		return nil, errors.New("publisher must not run for a divergent policy struct")
	}
	if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, inputs, publisher); err == nil || !strings.Contains(err.Error(), "policy struct differs") {
		t.Fatalf("Complete() error = %v, want divergent policy struct rejection", err)
	}
	if published {
		t.Fatal("Complete published artifacts for a divergent policy struct")
	}
}

func TestCompleteRejectsPublisherRecordContract(t *testing.T) {
	fixture := newCompletionFixture(t)
	t.Run("wrong artifact path", func(t *testing.T) {
		publisher := func(ctx context.Context, state artifactretention.State, repo string, artifacts []artifactretention.Artifact) ([]string, error) {
			paths, err := DefaultPublisher(ctx, state, repo, artifacts)
			if err != nil {
				return nil, err
			}
			paths[0] = strings.Replace(paths[0], "sha256-", "sha256-f", 1)
			return paths, nil
		}
		if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(spec.RetainedValidationOperationBuild), publisher); err == nil || !strings.Contains(err.Error(), "unexpected retained") {
			t.Fatalf("Complete() error = %v, want artifact path mismatch", err)
		}
	})
	t.Run("record publication failure", func(t *testing.T) {
		calls := 0
		wantErr := errors.New("record publication unavailable")
		publisher := func(ctx context.Context, state artifactretention.State, repo string, artifacts []artifactretention.Artifact) ([]string, error) {
			calls++
			if calls == 2 {
				return nil, wantErr
			}
			return DefaultPublisher(ctx, state, repo, artifacts)
		}
		_, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(spec.RetainedValidationOperationBuild), publisher)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Complete() error = %v, want record publication failure", err)
		}
	})
	t.Run("wrong record path", func(t *testing.T) {
		calls := 0
		publisher := func(ctx context.Context, state artifactretention.State, repo string, artifacts []artifactretention.Artifact) ([]string, error) {
			calls++
			paths, err := DefaultPublisher(ctx, state, repo, artifacts)
			if err != nil {
				return nil, err
			}
			if calls == 2 {
				paths[0] = strings.Replace(paths[0], "sha256-", "sha256-f", 1)
			}
			return paths, nil
		}
		if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(spec.RetainedValidationOperationBuild), publisher); err == nil || !strings.Contains(err.Error(), "unexpected retained validation record path") {
			t.Fatalf("Complete() error = %v, want record path mismatch", err)
		}
	})
}

func writeCompletionFile(t *testing.T, path string, data []byte, mode ...os.FileMode) {
	t.Helper()
	permissions := os.FileMode(0o600)
	if len(mode) > 0 {
		permissions = mode[0]
	}
	if err := os.WriteFile(path, data, permissions); err != nil {
		t.Fatal(err)
	}
}

func runCompletionGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, raw)
	}
	return strings.TrimSpace(string(raw))
}

func containsCompletionGate(gates []string, gate string) bool {
	for _, candidate := range gates {
		if candidate == gate {
			return true
		}
	}
	return false
}

func completionEvidenceBytes(t *testing.T, summary spec.ValidationSummary, statuses map[string]spec.Status, deferred []string) []byte {
	t.Helper()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	configured := spec.EvidenceEvent{
		Event: "validation_configured", Gate: "policy", ValidationLevel: summary.Level,
		EnabledGates: summary.EnabledGates, DisabledGates: summary.DisabledGates, DeferredGates: deferred,
	}
	if len(configured.DeferredGates) == 0 {
		configured.DeferredGates = []string{}
	}
	if err := enc.Encode(configured); err != nil {
		t.Fatal(err)
	}
	reason := "not applicable in completion fixture"
	deferredReason := spec.DeferredReasonToConsumer
	for _, gate := range spec.ProjectGateOrder {
		event := spec.EvidenceEvent{Event: "gate_finished", Gate: gate, Status: statuses[gate]}
		switch event.Status {
		case spec.StatusNotApplicable:
			event.Reason = &reason
		case spec.StatusDeferred:
			event.Reason = &deferredReason
		}
		if err := enc.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func completionPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in completion fixture"
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch {
		case id == "test.complete":
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: []string{"git", "rev-parse", "--is-inside-work-tree"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		case id == "coverage":
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		default:
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	raw, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func completionDraftContractBytes(t *testing.T) []byte {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--is-inside-work-tree"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	exitCode := 1
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.CommitIntentDraftChangeContractSchemaVersion, Kind: spec.ChangeKindDefect,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"app.txt", "app_test.go"}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"app_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "verify retained completeness evidence",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "retained evidence must be complete")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "completion is verified", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "retention never mutates Git state")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "missing evidence reports PASS")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "locked baseline")},
			Outputs:            []spec.SpecificationClause{clause("OUT-001", "bounded completeness record")},
			FailureSemantics:   []spec.SpecificationClause{clause("FAIL-001", "any mismatch blocks completion")},
		},
		Behavior: pass, Affected: pass,
		Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &regression, BaselineExitCode: &exitCode, BaselineOutputContains: []string{"BUG-RED"}},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}
