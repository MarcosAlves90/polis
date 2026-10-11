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
		"polis help",
		"go install github.com/MarcosAlves90/polis/v6/cmd/polis@v6.11.1",
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
		"polis help [command]",
		"[--executable <file>] [--runtime <GOOS/GOARCH>]",
		"`--executable` embeds",
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

func TestREADMEAndWorkflowGuidesSeparateAgentPaths(t *testing.T) {
	root := repositoryRoot(t)
	read := func(relative string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		return string(data)
	}
	readme := read("README.md")
	for _, fragment := range []string{
		"## Choose the agent workflow",
		"docs/workflows/web-agent-delivery.md",
		"docs/workflows/local-agent-workspace.md",
	} {
		if !strings.Contains(readme, fragment) {
			t.Errorf("README missing agent workflow choice %q", fragment)
		}
	}
	usage := read("docs/usage.md")
	for _, fragment := range []string{
		"## Choose the agent workflow",
		"[remote delivery guide](workflows/web-agent-delivery.md)",
		"[local workspace guide](workflows/local-agent-workspace.md)",
	} {
		if !strings.Contains(usage, fragment) {
			t.Errorf("usage guide missing workflow navigation %q", fragment)
		}
	}
	web := read("docs/workflows/web-agent-delivery.md")
	for _, fragment := range []string{
		"Producer prerequisites",
		"source checkout",
		"POLIS runtime",
		"polis build",
		"polis verify",
		"polis preflight",
		"polis apply",
		"outside the target repository",
		"disposable",
		"offline runtime bundle",
	} {
		if !strings.Contains(web, fragment) {
			t.Errorf("remote workflow guide missing %q", fragment)
		}
	}
	local := read("docs/workflows/local-agent-workspace.md")
	for _, fragment := range []string{
		"polis workspace validate",
		"does not build a `.polis` delivery package",
		"polis preflight` and `polis apply` are not part of this workflow",
		"external report",
		"Project Policy",
	} {
		if !strings.Contains(local, fragment) {
			t.Errorf("local workflow guide missing %q", fragment)
		}
	}
}

func TestArtifactRetentionContractForbidsPackageArchives(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"docs/sdd/0041-artifact-retention.md",
		"docs/usage.md",
		"spec/POLIS-SPEC-v6.md",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		content := string(data)
		if !strings.Contains(content, "delivery package") {
			t.Errorf("%s does not define delivery-package behavior", relative)
		}
		if strings.Contains(content, "| `polis build` | `packages` | Exact generated `.polis` archive |") {
			t.Errorf("%s still authorizes package retention", relative)
		}
	}
	sdd, err := os.ReadFile(filepath.Join(root, "docs/sdd/0041-artifact-retention.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"must remain outside the target worktree",
		"Existing historical package files are never deleted or modified",
		"contracts, plans, proofs, and evidence remain eligible for repository retention",
	} {
		if !strings.Contains(string(sdd), fragment) {
			t.Errorf("retention decision missing %q", fragment)
		}
	}
}

func TestSDDIdentifiersAreUnique(t *testing.T) {
	root := repositoryRoot(t)
	directory := filepath.Join(root, "docs", "sdd")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 5 || name[4] != '-' {
			continue
		}
		id := name[:4]
		digits := true
		for _, r := range id {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		if prior, exists := seen[id]; exists {
			t.Errorf("duplicate SDD ID %s in %s and %s", id, prior, name)
		}
		seen[id] = name
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "# SDD-"+id) {
			t.Errorf("%s heading does not match its SDD ID %s", name, id)
		}
	}
}

func TestREADMEAndUsageExposeFirstChangeQuickstart(t *testing.T) {
	root := repositoryRoot(t)
	read := func(relative string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		return string(data)
	}
	readme := read("README.md")
	if !strings.Contains(readme, "[First change quickstart](docs/quickstart.md)") {
		t.Fatal("README missing first-change quickstart link")
	}
	usage := read("docs/usage.md")
	if !strings.Contains(usage, "[First change quickstart](quickstart.md)") {
		t.Fatal("usage index missing first-change quickstart link")
	}
	quickstart := read("docs/quickstart.md")
	for _, fragment := range []string{
		"# Your first POLIS change",
		"## Choose the workflow",
		"Local coding agent",
		"polis start",
		"polis capture-red",
		"polis workspace validate",
		"Remote/web handoff",
		"polis build",
		"outside the target repository",
		"[Local workspace guide]",
		"[Remote delivery guide]",
	} {
		if !strings.Contains(quickstart, fragment) {
			t.Errorf("first-change quickstart missing %q", fragment)
		}
	}
}
