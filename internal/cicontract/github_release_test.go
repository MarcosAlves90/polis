package cicontract

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitHubReleaseDocumentationContract(t *testing.T) {
	root := repositoryRoot(t)

	assertFileContains(t, filepath.Join(root, "README.md"), []string{
		"## GitHub Releases",
		"docs/releases.md",
		"scripts/github-release.sh",
	})
	assertFileContains(t, filepath.Join(root, "docs", "releases.md"), []string{
		"# Publishing GitHub Releases",
		"--tag",
		"--publish",
		"--gh",
		"--go",
		"POLIS_GH",
		"POLIS_GO",
		"--verify-tag",
		"SHA256SUMS",
		"offline",
	})
}

func TestGitHubReleaseScriptPreflightUsesDefaultGHWithoutMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	defaultGH := fixture.writeFakeGH("gh", "default")
	fixture.prependPath(filepath.Dir(defaultGH))

	output, err := fixture.run("--tag", "v5.0.0")
	if err != nil {
		t.Fatalf("preflight failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "POLIS RELEASE PREFLIGHT: PASS") {
		t.Fatalf("missing preflight PASS output:\n%s", output)
	}
	if !strings.Contains(output, "Publication: blocked until --publish") {
		t.Fatalf("missing publish boundary:\n%s", output)
	}
	log := fixture.readGHLog()
	if !strings.Contains(log, "default|") {
		t.Fatalf("default gh was not used:\n%s", log)
	}
	if strings.Contains(log, "release create") {
		t.Fatalf("preflight performed release mutation:\n%s", log)
	}
	if fixture.tagExists("v5.0.0") {
		t.Fatal("preflight created a local tag")
	}
}

func TestGitHubReleaseScriptPreflightBuildsOfflineAsset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH)
	if err != nil {
		t.Fatalf("offline asset preflight failed: %v\n%s", err, output)
	}
	for _, fragment := range []string{
		"Offline runtime: linux/amd64",
		"Offline bundle: polis-v5.0.0-offline-linux-amd64.zip",
		"Assets: 2",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("offline asset evidence missing %q:\n%s", fragment, output)
		}
	}
	goLog := fixture.readGoLog()
	for _, fragment := range []string{"env GOOS", "env GOARCH", "build -trimpath -o"} {
		if !strings.Contains(goLog, fragment) {
			t.Fatalf("offline asset did not invoke Go with %q:\n%s", fragment, goLog)
		}
	}
}

func TestGitHubReleaseScriptUsesPOLISGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	envGH := fixture.writeFakeGH("env-gh", "env")
	fixture.env = append(fixture.env, "POLIS_GH="+envGH)

	output, err := fixture.run("--tag", "v5.0.0")
	if err != nil {
		t.Fatalf("preflight failed: %v\n%s", err, output)
	}
	if log := fixture.readGHLog(); !strings.Contains(log, "env|") {
		t.Fatalf("POLIS_GH was not used:\n%s", log)
	}
}

func TestGitHubReleaseScriptExplicitGoOverridesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	fixture.env = append(fixture.env, "POLIS_GO="+filepath.Join(fixture.binDir, "missing-go"))
	customGH := fixture.writeFakeGH("custom-gh", "custom")

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH, "--go", fixture.goPath)
	if err != nil {
		t.Fatalf("explicit Go preflight failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Offline bundle: polis-v5.0.0-offline-linux-amd64.zip") {
		t.Fatalf("explicit Go command was not used:\n%s", output)
	}
}

func TestGitHubReleaseScriptRejectsCrossCompiledOfflineAsset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")
	fixture.env = append(fixture.env, "POLIS_RELEASE_TEST_GOOS=darwin")

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH)
	if err == nil {
		t.Fatalf("cross-compiled offline asset was accepted:\n%s", output)
	}
	if !strings.Contains(output, "offline release target darwin/amd64 differs from Go host linux/amd64") {
		t.Fatalf("unexpected cross-compilation error:\n%s", output)
	}
	if strings.Contains(fixture.readGoLog(), "build -trimpath") {
		t.Fatal("cross-target validation happened after the build")
	}
}

func TestGitHubReleaseScriptPreflightHashesAssets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")
	asset := filepath.Join(t.TempDir(), "polis-test.tar.gz")
	if err := os.WriteFile(asset, []byte("frozen-release-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH, "--asset", asset)
	if err != nil {
		t.Fatalf("asset preflight failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Checksums:") || !strings.Contains(output, "polis-test.tar.gz") {
		t.Fatalf("asset checksum evidence missing:\n%s", output)
	}
	if strings.Contains(fixture.readGHLog(), "release create") {
		t.Fatal("asset preflight performed remote release mutation")
	}
}

func TestGitHubReleaseScriptRejectsOfflineAssetBasenameCollision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")
	asset := filepath.Join(t.TempDir(), "polis-v5.0.0-offline-linux-amd64.zip")
	if err := os.WriteFile(asset, []byte("ambiguous-release-asset"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH, "--asset", asset)
	if err == nil {
		t.Fatalf("offline asset basename collision was accepted:\n%s", output)
	}
	if !strings.Contains(output, "duplicate asset basename: polis-v5.0.0-offline-linux-amd64.zip") {
		t.Fatalf("unexpected collision error:\n%s", output)
	}
}

func TestGitHubReleaseScriptRejectsExistingRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")
	if err := os.WriteFile(fixture.ghState, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH, "--publish")
	if err == nil {
		t.Fatalf("existing release was accepted:\n%s", output)
	}
	if !strings.Contains(output, "GitHub Release already exists for tag v5.0.0") {
		t.Fatalf("unexpected existing-release error:\n%s", output)
	}
	if fixture.tagExists("v5.0.0") {
		t.Fatal("existing-release failure created a local tag")
	}
}

func TestGitHubReleaseScriptExplicitGHOverridesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	envGH := fixture.writeFakeGH("env-gh", "env")
	explicitGH := fixture.writeFakeGH("explicit-gh", "explicit")
	fixture.env = append(fixture.env, "POLIS_GH="+envGH)

	output, err := fixture.run("--tag", "v5.0.0", "--gh", explicitGH)
	if err != nil {
		t.Fatalf("preflight failed: %v\n%s", err, output)
	}
	log := fixture.readGHLog()
	if !strings.Contains(log, "explicit|") {
		t.Fatalf("explicit gh was not used:\n%s", log)
	}
	if strings.Contains(log, "env|") {
		t.Fatalf("POLIS_GH was used despite --gh override:\n%s", log)
	}
}

func TestGitHubReleaseScriptPublishPushesExactTagAndUsesVerifyTag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script is a Bash operator tool")
	}
	fixture := newReleaseScriptFixture(t)
	customGH := fixture.writeFakeGH("custom-gh", "custom")

	output, err := fixture.run("--tag", "v5.0.0", "--gh", customGH, "--publish")
	if err != nil {
		t.Fatalf("publish failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "POLIS RELEASE: PASS") {
		t.Fatalf("missing release PASS output:\n%s", output)
	}
	if !fixture.tagExists("v5.0.0") {
		t.Fatal("publish did not create local tag")
	}
	if got, want := fixture.remoteTagCommit("v5.0.0"), fixture.head(); got != want {
		t.Fatalf("remote tag target=%q want=%q", got, want)
	}
	log := fixture.readGHLog()
	if !strings.Contains(log, "release create v5.0.0") || !strings.Contains(log, "--verify-tag") {
		t.Fatalf("release create did not use explicit verified tag:\n%s", log)
	}
	if strings.Contains(log, "--clobber") {
		t.Fatalf("release flow must not clobber assets:\n%s", log)
	}
	if !strings.Contains(log, "polis-v5.0.0-offline-linux-amd64.zip") || !strings.Contains(log, "SHA256SUMS") {
		t.Fatalf("release did not upload the offline bundle and release checksums:\n%s", log)
	}
}

func assertFileContains(t *testing.T, path string, fragments []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(raw)
	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			t.Errorf("%s missing required fragment %q", path, fragment)
		}
	}
}

type releaseScriptFixture struct {
	t        *testing.T
	root     string
	remote   string
	binDir   string
	ghLog    string
	ghState  string
	ghAssets string
	goLog    string
	goPath   string
	env      []string
}

func newReleaseScriptFixture(t *testing.T) *releaseScriptFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	remote := filepath.Join(base, "remote.git")
	binDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, base, "init", "--bare", remote)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "POLIS Test")
	runGit(t, root, "config", "user.email", "polis@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "fixture")
	runGit(t, root, "branch", "-M", "main")
	runGit(t, root, "remote", "add", "origin", remote)
	runGit(t, root, "push", "-u", "origin", "main")

	fixture := &releaseScriptFixture{
		t:        t,
		root:     root,
		remote:   remote,
		binDir:   binDir,
		ghLog:    filepath.Join(base, "gh.log"),
		ghState:  filepath.Join(base, "gh.state"),
		ghAssets: filepath.Join(base, "gh.assets"),
		goLog:    filepath.Join(base, "go.log"),
		env: append(os.Environ(),
			"POLIS_RELEASE_TEST_LOG="+filepath.Join(base, "gh.log"),
			"POLIS_RELEASE_TEST_STATE="+filepath.Join(base, "gh.state"),
			"POLIS_RELEASE_TEST_ASSETS="+filepath.Join(base, "gh.assets"),
			"POLIS_RELEASE_TEST_GO_LOG="+filepath.Join(base, "go.log"),
		),
	}
	fixture.goPath = fixture.writeFakeGo()
	fixture.env = append(fixture.env, "POLIS_GO="+fixture.goPath)
	return fixture
}

func (f *releaseScriptFixture) writeFakeGo() string {
	f.t.Helper()
	path := filepath.Join(f.binDir, "fake-go")
	script := `#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$POLIS_RELEASE_TEST_GO_LOG"
if [[ "${1:-}" == "env" && "${2:-}" == "GOOS" ]]; then
  echo "${POLIS_RELEASE_TEST_GOOS:-linux}"
  exit 0
fi
if [[ "${1:-}" == "env" && "${2:-}" == "GOARCH" ]]; then
  echo "${POLIS_RELEASE_TEST_GOARCH:-amd64}"
  exit 0
fi
if [[ "${1:-}" == "env" && "${2:-}" == "GOHOSTOS" ]]; then
  echo linux
  exit 0
fi
if [[ "${1:-}" == "env" && "${2:-}" == "GOHOSTARCH" ]]; then
  echo amd64
  exit 0
fi
if [[ "${1:-}" == "build" ]]; then
  output=''
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "-o" ]]; then
      [[ $# -ge 2 ]]
      output=$2
      shift 2
    else
      shift
    fi
  done
  [[ -n "$output" ]]
  cat > "$output" <<'BINARY'
#!/usr/bin/env bash
set -eu
if [[ "${1:-}" == "export" && "${2:-}" == "--out" && $# -eq 3 ]]; then
  printf 'offline bundle fixture\n' > "$3"
  exit 0
fi
exit 1
BINARY
  chmod +x "$output"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *releaseScriptFixture) writeFakeGH(name, marker string) string {
	f.t.Helper()
	path := filepath.Join(f.binDir, name)
	script := `#!/usr/bin/env bash
set -eu
printf '%s|%s\n' '` + marker + `' "$*" >> "$POLIS_RELEASE_TEST_LOG"
if [[ "${1:-}" == "--version" ]]; then
  echo 'gh version 9.9.9-test'
  exit 0
fi
if [[ "${1:-}" == "auth" && "${2:-}" == "status" ]]; then
  exit 0
fi
if [[ "${1:-}" == "repo" && "${2:-}" == "view" ]]; then
  echo 'MarcosAlves90/polis'
  exit 0
fi
if [[ "${1:-}" == "release" && "${2:-}" == "list" ]]; then
  if [[ -f "$POLIS_RELEASE_TEST_STATE" ]]; then
    printf '%s\n' 'v5.0.0'
  fi
  exit 0
fi
if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  if [[ -f "$POLIS_RELEASE_TEST_STATE" ]]; then
    if [[ "$*" == *"--json assets"* ]]; then
      [[ -f "$POLIS_RELEASE_TEST_ASSETS" ]]
      cat "$POLIS_RELEASE_TEST_ASSETS"
    elif [[ "$*" == *"--json tagName,isDraft,isPrerelease,isImmutable,url"* ]]; then
      printf '%s\n' 'v5.0.0|false|false|false|https://example.invalid/release'
    else
      printf '%s\n' 'v5.0.0'
    fi
    exit 0
  fi
  exit 1
fi
if [[ "${1:-}" == "release" && "${2:-}" == "create" ]]; then
  : > "$POLIS_RELEASE_TEST_STATE"
  : > "$POLIS_RELEASE_TEST_ASSETS"
  skip_next=false
  for arg in "$@"; do
    if [[ "$skip_next" == true ]]; then
      skip_next=false
      continue
    fi
    case "$arg" in
      --repo|--title|--notes-file)
        skip_next=true
        continue
        ;;
      --*)
        continue
        ;;
    esac
    if [[ -f "$arg" ]]; then
      if command -v sha256sum >/dev/null 2>&1; then
        digest=$(sha256sum "$arg" | awk '{print $1}')
      else
        digest=$(shasum -a 256 "$arg" | awk '{print $1}')
      fi
      size=$(wc -c < "$arg" | tr -d '[:space:]')
      printf '%s|%s|sha256:%s\n' "$(basename "$arg")" "$size" "$digest" >> "$POLIS_RELEASE_TEST_ASSETS"
    fi
  done
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *releaseScriptFixture) prependPath(dir string) {
	f.t.Helper()
	for i, item := range f.env {
		if strings.HasPrefix(item, "PATH=") {
			f.env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(item, "PATH=")
			return
		}
	}
	f.env = append(f.env, "PATH="+dir)
}

func (f *releaseScriptFixture) run(args ...string) (string, error) {
	f.t.Helper()
	script := filepath.Join(repositoryRoot(f.t), "scripts", "github-release.sh")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = f.root
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *releaseScriptFixture) readGHLog() string {
	f.t.Helper()
	raw, err := os.ReadFile(f.ghLog)
	if err != nil {
		f.t.Fatalf("read gh log: %v", err)
	}
	return string(raw)
}

func (f *releaseScriptFixture) readGoLog() string {
	f.t.Helper()
	raw, err := os.ReadFile(f.goLog)
	if err != nil {
		f.t.Fatalf("read go log: %v", err)
	}
	return string(raw)
}

func (f *releaseScriptFixture) tagExists(tag string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/tags/"+tag)
	cmd.Dir = f.root
	return cmd.Run() == nil
}

func (f *releaseScriptFixture) remoteTagCommit(tag string) string {
	f.t.Helper()
	cmd := exec.Command("git", "ls-remote", "--tags", f.remote, "refs/tags/"+tag, "refs/tags/"+tag+"^{}")
	raw, err := cmd.Output()
	if err != nil {
		f.t.Fatal(err)
	}
	var direct string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.HasSuffix(fields[1], "^{}") {
			return fields[0]
		}
		direct = fields[0]
	}
	return direct
}

func (f *releaseScriptFixture) head() string {
	f.t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = f.root
	raw, err := cmd.Output()
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
