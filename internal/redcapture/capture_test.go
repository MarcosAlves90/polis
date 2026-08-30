package redcapture

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v5/spec"
)

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", repo}, args...)...)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v\n%s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "app.txt"), []byte("baseline\n"), 0o644)
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=POLIS", "-c", "user.email=x@y", "commit", "-qm", "base")
	// Red probe only: the captured patch stages a file whose diff carries a stable oracle token.
	os.WriteFile(filepath.Join(repo, "regression.txt"), []byte("BUG-RED\n"), 0o644)
	code := 1
	pass := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "HEAD"}, Cwd: ".", TimeoutSeconds: 30}
	c := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindDefect, Behavior: pass, Affected: pass, Regression: spec.RegressionContract{Mode: spec.RegressionModeRedGreen, Command: &spec.CommandSpec{Argv: []string{"git", "diff", "--cached", "--exit-code", "HEAD", "--", "regression.txt"}, Cwd: ".", TimeoutSeconds: 30}, BaselineExitCode: &code, BaselineOutputContains: []string{"BUG-RED"}}}
	b, _ := json.Marshal(c)
	cp := filepath.Join(t.TempDir(), "change.json")
	os.WriteFile(cp, b, 0o600)
	return repo, cp
}
func TestCaptureProducesValidatedPatchWithoutMutatingSource(t *testing.T) {
	repo, cp := fixture(t)
	out := filepath.Join(t.TempDir(), "red.patch")
	head := git(t, repo, "rev-parse", "HEAD")
	idx := git(t, repo, "write-tree")
	status := git(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	r, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: out})
	if e != nil {
		t.Fatal(e)
	}
	if r.SHA256 == "" {
		t.Fatal("missing hash")
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "regression.txt") {
		t.Fatalf("patch=%s", b)
	}
	if git(t, repo, "rev-parse", "HEAD") != head || git(t, repo, "write-tree") != idx || git(t, repo, "status", "--porcelain=v1", "--untracked-files=all") != status {
		t.Fatal("source mutated")
	}
}
func TestCaptureRejectsStagedAndExistingOutput(t *testing.T) {
	repo, cp := fixture(t)
	git(t, repo, "add", "regression.txt")
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(t.TempDir(), "x")}); e == nil {
		t.Fatal("expected staged rejection")
	}
	git(t, repo, "restore", "--staged", "regression.txt")
	out := filepath.Join(t.TempDir(), "x")
	os.WriteFile(out, []byte("keep"), 0o600)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: out}); e == nil {
		t.Fatal("expected collision")
	}
}
func TestCaptureRejectsContractOrOutputInsideRepo(t *testing.T) {
	repo, cp := fixture(t)
	raw, _ := os.ReadFile(cp)
	inside := filepath.Join(repo, "change.json")
	os.WriteFile(inside, raw, 0o600)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: inside, Out: filepath.Join(t.TempDir(), "x")}); e == nil {
		t.Fatal("expected contract path rejection")
	}
	os.Remove(inside)
	if _, e := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(repo, "red.patch")}); e == nil {
		t.Fatal("expected output path rejection")
	}
}

func TestCaptureRejectsInvalidInputsAndNonDefect(t *testing.T) {
	if _, err := Capture(context.Background(), Options{}); err == nil {
		t.Fatal("expected required input error")
	}
	if _, err := Capture(context.Background(), Options{Repo: t.TempDir(), Contract: "x", Out: "y"}); err == nil {
		t.Fatal("expected non-repo error")
	}
	repo, cp := fixture(t)
	if err := os.Remove(filepath.Join(repo, "regression.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: cp, Out: filepath.Join(t.TempDir(), "x")}); err == nil {
		t.Fatal("expected clean worktree error")
	}

	cmd := spec.CommandSpec{Argv: []string{"git", "rev-parse", "--verify", "HEAD"}, Cwd: ".", TimeoutSeconds: 30}
	feature := spec.ChangeContract{SchemaVersion: 1, Kind: spec.ChangeKindFeature, Behavior: cmd, Affected: cmd, Regression: spec.RegressionContract{Mode: spec.RegressionModeNotApplicable, ReasonCode: spec.RegressionReasonNotDefect}}
	raw, _ := json.Marshal(feature)
	featurePath := filepath.Join(t.TempDir(), "feature.json")
	os.WriteFile(featurePath, raw, 0o600)
	if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: featurePath, Out: filepath.Join(t.TempDir(), "y")}); err == nil {
		t.Fatal("expected non-defect rejection")
	}

	badPath := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(badPath, []byte(`{}`), 0o600)
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: badPath, Out: filepath.Join(t.TempDir(), "z")}); err == nil {
		t.Fatal("expected malformed contract rejection")
	}
}

func TestCaptureRejectsWrongRedOracle(t *testing.T) {
	repo, cp := fixture(t)
	raw, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	var c spec.ChangeContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	wrong := 9
	c.Regression.BaselineExitCode = &wrong
	raw, _ = json.Marshal(c)
	wrongPath := filepath.Join(t.TempDir(), "wrong.json")
	os.WriteFile(wrongPath, raw, 0o600)
	if _, err := Capture(context.Background(), Options{Repo: repo, Contract: wrongPath, Out: filepath.Join(t.TempDir(), "red.patch")}); err == nil {
		t.Fatal("expected oracle rejection")
	}
}

func TestReadExternalRejectsDirectoryAndOversize(t *testing.T) {
	repo := t.TempDir()
	dir := t.TempDir()
	if _, err := readExternal(repo, dir, 10); err == nil {
		t.Fatal("expected directory rejection")
	}
	p := filepath.Join(t.TempDir(), "big")
	os.WriteFile(p, []byte("123456"), 0o600)
	if _, err := readExternal(repo, p, 3); err == nil {
		t.Fatal("expected size rejection")
	}
}

func TestReadExternalRejectsPhysicalAliasIntoRepo(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "real", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "real"), alias); err != nil {
		t.Fatal(err)
	}
	insideReal := filepath.Join(repo, "change.json")
	if err := os.WriteFile(insideReal, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	insideAlias := filepath.Join(alias, "repo", "change.json")
	if _, err := readExternal(repo, insideAlias, 1024); err == nil {
		t.Fatal("expected physical alias into repo to be rejected")
	}
}
