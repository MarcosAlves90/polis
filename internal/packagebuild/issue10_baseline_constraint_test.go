package packagebuild

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue10OversizedBaselineIsNotAGateFailure(t *testing.T) {
	repo := newV6Repo(t, true)
	largePath := filepath.Join(repo, "too-large.bin")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(int64(spec.MaxBaselineMemberBytes) + 4096); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "too-large.bin")
	runGit(t, repo, "-c", "user.name=POLIS Test", "-c", "user.email=polis@example.invalid", "commit", "-qm", "oversized locked baseline")
	contract := lockedCharacterizationContract(t, repo, "app.txt", "calc_test.go")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	_, err = Build(context.Background(), Options{Repo: repo, Project: "polis", Change: "oversized-baseline", Out: out, Contract: contract})
	if err == nil || !strings.Contains(err.Error(), "locked baseline constraint") {
		t.Fatalf("expected baseline constraint, got %v", err)
	}
	structured, ok := diagnostic.As(err)
	if !ok {
		t.Fatalf("expected structured baseline diagnostic, got %v", err)
	}
	report := structured.Report
	if report.Stage != "locked baseline constraint" || report.Expected["maximum_bytes"] != spec.MaxBaselineMemberBytes || len(report.GateStatuses) != 0 {
		t.Fatalf("baseline failure was reported as a gate result: %+v", report)
	}
	if !slices.Contains(report.NotRun, "test.complete") || !slices.Contains(report.NotRun, "coverage") || !slices.Contains(report.NotRun, "artifact packaging") {
		t.Fatalf("baseline failure did not identify unrun steps: %+v", report)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("baseline failure created artifact output: %v", err)
	}
}
