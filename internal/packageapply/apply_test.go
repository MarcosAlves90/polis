package packageapply

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func fixturePassCommand() []string {
	return []string{"git", "rev-parse", "--is-inside-work-tree"}
}

func policyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in package apply fixture"
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}})
		} else if id == "coverage" {
			threshold := 80.0
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &spec.CommandSpec{Argv: []string{"git", "checkout", "--", ".polis/coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	b, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func lockedApplyFixtureContract(t *testing.T, repo string) string {
	return lockedApplyFixtureContractWithMessage(t, repo, nil)
}

func lockedApplyFixtureContractWithMessage(t *testing.T, repo string, commitMessage *string) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindBehaviorPreserving,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"."}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}},
		DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective:          "produce apply fixture under V6 locked workflow",
			Requirements:       []spec.SpecificationClause{clause("REQ-001", "existing Add behavior remains Green")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "Add passes on baseline and target", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "consumer HEAD and index are preserved")},
			ForbiddenStates:    []spec.SpecificationClause{clause("FORBID-001", "apply bypasses validation")},
			Inputs:             []spec.SpecificationClause{clause("IN-001", "clean baseline")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "validated target")},
			FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "baseline or validation mismatch blocks apply")},
		},
		Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	if commitMessage != nil {
		draft.SchemaVersion = spec.CommitIntentDraftChangeContractSchemaVersion
		draft.Commit = &spec.CommitMetadata{Message: *commitMessage}
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "apply-draft-v3.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "apply-locked-v4.json")
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Contract: draftPath, Out: locked}); err != nil {
		t.Fatalf("polis start apply fixture: %v", err)
	}
	return locked
}

func repoWithArtifact(t *testing.T) (repo, artifact, target string) {
	return repoWithPolicyArtifact(t, policyBytes(t))
}

type commitArtifactFixture struct {
	repo     string
	artifact string
	target   string
}

var (
	commitArtifactFixtureRoot string
	commitArtifactFixtures    = make(map[string]commitArtifactFixture)
)

func repoWithCommitArtifact(t *testing.T, message string) (repo, artifact, target string) {
	t.Helper()
	if fixture, ok := commitArtifactFixtures[message]; ok {
		repo := cloneCommitTestRepo(t, fixture.repo)
		if remotes := git(t, repo, "remote"); remotes != "" {
			git(t, repo, "remote", "remove", "origin")
		}
		artifactBytes, err := os.ReadFile(fixture.artifact)
		if err != nil {
			t.Fatalf("read cached commit artifact fixture: %v", err)
		}
		artifact := filepath.Join(t.TempDir(), "commit-intent.polis")
		if err := os.WriteFile(artifact, artifactBytes, 0o600); err != nil {
			t.Fatalf("copy cached commit artifact fixture: %v", err)
		}
		return repo, artifact, fixture.target
	}

	repo, artifact, target = repoWithPolicyArtifactAndCommit(t, policyBytes(t), nil, &message)
	fixtureDir := filepath.Join(commitArtifactFixtureRoot, "commit-"+commitArtifactFixtureKey(message))
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatalf("create cached commit fixture directory: %v", err)
	}
	cachedRepo := filepath.Join(fixtureDir, "repo")
	command := exec.Command("git", "clone", "--quiet", repo, cachedRepo)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cache commit fixture repository: %v\n%s", err, output)
	}
	git(t, cachedRepo, "remote", "remove", "origin")
	cachedArtifact := filepath.Join(fixtureDir, "artifact.polis")
	artifactBytes, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("read commit artifact fixture: %v", err)
	}
	if err := os.WriteFile(cachedArtifact, artifactBytes, 0o600); err != nil {
		t.Fatalf("cache commit artifact fixture: %v", err)
	}
	commitArtifactFixtures[message] = commitArtifactFixture{repo: cachedRepo, artifact: cachedArtifact, target: target}
	return repo, artifact, target
}

func commitArtifactFixtureKey(message string) string {
	digest := sha256.Sum256([]byte(message))
	return hex.EncodeToString(digest[:])
}

func repoWithPolicyArtifact(t *testing.T, projectPolicy []byte, deferredGates ...string) (repo, artifact, target string) {
	return repoWithPolicyArtifactAndCommit(t, projectPolicy, deferredGates, nil)
}

func repoWithPolicyArtifactAndCommit(t *testing.T, projectPolicy []byte, deferredGates []string, commitMessage *string) (repo, artifact, target string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), projectPolicy, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "coverage.out"), []byte("mode: set\nexample.com/polisfixture/calc.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/polisfixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc.go"), []byte("package polisfixture\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "calc_test.go"), []byte("package polisfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	contractPath := lockedApplyFixtureContractWithMessage(t, repo, commitMessage)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "gitrex", Change: "apply-test", Out: t.TempDir(), Contract: contractPath, DeferredGates: deferredGates})
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	artifact, target = built.Path, built.TargetTree
	git(t, repo, "restore", "--", "app.txt")
	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture not clean: %q", status)
	}
	return repo, artifact, target
}

func independentConsumerFromRepo(t *testing.T, producer string) string {
	t.Helper()
	consumer := filepath.Join(t.TempDir(), "independent-consumer")
	if err := os.MkdirAll(consumer, 0o755); err != nil {
		t.Fatal(err)
	}
	objectFormat := git(t, producer, "rev-parse", "--show-object-format")
	git(t, consumer, "init", "-q", "--object-format="+objectFormat)

	cmd := exec.Command("git", "-C", producer, "ls-files", "-z")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("list producer files: %v", err)
	}
	for _, item := range strings.Split(string(raw), "\x00") {
		if item == "" {
			continue
		}
		source := filepath.Join(producer, filepath.FromSlash(item))
		target := filepath.Join(consumer, filepath.FromSlash(item))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(source)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(link, target); err != nil {
				t.Fatal(err)
			}
			continue
		}
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	git(t, consumer, "add", ".")
	git(t, consumer, "-c", "user.name=Independent Consumer", "-c", "user.email=consumer@example.invalid", "commit", "-qm", "independent baseline")
	if got, want := git(t, consumer, "rev-parse", "HEAD^{tree}"), git(t, producer, "rev-parse", "HEAD^{tree}"); got != want {
		t.Fatalf("independent consumer tree=%s want producer tree=%s", got, want)
	}
	return consumer
}

func readArtifactMembers(t *testing.T, artifact string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	members := make(map[string][]byte, len(zr.File))
	for _, file := range zr.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		members[file.Name] = body
	}
	return members
}

func writeArtifactMembers(t *testing.T, members map[string][]byte, filename string) string {
	t.Helper()
	delete(members, spec.MemberChecksums)
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		sum := sha256.Sum256(members[name])
		checksums.WriteString(hex.EncodeToString(sum[:]))
		checksums.WriteString("  ")
		checksums.WriteString(name)
		checksums.WriteByte('\n')
	}
	members[spec.MemberChecksums] = []byte(checksums.String())
	return writeArtifactArchive(t, members, filename)
}

func writeArtifactMembersPreservingChecksums(t *testing.T, members map[string][]byte, filename string) string {
	t.Helper()
	return writeArtifactArchive(t, members, filename)
}

func writeArtifactArchive(t *testing.T, members map[string][]byte, filename string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), filename)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o644)
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(members[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func legacyV3Artifact(t *testing.T, artifact string) string {
	t.Helper()
	members := readArtifactMembers(t, artifact)
	delete(members, spec.MemberBaseline)

	events, err := spec.DecodeEvidenceVersion(members[spec.MemberEvidence], spec.EvidenceVersionV3)
	if err != nil {
		t.Fatalf("decode v5 evidence for v3 fixture: %v", err)
	}
	var evidence strings.Builder
	encoder := json.NewEncoder(&evidence)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if event.Status == spec.StatusDeferred {
			t.Fatal("cannot encode deferred gate evidence as historical Evidence v2")
		}
		if event.Event == "validation_configured" {
			event.DeferredGates = nil
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	members[spec.MemberEvidence] = []byte(evidence.String())

	var manifest spec.Manifest
	if err := json.Unmarshal(members[spec.MemberManifest], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.FormatVersion = spec.IntermediateFormatVersion
	manifest.BaselineSHA256 = ""
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	members[spec.MemberManifest] = manifestRaw

	path := writeArtifactMembers(t, members, "legacy-v3.polis")
	if _, err := packageverify.Verify(path); err != nil {
		t.Fatalf("generated legacy v3 artifact is invalid: %v", err)
	}
	return path
}

func legacyV4Artifact(t *testing.T, artifact string) string {
	t.Helper()
	members := readArtifactMembers(t, artifact)
	events, err := spec.DecodeEvidenceVersion(members[spec.MemberEvidence], spec.EvidenceVersionV3)
	if err != nil {
		t.Fatalf("decode v5 evidence for v4 fixture: %v", err)
	}
	var evidence strings.Builder
	encoder := json.NewEncoder(&evidence)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if event.Event == "validation_configured" {
			event.DeferredGates = nil
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	members[spec.MemberEvidence] = []byte(evidence.String())
	var manifest spec.Manifest
	if err := json.Unmarshal(members[spec.MemberManifest], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.FormatVersion = spec.PreviousFormatVersion
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	members[spec.MemberManifest] = manifestRaw
	path := writeArtifactMembers(t, members, "legacy-v4.polis")
	if _, err := packageverify.Verify(path); err != nil {
		t.Fatalf("generated legacy v4 artifact is invalid: %v", err)
	}
	return path
}

func encodeEvidenceEvents(t *testing.T, events []spec.EvidenceEvent) []byte {
	t.Helper()
	var evidence strings.Builder
	encoder := json.NewEncoder(&evidence)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return []byte(evidence.String())
}

func TestDeferredEvidenceTamperingFailsChecksumAndTraceValidation(t *testing.T) {
	_, artifact, _ := repoWithPolicyArtifact(t, policyBytes(t), "coverage")
	original := readArtifactMembers(t, artifact)
	events, err := spec.DecodeEvidenceVersion(original[spec.MemberEvidence], spec.EvidenceVersionV3)
	if err != nil {
		t.Fatal(err)
	}

	checksumInventory := readArtifactMembers(t, artifact)
	originalEvidence := string(checksumInventory[spec.MemberEvidence])
	checksumInventory[spec.MemberEvidence] = []byte(strings.Replace(originalEvidence, `"deferred_gates":["coverage"]`, `"deferred_gates": ["coverage"]`, 1))
	if string(checksumInventory[spec.MemberEvidence]) == originalEvidence {
		t.Fatal("test failed to alter deferred metadata bytes")
	}
	checksumTampered := writeArtifactMembersPreservingChecksums(t, checksumInventory, "deferred-checksum-tamper.polis")
	if _, err := packageverify.Verify(checksumTampered); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("deferred metadata checksum tampering accepted: %v", err)
	}

	wrongTrace := readArtifactMembers(t, artifact)
	wrongTraceEvents := cloneEvidenceEventsForTest(t, events)
	for i := range wrongTraceEvents {
		if wrongTraceEvents[i].Event == "validation_configured" {
			wrongTraceEvents[i].DeferredGates = []string{}
		}
	}
	wrongTrace[spec.MemberEvidence] = encodeEvidenceEvents(t, wrongTraceEvents)
	semanticTampered := writeArtifactMembers(t, wrongTrace, "deferred-trace-tamper.polis")
	if _, err := packageverify.Verify(semanticTampered); err == nil || !strings.Contains(err.Error(), "validate evidence contract") {
		t.Fatalf("rehash of mismatched deferred trace accepted: %v", err)
	}
}

func cloneEvidenceEventsForTest(t *testing.T, events []spec.EvidenceEvent) []spec.EvidenceEvent {
	t.Helper()
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	var cloned []spec.EvidenceEvent
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func TestLegacyV4RetainsEvidenceV2AndEmbeddedBaseline(t *testing.T) {
	producer, currentArtifact, _ := repoWithArtifact(t)
	artifact := legacyV4Artifact(t, currentArtifact)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.FormatVersion != spec.PreviousFormatVersion || len(pkg.Baseline) == 0 || pkg.Result.ConsumerValidationRequired || len(pkg.Result.DeferredGates) != 0 {
		t.Fatalf("v4 package result=%+v manifest=%+v baseline=%d", pkg.Result, pkg.Manifest, len(pkg.Baseline))
	}
	if _, err := spec.DecodeEvidenceVersion(pkg.Evidence, spec.EvidenceVersionV2); err != nil {
		t.Fatalf("v4 Evidence v2 decode failed: %v", err)
	}
	preflight, err := PreflightWithOptions(context.Background(), artifact, producer, Options{BaselineMode: BaselineModeStrict})
	if err != nil {
		t.Fatalf("v4 preflight failed: %v", err)
	}
	if preflight.ConsumerValidationStatus != spec.StatusPass || len(preflight.ConsumerGateStatuses) == 0 {
		t.Fatalf("v4 consumer result=%+v", preflight)
	}
}

func TestDeferredGateIsRevalidatedBeforeApplyMutation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		argv   []string
		status string
	}{
		{name: "fail", argv: []string{"git", "rev-parse", "--does-not-exist"}, status: "FAIL"},
		{name: "block", argv: []string{"polis-deferred-gate-executable-404"}, status: "BLOCKED"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			policy, err := spec.DecodePolicy(policyBytes(t))
			if err != nil {
				t.Fatal(err)
			}
			for i := range policy.Gates {
				if policy.Gates[i].ID == "coverage" {
					policy.Gates[i].Command.Argv = scenario.argv
				}
			}
			policyRaw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			consumer, artifact, target := repoWithPolicyArtifact(t, policyRaw, "coverage")
			beforeHead := git(t, consumer, "rev-parse", "HEAD")
			beforeIndex := git(t, consumer, "write-tree")
			beforeStatus := git(t, consumer, "status", "--porcelain=v1", "--untracked-files=all")
			if _, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModeStrict}); err == nil || !errors.Is(err, ErrValidationFailed) || !strings.Contains(err.Error(), scenario.status) {
				t.Fatalf("preflight accepted deferred gate with status %s: %v", scenario.status, err)
			}
			if _, err := ApplyWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModeStrict}); err == nil || !errors.Is(err, ErrValidationFailed) || !strings.Contains(err.Error(), scenario.status) {
				t.Fatalf("apply accepted deferred gate with status %s: %v", scenario.status, err)
			}
			if got := git(t, consumer, "rev-parse", "HEAD"); got != beforeHead {
				t.Fatalf("consumer HEAD mutated after failed validation: %s -> %s", beforeHead, got)
			}
			if got := git(t, consumer, "write-tree"); got != beforeIndex {
				t.Fatalf("consumer index mutated after failed validation: %s -> %s", beforeIndex, got)
			}
			if got := git(t, consumer, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
				t.Fatalf("consumer worktree mutated after failed validation: before=%q after=%q", beforeStatus, got)
			}
			if target == "" {
				t.Fatal("fixture target tree is empty")
			}
		})
	}
}

func TestPreflightAndApplyReportConsumerPassForDeferredGate(t *testing.T) {
	repo, artifact, _ := repoWithPolicyArtifact(t, policyBytes(t), "coverage")
	preflight, err := PreflightWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict})
	if err != nil {
		t.Fatalf("preflight deferred package: %v", err)
	}
	if !preflight.ConsumerValidationRequired || preflight.ConsumerValidationStatus != spec.StatusPass || len(preflight.ProducerDeferredGates) != 1 || preflight.ProducerDeferredGates[0] != "coverage" || len(preflight.OutstandingDeferredGates) != 0 || preflight.ConsumerGateStatuses["coverage"] != spec.StatusPass {
		t.Fatalf("preflight consumer result=%+v", preflight)
	}
	applied, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict})
	if err != nil {
		t.Fatalf("apply deferred package: %v", err)
	}
	if !applied.ConsumerValidationRequired || applied.ConsumerValidationStatus != spec.StatusPass || len(applied.ProducerDeferredGates) != 1 || applied.ProducerDeferredGates[0] != "coverage" || len(applied.OutstandingDeferredGates) != 0 || applied.ConsumerGateStatuses["coverage"] != spec.StatusPass {
		t.Fatalf("apply consumer result=%+v", applied)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "app.txt")); err != nil || string(got) != "changed\n" {
		t.Fatalf("applied app.txt=%q err=%v", got, err)
	}
}

func semanticallyCorruptedV4Artifact(t *testing.T, artifact string) string {
	t.Helper()
	members := readArtifactMembers(t, artifact)
	baseline := append([]byte(nil), members[spec.MemberBaseline]...)
	if len(baseline) <= 512 {
		t.Fatalf("baseline member unexpectedly small: %d", len(baseline))
	}
	baseline[512] ^= 0x01
	members[spec.MemberBaseline] = baseline

	var manifest spec.Manifest
	if err := json.Unmarshal(members[spec.MemberManifest], &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(baseline)
	manifest.BaselineSHA256 = hex.EncodeToString(sum[:])
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	members[spec.MemberManifest] = manifestRaw
	return writeArtifactMembers(t, members, "corrupted-v4.polis")
}

func assertCommitMissing(t *testing.T, repo, commit string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "cat-file", "-e", commit+"^{commit}")
	if err := cmd.Run(); err == nil {
		t.Fatalf("commit %s unexpectedly exists in independent consumer object database", commit)
	}
}

func TestPreflightPermissiveUsesEmbeddedBaselineAcrossIndependentObjectDatabase(t *testing.T) {
	producer, artifact, _ := repoWithArtifact(t)
	producerBase := git(t, producer, "rev-parse", "HEAD")
	consumer := independentConsumerFromRepo(t, producer)
	consumerHead := git(t, consumer, "rev-parse", "HEAD")
	if consumerHead == producerBase {
		t.Fatal("independent consumer unexpectedly reused producer commit")
	}
	assertCommitMissing(t, consumer, producerBase)

	beforeHead := consumerHead
	beforeIndex := git(t, consumer, "write-tree")
	beforeRefs := git(t, consumer, "for-each-ref", "--format=%(refname) %(objectname)")
	beforeWorktrees := git(t, consumer, "worktree", "list", "--porcelain")
	beforeObjects := git(t, consumer, "count-objects", "-v")
	beforeStatus := git(t, consumer, "status", "--porcelain=v1", "--untracked-files=all")
	beforeConfig, err := os.ReadFile(filepath.Join(consumer, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive})
	if err != nil {
		t.Fatalf("embedded permissive preflight: %v", err)
	}
	if result.BaselineSource != BaselineSourceEmbedded || result.BaselineAncestry != BaselineAncestryUnproven || result.OverrideActive {
		t.Fatalf("unexpected baseline result: %+v", result)
	}
	if len(result.BypassedGuarantees) != 0 {
		t.Fatalf("embedded proof unexpectedly bypassed guarantees: %v", result.BypassedGuarantees)
	}
	if got := git(t, consumer, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := git(t, consumer, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s -> %s", beforeIndex, got)
	}
	if got := git(t, consumer, "for-each-ref", "--format=%(refname) %(objectname)"); got != beforeRefs {
		t.Fatalf("refs changed\nbefore=%s\nafter=%s", beforeRefs, got)
	}
	if got := git(t, consumer, "worktree", "list", "--porcelain"); got != beforeWorktrees {
		t.Fatalf("linked-worktree administration changed\nbefore=%s\nafter=%s", beforeWorktrees, got)
	}
	if got := git(t, consumer, "count-objects", "-v"); got != beforeObjects {
		t.Fatalf("persistent object database changed\nbefore=%s\nafter=%s", beforeObjects, got)
	}
	if got := git(t, consumer, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("status changed: before=%q after=%q", beforeStatus, got)
	}
	afterConfig, err := os.ReadFile(filepath.Join(consumer, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterConfig) != string(beforeConfig) {
		t.Fatal("git config changed")
	}
	assertCommitMissing(t, consumer, producerBase)
}

func TestApplyPermissiveUsesEmbeddedBaselineAcrossIndependentObjectDatabase(t *testing.T) {
	producer, artifact, _ := repoWithArtifact(t)
	producerBase := git(t, producer, "rev-parse", "HEAD")
	consumer := independentConsumerFromRepo(t, producer)
	consumerHead := git(t, consumer, "rev-parse", "HEAD")
	assertCommitMissing(t, consumer, producerBase)

	result, err := ApplyWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive})
	if err != nil {
		t.Fatalf("embedded permissive apply: %v", err)
	}
	if result.BaselineSource != BaselineSourceEmbedded || result.ConsumerBaseCommit != consumerHead || result.OverrideActive {
		t.Fatalf("unexpected baseline result: %+v", result)
	}
	if b, _ := os.ReadFile(filepath.Join(consumer, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(consumer, "new.txt")); string(b) != "new\n" {
		t.Fatalf("new.txt=%q", b)
	}
	if got := git(t, consumer, "rev-parse", "HEAD"); got != consumerHead {
		t.Fatalf("HEAD changed: %s -> %s", consumerHead, got)
	}
	assertCommitMissing(t, consumer, producerBase)
}

func TestLegacyV3MissingBaselineRequiresExplicitOverrideOnPreflightAndApply(t *testing.T) {
	producer, currentArtifact, _ := repoWithArtifact(t)
	producerBase := git(t, producer, "rev-parse", "HEAD")
	artifact := legacyV3Artifact(t, currentArtifact)
	consumer := independentConsumerFromRepo(t, producer)
	consumerHead := git(t, consumer, "rev-parse", "HEAD")
	assertCommitMissing(t, consumer, producerBase)

	if _, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive}); err == nil || !errors.Is(err, ErrBaselineMismatch) {
		t.Fatalf("normal permissive preflight should fail without baseline, got %v", err)
	}
	preflight, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive, AllowMissingBaselineProof: true})
	if err != nil {
		t.Fatalf("explicit override preflight: %v", err)
	}
	if preflight.BaselineSource != BaselineSourceOverridden || !preflight.OverrideActive || len(preflight.BypassedGuarantees) == 0 {
		t.Fatalf("override state not reported: %+v", preflight)
	}
	if got := git(t, consumer, "rev-parse", "HEAD"); got != consumerHead {
		t.Fatalf("preflight changed HEAD: %s -> %s", consumerHead, got)
	}
	if _, err := ApplyWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive}); err == nil || !errors.Is(err, ErrBaselineMismatch) {
		t.Fatalf("apply without repeated override should fail, got %v", err)
	}
	applied, err := ApplyWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive, AllowMissingBaselineProof: true})
	if err != nil {
		t.Fatalf("explicit override apply: %v", err)
	}
	if applied.BaselineSource != BaselineSourceOverridden || !applied.OverrideActive || len(applied.BypassedGuarantees) == 0 {
		t.Fatalf("override state not reported after apply: %+v", applied)
	}
	if b, _ := os.ReadFile(filepath.Join(consumer, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if got := git(t, consumer, "rev-parse", "HEAD"); got != consumerHead {
		t.Fatalf("apply changed HEAD: %s -> %s", consumerHead, got)
	}
}

func TestMissingBaselineOverrideDoesNotBypassPayloadConflict(t *testing.T) {
	producer, currentArtifact, _ := repoWithArtifact(t)
	artifact := legacyV3Artifact(t, currentArtifact)
	consumer := independentConsumerFromRepo(t, producer)
	if err := os.WriteFile(filepath.Join(consumer, "app.txt"), []byte("consumer-conflict\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, consumer, "add", "app.txt")
	git(t, consumer, "-c", "user.name=Independent Consumer", "-c", "user.email=consumer@example.invalid", "commit", "-qm", "conflicting consumer state")

	_, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive, AllowMissingBaselineProof: true})
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "payload is incompatible") {
		t.Fatalf("override accepted payload conflict: %v", err)
	}
}

func TestMissingBaselineOverrideCannotBypassMalformedEmbeddedBaseline(t *testing.T) {
	producer, artifact, _ := repoWithArtifact(t)
	consumer := independentConsumerFromRepo(t, producer)
	corrupted := semanticallyCorruptedV4Artifact(t, artifact)

	if _, err := packageverify.Verify(corrupted); err == nil {
		t.Fatal("expected semantic embedded baseline corruption to invalidate package")
	}
	_, err := PreflightWithOptions(context.Background(), corrupted, consumer, Options{BaselineMode: BaselineModePermissive, AllowMissingBaselineProof: true})
	if err == nil || errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "verify package") {
		t.Fatalf("override converted malformed embedded baseline into an admission decision: %v", err)
	}
}

func TestMissingBaselineOverrideDoesNotBypassDirtyConsumer(t *testing.T) {
	producer, currentArtifact, _ := repoWithArtifact(t)
	artifact := legacyV3Artifact(t, currentArtifact)
	consumer := independentConsumerFromRepo(t, producer)
	if err := os.WriteFile(filepath.Join(consumer, "local-untracked.txt"), []byte("user work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := PreflightWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive, AllowMissingBaselineProof: true})
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("override accepted dirty consumer: %v", err)
	}
}

func TestApplyExactBaselinePreservesIndexAndUsesEphemeralEvidence(t *testing.T) {
	repo, artifact, target := repoWithArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	result, err := Apply(context.Background(), artifact, repo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s -> %s", beforeIndex, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "new.txt")); string(b) != "new\n" {
		t.Fatalf("new.txt=%q", b)
	}
	if result.EvidencePath != "" {
		t.Fatalf("persistent evidence path=%q", result.EvidencePath)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("git evidence residue: %v", err)
	}
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if !strings.Contains(status, "M app.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("unexpected post-apply status: %q", status)
	}
	if strings.Contains(status, "polis-results") {
		t.Fatalf("evidence polluted worktree: %q", status)
	}
}

func TestApplyRejectsDirtyWorktreeBeforeMutation(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("user work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected dirty worktree rejection")
	}
	after := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if after != before {
		t.Fatalf("dirty state changed: before=%q after=%q", before, after)
	}
}

func TestApplyRejectsWrongHead(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "other.txt")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "other")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected baseline mismatch")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "base\n" {
		t.Fatalf("app mutated on wrong head: %q", b)
	}
}

func TestApplyRejectsMalformedPackage(t *testing.T) {
	repo, _, _ := repoWithArtifact(t)
	bad := filepath.Join(t.TempDir(), "bad.polis")
	if err := os.WriteFile(bad, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), bad, repo); err == nil {
		t.Fatal("expected malformed package rejection")
	}
}

func TestApplySecondAttemptFailsClosedBecauseWorktreeIsNoLongerClean(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	if _, err := Apply(context.Background(), artifact, repo); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if _, err := Apply(context.Background(), artifact, repo); err == nil {
		t.Fatal("expected second apply rejection")
	}
	after := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if after != before {
		t.Fatalf("second apply changed state: before=%q after=%q", before, after)
	}
}

func TestApplyRejectsNonRepositoryTarget(t *testing.T) {
	_, artifact, _ := repoWithArtifact(t)
	if _, err := Apply(context.Background(), artifact, t.TempDir()); err == nil {
		t.Fatal("expected non-repository target rejection")
	}
}

func TestApplyUsesCurrentDirectoryWhenRepoEmpty(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if _, err := Apply(context.Background(), artifact, ""); err != nil {
		t.Fatalf("Apply() with default repo error = %v", err)
	}
}

func TestPreflightValidatesWithoutMutatingConsumerFiles(t *testing.T) {
	repo, artifact, target := repoWithArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeStatus := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	result, err := Preflight(context.Background(), artifact, repo)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed: %s -> %s", beforeIndex, got)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("status changed: before=%q after=%q", beforeStatus, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "base\n" {
		t.Fatalf("preflight mutated app.txt: %q", b)
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("preflight created new.txt: %v", err)
	}
}

func externalApplyPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in zero-residue apply fixture"
	threshold := 80.0
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		switch id {
		case "test.complete":
			cmd := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &cmd})
		case "coverage":
			cmd := spec.CommandSpec{Argv: []string{"cp", "coverage.fixture", "coverage.out"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCoverage, Command: &cmd, Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: "coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold})
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

func lockedExternalApplyContract(t *testing.T, repo, policyPath string) string {
	t.Helper()
	env := &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
	pass := spec.CommandSpec{Argv: fixturePassCommand(), Cwd: ".", TimeoutSeconds: 60, Environment: env}
	regression := spec.CommandSpec{Argv: []string{"go", "test", "-p=1", "./...", "-run", "TestAdd"}, Cwd: ".", TimeoutSeconds: 60, Environment: env}
	clause := func(id, statement string) spec.SpecificationClause {
		return spec.SpecificationClause{ID: id, Statement: statement}
	}
	draft := spec.ChangeContract{
		SchemaVersion: spec.StrictChangeContractSchemaVersion, Kind: spec.ChangeKindBehaviorPreserving,
		Scope: &spec.ChangeScope{AllowedPaths: []string{"app.txt", "new.txt", "calc_test.go"}}, TestScope: &spec.ChangeScope{AllowedPaths: []string{"calc_test.go"}}, DevelopmentMethod: spec.DevelopmentMethodStrictSDDTDDV1,
		Specification: &spec.DevelopmentSpecification{
			Objective: "apply without target residue", Requirements: []spec.SpecificationClause{clause("REQ-001", "consumer applies package-contained policy without repository policy state")},
			AcceptanceCriteria: []spec.AcceptanceCriterion{{ID: "AC-001", Statement: "apply changes only payload paths", Requirements: []string{"REQ-001"}, Proof: spec.ProofGateRegression}},
			Invariants:         []spec.SpecificationClause{clause("INV-001", "HEAD and index are preserved")}, ForbiddenStates: []spec.SpecificationClause{clause("FORBID-001", "tool metadata remains in target Git directory")},
			Inputs: []spec.SpecificationClause{clause("IN-001", "external-policy artifact")}, Outputs: []spec.SpecificationClause{clause("OUT-001", "payload-only working tree")}, FailureSemantics: []spec.SpecificationClause{clause("FAIL-001", "baseline or validation mismatch blocks mutation")},
		},
		Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: &regression},
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	draftPath := filepath.Join(t.TempDir(), "apply-external-draft.json")
	lockedPath := filepath.Join(t.TempDir(), "apply-external-locked.json")
	if err := os.WriteFile(draftPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := devstart.Start(context.Background(), devstart.Options{Repo: repo, Policy: policyPath, Contract: draftPath, Out: lockedPath}); err != nil {
		t.Fatalf("start external apply fixture: %v", err)
	}
	return lockedPath
}

func repoWithExternalArtifact(t *testing.T) (repo, artifact, target string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "external-consumer")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	files := map[string]string{
		"app.txt":          "base\n",
		"go.mod":           "module example.com/applyexternal\n\ngo 1.23\n",
		"calc.go":          "package applyexternal\n\nfunc Add(a, b int) int { return a + b }\n",
		"calc_test.go":     "package applyexternal\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n",
		"coverage.out":     "mode: set\nexample.com/applyexternal/calc.go:3.24,3.38 1 1\n",
		"coverage.fixture": "mode: set\nexample.com/applyexternal/calc.go:3.24,3.38 1 1\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policyPath, externalApplyPolicyBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	contract := lockedExternalApplyContract(t, repo, policyPath)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Policy: policyPath, Project: "external", Change: "apply-zero-residue", Out: t.TempDir(), Contract: contract})
	if err != nil {
		t.Fatalf("build external fixture: %v", err)
	}
	artifact, target = built.Path, built.TargetTree
	git(t, repo, "restore", "--", "app.txt")
	if err := os.Remove(filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture not clean: %q", status)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("unexpected .polis baseline state: %v", err)
	}
	return repo, artifact, target
}

func TestPreflightExternalPolicyNeedsNoRepositoryPolicyState(t *testing.T) {
	repo, artifact, target := repoWithExternalArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(context.Background(), artifact, repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed")
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed")
	}
	afterConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatal("git config changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("preflight git residue: %v", err)
	}
}

func TestApplyExternalPolicyLeavesNoToolGitMetadata(t *testing.T) {
	repo, artifact, target := repoWithExternalArtifact(t)
	beforeHead := git(t, repo, "rev-parse", "HEAD")
	beforeIndex := git(t, repo, "write-tree")
	beforeRefs := git(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	beforeConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), artifact, repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetTree != target {
		t.Fatalf("target=%s want=%s", result.TargetTree, target)
	}
	if result.EvidencePath != "" {
		t.Fatalf("persistent evidence path=%q", result.EvidencePath)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed")
	}
	if got := git(t, repo, "write-tree"); got != beforeIndex {
		t.Fatalf("index changed")
	}
	if got := git(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"); got != beforeRefs {
		t.Fatalf("refs changed\nbefore=%s\nafter=%s", beforeRefs, got)
	}
	afterConfig, err := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatal("git config changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "polis")); !os.IsNotExist(err) {
		t.Fatalf("git metadata residue: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".polis")); !os.IsNotExist(err) {
		t.Fatalf("worktree residue: %v", err)
	}
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	if !strings.Contains(status, "M app.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("unexpected payload status: %q", status)
	}
}

func commitFile(t *testing.T, repo, name, body, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", name)
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", message)
	return git(t, repo, "rev-parse", "HEAD")
}

func TestApplyCompatibleAcceptsDescendantWithUnrelatedCommit(t *testing.T) {
	repo, artifact, artifactTarget := repoWithArtifact(t)
	base := git(t, repo, "rev-parse", "HEAD")
	consumerHead := commitFile(t, repo, "consumer.txt", "consumer-only\n", "consumer unrelated change")
	if consumerHead == base {
		t.Fatal("consumer HEAD did not advance")
	}

	result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible})
	if err != nil {
		t.Fatalf("ApplyWithOptions() error = %v", err)
	}
	if result.BaselineMode != BaselineModeCompatible || result.ConsumerBaseCommit != consumerHead {
		t.Fatalf("assessment result=%+v", result)
	}
	if result.TargetTree == artifactTarget {
		t.Fatalf("rebased consumer target unexpectedly equals artifact target %s", artifactTarget)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "consumer.txt")); string(b) != "consumer-only\n" {
		t.Fatalf("consumer.txt=%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != consumerHead {
		t.Fatalf("HEAD changed: %s -> %s", consumerHead, got)
	}
}

func TestApplyCompatibleRejectsConflictingDescendant(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	commitFile(t, repo, "app.txt", "consumer-conflict\n", "consumer conflicting change")
	before := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible})
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "payload is incompatible") {
		t.Fatalf("expected compatible conflict rejection, got %v", err)
	}
	if after := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); after != before {
		t.Fatalf("consumer state changed: before=%q after=%q", before, after)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "consumer-conflict\n" {
		t.Fatalf("app.txt mutated after rejection: %q", b)
	}
}

func TestApplyCompatibleRejectsNonDescendantHistory(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	base := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "--orphan", "unrelated-compatible")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qam", "unrelated root")
	if cmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", base, "HEAD"); cmd.Run() == nil {
		t.Fatal("fixture is unexpectedly descendant")
	}

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible})
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "not a descendant") {
		t.Fatalf("expected non-descendant rejection, got %v", err)
	}
}

func TestApplyPermissiveAcceptsNonDescendantPatchCompatibleHistory(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	base := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "--orphan", "unrelated-permissive")
	if err := os.WriteFile(filepath.Join(repo, "consumer.txt"), []byte("consumer-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "unrelated compatible root")
	consumerHead := git(t, repo, "rev-parse", "HEAD")
	if cmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", base, "HEAD"); cmd.Run() == nil {
		t.Fatal("fixture is unexpectedly descendant")
	}

	result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModePermissive})
	if err != nil {
		t.Fatalf("permissive apply error = %v", err)
	}
	if result.BaselineMode != BaselineModePermissive || result.ConsumerBaseCommit != consumerHead {
		t.Fatalf("assessment result=%+v", result)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, " "), "not proven") {
		t.Fatalf("expected explicit permissive warning, got %v", result.Warnings)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "changed\n" {
		t.Fatalf("app.txt=%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "consumer.txt")); string(b) != "consumer-only\n" {
		t.Fatalf("consumer.txt=%q", b)
	}
}

func TestApplyPermissiveStillRejectsPatchConflict(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	git(t, repo, "checkout", "--orphan", "unrelated-conflict")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("consumer-conflict\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "unrelated conflicting root")

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModePermissive})
	if err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "payload is incompatible") {
		t.Fatalf("expected permissive conflict rejection, got %v", err)
	}
}

func TestPreflightCompatibleValidatesDescendantWithoutMutation(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	consumerHead := commitFile(t, repo, "consumer.txt", "consumer-only\n", "consumer unrelated change")
	beforeStatus := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	beforeObjects := git(t, repo, "count-objects", "-v")

	result, err := PreflightWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible})
	if err != nil {
		t.Fatalf("compatible preflight error = %v", err)
	}
	if result.ConsumerBaseCommit != consumerHead || result.BaselineMode != BaselineModeCompatible {
		t.Fatalf("result=%+v", result)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != consumerHead {
		t.Fatalf("HEAD changed: %s -> %s", consumerHead, got)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != beforeStatus {
		t.Fatalf("status changed: before=%q after=%q", beforeStatus, got)
	}
	if got := git(t, repo, "count-objects", "-v"); got != beforeObjects {
		t.Fatalf("Git object state changed: before=%q after=%q", beforeObjects, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != "base\n" {
		t.Fatalf("preflight mutated app.txt: %q", b)
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("preflight created new.txt: %v", err)
	}
}

func repoWithContextualArtifact(t *testing.T) (repo, artifact string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "contextual-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), policyBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".polis", "coverage.out"), []byte("mode: set\nexample.com/polisfixture/calc.go:1.1,1.2 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"app.txt":      "line-1\nline-2\nline-3\nline-4\nline-5\nline-6\nline-7\nbase-line-8\nline-9\nline-10\n",
		"go.mod":       "module example.com/polisfixture\n\ngo 1.23\n",
		"calc.go":      "package polisfixture\n\nfunc Add(a, b int) int { return a + b }\n",
		"calc_test.go": "package polisfixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad add\") } }\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	contractPath := lockedApplyFixtureContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("line-1\nline-2\nline-3\nline-4\nline-5\nline-6\nline-7\nchanged-line-8\nline-9\nline-10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := packagebuild.Build(context.Background(), packagebuild.Options{Repo: repo, Project: "gitrex", Change: "contextual-apply-test", Out: t.TempDir(), Contract: contractPath})
	if err != nil {
		t.Fatalf("build contextual fixture: %v", err)
	}
	git(t, repo, "restore", "--", "app.txt")
	return repo, built.Path
}

func TestApplyCompatibleAcceptsNonOverlappingChangeInPayloadFile(t *testing.T) {
	repo, artifact := repoWithContextualArtifact(t)
	consumerBody := "consumer-line-1\nline-2\nline-3\nline-4\nline-5\nline-6\nline-7\nbase-line-8\nline-9\nline-10\n"
	commitFile(t, repo, "app.txt", consumerBody, "consumer edits distant context")

	if _, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible}); err != nil {
		t.Fatalf("compatible contextual apply error = %v", err)
	}
	want := "consumer-line-1\nline-2\nline-3\nline-4\nline-5\nline-6\nline-7\nchanged-line-8\nline-9\nline-10\n"
	if b, _ := os.ReadFile(filepath.Join(repo, "app.txt")); string(b) != want {
		t.Fatalf("app.txt=%q want=%q", b, want)
	}
}

func TestVerifyAssessmentStableRejectsHeadChange(t *testing.T) {
	repo, artifact, _ := repoWithArtifact(t)
	commitFile(t, repo, "consumer.txt", "consumer-only\n", "consumer unrelated change")
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := assessBaseline(context.Background(), repo, pkg, Options{BaselineMode: BaselineModeCompatible})
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "later.txt", "changed-after-validation\n", "move consumer head")
	if err := verifyAssessmentStable(context.Background(), repo, pkg, assessment); err == nil || !errors.Is(err, ErrBaselineMismatch) || !strings.Contains(err.Error(), "consumer HEAD changed") {
		t.Fatalf("expected assessed HEAD drift rejection, got %v", err)
	}
}
