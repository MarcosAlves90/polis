package cicontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSonarQubeContract(t *testing.T) {
	root := repositoryRoot(t)

	propertiesRaw, err := os.ReadFile(filepath.Join(root, "sonar-project.properties"))
	if err != nil {
		t.Fatalf("read sonar-project.properties: %v", err)
	}
	properties := string(propertiesRaw)
	for _, fragment := range []string{
		"sonar.projectKey=polis",
		"sonar.go.coverage.reportPaths=.polis/coverage.out",
		"sonar.test.inclusions=**/*_test.go",
		"sonar.exclusions=**/*_test.go",
	} {
		if !strings.Contains(properties, fragment) {
			t.Errorf("Sonar configuration missing required fragment %q", fragment)
		}
	}
	if strings.Contains(properties, "sonar.token") {
		t.Error("Sonar token must not be stored in sonar-project.properties")
	}

	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "sonar-local.sh"))
	if err != nil {
		t.Fatalf("read local Sonar script: %v", err)
	}
	script := string(scriptRaw)
	for _, fragment := range []string{
		"SONAR_TOKEN must be set",
		"SONAR_HOST_URL:-http://localhost:9000",
		"go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out",
		"sonar-scanner",
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("local Sonar script missing required fragment %q", fragment)
		}
	}
}

func TestChangelogContract(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	changelog := string(raw)
	for _, fragment := range []string{
		"# Changelog",
		"Keep a Changelog",
		"Semantic Versioning",
		"## [Unreleased]",
		"## [4.0.0] - 2026-08-29",
		"github.com/MarcosAlves90/polis/compare/v4.0.0...HEAD",
	} {
		if !strings.Contains(changelog, fragment) {
			t.Errorf("CHANGELOG missing required fragment %q", fragment)
		}
	}
}
