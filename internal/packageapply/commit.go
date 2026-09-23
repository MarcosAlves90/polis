package packageapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
)

type artifactCommitSnapshot struct {
	head      string
	ref       string
	indexTree string
}

type artifactCommitOperations struct {
	createObject     func(context.Context, string, string, string, string) (string, error)
	updateRef        func(context.Context, string, string, string, string) error
	updateIndex      func(context.Context, string, string) error
	verifyRepository func(context.Context, string, string, string) error
}

func validateArtifactCommitIdentity(ctx context.Context, repo string) error {
	for _, identity := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		value, err := gitutil.Output(ctx, repo, nil, nil, "var", identity)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", identity, err)
		}
		if value == "" {
			return fmt.Errorf("resolve %s: Git returned an empty identity", identity)
		}
	}
	return nil
}

func captureArtifactCommitSnapshot(ctx context.Context, repo, expectedHead string) (artifactCommitSnapshot, error) {
	head, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	if head != expectedHead {
		return artifactCommitSnapshot{}, fmt.Errorf("HEAD changed before commit: got %s want %s", head, expectedHead)
	}
	status, err := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("inspect worktree before commit: %w", err)
	}
	if status != "" {
		return artifactCommitSnapshot{}, errors.New("worktree changed before commit")
	}
	ref, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("resolve current HEAD reference: %w", err)
	}
	if ref != "HEAD" && !strings.HasPrefix(ref, "refs/heads/") {
		return artifactCommitSnapshot{}, fmt.Errorf("artifact-backed commits require a local branch or detached HEAD, got %q", ref)
	}
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("write current index tree: %w", err)
	}
	headTree, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", expectedHead+"^{tree}")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("resolve current HEAD tree: %w", err)
	}
	if indexTree != headTree {
		return artifactCommitSnapshot{}, fmt.Errorf("index tree %s does not match HEAD tree %s", indexTree, headTree)
	}
	indexPath, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--git-path", "index")
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("resolve Git index path: %w", err)
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo, indexPath)
	}
	indexPath, err = filepath.Abs(indexPath)
	if err != nil {
		return artifactCommitSnapshot{}, fmt.Errorf("resolve absolute Git index path: %w", err)
	}
	if _, err := os.Stat(indexPath + ".lock"); err == nil {
		return artifactCommitSnapshot{}, errors.New("Git index is locked by another process")
	} else if !errors.Is(err, os.ErrNotExist) {
		return artifactCommitSnapshot{}, fmt.Errorf("inspect Git index lock: %w", err)
	}
	return artifactCommitSnapshot{head: head, ref: ref, indexTree: indexTree}, nil
}

func createArtifactCommit(ctx context.Context, repo string, snapshot artifactCommitSnapshot, targetTree, message string, patch []byte) (string, error) {
	return createArtifactCommitWithOperations(ctx, repo, snapshot, targetTree, message, patch, artifactCommitOperations{})
}

func createArtifactCommitWithOperations(ctx context.Context, repo string, snapshot artifactCommitSnapshot, targetTree, message string, patch []byte, operations artifactCommitOperations) (string, error) {
	if operations.createObject == nil {
		operations.createObject = createArtifactCommitObject
	}
	if operations.updateRef == nil {
		operations.updateRef = updateLocalRef
	}
	if operations.updateIndex == nil {
		operations.updateIndex = updateArtifactCommitIndex
	}
	if operations.verifyRepository == nil {
		operations.verifyRepository = verifyCommittedRepository
	}
	tempDir, err := os.MkdirTemp("", "polis-commit-index-*")
	if err != nil {
		return "", rollbackArtifactCommit(ctx, repo, snapshot, targetTree, patch, "", false, false, fmt.Errorf("create temporary commit index: %w", err))
	}
	tempIndex := filepath.Join(tempDir, "index")
	cleanupTemp := func() error {
		if tempDir == "" {
			return nil
		}
		path := tempDir
		tempDir = ""
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove temporary commit index: %w", err)
		}
		return nil
	}
	fail := func(cause error, commit string, refChanged, indexChanged bool) (string, error) {
		if cleanupErr := cleanupTemp(); cleanupErr != nil {
			cause = errors.Join(cause, cleanupErr)
		}
		return "", rollbackArtifactCommit(ctx, repo, snapshot, targetTree, patch, commit, refChanged, indexChanged, cause)
	}
	indexEnv := gitEnvironmentWithIndex(tempIndex)
	if _, err := gitutil.Bytes(ctx, repo, indexEnv, nil, "read-tree", snapshot.head); err != nil {
		return fail(fmt.Errorf("prepare temporary commit index: %w", err), "", false, false)
	}
	if _, err := gitutil.Bytes(ctx, repo, indexEnv, bytes.NewReader(patch), "apply", "--cached", "-"); err != nil {
		return fail(fmt.Errorf("apply validated payload to temporary commit index: %w", err), "", false, false)
	}
	stagedTree, err := gitutil.Output(ctx, repo, indexEnv, nil, "write-tree")
	if err != nil {
		return fail(fmt.Errorf("write candidate commit tree: %w", err), "", false, false)
	}
	if stagedTree != targetTree {
		return fail(fmt.Errorf("candidate commit tree mismatch: got %s want %s", stagedTree, targetTree), "", false, false)
	}
	commit, err := operations.createObject(ctx, repo, stagedTree, snapshot.head, message)
	if err != nil {
		return fail(fmt.Errorf("create commit object: %w", err), "", false, false)
	}
	if commit == "" {
		return fail(errors.New("create commit object returned an empty object ID"), "", false, false)
	}
	if err := verifyArtifactCommit(ctx, repo, commit, snapshot.head, targetTree, []byte(message)); err != nil {
		return fail(fmt.Errorf("verify candidate commit: %w", err), commit, false, false)
	}
	if err := cleanupTemp(); err != nil {
		return fail(err, commit, false, false)
	}
	if err := verifyArtifactCommitPreRefState(ctx, repo, snapshot, targetTree); err != nil {
		return fail(fmt.Errorf("consumer state changed before commit ref update: %w", err), commit, false, false)
	}
	if err := operations.updateRef(ctx, repo, snapshot.ref, commit, snapshot.head); err != nil {
		current, readErr := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--verify", snapshot.ref)
		if readErr == nil && current == commit {
			return fail(fmt.Errorf("update HEAD reference: %w", err), commit, true, false)
		}
		if readErr != nil {
			err = errors.Join(err, fmt.Errorf("inspect HEAD reference after failed update: %w", readErr))
		} else if current != snapshot.head {
			err = errors.Join(err, fmt.Errorf("HEAD reference changed concurrently to %s", current))
		}
		return fail(fmt.Errorf("update HEAD reference: %w", err), commit, false, false)
	}
	if err := verifyArtifactCommitIndexBeforeUpdate(ctx, repo, snapshot, commit); err != nil {
		return fail(fmt.Errorf("consumer state changed before commit index update: %w", err), commit, true, false)
	}
	if err := operations.updateIndex(ctx, repo, commit); err != nil {
		return fail(fmt.Errorf("update real index to committed tree: %w", err), commit, true, true)
	}
	if err := verifyArtifactCommitRef(ctx, repo, snapshot.ref, commit); err != nil {
		return fail(fmt.Errorf("verify committed HEAD reference: %w", err), commit, true, true)
	}
	if err := operations.verifyRepository(ctx, repo, commit, targetTree); err != nil {
		return fail(fmt.Errorf("verify committed repository state: %w", err), commit, true, true)
	}
	return commit, nil
}

func createArtifactCommitObject(ctx context.Context, repo, tree, parent, message string) (string, error) {
	output, err := gitutil.Bytes(ctx, repo, nil, bytes.NewReader([]byte(message)), "-c", "commit.gpgsign=false", "commit-tree", tree, "-p", parent, "-F", "-")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func updateArtifactCommitIndex(ctx context.Context, repo, commit string) error {
	_, err := gitutil.Bytes(ctx, repo, nil, nil, "read-tree", commit)
	return err
}

func verifyArtifactCommitPreRefState(ctx context.Context, repo string, snapshot artifactCommitSnapshot, targetTree string) error {
	head, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	if head != snapshot.head {
		return fmt.Errorf("HEAD changed: got %s want %s", head, snapshot.head)
	}
	ref, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve current HEAD reference: %w", err)
	}
	if ref != snapshot.ref {
		return fmt.Errorf("HEAD reference changed: got %s want %s", ref, snapshot.ref)
	}
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("inspect real index: %w", err)
	}
	if indexTree != snapshot.indexTree {
		return fmt.Errorf("real index changed: got tree %s want %s", indexTree, snapshot.indexTree)
	}
	worktreeTree, err := workingTreeID(ctx, repo, snapshot.head)
	if err != nil {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	if worktreeTree != targetTree {
		return fmt.Errorf("worktree tree changed: got %s want %s", worktreeTree, targetTree)
	}
	return nil
}

func verifyArtifactCommitIndexBeforeUpdate(ctx context.Context, repo string, snapshot artifactCommitSnapshot, commit string) error {
	if err := verifyArtifactCommitRef(ctx, repo, snapshot.ref, commit); err != nil {
		return err
	}
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("inspect real index: %w", err)
	}
	if indexTree != snapshot.indexTree {
		return fmt.Errorf("real index changed: got tree %s want %s", indexTree, snapshot.indexTree)
	}
	return nil
}

func verifyArtifactCommitRef(ctx context.Context, repo, expectedRef, commit string) error {
	ref, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--symbolic-full-name", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve current HEAD reference: %w", err)
	}
	if ref != expectedRef {
		return fmt.Errorf("HEAD reference changed: got %s want %s", ref, expectedRef)
	}
	value, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--verify", expectedRef)
	if err != nil {
		return fmt.Errorf("resolve expected HEAD reference %s: %w", expectedRef, err)
	}
	if value != commit {
		return fmt.Errorf("HEAD reference %s points to %s want %s", expectedRef, value, commit)
	}
	return nil
}

func rollbackArtifactCommit(ctx context.Context, repo string, snapshot artifactCommitSnapshot, targetTree string, patch []byte, commit string, refChanged, indexChanged bool, cause error) error {
	if commit != "" {
		current, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--verify", snapshot.ref)
		if err != nil {
			return fmt.Errorf("%v; rollback failed to inspect HEAD reference: %w", cause, err)
		}
		switch current {
		case commit:
			if err := updateLocalRef(ctx, repo, snapshot.ref, snapshot.head, commit); err != nil {
				return fmt.Errorf("%v; rollback failed to restore HEAD reference: %w", cause, err)
			}
		case snapshot.head:
		default:
			return fmt.Errorf("%v; rollback stopped because HEAD reference changed concurrently to %s", cause, current)
		}
	} else if refChanged {
		return fmt.Errorf("%v; rollback cannot verify the created commit", cause)
	}
	head, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("%v; rollback failed to resolve HEAD: %w", cause, err)
	}
	if head != snapshot.head {
		return fmt.Errorf("%v; rollback stopped because checked out HEAD changed to %s", cause, head)
	}
	if indexChanged {
		if err := restoreArtifactIndex(ctx, repo, snapshot, targetTree); err != nil {
			return fmt.Errorf("%v; rollback failed to restore index: %w", cause, err)
		}
	}
	currentTree, err := workingTreeID(ctx, repo, snapshot.head)
	if err != nil {
		return fmt.Errorf("%v; rollback failed to inspect worktree: %w", cause, err)
	}
	baseTree, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", snapshot.head+"^{tree}")
	if err != nil {
		return fmt.Errorf("%v; rollback failed to inspect original tree: %w", cause, err)
	}
	switch currentTree {
	case targetTree:
		if err := reversePatch(ctx, repo, patch); err != nil {
			return fmt.Errorf("%v; rollback failed to reverse applied payload: %w", cause, err)
		}
	case baseTree:
	default:
		return fmt.Errorf("%v; rollback stopped because worktree tree is %s, expected applied tree %s or original tree %s", cause, currentTree, targetTree, baseTree)
	}
	head, headErr := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	ref, refErr := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "--symbolic-full-name", "HEAD")
	indexTree, indexErr := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	status, statusErr := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if headErr != nil || refErr != nil || indexErr != nil || statusErr != nil || head != snapshot.head || ref != snapshot.ref || indexTree != snapshot.indexTree || status != "" {
		return fmt.Errorf("%v; rollback state verification failed: HEAD=%q ref=%q index=%q status=%q errors=%v", cause, head, ref, indexTree, status, errors.Join(headErr, refErr, indexErr, statusErr))
	}
	return cause
}

func restoreArtifactIndex(ctx context.Context, repo string, snapshot artifactCommitSnapshot, targetTree string) error {
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("inspect index before restore: %w", err)
	}
	switch indexTree {
	case snapshot.indexTree:
		return nil
	case targetTree:
	default:
		return fmt.Errorf("refusing to overwrite concurrently changed index tree %s", indexTree)
	}
	if _, err := gitutil.Bytes(ctx, repo, nil, nil, "read-tree", snapshot.head); err != nil {
		return fmt.Errorf("read original HEAD tree into index: %w", err)
	}
	indexTree, err = gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("verify restored index: %w", err)
	}
	if indexTree != snapshot.indexTree {
		return fmt.Errorf("restored index tree=%s want %s", indexTree, snapshot.indexTree)
	}
	return nil
}

func verifyArtifactCommit(ctx context.Context, repo, commit, parent, targetTree string, message []byte) error {
	raw, err := gitutil.Bytes(ctx, repo, nil, nil, "cat-file", "commit", commit)
	if err != nil {
		return err
	}
	separator := bytes.Index(raw, []byte("\n\n"))
	if separator < 0 {
		return errors.New("commit object has no header separator")
	}
	var gotTree string
	var parents []string
	for _, line := range strings.Split(string(raw[:separator]), "\n") {
		if strings.HasPrefix(line, "tree ") {
			gotTree = strings.TrimPrefix(line, "tree ")
		}
		if strings.HasPrefix(line, "parent ") {
			parents = append(parents, strings.TrimPrefix(line, "parent "))
		}
	}
	if gotTree != targetTree {
		return fmt.Errorf("commit tree=%s want %s", gotTree, targetTree)
	}
	if len(parents) != 1 || parents[0] != parent {
		return fmt.Errorf("commit parents=%v want exactly [%s]", parents, parent)
	}
	if !bytes.Equal(raw[separator+2:], message) {
		return errors.New("commit message differs from packaged bytes")
	}
	return nil
}

func verifyCommittedRepository(ctx context.Context, repo, commit, targetTree string) error {
	head, err := gitutil.Output(ctx, repo, nil, nil, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	if head != commit {
		return fmt.Errorf("HEAD=%s want commit %s", head, commit)
	}
	indexTree, err := gitutil.Output(ctx, repo, nil, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("write committed index tree: %w", err)
	}
	if indexTree != targetTree {
		return fmt.Errorf("index tree=%s want %s", indexTree, targetTree)
	}
	status, err := gitutil.Output(ctx, repo, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect committed worktree: %w", err)
	}
	if status != "" {
		return fmt.Errorf("committed worktree is not clean: %q", status)
	}
	return nil
}

func updateLocalRef(ctx context.Context, repo, ref, newValue, oldValue string) error {
	hooksPath, err := os.MkdirTemp("", "polis-empty-hooks-*")
	if err != nil {
		return fmt.Errorf("create empty hooks directory: %w", err)
	}
	_, updateErr := gitutil.Bytes(ctx, repo, nil, nil, "-c", "core.hooksPath="+hooksPath, "update-ref", "--no-deref", "-m", "POLIS artifact-backed apply", ref, newValue, oldValue)
	cleanupErr := os.RemoveAll(hooksPath)
	if updateErr != nil || cleanupErr != nil {
		return errors.Join(updateErr, wrapCleanupError("remove empty hooks directory", cleanupErr))
	}
	return nil
}

func wrapCleanupError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", label, err)
}

func gitEnvironmentWithIndex(indexPath string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if ok && key == "GIT_INDEX_FILE" {
			continue
		}
		env = append(env, value)
	}
	return append(env, "GIT_INDEX_FILE="+indexPath)
}
