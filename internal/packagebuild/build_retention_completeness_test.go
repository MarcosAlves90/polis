package packagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/internal/retentioncompleteness"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type completenessBuildFixture struct {
	repo         string
	contractPath string
	contractRaw  []byte
	contract     spec.ChangeContract
	policyRaw    []byte
	policy       spec.Policy
}

func newCompletenessBuildFixture(t *testing.T) completenessBuildFixture {
	t.Helper()
	repo := newV6Repo(t, false, true)
	contractPath := lockedCharacterizationContract(t, repo, ".")
	contractRaw, err := os.ReadFile(contractPath)
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
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("issue38 implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return completenessBuildFixture{
		repo:         repo,
		contractPath: contractPath,
		contractRaw:  contractRaw,
		contract:     contract,
		policyRaw:    policyRaw,
		policy:       policy,
	}
}

func readCompletenessRetained(t *testing.T, repo, path string) []byte {
	t.Helper()
	if !strings.HasPrefix(path, ".polis/artifacts/") {
		t.Fatalf("retained path %q is outside the managed artifact root", path)
	}
	raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read retained %s: %v", path, err)
	}
	return raw
}

func TestBuildPublishesVerifiedRetentionCompleteness(t *testing.T) {
	fixture := newCompletenessBuildFixture(t)
	ctx := context.Background()
	beforeHead := runGit(t, fixture.repo, "rev-parse", "HEAD")
	beforeIndex := runGit(t, fixture.repo, "write-tree")
	beforeRefs := runGit(t, fixture.repo, "for-each-ref", "--format=%(refname) %(objectname)")
	beforeStatus, err := artifactretention.WorktreeStatus(ctx, fixture.repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Build(ctx, Options{
		Repo: fixture.repo, Project: "polis", Change: "issue38-completeness",
		Out: t.TempDir(), Contract: fixture.contractPath,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.RetainedRecordPath == "" {
		t.Fatal("Build() did not report a retained validation completion record")
	}
	for _, path := range result.RetainedPaths {
		if strings.HasPrefix(path, ".polis/artifacts/packages/") {
			t.Fatalf("Build() retained a delivery package: %s", path)
		}
		if path == result.RetainedRecordPath {
			t.Fatalf("completion record was added to RetainedPaths: %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.repo, ".polis", "artifacts", "packages")); !os.IsNotExist(err) {
		t.Fatalf("Build() created a retained package directory: stat err=%v", err)
	}

	recordRaw := readCompletenessRetained(t, fixture.repo, result.RetainedRecordPath)
	sum := sha256.Sum256(recordRaw)
	wantRecordPath := ".polis/artifacts/evidence/sha256-" + hex.EncodeToString(sum[:]) + ".json"
	if result.RetainedRecordPath != wantRecordPath {
		t.Fatalf("record path=%q, want content-addressed %q", result.RetainedRecordPath, wantRecordPath)
	}
	record, err := spec.DecodeRetainedValidationRecord(recordRaw)
	if err != nil {
		t.Fatalf("published completion record is not strictly decodable: %v", err)
	}
	if record.Kind != spec.RetainedValidationKind || record.Operation != spec.RetainedValidationOperationBuild ||
		record.Status != spec.RetainedValidationStatusPASS || record.TargetTree != result.TargetTree {
		t.Fatalf("completion record = %+v", record)
	}
	if len(record.ProducerGateStatuses) != len(spec.ProjectGateOrder) {
		t.Fatalf("completion record gate outcomes = %v", record.ProducerGateStatuses)
	}

	evidenceRaw := readCompletenessRetained(t, fixture.repo, record.RetainedEvidence)
	var planRaw []byte
	for _, path := range result.RetainedPaths {
		if strings.HasPrefix(path, ".polis/artifacts/plans/") {
			planRaw = readCompletenessRetained(t, fixture.repo, path)
		}
	}
	retention, err := artifactretention.Load(ctx, fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := retentioncompleteness.Verify(ctx, retentioncompleteness.VerifyOptions{
		Retention:   retention,
		Repo:        fixture.repo,
		RecordPath:  result.RetainedRecordPath,
		ContractRaw: fixture.contractRaw,
		Contract:    fixture.contract,
		PolicyRaw:   fixture.policyRaw,
		Policy:      fixture.policy,
		EvidenceRaw: evidenceRaw,
		PlanRaw:     planRaw,
		TargetTree:  result.TargetTree,
	})
	if err != nil {
		t.Fatalf("retained completion does not verify: %v", err)
	}
	if len(verification.Paths) == 0 {
		t.Fatal("post-publication verification returned no retained artifacts")
	}

	if got := runGit(t, fixture.repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := runGit(t, fixture.repo, "write-tree"); got != beforeIndex {
		t.Fatalf("real index changed: %s -> %s", beforeIndex, got)
	}
	if got := runGit(t, fixture.repo, "for-each-ref", "--format=%(refname) %(objectname)"); got != beforeRefs {
		t.Fatalf("refs changed: %s -> %s", beforeRefs, got)
	}
	afterStatus, err := artifactretention.WorktreeStatus(ctx, fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterStatus) != string(beforeStatus) {
		t.Fatalf("source status changed: before=%q after=%q", beforeStatus, afterStatus)
	}
}

func TestRetentionCompletenessPublicationFailureFailsClosed(t *testing.T) {
	t.Run("build", func(t *testing.T) {
		fixture := newCompletenessBuildFixture(t)
		wantErr := errors.New("issue38 build retention unavailable")
		ctx := context.WithValue(context.Background(), retainedArtifactPublisherContextKey{}, failingCompletenessPublisher(wantErr))
		out := t.TempDir()
		_, err := Build(ctx, Options{
			Repo: fixture.repo, Project: "polis", Change: "issue38-fail-closed",
			Out: out, Contract: fixture.contractPath,
		})
		if !errors.Is(err, wantErr) || err == nil || !strings.Contains(err.Error(), "retain generated delivery artifacts") {
			t.Fatalf("Build() error = %v, want wrapped retention failure", err)
		}
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("failed build left delivery output entries: %v", entries)
		}
		assertNoRetentionCompletionRecord(t, fixture.repo)
	})
	t.Run("workspace validate", func(t *testing.T) {
		fixture := newCompletenessBuildFixture(t)
		wantErr := errors.New("issue38 workspace retention unavailable")
		ctx := context.WithValue(context.Background(), retainedArtifactPublisherContextKey{}, failingCompletenessPublisher(wantErr))
		_, err := ValidateWorkspace(ctx, WorkspaceOptions{Repo: fixture.repo, Contract: fixture.contractPath})
		if !errors.Is(err, wantErr) || err == nil || !strings.Contains(err.Error(), "retain current workspace validation evidence") {
			t.Fatalf("ValidateWorkspace() error = %v, want wrapped retention failure", err)
		}
		assertNoRetentionCompletionRecord(t, fixture.repo)
	})
}

func failingCompletenessPublisher(err error) retainedArtifactPublisherFunc {
	return retainedArtifactPublisherFunc(func(artifactretention.State, string, []artifactretention.Artifact) ([]string, error) {
		return nil, err
	})
}

func assertNoRetentionCompletionRecord(t *testing.T, repo string) {
	t.Helper()
	retention, err := artifactretention.Load(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := retention.ListPaths(repo, "evidence")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"kind":"repository_validation"`) {
			t.Fatalf("failed publication left a completion record at %s", path)
		}
	}
}

func TestBuildRetentionCompletionRecordIsBoundJSON(t *testing.T) {
	fixture := newCompletenessBuildFixture(t)
	result, err := Build(context.Background(), Options{
		Repo: fixture.repo, Project: "polis", Change: "issue38-record-json",
		Out: t.TempDir(), Contract: fixture.contractPath,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	recordRaw := readCompletenessRetained(t, fixture.repo, result.RetainedRecordPath)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(recordRaw, &fields); err != nil {
		t.Fatalf("decode completion record fields: %v", err)
	}
	if _, ok := fields["producer_gate_statuses"]; !ok {
		t.Fatalf("completion record omits producer gate statuses: %s", recordRaw)
	}
	if _, ok := fields["target_tree"]; !ok {
		t.Fatalf("completion record omits target tree: %s", recordRaw)
	}
}
