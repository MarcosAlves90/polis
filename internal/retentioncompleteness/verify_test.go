package retentioncompleteness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/artifactretention"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func publishFixtureCompletion(t *testing.T, fixture completionFixture, operation string) Outcome {
	t.Helper()
	outcome, err := Complete(fixture.ctx, fixture.retention, fixture.repo, fixture.inputs(operation), nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	return outcome
}

func verifyOptionsForFixture(fixture completionFixture, outcome Outcome) VerifyOptions {
	return VerifyOptions{
		Retention:   fixture.retention,
		Repo:        fixture.repo,
		RecordPath:  outcome.RecordPath,
		ContractRaw: fixture.contractRaw,
		Contract:    fixture.contract,
		PolicyRaw:   fixture.policyRaw,
		Policy:      fixture.policy,
		EvidenceRaw: fixture.evidence,
		PlanRaw:     fixture.planRaw,
		ProofRaw:    fixture.patch,
		TargetTree:  fixture.targetTree,
	}
}

func TestVerifyRejectsUnsafeRetainedEvidence(t *testing.T) {
	t.Run("tampered evidence", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationBuild)
		path := filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedEvidence))
		if err := os.WriteFile(path, []byte("tampered\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted tampered retained evidence")
		}
	})
	t.Run("removed proof", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationBuild)
		path := filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedProof))
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted a missing retained Red proof")
		}
	})
	t.Run("symlinked record", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		recordPath := filepath.Join(fixture.repo, filepath.FromSlash(outcome.RecordPath))
		backup := recordPath + ".backup"
		if err := os.Rename(recordPath, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(backup, recordPath); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted a symlinked retained record")
		}
	})
	t.Run("stale target tree", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		opts := verifyOptionsForFixture(fixture, outcome)
		opts.TargetTree = strings.Repeat("9", len(opts.TargetTree))
		if _, err := Verify(fixture.ctx, opts); err == nil {
			t.Fatal("Verify accepted a record bound to a different target tree")
		}
	})
	t.Run("different policy", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		opts := verifyOptionsForFixture(fixture, outcome)
		policy := fixture.policy
		policy.Gates[0].Command.TimeoutSeconds++
		raw, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		opts.PolicyRaw = raw
		if _, err := Verify(fixture.ctx, opts); err == nil {
			t.Fatal("Verify accepted a record bound to different policy bytes")
		}
	})
}

func TestAssessStatusClassifiesRetainedReadiness(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		assessment := AssessStatus(fixture.ctx, StatusOptions{Retention: fixture.retention, Repo: fixture.repo})
		if assessment.State != StatusEmpty {
			t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
		}
	})
	t.Run("external", func(t *testing.T) {
		assessment := AssessStatus(contextBackground(), StatusOptions{})
		if assessment.State != StatusExternal {
			t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
		}
	})
	t.Run("incomplete", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
		if assessment.State != StatusIncomplete {
			t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
		}
		if assessment.Corrective == "" || assessment.Corrective == "none" {
			t.Fatalf("incomplete readiness has no corrective action: %+v", assessment)
		}
	})
	t.Run("complete", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
		if assessment.State != StatusComplete {
			t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
		}
		if assessment.Record == nil || assessment.Record.Operation != outcome.Record.Operation ||
			len(assessment.RecordPaths) != 1 || assessment.RecordPaths[0] != outcome.RecordPath {
			t.Fatalf("complete assessment = %+v", assessment)
		}
	})
	t.Run("stale", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		writeCompletionFile(t, filepath.Join(fixture.repo, "app.txt"), []byte("modified\n"))
		currentTree, err := TargetTree(fixture.ctx, fixture.repo, fixture.contract.BaselineLock.BaseCommit)
		if err != nil {
			t.Fatal(err)
		}
		if currentTree == fixture.targetTree {
			t.Fatal("fixture source tree did not change")
		}
		assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, currentTree))
		if assessment.State != StatusStale {
			t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		alternate := fixture.inputs(spec.RetainedValidationOperationWorkspace)
		alternate.Evidence = append(append([]byte(nil), fixture.evidence...), []byte("{\"event\":\"gate_started\",\"gate\":\"regression\"}\n")...)
		if _, err := Complete(fixture.ctx, fixture.retention, fixture.repo, alternate, nil); err != nil {
			t.Fatalf("publish alternate record: %v", err)
		}
		assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
		if assessment.State != StatusAmbiguous || len(assessment.RecordPaths) != 2 {
			t.Fatalf("state=%s paths=%v detail=%s", assessment.State, assessment.RecordPaths, assessment.Detail)
		}
	})
}

func statusOptionsForFixture(fixture completionFixture, targetTree string) StatusOptions {
	return StatusOptions{
		Retention:   fixture.retention,
		Repo:        fixture.repo,
		ContractRaw: fixture.contractRaw,
		Contract:    fixture.contract,
		PolicyRaw:   fixture.policyRaw,
		Policy:      fixture.policy,
		TargetTree:  targetTree,
	}
}

func contextBackground() context.Context {
	return context.Background()
}

func publishCompletionRecord(t *testing.T, fixture completionFixture, record spec.RetainedValidationRecord) string {
	t.Helper()
	raw, err := spec.EncodeRetainedValidationRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := fixture.retention.PublishMany(fixture.repo, []artifactretention.Artifact{{Class: classEvidence, Data: raw}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("published %d record paths", len(paths))
	}
	return paths[0]
}

func TestVerifyRejectsInvalidOptions(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	cases := []struct {
		name   string
		mutate func(*VerifyOptions)
		want   string
	}{
		{"external retention", func(opts *VerifyOptions) { opts.Retention = artifactretention.State{} }, "repository retention is not enabled"},
		{"missing contract lock", func(opts *VerifyOptions) { opts.Contract.BaselineLock = nil }, "requires locked contract"},
		{"missing contract bytes", func(opts *VerifyOptions) { opts.ContractRaw = nil }, "requires locked contract"},
		{"missing policy bytes", func(opts *VerifyOptions) { opts.PolicyRaw = nil }, "requires locked contract"},
		{"missing target tree", func(opts *VerifyOptions) { opts.TargetTree = "" }, "requires locked contract"},
		{"missing record path", func(opts *VerifyOptions) { opts.RecordPath = "" }, "requires a record path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := verifyOptionsForFixture(fixture, outcome)
			tc.mutate(&opts)
			if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVerifyRejectsDivergentContractStruct(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*spec.ChangeContract)
	}{
		{"change kind", func(c *spec.ChangeContract) { c.Kind = spec.ChangeKindBehaviorPreserving }},
		{"specification", func(c *spec.ChangeContract) { c.Specification.Objective = "divergent objective" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCompletionFixture(t)
			outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationBuild)
			opts := verifyOptionsForFixture(fixture, outcome)
			tc.mutate(&opts.Contract)

			if _, err := Verify(fixture.ctx, opts); err == nil {
				t.Fatal("Verify accepted a contract struct that diverges from the retained contract bytes")
			}
		})
	}
}

func TestVerifyRejectsDivergentPolicyStruct(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	opts := verifyOptionsForFixture(fixture, outcome)

	// The timeout does not change the effective validation summary, so only an
	// explicit bytes/struct binding can detect that the struct is not the
	// exact policy named by the locked baseline and the completion record.
	opts.Policy.Gates[0].Command.TimeoutSeconds++

	if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), "policy struct differs") {
		t.Fatalf("Verify() error = %v, want divergent policy struct rejection", err)
	}
}

func TestVerifyRejectsExpectedRecordMismatch(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	opts := verifyOptionsForFixture(fixture, outcome)
	alternate := outcome.Record
	alternate.Operation = spec.RetainedValidationOperationBuild
	opts.ExpectedRecord = &alternate
	if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), "differ from the published record") {
		t.Fatalf("Verify() error = %v, want expected-record mismatch", err)
	}
}

func TestVerifyRejectsRecordBindingMismatches(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	level := spec.ValidationLevelStrict
	if outcome.Record.ValidationLevel == level {
		level = spec.ValidationLevelMinimal
	}
	cases := []struct {
		name   string
		mutate func(*spec.RetainedValidationRecord)
		want   string
	}{
		{"contract", func(record *spec.RetainedValidationRecord) {
			record.ContractSHA256 = strings.Repeat("a", 64)
			record.RetainedContract = expectedPath(classContracts, record.ContractSHA256)
		}, "different Change Contract"},
		{"policy", func(record *spec.RetainedValidationRecord) { record.PolicySHA256 = strings.Repeat("b", 64) }, "different Project Policy"},
		{"baseline", func(record *spec.RetainedValidationRecord) { record.BaseCommit = strings.Repeat("c", 40) }, "different locked baseline"},
		{"target tree", func(record *spec.RetainedValidationRecord) { record.TargetTree = strings.Repeat("d", 40) }, "different target tree"},
		{"policy summary", func(record *spec.RetainedValidationRecord) { record.ValidationLevel = level }, "different effective Project Policy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := outcome.Record
			tc.mutate(&record)
			opts := verifyOptionsForFixture(fixture, outcome)
			opts.RecordPath = publishCompletionRecord(t, fixture, record)
			opts.ExpectedRecord = nil
			if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVerifyRejectsArtifactBindingMismatches(t *testing.T) {
	t.Run("missing contract", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedContract))); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted a missing retained Change Contract")
		}
	})
	t.Run("missing evidence", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedEvidence))); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted missing retained evidence")
		}
	})
	t.Run("missing plan", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedPlan))); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(fixture.ctx, verifyOptionsForFixture(fixture, outcome)); err == nil {
			t.Fatal("Verify accepted a missing retained Implementation Plan")
		}
	})
	t.Run("omitted plan", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
		record := outcome.Record
		record.RetainedPlan = ""
		record.ImplementationPlanSHA256 = ""
		opts := verifyOptionsForFixture(fixture, outcome)
		opts.RecordPath = publishCompletionRecord(t, fixture, record)
		opts.ExpectedRecord = nil
		if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), "omits the selected Implementation Plan") {
			t.Fatalf("Verify() error = %v, want omitted plan", err)
		}
	})
	t.Run("different proof bytes", func(t *testing.T) {
		fixture := newCompletionFixture(t)
		outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationBuild)
		opts := verifyOptionsForFixture(fixture, outcome)
		opts.ProofRaw = []byte("different captured proof\n")
		if _, err := Verify(fixture.ctx, opts); err == nil || !strings.Contains(err.Error(), "differs from the captured proof bytes") {
			t.Fatalf("Verify() error = %v, want proof mismatch", err)
		}
	})
}

func TestAssessStatusAcceptsCurrentWorkspaceAndBuildRecords(t *testing.T) {
	fixture := newCompletionFixture(t)
	workspace := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	build := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationBuild)

	assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
	if assessment.State != StatusComplete {
		t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
	}
	if assessment.Record == nil || assessment.Record.Operation != spec.RetainedValidationOperationBuild {
		t.Fatalf("complete assessment did not select the build record: %+v", assessment)
	}
	wantPaths := []string{build.RecordPath, workspace.RecordPath}
	sort.Strings(wantPaths)
	gotPaths := append([]string(nil), assessment.RecordPaths...)
	sort.Strings(gotPaths)
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("record paths = %v, want %v", gotPaths, wantPaths)
	}
	if assessment.Corrective != "none" {
		t.Fatalf("complete assessment has corrective action %q", assessment.Corrective)
	}
}

func TestAssessStatusReportsUnreadableEvidence(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)
	if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.Record.RetainedEvidence))); err != nil {
		t.Fatal(err)
	}
	assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
	if assessment.State != StatusIncomplete || len(assessment.RecordPaths) != 1 || assessment.Corrective != "polis workspace validate" {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestAssessStatusIgnoresUnrelatedContractRecords(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)

	unrelated := outcome.Record
	unrelated.ContractSHA256 = digestHex([]byte("unrelated contract"))
	unrelated.RetainedContract = expectedPath(classContracts, unrelated.ContractSHA256)
	unrelated.PolicySHA256 = strings.Repeat("a", 64)
	unrelated.SpecificationSHA256 = strings.Repeat("b", 64)
	unrelated.BaseCommit = strings.Repeat("c", 40)
	publishCompletionRecord(t, fixture, unrelated)
	if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.RecordPath))); err != nil {
		t.Fatal(err)
	}

	assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
	if assessment.State != StatusIncomplete {
		t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
	}
	if len(assessment.RecordPaths) != 0 {
		t.Fatalf("unrelated contract record paths = %v", assessment.RecordPaths)
	}
}

func TestAssessStatusReportsSupersededContractBaselineAsStale(t *testing.T) {
	fixture := newCompletionFixture(t)
	outcome := publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)

	superseded := outcome.Record
	superseded.ContractSHA256 = digestHex([]byte("superseded contract"))
	superseded.RetainedContract = expectedPath(classContracts, superseded.ContractSHA256)
	superseded.BaseCommit = strings.Repeat("1", len(superseded.BaseCommit))
	superseded.BaseTree = strings.Repeat("2", len(superseded.BaseTree))
	// The specification and policy stay bound to this workflow; only the
	// locked contract and baseline are from an earlier run.
	stalePath := publishCompletionRecord(t, fixture, superseded)
	if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(outcome.RecordPath))); err != nil {
		t.Fatal(err)
	}

	assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
	if assessment.State != StatusStale {
		t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
	}
	if len(assessment.RecordPaths) != 1 || assessment.RecordPaths[0] != stalePath {
		t.Fatalf("stale record paths = %v, want %s", assessment.RecordPaths, stalePath)
	}
}

func TestAssessStatusRejectsCompleteWhenCurrentRecordEvidenceIsUnreadable(t *testing.T) {
	fixture := newCompletionFixture(t)
	publishFixtureCompletion(t, fixture, spec.RetainedValidationOperationWorkspace)

	alternate := fixture.inputs(spec.RetainedValidationOperationWorkspace)
	alternate.Evidence = append(append([]byte(nil), fixture.evidence...), []byte("{\"event\":\"gate_started\",\"gate\":\"regression\"}\n")...)
	broken, err := Complete(fixture.ctx, fixture.retention, fixture.repo, alternate, nil)
	if err != nil {
		t.Fatalf("publish alternate completion: %v", err)
	}
	if broken.RecordPath == "" {
		t.Fatal("alternate completion has no record path")
	}
	if err := os.Remove(filepath.Join(fixture.repo, filepath.FromSlash(broken.Record.RetainedEvidence))); err != nil {
		t.Fatal(err)
	}

	assessment := AssessStatus(fixture.ctx, statusOptionsForFixture(fixture, fixture.targetTree))
	if assessment.State != StatusIncomplete {
		t.Fatalf("state=%s detail=%s", assessment.State, assessment.Detail)
	}
	if len(assessment.RecordPaths) != 1 || assessment.RecordPaths[0] != broken.RecordPath {
		t.Fatalf("unreadable record paths = %v, want %s", assessment.RecordPaths, broken.RecordPath)
	}
	if assessment.Corrective != "polis workspace validate" {
		t.Fatalf("corrective = %q", assessment.Corrective)
	}
}

func TestFormatAssessmentDetails(t *testing.T) {
	withPaths := FormatAssessmentDetails(StatusAssessment{Detail: "detail", RecordPaths: []string{"a", "b"}})
	if withPaths != "detail; paths: a, b" {
		t.Fatalf("details with paths = %q", withPaths)
	}
	withOperation := FormatAssessmentDetails(StatusAssessment{Detail: "detail", Record: &spec.RetainedValidationRecord{Operation: spec.RetainedValidationOperationBuild}})
	if withOperation != "detail; operation: build" {
		t.Fatalf("details with operation = %q", withOperation)
	}
}
