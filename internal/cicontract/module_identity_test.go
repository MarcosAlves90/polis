package cicontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const polisV5Module = "github.com/MarcosAlves90/polis/v5"

func TestGoModuleUsesV5SemanticImportPath(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	if got, want := strings.TrimSpace(first), "module "+polisV5Module; got != want {
		t.Fatalf("module declaration=%q want=%q", got, want)
	}
}

func TestPublicInstallationUsesV5ModulePath(t *testing.T) {
	root := repositoryRoot(t)
	want := "go install " + polisV5Module + "/cmd/polis@latest"
	for _, rel := range []string{"README.md", filepath.Join("docs", "installation.md")} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing canonical v5 install command %q", rel, want)
		}
	}
}
