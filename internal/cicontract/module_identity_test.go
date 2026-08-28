package cicontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const polisV4Module = "github.com/MarcosAlves90/polis/v4"

func TestGoModuleUsesV4SemanticImportPath(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	if got, want := strings.TrimSpace(first), "module "+polisV4Module; got != want {
		t.Fatalf("module declaration=%q want=%q", got, want)
	}
}

func TestPublicInstallationUsesV4ModulePath(t *testing.T) {
	root := repositoryRoot(t)
	want := "go install " + polisV4Module + "/cmd/polis@latest"
	for _, rel := range []string{"README.md", filepath.Join("docs", "installation.md")} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing canonical v4 install command %q", rel, want)
		}
	}
}
