package cicontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallationDocumentationContract(t *testing.T) {
	root := repositoryRoot(t)

	readmeRaw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(readmeRaw)
	for _, fragment := range []string{
		"## Installation",
		"go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest",
		"[installation guide](docs/installation.md)",
		"polis doctor",
	} {
		if !strings.Contains(readme, fragment) {
			t.Errorf("README missing installation contract fragment %q", fragment)
		}
	}

	guideRaw, err := os.ReadFile(filepath.Join(root, "docs", "installation.md"))
	if err != nil {
		t.Fatalf("read installation guide: %v", err)
	}
	guide := string(guideRaw)
	for _, fragment := range []string{
		"# Installing POLIS as a terminal command",
		"## Linux",
		"## macOS",
		"## Windows (PowerShell)",
		"go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest",
		"go env GOBIN",
		"go env GOPATH",
		"export PATH=",
		"[Environment]::SetEnvironmentVariable",
		"go install ./cmd/polis",
		"## Upgrade",
		"## Remove POLIS",
		"polis doctor",
	} {
		if !strings.Contains(guide, fragment) {
			t.Errorf("installation guide missing required fragment %q", fragment)
		}
	}
}
