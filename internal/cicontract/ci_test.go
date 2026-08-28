package cicontract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCIWorkflowContract(t *testing.T) {
	root := repositoryRoot(t)
	workflowPath := filepath.Join(root, ".github", "workflows", "ci.yml")
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	workflow := string(content)

	requiredFragments := []string{
		"name: CI",
		"push:",
		"pull_request:",
		"workflow_dispatch:",
		"contents: read",
		"ubuntu-latest",
		"macos-latest",
		"windows-latest",
		"go-version-file: go.mod",
		"go test ./...",
		"go test -race ./...",
		"go test -coverpkg=./... ./... -coverprofile=\"$RUNNER_TEMP/coverage.out\"",
		"go vet ./...",
		"go mod verify",
		"gofmt -l .",
		"go build -trimpath ./cmd/polis",
		"go run ./cmd/polis doctor",
		"threshold=80.0",
		"if (!(pct > threshold)) exit 43",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(workflow, fragment) {
			t.Errorf("CI workflow missing required contract fragment %q", fragment)
		}
	}

	if !regexp.MustCompile(`actions/checkout@[0-9a-f]{40}`).MatchString(workflow) {
		t.Error("actions/checkout must be pinned to a full commit SHA")
	}
	if !regexp.MustCompile(`actions/setup-go@[0-9a-f]{40}`).MatchString(workflow) {
		t.Error("actions/setup-go must be pinned to a full commit SHA")
	}
	if strings.Contains(workflow, "permissions: write-all") {
		t.Error("CI must not request write-all permissions")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root containing go.mod not found")
		}
		dir = parent
	}
}
