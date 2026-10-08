package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIncrementalGateWorkflow(t *testing.T) {
	repo := makeBuildRepo(t)
	record := filepath.Join(t.TempDir(), "run.json")
	invoke := func(extra ...string) map[string]any {
		t.Helper()
		var out, errOut bytes.Buffer
		args := append([]string{"gates", "--repo", repo, "--format", "json", "--environment-id", "fixture-v1"}, extra...)
		if code := run(args, &out, &errOut); code != exitPass {
			t.Fatalf("incremental gate workflow unavailable: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := invoke("--gate", "test.complete", "--out-run", record)
	manifest := first["run"].(map[string]any)
	if manifest["run_id"] == "" || first["delivery_artifact_built"] != false {
		t.Fatalf("invalid run: %v", first)
	}
	assertAssurance := func(result map[string]any) {
		t.Helper()
		inputs := result["run"].(map[string]any)["inputs"].(map[string]any)
		if inputs["environment_id"] != "fixture-v1" || inputs["environment_assurance"] != "caller_asserted" {
			t.Fatalf("missing caller assertion: %v", inputs)
		}
	}
	assertAssurance(first)
	assertAction := func(result map[string]any, id, action string) {
		t.Helper()
		for _, raw := range result["run"].(map[string]any)["gates"].([]any) {
			gate := raw.(map[string]any)
			if gate["id"] == id && gate["action"] == action {
				return
			}
		}
		t.Fatalf("gate %s did not have action %s: %v", id, action, result)
	}
	assertAction(first, "test.complete", "executed")
	assertAction(first, "coverage", "omitted")
	second := invoke("--gate", "test.complete", "--reuse", record)
	assertAssurance(second)
	assertAction(second, "test.complete", "reused")
	replayed := invoke("--replay", record)
	assertAssurance(replayed)
	assertAction(replayed, "test.complete", "executed")
	if replayed["run"].(map[string]any)["run_id"] == manifest["run_id"] {
		t.Fatal("replay reused the old run identity")
	}
	var inspectJSON, inspectErr bytes.Buffer
	if code := run([]string{"gates", "--inspect-run", record, "--format", "json"}, &inspectJSON, &inspectErr); code != exitPass {
		t.Fatalf("inspect code=%d stderr=%s", code, inspectErr.String())
	}
	var inspected map[string]any
	if err := json.Unmarshal(inspectJSON.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inputs := inspected["inputs"].(map[string]any); inputs["environment_assurance"] != "caller_asserted" {
		t.Fatalf("missing saved environment assurance: %v", inputs)
	}
	for _, args := range [][]string{
		{"gates", "--inspect-run", record, "--format", "text"},
		{"gates", "--repo", repo, "--gate", "test.complete", "--reuse", record, "--environment-id", "fixture-v1", "--format", "text"},
		{"gates", "--repo", repo, "--replay", record, "--environment-id", "fixture-v1", "--format", "text"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitPass {
			t.Fatalf("text report code=%d stderr=%s", code, errOut.String())
		}
		for _, want := range []string{`Environment ID: "fixture-v1"`, `Environment assurance: "caller_asserted"`, "does not verify the environment or guarantee hermetic execution"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("text report missing %q: %s", want, out.String())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "new-source.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := invoke("--gate", "test.complete", "--reuse", record)
	assertAction(changed, "test.complete", "executed")
	raw, _ := json.Marshal(changed)
	if !strings.Contains(string(raw), "source") {
		t.Fatal("stale source category was not reported")
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"gates", "--repo", repo, "--replay", record, "--environment-id", "fixture-v1"}, &out, &errOut); code == exitPass || !strings.Contains(errOut.String(), "source") {
		t.Fatalf("changed source replay not rejected: code=%d stderr=%s", code, errOut.String())
	}
}
