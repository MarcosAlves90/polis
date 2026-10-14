package cicontract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFastLocalValidationPathIsDocumented(t *testing.T) {
	root := repositoryRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "scripts", "test-fast.sh"))
	if err != nil {
		t.Fatalf("fast local test script missing: %v", err)
	}
	validation, err := os.ReadFile(filepath.Join(root, "VALIDATION.md"))
	if err != nil {
		t.Fatalf("read validation guide: %v", err)
	}
	guide := string(validation)
	for _, fragment := range []string{
		"## Fast local feedback",
		"./scripts/test-fast.sh",
		"go test -count=1 -timeout=15m",
		"partial feedback only",
		"not a substitute for the full suite",
		"10.70s",
	} {
		if !strings.Contains(guide, fragment) {
			t.Errorf("validation guide missing fast-path contract %q", fragment)
		}
	}
	packages := []string{
		"./docs", "./docs/examples", "./spec",
		"./internal/artifactretention", "./internal/baselineproof", "./internal/changeexec",
		"./internal/changestatus", "./internal/commandexec", "./internal/devlock",
		"./internal/devstart", "./internal/diagnostic", "./internal/fileutil",
		"./internal/offlinekit", "./internal/packageverify", "./internal/pathguard",
		"./internal/policyexec", "./internal/policyinit", "./internal/policyload",
		"./internal/policyorigin", "./internal/policyplan", "./internal/signature",
	}
	for _, pkg := range packages {
		pattern := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(pkg) + `(?:$|\s)`)
		if !pattern.MatchString(string(script)) {
			t.Errorf("fast script is missing package %q", pkg)
		}
		if !strings.Contains(guide, pkg) {
			t.Errorf("validation guide command is missing package %q", pkg)
		}
	}
	actualPackages := fastTestPackages(string(script))
	if len(actualPackages) != len(packages) {
		t.Fatalf("fast script package allowlist has %d entries, want exactly %d: %v", len(actualPackages), len(packages), actualPackages)
	}
	for i, pkg := range packages {
		if actualPackages[i] != pkg {
			t.Fatalf("fast script package allowlist differs at entry %d: got %q, want %q (all: %v)", i, actualPackages[i], pkg, actualPackages)
		}
	}
	inlineExtra := strings.Replace(string(script), "exec go test -count=1 -timeout=15m \\", "exec go test -count=1 -timeout=15m ./internal/unapproved \\", 1)
	if inlineExtra == string(script) {
		t.Fatal("fast script fixture no longer contains the expected go test command")
	}
	if got := fastTestPackages(inlineExtra); len(got) != len(packages)+1 {
		t.Fatalf("fast package parser did not include an extra package on the command line: %v", got)
	}
	for _, excluded := range []string{
		"cmd/polis", "internal/cicontract", "internal/gaterun",
		"internal/implementationplan", "internal/packageapply",
		"internal/packagebuild", "internal/redcapture",
	} {
		if !strings.Contains(guide, excluded) {
			t.Errorf("validation guide must identify excluded package %q", excluded)
		}
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	for _, fullGate := range []string{
		"go test ./... -timeout=15m",
		"go test -race ./...",
		"go test -coverpkg=./... ./... -coverprofile=",
	} {
		if !strings.Contains(string(ci), fullGate) {
			t.Errorf("CI no longer contains authoritative full gate %q", fullGate)
		}
	}
}

func fastTestPackages(script string) []string {
	var packages []string
	inCommand := false
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if !inCommand && strings.HasPrefix(line, "exec go test ") {
			inCommand = true
			line = strings.TrimPrefix(line, "exec ")
		}
		if !inCommand {
			continue
		}
		for _, field := range strings.Fields(strings.TrimSuffix(line, "\\")) {
			if strings.HasPrefix(field, "./") {
				packages = append(packages, field)
			}
		}
	}
	return packages
}
