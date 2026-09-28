package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunStatusRegression(t *testing.T) {
	repo := makeBuildRepo(t)
	enableCLIRetention(t, repo)
	var out, errOut bytes.Buffer
	if code := run([]string{"status", "--repo", repo, "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("status code=%d stderr=%s", code, errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("status JSON: %v raw=%s", err, out.String())
	}
	if payload["state"] != "empty" || payload["retention_mode"] != "repository" || payload["consistent"] != true {
		t.Fatalf("unexpected empty persisted status: %v", payload)
	}
}
