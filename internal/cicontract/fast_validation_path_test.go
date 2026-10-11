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
