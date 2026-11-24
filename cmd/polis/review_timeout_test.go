package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestReviewProjectTimeoutHelper(t *testing.T) {
	if os.Getenv("POLIS_REVIEW_TIMEOUT_HELPER") == "1" {
		time.Sleep(5 * time.Second)
	}
}

func TestReviewProjectTimeout(t *testing.T) {
	t.Setenv("POLIS_REVIEW_TIMEOUT_HELPER", "1")
	repo := makeBuildRepo(t)
	var policy spec.Policy
	if err := json.Unmarshal(committedPolicyBytes(t), &policy); err != nil {
		t.Fatal(err)
	}
	for i := range policy.Gates {
		if policy.Gates[i].ID == "test.complete" {
			policy.Gates[i].Command.Argv = []string{os.Args[0], "-test.run=^TestReviewProjectTimeoutHelper$"}
			policy.Gates[i].Command.TimeoutSeconds = 1
			policy.Gates[i].Command.Environment = &spec.EnvironmentSpec{Mode: spec.EnvironmentModeInherit}
		}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "text"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"gates", "--repo", repo, "--policy", path, "--format", format}, &out, &errOut); code == exitPass {
			t.Fatalf("REVIEW-REGRESSION: timeout passed: %s", out.String())
		}
		want := `"timed_out":true`
		if format == "text" {
			want = "Timed out: true"
		}
		if !strings.Contains(out.String(), want) {
			t.Errorf("REVIEW-REGRESSION: %s timeout not identified: %s stderr=%s", format, out.String(), errOut.String())
		}
	}
}
