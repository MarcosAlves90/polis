package implementationplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestCreateWritesDeterministicPlanOutsideCleanRepoWithoutGitResidue(t *testing.T) {
	repo, contractPath, external := createPlanRepo(t)
	before := snapshotPlanRepo(t, repo)
	firstPath := filepath.Join(external, "first.plan.json")
	first, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	firstRaw, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	wantRaw, err := json.MarshalIndent(first.Plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wantRaw = append(wantRaw, '\n')
	if !bytes.Equal(firstRaw, wantRaw) {
		t.Fatal("written plan is not the canonical deterministic encoding")
	}
	sum := sha256.Sum256(firstRaw)
	if first.Path != firstPath || first.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("result=%+v does not describe written plan", first)
	}
	second, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(external, "second.plan.json")})
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstRaw, secondRaw) || first.SHA256 != second.SHA256 {
		t.Fatal("identical inputs did not produce byte-identical plans")
	}
	if after := snapshotPlanRepo(t, repo); !reflect.DeepEqual(after, before) {
		t.Fatalf("plan creation changed repository state:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestCreateKeepsLargeContractPlanWithinMemberLimit(t *testing.T) {
	repo, contractPath, external := createPlanRepo(t)
	contractRaw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		t.Fatal(err)
	}
	contract.Kind = spec.ChangeKindBehaviorPreserving
	contract.Regression = spec.RegressionContract{Mode: spec.RegressionModeGreenGreen, Command: contract.Regression.Command}
	largeStatement := strings.Repeat("x", 600*1024)
	contract.Specification.AcceptanceCriteria[0].Statement = largeStatement
	policyRaw, err := os.ReadFile(filepath.Join(repo, ".polis", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := devlock.SnapshotWithPolicy(context.Background(), repo, contract.Specification, policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	contract.BaselineLock = &lock
	if err := contract.Validate(); err != nil {
		t.Fatalf("large locked contract is invalid: %v", err)
	}
	contractRaw, err = json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(contractRaw) >= int(spec.MaxContractMemberBytes) {
		t.Fatalf("fixture contract unexpectedly exceeds its member limit: %d", len(contractRaw))
	}
	if err := os.WriteFile(contractPath, contractRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(external, "large.plan.json")})
	if err != nil {
		t.Fatalf("create plan from valid large contract: %v", err)
	}
	planRaw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(planRaw) > spec.MaxImplementationPlanBytes {
		t.Fatalf("generated plan exceeds its member limit: got %d want <= %d", len(planRaw), spec.MaxImplementationPlanBytes)
	}
	if bytes.Contains(planRaw, []byte(largeStatement)) {
		t.Fatal("generated plan copied a large contract statement instead of referencing its ID")
	}
}

func TestEncodePlanRejectsOversize(t *testing.T) {
	plan := spec.ImplementationPlan{Steps: []spec.ImplementationPlanStep{{Objective: strings.Repeat("x", spec.MaxImplementationPlanBytes)}}}
	if _, err := encodePlan(plan); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("oversized plan was encoded: %v", err)
	}
}

func TestCreateRejectsDirtyRepositoryAndUnsafeOutput(t *testing.T) {
	t.Run("tracked worktree change", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(external, "plan.json")
		if _, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: out}); err == nil || !strings.Contains(err.Error(), "clean") {
			t.Fatalf("dirty worktree accepted: %v", err)
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatalf("dirty repository created output: stat error=%v", err)
		}
	})
	t.Run("staged change", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("staged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runPlanGit(t, repo, "add", "source.txt")
		out := filepath.Join(external, "plan.json")
		if _, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: out}); err == nil || !strings.Contains(err.Error(), "clean") {
			t.Fatalf("staged repository accepted: %v", err)
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatalf("staged repository created output: stat error=%v", err)
		}
	})
	t.Run("untracked file", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(external, "plan.json")}); err == nil || !strings.Contains(err.Error(), "clean") {
			t.Fatalf("untracked file accepted: %v", err)
		}
	})
	t.Run("output inside repository", func(t *testing.T) {
		repo, contractPath, _ := createPlanRepo(t)
		if _, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(repo, "plan.json")}); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("in-repository output accepted: %v", err)
		}
	})
	t.Run("existing output", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		out := filepath.Join(external, "plan.json")
		original := []byte("keep this file")
		if err := os.WriteFile(out, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: out}); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("existing output accepted: %v", err)
		}
		got, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatalf("existing output changed: %q err=%v", got, err)
		}
	})
}

func TestCreateRejectsMalformedAndUnsafeInputs(t *testing.T) {
	t.Run("missing required inputs", func(t *testing.T) {
		if _, err := Create(context.Background(), Options{}); err == nil || !strings.Contains(err.Error(), "repo, contract, and out are required") {
			t.Fatalf("missing required inputs accepted: %v", err)
		}
	})
	t.Run("repository is not a Git worktree", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		_, err := Create(context.Background(), Options{Repo: external, Contract: contractPath, Out: filepath.Join(external, "plan.json")})
		if err == nil || !strings.Contains(err.Error(), "not a Git worktree") {
			t.Fatalf("non-repository accepted: %v", err)
		}
		if _, err := os.Stat(repo); err != nil {
			t.Fatalf("fixture repository disappeared: %v", err)
		}
	})
	t.Run("contract inside worktree", func(t *testing.T) {
		repo, _, external := createPlanRepo(t)
		_, err := Create(context.Background(), Options{Repo: repo, Contract: filepath.Join(repo, "source.txt"), Out: filepath.Join(external, "plan.json")})
		if err == nil || !strings.Contains(err.Error(), "outside target worktree") {
			t.Fatalf("in-worktree contract accepted: %v", err)
		}
	})
	t.Run("missing external contract", func(t *testing.T) {
		repo, _, external := createPlanRepo(t)
		_, err := Create(context.Background(), Options{Repo: repo, Contract: filepath.Join(external, "missing.json"), Out: filepath.Join(external, "plan.json")})
		if err == nil || !strings.Contains(err.Error(), "load Change Contract") {
			t.Fatalf("missing contract accepted: %v", err)
		}
	})
	t.Run("malformed external contract", func(t *testing.T) {
		repo, _, external := createPlanRepo(t)
		contractPath := filepath.Join(external, "malformed.json")
		if err := os.WriteFile(contractPath, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(external, "plan.json")})
		if err == nil || !strings.Contains(err.Error(), "invalid Change Contract") {
			t.Fatalf("malformed contract accepted: %v", err)
		}
	})
	t.Run("draft contract", func(t *testing.T) {
		repo, _, external := createPlanRepo(t)
		contract := planContract(spec.StrictChangeContractSchemaVersion, spec.ChangeKindFeature)
		raw, err := json.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		contractPath := filepath.Join(external, "draft.json")
		if err := os.WriteFile(contractPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: filepath.Join(external, "plan.json")})
		if err == nil || !strings.Contains(err.Error(), "requires locked Change Contract") {
			t.Fatalf("draft contract accepted: %v", err)
		}
	})
	t.Run("symlinked output resolves inside worktree", func(t *testing.T) {
		repo, contractPath, external := createPlanRepo(t)
		alias := filepath.Join(external, "repo-alias")
		if err := os.Symlink(repo, alias); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(alias, "plan.json")
		_, err := Create(context.Background(), Options{Repo: repo, Contract: contractPath, Out: out})
		if err == nil || !strings.Contains(err.Error(), "outside target worktree") {
			t.Fatalf("symlinked in-worktree output accepted: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(repo, "plan.json")); !os.IsNotExist(err) {
			t.Fatalf("symlinked output created a file in the repository: %v", err)
		}
	})
}

func TestWritePlanRejectsParentThatIsAFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePlan(filepath.Join(parent, "plan.json"), []byte("{}\n")); err == nil {
		t.Fatal("plan written under a regular file")
	}
}

func TestCreateRejectsPolicyMismatchWithoutCreatingOutput(t *testing.T) {
	repo, contractPath, external := createPlanRepo(t)
	otherPolicy := filepath.Join(external, "other-policy.json")
	var policy spec.Policy
	if err := json.Unmarshal(planPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	policy.ValidationLevel = spec.ValidationLevelMinimal
	reason := "disabled for mismatched fixture"
	policy.Gates[0] = spec.GatePolicy{ID: "test.complete", Mode: spec.GateModeNotApplicable, Reason: &reason}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	policyRaw = append(policyRaw, '\n')
	if err := os.WriteFile(otherPolicy, policyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(external, "plan.json")
	if _, err := Create(context.Background(), Options{Repo: repo, Policy: otherPolicy, Contract: contractPath, Out: out}); err == nil || !strings.Contains(err.Error(), "policy_sha256 mismatch") {
		t.Fatalf("mismatched policy accepted: %v", err)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("policy mismatch created output: stat error=%v", err)
	}
}

func TestLoadOptionalPlanAndRejectsInWorktreeInput(t *testing.T) {
	repo, contractPath, _ := createPlanRepo(t)
	contractRaw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		t.Fatal(err)
	}
	raw, plan, err := Load(repo, "", contract, contractRaw)
	if err != nil || raw != nil || plan != nil {
		t.Fatalf("omitted plan loaded as raw=%q plan=%+v err=%v", raw, plan, err)
	}
	if _, _, err := Load(repo, filepath.Join(repo, "source.txt"), contract, contractRaw); err == nil || !strings.Contains(err.Error(), "outside target worktree") {
		t.Fatalf("in-worktree plan accepted: %v", err)
	}
}

func TestEffectiveExecutionOrderRejectsDuplicateAndMissingGates(t *testing.T) {
	duplicate := policyplan.Plan{
		EnabledGates:   []string{"test.complete"},
		ExecutionOrder: []string{"test.complete", "test.complete"},
	}
	if _, err := EffectiveExecutionOrder(duplicate); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatalf("duplicate execution-order gate accepted: %v", err)
	}
	missing := policyplan.Plan{EnabledGates: []string{"test.complete"}}
	if _, err := EffectiveExecutionOrder(missing); err == nil || !strings.Contains(err.Error(), "does not cover") {
		t.Fatalf("incomplete execution order accepted: %v", err)
	}
}

type planRepoState struct {
	head    string
	index   []byte
	status  string
	refs    string
	config  string
	objects []string
}

func snapshotPlanRepo(t *testing.T, repo string) planRepoState {
	t.Helper()
	status := runPlanGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	indexPath := runPlanGit(t, repo, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo, indexPath)
	}
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read repository index: %v", err)
	}
	state := planRepoState{
		head:   runPlanGit(t, repo, "rev-parse", "HEAD"),
		index:  indexBytes,
		status: status,
		refs:   runPlanGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"),
		config: runPlanGit(t, repo, "config", "--local", "--null", "--list"),
	}
	root := filepath.Join(repo, ".git", "objects")
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			state.objects = append(state.objects, filepath.ToSlash(name))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(state.objects)
	return state
}

func createPlanRepo(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	external := filepath.Join(root, "external")
	if err := os.MkdirAll(filepath.Join(repo, ".polis"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	policyRaw := planPolicyBytes(t)
	if err := os.WriteFile(filepath.Join(repo, ".polis", "policy.json"), policyRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPlanGit(t, repo, "init", "-q")
	runPlanGit(t, repo, "add", ".")
	runPlanGit(t, repo, "-c", "user.name=POLIS", "-c", "user.email=polis@example.invalid", "commit", "-qm", "baseline")
	contract := planContract(spec.LockedChangeContractSchemaVersion, spec.ChangeKindFeature)
	lock, err := devlock.SnapshotWithPolicy(context.Background(), repo, contract.Specification, policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	contract.BaselineLock = &lock
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	contractRaw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(external, "locked-contract.json")
	if err := os.WriteFile(contractPath, contractRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, contractPath, external
}

func planPolicyBytes(t *testing.T) []byte {
	t.Helper()
	reason := "not applicable in planning fixture"
	gates := make([]spec.GatePolicy, 0, len(spec.ProjectGateOrder))
	for _, id := range spec.ProjectGateOrder {
		if id == "test.complete" {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeCommand, Command: &spec.CommandSpec{
				Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutSeconds: 60,
				Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit},
			}})
		} else {
			gates = append(gates, spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason})
		}
	}
	raw, err := json.Marshal(spec.Policy{SchemaVersion: spec.PolicySchemaVersion, ValidationLevel: spec.ValidationLevelStandard, Gates: gates})
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func runPlanGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
