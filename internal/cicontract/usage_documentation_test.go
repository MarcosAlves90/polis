package cicontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestREADMEIsLandingPage(t *testing.T) {
	root := repositoryRoot(t)

	readmeRaw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(readmeRaw)
	for _, fragment := range []string{
		"# POLIS V6",
		"## Why POLIS",
		"## Installation",
		"go install github.com/MarcosAlves90/polis/v6/cmd/polis@latest",
		"go install github.com/MarcosAlves90/polis/v6/cmd/polis@v6.3.0",
		"[installation guide](docs/installation.md)",
		"[Usage and V6 workflows](docs/usage.md)",
		"[latest release](https://github.com/MarcosAlves90/polis/releases/latest)",
		"polis doctor",
	} {
		if !strings.Contains(readme, fragment) {
			t.Errorf("README missing landing-page fragment %q", fragment)
		}
	}
	for _, fragment := range []string{
		"## Command reference",
		"## V6 contract summary",
		"## Strict SDD/TDD",
		"## Exit categories",
		"## GitHub Releases",
	} {
		if strings.Contains(readme, fragment) {
			t.Errorf("README should not contain internal reference section %q", fragment)
		}
	}

	usageRaw, err := os.ReadFile(filepath.Join(root, "docs", "usage.md"))
	if err != nil {
		t.Fatalf("read usage guide: %v", err)
	}
	usage := string(usageRaw)
	for _, fragment := range []string{
		"# Using POLIS V6",
		"## Command reference",
		"## Canonical V6 delivery flow",
		"## Project Policy and validation",
		"## V6 contract summary",
		"## Strict SDD/TDD",
		"## Exit categories",
	} {
		if !strings.Contains(usage, fragment) {
			t.Errorf("usage guide missing internal reference section %q", fragment)
		}
	}
}
