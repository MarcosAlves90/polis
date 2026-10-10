package policyplan

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/policyinit"
	"github.com/MarcosAlves90/polis/v6/internal/policyorigin"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func profileRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	profileGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/profile\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profileGit(t, repo, "add", "go.mod")
	profileGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "base")
	return repo
}

func profileGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func commitProfilePolicy(t *testing.T, repo string) {
	t.Helper()
	profileGit(t, repo, "add", ".polis/policy.json", ".polis/"+policyorigin.Filename)
	profileGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "policy")
}

func TestLoadUsesPersistedProfileOriginOnlyForUnchangedGates(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileAuto})
	if err != nil {
		t.Fatal(err)
	}
	commitProfilePolicy(t, repo)
	plan, err := Load(context.Background(), Options{Repo: repo, DeferredGates: []string{"coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	testGate, coverage, disabled := plan.Gates[0], plan.Gates[1], plan.Gates[3]
	if testGate.Provenance.Configuration.Source != DecisionSourceProfileDefault || testGate.Provenance.State.Source != DecisionSourceProfileDefault || testGate.Provenance.ProducerAction.Source != DecisionSourceProfileDefault {
		t.Fatalf("generated enabled gate origin=%+v", testGate.Provenance)
	}
	if disabled.Provenance.Configuration.Source != DecisionSourceProfileDefault || disabled.Provenance.State.Source != DecisionSourceNotApplicable || disabled.Provenance.ProducerAction.Source != DecisionSourceNotApplicable || plan.Guarantees[3].Provenance.Source != DecisionSourceNotApplicable {
		t.Fatalf("generated not_applicable gate origin=%+v", disabled.Provenance)
	}
	if coverage.Provenance.Configuration.Source != DecisionSourceProfileDefault || coverage.Provenance.ProducerAction.Source != DecisionSourceExecutorDeferral || coverage.Provenance.ConsumerRequirement.Source != DecisionSourceExecutorDeferral || plan.Guarantees[1].Provenance.Source != DecisionSourceExecutorDeferral {
		t.Fatalf("deferred gate origin=%+v", coverage.Provenance)
	}
	if plan.DependencyEdges[0].Provenance.Source != DecisionSourceMandatoryDependency {
		t.Fatalf("required dependency=%+v", plan.DependencyEdges[0])
	}

	var policy spec.Policy
	if err := json.Unmarshal(result.Policy, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Gates[2].Command.TimeoutSeconds = 50
	changed, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.PolicyPath, append(changed, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	profileGit(t, repo, "add", ".polis/policy.json")
	profileGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "manual gate change")
	plan, err = Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Gates[2].Provenance.Configuration.Source != DecisionSourcePolicyExplicit || plan.Gates[2].Provenance.State.Reason != "declared_gate_mode" || plan.Gates[0].Provenance.Configuration.Source != DecisionSourceProfileDefault {
		t.Fatalf("stale declaration misattributed: test=%+v lint=%+v", plan.Gates[0].Provenance, plan.Gates[2].Provenance)
	}

	// Uncommitted provenance modifications must never override HEAD evidence.
	if err := os.WriteFile(result.ProvenancePath, []byte("invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err = Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Gates[0].Provenance.Configuration.Source != DecisionSourcePolicyExplicit {
		t.Fatalf("modified provenance was trusted: %+v", plan.Gates[0].Provenance)
	}
}

func TestLoadWithoutUsableOriginRemainsConservative(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo})
	if err != nil {
		t.Fatal(err)
	}
	// Legacy policy exists in HEAD with no committed sidecar.
	profileGit(t, repo, "add", ".polis/policy.json")
	profileGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "legacy policy")
	plan, err := Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Gates[0].Provenance.Configuration.Source != DecisionSourcePolicyExplicit {
		t.Fatalf("uncommitted provenance was trusted: %+v", plan.Gates[0].Provenance)
	}
	if err := os.Remove(result.ProvenancePath); err != nil {
		t.Fatal(err)
	}
	plan, err = Load(context.Background(), Options{Repo: repo})
	if err != nil || plan.Gates[0].Provenance.Configuration.Source != DecisionSourcePolicyExplicit {
		t.Fatalf("legacy attribution failed: plan=%+v err=%v", plan.Gates[0].Provenance, err)
	}
}

func TestInitValidationLevelOriginDistinguishesExplicitStrictFromDefault(t *testing.T) {
	for _, tc := range []struct {
		name, selected, source, reason string
	}{
		{"omitted", "", DecisionSourceProfileDefault, "init_default_validation_level"},
		{"explicit_strict", spec.ValidationLevelStrict, DecisionSourcePolicyExplicit, "init_explicit_validation_level"},
		{"explicit_standard", spec.ValidationLevelStandard, DecisionSourcePolicyExplicit, "init_explicit_validation_level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := profileRepo(t)
			result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo, ValidationLevel: tc.selected})
			if err != nil {
				t.Fatal(err)
			}
			commitProfilePolicy(t, repo)
			plan, err := Load(context.Background(), Options{Repo: repo})
			if err != nil {
				t.Fatal(err)
			}
			if got := plan.ValidationLevelProvenance; got != (Decision{Source: tc.source, Reason: tc.reason}) {
				t.Fatalf("validation level origin=%+v", got)
			}
			if tc.selected == spec.ValidationLevelStrict && plan.ValidationLevel != spec.ValidationLevelStrict {
				t.Fatalf("strict selected=%q", plan.ValidationLevel)
			}
			if tc.selected == spec.ValidationLevelStrict || tc.selected == "" {
				policy, err := spec.DecodePolicy(result.Policy)
				if err != nil {
					t.Fatal(err)
				}
				if policy.ValidationLevel != "" {
					t.Fatalf("strict representation changed=%q", policy.ValidationLevel)
				}
			}
		})
	}
}

func TestValidationLevelChangedAfterInitFallsBackWithoutLosingGateOrigin(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo, ValidationLevel: spec.ValidationLevelStrict})
	if err != nil {
		t.Fatal(err)
	}
	var policy spec.Policy
	if err := json.Unmarshal(result.Policy, &policy); err != nil {
		t.Fatal(err)
	}
	policy.ValidationLevel = spec.ValidationLevelStrict // Same effective value, different representation.
	raw, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.PolicyPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	commitProfilePolicy(t, repo)
	plan, err := Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ValidationLevelProvenance != (Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_validation_level"}) {
		t.Fatalf("stale validation-level origin trusted: %+v", plan.ValidationLevelProvenance)
	}
	if plan.Gates[0].Provenance.Configuration.Source != DecisionSourceProfileDefault {
		t.Fatalf("valid gate origin lost after level edit: %+v", plan.Gates[0].Provenance)
	}
}

func TestCompileForRepositorySharesPlanDecisionsAndRejectsExternalOrigin(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo, ValidationLevel: spec.ValidationLevelStrict})
	if err != nil {
		t.Fatal(err)
	}
	commitProfilePolicy(t, repo)
	policy, err := spec.DecodePolicy(result.Policy)
	if err != nil {
		t.Fatal(err)
	}
	deferred := []string{"coverage"}
	fromPlan, err := Load(context.Background(), Options{Repo: repo, DeferredGates: deferred})
	if err != nil {
		t.Fatal(err)
	}
	fromProducer, err := CompileForRepository(context.Background(), repo, policy, true, deferred)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromPlan.Gates, fromProducer.Gates) || !reflect.DeepEqual(fromPlan.Guarantees, fromProducer.Guarantees) || !reflect.DeepEqual(fromPlan.DependencyEdges, fromProducer.DependencyEdges) || fromPlan.ValidationLevelProvenance != fromProducer.ValidationLevelProvenance {
		t.Fatalf("plan and producer decisions differ: plan=%+v producer=%+v", fromPlan, fromProducer)
	}
	external, err := CompileForRepository(context.Background(), repo, policy, false, deferred)
	if err != nil {
		t.Fatal(err)
	}
	if external.Gates[0].Provenance.Configuration.Source != DecisionSourcePolicyExplicit || external.ValidationLevelProvenance.Source != DecisionSourcePolicyDefault {
		t.Fatalf("external policy inherited repository provenance: %+v", external)
	}
}

func TestLargeCommittedProvenanceFallsBackWithoutLoadingBlob(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.ProvenancePath, make([]byte, maxProvenanceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	commitProfilePolicy(t, repo)
	// A small working copy must not cause an unbounded read of the large HEAD
	// blob. It is intentionally different from HEAD and must be ignored.
	if err := os.WriteFile(result.ProvenancePath, result.Provenance, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Gates[0].Provenance.Configuration.Source != DecisionSourcePolicyExplicit || plan.ValidationLevelProvenance.Source != DecisionSourcePolicyDefault {
		t.Fatalf("oversized sidecar trusted: %+v", plan)
	}
}

func TestOldManifestWithoutLevelRecordRetainsGateOrigins(t *testing.T) {
	repo := profileRepo(t)
	result, err := policyinit.Init(context.Background(), policyinit.Options{Repo: repo, Profile: policyinit.ProfileGo, ValidationLevel: spec.ValidationLevelStrict})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := policyorigin.Decode(result.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ValidationLevel = nil // Prior manifest v1 representation.
	raw, err := policyorigin.Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.ProvenancePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	commitProfilePolicy(t, repo)
	plan, err := Load(context.Background(), Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Gates[0].Provenance.Configuration.Source != DecisionSourceProfileDefault || plan.ValidationLevelProvenance != (Decision{Source: DecisionSourcePolicyDefault, Reason: "omitted_validation_level_defaults_to_strict"}) {
		t.Fatalf("old manifest did not fall back selectively: %+v", plan)
	}
}
