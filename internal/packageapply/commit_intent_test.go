package packageapply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
)

const commitIntentFixtureMessage = "feat(apply): preserve the artifact message\n\nsecond line with spaces  \n"

func TestBuildTransportsLockedCommitIntent(t *testing.T) {
	message := commitIntentFixtureMessage
	_, artifact, _ := repoWithCommitArtifact(t, message)

	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatalf("load built artifact: %v", err)
	}
	if pkg.Change.Commit == nil || pkg.Change.Commit.Message != message {
		t.Fatalf("commit intent=%+v want message %q", pkg.Change.Commit, message)
	}

	inspection, err := packageverify.Inspect(artifact)
	if err != nil {
		t.Fatalf("inspect built artifact: %v", err)
	}
	if inspection.Commit == nil || inspection.Commit.Message != message {
		t.Fatalf("inspection commit intent=%+v want message %q", inspection.Commit, message)
	}
}

func TestApplyAutoCommitsExactArtifactIntent(t *testing.T) {
	message := commitIntentFixtureMessage + "controls:\x1b\a\u0085 café\n"
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatalf("verify built artifact: %v", err)
	}
	inspection, err := packageverify.Inspect(artifact)
	if err != nil {
		t.Fatalf("inspect built artifact: %v", err)
	}
	if pkg.Change.Commit == nil || pkg.Change.Commit.Message != message || inspection.Commit == nil || inspection.Commit.Message != message {
		t.Fatalf("verified/inspected commit intent differs from start input: package=%+v inspection=%+v", pkg.Change.Commit, inspection.Commit)
	}
	parent := git(t, repo, "rev-parse", "HEAD")
	installCommitTripwires(t, repo)

	result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict, CommitMode: CommitModeAuto})
	if err != nil {
		t.Fatalf("apply with automatic commit: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("automatic commit mode did not return the created commit")
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != result.CommitSHA {
		t.Fatalf("HEAD=%s want created commit %s", got, result.CommitSHA)
	}
	if got := git(t, repo, "show", "-s", "--format=%P", "HEAD"); got != parent {
		t.Fatalf("commit parent=%s want %s", got, parent)
	}
	if got := git(t, repo, "rev-parse", "HEAD^{tree}"); got != targetTree {
		t.Fatalf("commit tree=%s want package target %s", got, targetTree)
	}
	if got := commitMessageFromObject(t, repo, result.CommitSHA); !bytes.Equal(got, []byte(message)) {
		t.Fatalf("commit message=%q want exact message %q", got, message)
	}
	if got := git(t, repo, "write-tree"); got != targetTree {
		t.Fatalf("index tree=%s want committed tree %s", got, targetTree)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("successful commit left worktree changes: %q", got)
	}
	if remotes := git(t, repo, "remote"); remotes != "" {
		t.Fatalf("automatic commit configured or used remotes: %q", remotes)
	}
	marker := os.Getenv("POLIS_COMMIT_TRIPWIRE")
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git hook or signing program ran during commit: marker stat error=%v", err)
	}
}

func TestApplyAutoCommitsMessageWithoutFinalNewline(t *testing.T) {
	message := "feat(apply): preserve a message without its final newline"
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	parent := git(t, repo, "rev-parse", "HEAD")

	result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict, CommitMode: CommitModeAuto})
	if err != nil {
		t.Fatalf("apply with automatic commit: %v", err)
	}
	if got := git(t, repo, "show", "-s", "--format=%P", "HEAD"); got != parent {
		t.Fatalf("commit parent=%s want %s", got, parent)
	}
	if got := git(t, repo, "rev-parse", "HEAD^{tree}"); got != targetTree {
		t.Fatalf("commit tree=%s want package target %s", got, targetTree)
	}
	if got := commitMessageFromObject(t, repo, result.CommitSHA); !bytes.Equal(got, []byte(message)) {
		t.Fatalf("commit message=%q want exact message %q", got, message)
	}
}

func TestApplyPromptDeclineLeavesRepositoryUnchanged(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	called := false

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{
		BaselineMode: BaselineModeStrict,
		CommitMode:   CommitModePrompt,
		ConfirmCommit: func(got, gotTargetTree string) (bool, error) {
			called = true
			if got != message {
				t.Fatalf("confirmation message=%q want %q", got, message)
			}
			if gotTargetTree != targetTree {
				t.Fatalf("confirmation target tree=%q want %q", gotTargetTree, targetTree)
			}
			return false, nil
		},
	})
	if !called {
		t.Fatal("prompt mode did not request confirmation")
	}
	if !errors.Is(err, ErrCommitBlocked) {
		t.Fatalf("declined commit error=%v, want ErrCommitBlocked", err)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD changed on declined prompt: got %s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index changed on declined prompt: got %s want %s", got, indexBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree changed on declined prompt: %q", got)
	}
}

func TestApplyPromptAcceptanceCommitsExactArtifactIntent(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	parent := git(t, repo, "rev-parse", "HEAD")
	confirmed := false

	result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{
		BaselineMode: BaselineModeStrict,
		CommitMode:   CommitModePrompt,
		ConfirmCommit: func(gotMessage, gotTargetTree string) (bool, error) {
			confirmed = true
			if gotMessage != message {
				t.Fatalf("confirmation message=%q want %q", gotMessage, message)
			}
			if gotTargetTree != targetTree {
				t.Fatalf("confirmation target tree=%q want %q", gotTargetTree, targetTree)
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("apply with confirmed commit: %v", err)
	}
	if !confirmed || !result.Committed || result.CommitSHA == "" {
		t.Fatalf("confirmed result=%+v confirmation_called=%t", result, confirmed)
	}
	if got := git(t, repo, "show", "-s", "--format=%P", "HEAD"); got != parent {
		t.Fatalf("commit parent=%s want %s", got, parent)
	}
	if got := git(t, repo, "rev-parse", "HEAD^{tree}"); got != targetTree {
		t.Fatalf("commit tree=%s want package target %s", got, targetTree)
	}
	if got := commitMessageFromObject(t, repo, result.CommitSHA); !bytes.Equal(got, []byte(message)) {
		t.Fatalf("commit message=%q want exact message %q", got, message)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("confirmed commit left worktree changes: %q", got)
	}
}

func TestApplyCommitIntentDefaultModeDoesNotCommit(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, _ := repoWithCommitArtifact(t, message)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	if _, err := Apply(context.Background(), artifact, repo); err != nil {
		t.Fatalf("default apply: %v", err)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("default apply created a commit: HEAD=%s want %s", got, headBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got == "" {
		t.Fatal("default apply did not leave the package payload in the worktree")
	}
}

func TestApplyCommitModesPreserveConsumerBaselinesAndRemoteRefs(t *testing.T) {
	message := commitIntentFixtureMessage
	producer, artifact, _ := repoWithCommitArtifact(t, message)
	producerBase := git(t, producer, "rev-parse", "HEAD")

	t.Run("explicit none", func(t *testing.T) {
		repo := cloneCommitTestRepo(t, producer)
		headBefore := git(t, repo, "rev-parse", "HEAD")
		result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict, CommitMode: CommitModeNone})
		if err != nil {
			t.Fatalf("apply in explicit none mode: %v", err)
		}
		if result.Committed || result.CommitSHA != "" {
			t.Fatalf("explicit none result=%+v", result)
		}
		if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
			t.Fatalf("explicit none moved HEAD: got %s want %s", got, headBefore)
		}
		if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got == "" {
			t.Fatal("explicit none did not leave the applied payload in the worktree")
		}
	})

	t.Run("compatible parent", func(t *testing.T) {
		repo := cloneCommitTestRepo(t, producer)
		configureCommitTestIdentity(t, repo)
		consumerHead := commitFile(t, repo, "consumer.txt", "consumer-only\n", "consumer descendant")

		result, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeCompatible, CommitMode: CommitModeAuto})
		if err != nil {
			t.Fatalf("compatible commit apply: %v", err)
		}
		if result.BaselineMode != BaselineModeCompatible || result.ConsumerBaseCommit != consumerHead || !result.Committed {
			t.Fatalf("compatible result=%+v want consumer base %s", result, consumerHead)
		}
		assertArtifactCommitParentTreeAndClean(t, repo, result, consumerHead)
	})

	t.Run("permissive parent", func(t *testing.T) {
		consumer := independentConsumerFromRepo(t, producer)
		consumerHead := git(t, consumer, "rev-parse", "HEAD")
		configureCommitTestIdentity(t, consumer)

		result, err := ApplyWithOptions(context.Background(), artifact, consumer, Options{BaselineMode: BaselineModePermissive, CommitMode: CommitModeAuto})
		if err != nil {
			t.Fatalf("permissive commit apply: %v", err)
		}
		if result.BaselineMode != BaselineModePermissive || result.ConsumerBaseCommit != consumerHead || !result.Committed {
			t.Fatalf("permissive result=%+v want consumer base %s", result, consumerHead)
		}
		assertArtifactCommitParentTreeAndClean(t, consumer, result, consumerHead)
		assertCommitMissing(t, consumer, producerBase)
	})

	t.Run("remote refs", func(t *testing.T) {
		repo := cloneCommitTestRepo(t, producer)
		configureCommitTestIdentity(t, repo)
		remote := filepath.Join(t.TempDir(), "remote.git")
		if err := os.MkdirAll(remote, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, remote, "init", "--bare", "-q")
		git(t, repo, "remote", "add", "sentinel", remote)
		git(t, repo, "push", "sentinel", "HEAD:refs/heads/polis-sentinel")
		before := git(t, remote, "for-each-ref", "--format=%(refname) %(objectname)")
		if before == "" {
			t.Fatal("remote sentinel ref was not created")
		}

		if _, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict, CommitMode: CommitModeAuto}); err != nil {
			t.Fatalf("apply with a configured remote: %v", err)
		}
		if after := git(t, remote, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
			t.Fatalf("remote refs changed during local apply\nbefore=%s\nafter=%s", before, after)
		}
	})
}

func cloneCommitTestRepo(t *testing.T, source string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "consumer")
	command := exec.Command("git", "clone", "--quiet", source, target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clone commit test consumer: %v\n%s", err, output)
	}
	return target
}

func assertArtifactCommitParentTreeAndClean(t *testing.T, repo string, result Result, parent string) {
	t.Helper()
	if got := git(t, repo, "show", "-s", "--format=%P", result.CommitSHA); got != parent {
		t.Fatalf("commit parent=%s want validated consumer HEAD %s", got, parent)
	}
	if got := git(t, repo, "rev-parse", result.CommitSHA+"^{tree}"); got != result.TargetTree {
		t.Fatalf("commit tree=%s want validated consumer target %s", got, result.TargetTree)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("commit left worktree changes: %q", got)
	}
}

func TestApplyMissingGitIdentityDoesNotMutateRepository(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, _ := repoWithCommitArtifact(t, message)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	clearGitIdentity(t)

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{BaselineMode: BaselineModeStrict, CommitMode: CommitModeAuto})
	if !errors.Is(err, ErrCommitBlocked) || !strings.Contains(err.Error(), "identity precondition") {
		t.Fatalf("missing identity error=%v, want pre-mutation commit block", err)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD changed after failed commit: got %s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index changed after failed commit: got %s want %s", got, indexBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree was not restored after failed commit: %q", got)
	}
}

func TestApplyCommitModeRequiresArtifactIntent(t *testing.T) {
	for _, mode := range []CommitMode{CommitModePrompt, CommitModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			repo, artifact, _ := repoWithPolicyArtifact(t, policyBytes(t))
			headBefore := git(t, repo, "rev-parse", "HEAD")
			indexBefore := git(t, repo, "write-tree")
			_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{
				BaselineMode: BaselineModeStrict,
				CommitMode:   mode,
			})
			if !errors.Is(err, ErrCommitBlocked) {
				t.Fatalf("missing artifact commit intent error=%v, want ErrCommitBlocked", err)
			}
			if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
				t.Fatalf("HEAD changed without artifact intent: got %s want %s", got, headBefore)
			}
			if got := git(t, repo, "write-tree"); got != indexBefore {
				t.Fatalf("index changed without artifact intent: got %s want %s", got, indexBefore)
			}
			if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
				t.Fatalf("worktree changed without artifact intent: %q", got)
			}
		})
	}
}

func TestApplyPromptChecksGitIdentityBeforeConfirmation(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, _ := repoWithCommitArtifact(t, message)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	clearGitIdentity(t)
	confirmed := false

	_, err := ApplyWithOptions(context.Background(), artifact, repo, Options{
		BaselineMode: BaselineModeStrict,
		CommitMode:   CommitModePrompt,
		ConfirmCommit: func(string, string) (bool, error) {
			confirmed = true
			return true, nil
		},
	})
	if !errors.Is(err, ErrCommitBlocked) || !strings.Contains(err.Error(), "identity precondition") {
		t.Fatalf("missing identity error=%v, want pre-mutation commit block", err)
	}
	if confirmed {
		t.Fatal("prompt ran before Git author and committer identity were validated")
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD changed on missing identity: got %s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index changed on missing identity: got %s want %s", got, indexBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree changed on missing identity: %q", got)
	}
}

func TestApplyCommitFailureAfterRefUpdateRestoresRepositoryState(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	snapshot, err := captureArtifactCommitSnapshot(context.Background(), repo, headBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		t.Fatalf("apply payload before commit transaction: %v", err)
	}
	indexPath := git(t, repo, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo, indexPath)
	}
	indexLock := indexPath + ".lock"
	if err := os.WriteFile(indexLock, []byte("another git operation owns the index"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitErr := createArtifactCommit(context.Background(), repo, snapshot, targetTree, message, pkg.Patch)
	if err := os.Remove(indexLock); err != nil {
		t.Fatal(err)
	}
	if commitErr == nil {
		t.Fatal("expected the locked index to fail after updating the ref")
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD not rolled back after index failure: got %s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index not rolled back after index failure: got %s want %s", got, indexBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree not rolled back after index failure: %q (transaction error %v)", got, commitErr)
	}
}

func TestApplyCommitConstructionAndFinalVerificationFailuresRollback(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	baseTree := git(t, repo, "rev-parse", "HEAD^{tree}")

	for _, testCase := range []struct {
		name       string
		operations artifactCommitOperations
	}{
		{
			name: "commit construction",
			operations: artifactCommitOperations{
				createObject: func(context.Context, string, string, string, string) (string, error) {
					return "", errors.New("injected commit-tree failure")
				},
			},
		},
		{
			name: "candidate tree mismatch",
			operations: artifactCommitOperations{
				createObject: func(ctx context.Context, repo, _, parent, message string) (string, error) {
					out, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader([]byte(message)), "commit-tree", baseTree, "-p", parent, "-F", "-")
					return strings.TrimSpace(string(out)), err
				},
			},
		},
		{
			name: "final verification",
			operations: artifactCommitOperations{
				verifyRepository: func(context.Context, string, string, string) error {
					return errors.New("injected final verification failure")
				},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot, err := captureArtifactCommitSnapshot(context.Background(), repo, headBefore)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
				t.Fatalf("apply payload before injected failure: %v", err)
			}
			_, err = createArtifactCommitWithOperations(context.Background(), repo, snapshot, targetTree, message, pkg.Patch, testCase.operations)
			if err == nil {
				t.Fatal("injected transaction failure returned success")
			}
			if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
				t.Fatalf("HEAD after rollback=%s want %s", got, headBefore)
			}
			if got := git(t, repo, "write-tree"); got != indexBefore {
				t.Fatalf("index after rollback=%s want %s", got, indexBefore)
			}
			if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
				t.Fatalf("worktree after rollback=%q (transaction error %v)", got, err)
			}
		})
	}
}

func TestApplyCommitRefCASConflictDoesNotOverwriteConcurrentRef(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	snapshot, err := captureArtifactCommitSnapshot(context.Background(), repo, headBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		t.Fatalf("apply payload before ref conflict: %v", err)
	}
	concurrentRef := ""
	operations := artifactCommitOperations{
		updateRef: func(ctx context.Context, repo, ref, candidate, expected string) error {
			concurrentOutput, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader([]byte("concurrent commit\n")), "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "-c", "commit.gpgsign=false", "commit-tree", targetTree, "-p", expected, "-F", "-")
			if err != nil {
				return err
			}
			concurrentRef = strings.TrimSpace(string(concurrentOutput))
			if err := updateLocalRef(ctx, repo, ref, concurrentRef, expected); err != nil {
				return err
			}
			return updateLocalRef(ctx, repo, ref, candidate, expected)
		},
	}
	_, err = createArtifactCommitWithOperations(context.Background(), repo, snapshot, targetTree, message, pkg.Patch, operations)
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("ref CAS conflict error=%v, want explicit concurrent-ref failure", err)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != concurrentRef {
		t.Fatalf("concurrent HEAD=%s want preserved ref %s", got, concurrentRef)
	}
}

func TestArtifactCommitCandidateUsesValidatedPatch(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	snapshot, err := captureArtifactCommitSnapshot(context.Background(), repo, headBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		t.Fatalf("apply payload before commit transaction: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "concurrent.txt"), []byte("concurrent change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var candidateTree string
	operations := artifactCommitOperations{
		createObject: func(ctx context.Context, repo, tree, parent, message string) (string, error) {
			candidateTree = tree
			return createArtifactCommitObject(ctx, repo, tree, parent, message)
		},
	}
	_, err = createArtifactCommitWithOperations(context.Background(), repo, snapshot, targetTree, message, pkg.Patch, operations)
	if err == nil {
		t.Fatal("concurrent worktree change was accepted during commit construction")
	}
	if candidateTree != targetTree {
		t.Fatalf("candidate tree=%s want tree derived from the exact validated patch %s", candidateTree, targetTree)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD moved after worktree change: got %s want %s", got, headBefore)
	}
}

func TestArtifactCommitRejectsConcurrentIndexChangeBeforeRefUpdate(t *testing.T) {
	message := commitIntentFixtureMessage
	repo, artifact, targetTree := repoWithCommitArtifact(t, message)
	configureCommitTestIdentity(t, repo)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	snapshot, err := captureArtifactCommitSnapshot(context.Background(), repo, headBefore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		t.Fatalf("apply payload before commit transaction: %v", err)
	}
	operations := artifactCommitOperations{
		createObject: func(ctx context.Context, repo, tree, parent, message string) (string, error) {
			if tree != targetTree {
				t.Fatalf("candidate tree=%s want validated target %s", tree, targetTree)
			}
			if _, err := gitutil.Bytes(ctx, repo, nil, nil, "add", "-A", "--", "."); err != nil {
				return "", err
			}
			return createArtifactCommitObject(ctx, repo, tree, parent, message)
		},
	}
	_, err = createArtifactCommitWithOperations(context.Background(), repo, snapshot, targetTree, message, pkg.Patch, operations)
	if err == nil {
		t.Fatal("concurrent real-index change was overwritten by commit mode")
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD moved after concurrent index change: got %s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != targetTree {
		t.Fatalf("concurrent index tree=%s want preserved tree %s", got, targetTree)
	}
}

func TestRollbackAppliedPatchVerifiesOriginalRepositoryState(t *testing.T) {
	repo, artifact, _ := repoWithCommitArtifact(t, commitIntentFixtureMessage)
	pkg, err := packageverify.Load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	headBefore := git(t, repo, "rev-parse", "HEAD")
	indexBefore := git(t, repo, "write-tree")
	if _, err := gitutil.Bytes(context.Background(), repo, nil, bytes.NewReader(pkg.Patch), "apply", "-"); err != nil {
		t.Fatalf("apply payload before rollback: %v", err)
	}
	if err := rollbackAppliedPatchAndVerify(context.Background(), repo, headBefore, indexBefore, pkg.Patch); err != nil {
		t.Fatalf("rollback applied payload: %v", err)
	}
	if got := git(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD after rollback=%s want %s", got, headBefore)
	}
	if got := git(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index after rollback=%s want %s", got, indexBefore)
	}
	if got := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("worktree after rollback is not clean: %q", got)
	}
}

func configureCommitTestIdentity(t *testing.T, repo string) {
	t.Helper()
	git(t, repo, "config", "user.name", "POLIS Test Consumer")
	git(t, repo, "config", "user.email", "polis-consumer@example.invalid")
}

func clearGitIdentity(t *testing.T) {
	t.Helper()
	globalConfig := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(key, "")
	}
}

func installCommitTripwires(t *testing.T, repo string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "git-mechanism-ran")
	t.Setenv("POLIS_COMMIT_TRIPWIRE", marker)
	hooksPath := git(t, repo, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksPath) {
		hooksPath = filepath.Join(repo, hooksPath)
	}
	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := []byte("#!/bin/sh\nprintf called > \"$POLIS_COMMIT_TRIPWIRE\"\nexit 97\n")
	for _, name := range []string{"pre-commit", "commit-msg", "post-commit", "reference-transaction"} {
		if err := os.WriteFile(filepath.Join(hooksPath, name), hook, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gpgProgram := filepath.Join(t.TempDir(), "fake-gpg")
	if err := os.WriteFile(gpgProgram, hook, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "commit.gpgsign", "true")
	git(t, repo, "config", "gpg.program", gpgProgram)
}

func commitMessageFromObject(t *testing.T, repo, commit string) []byte {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "cat-file", "commit", commit)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("read commit object: %v", err)
	}
	separator := bytes.Index(raw, []byte("\n\n"))
	if separator < 0 {
		t.Fatalf("commit object has no header separator: %q", raw)
	}
	return raw[separator+2:]
}
