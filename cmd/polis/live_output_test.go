package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestIssue44CLIStreamHelper(t *testing.T) {
	if os.Getenv("ISSUE44_CLI_HELPER") == "" {
		return
	}
	switch os.Getenv("ISSUE44_CLI_HELPER") {
	case "stream":
		_, _ = os.Stdout.WriteString("ISSUE44_LIVE_STDOUT\n")
		time.Sleep(350 * time.Millisecond)
		_, _ = os.Stderr.WriteString("ISSUE44_LIVE_STDERR\n")
	case "ansi":
		_, _ = os.Stdout.Write([]byte("\x1b[31mISSUE44_ANSI_SAFE\x1b[0m\n"))
	default:
		os.Exit(9)
	}
	if marker := os.Getenv("ISSUE44_CLI_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("done"), 0o600)
	}
	os.Exit(0)
}

func issue44PolicyWithHelper(t *testing.T, mode string) string {
	t.Helper()
	raw := canonicalPolicyBytes(t)
	var policy spec.Policy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	for i := range policy.Gates {
		switch policy.Gates[i].ID {
		case "test.complete":
			policy.Gates[i].Command = &spec.CommandSpec{
				Argv:           []string{os.Args[0], "-test.run=^TestIssue44CLIStreamHelper$"},
				Cwd:            ".",
				TimeoutSeconds: 20,
			}
		case "coverage":
			reason := "disabled for live-output fixture"
			policy.Gates[i].Mode = spec.GateModeNotApplicable
			policy.Gates[i].Command = nil
			policy.Gates[i].Reason = &reason
		}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "issue44-policy.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func issue44LiveRecords(t *testing.T, raw string) []map[string]any {
	t.Helper()
	records := decodeJSONRecords(t, raw)
	for _, record := range records {
		if record["schema_version"] != "polis-live-v1" || record["run_id"] == "" {
			t.Fatalf("live record lacks schema/run identity: %v", record)
		}
	}
	return records
}

func TestIssue44GatesStreamsBeforeExitAndWritesEventsFile(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := issue44PolicyWithHelper(t, "stream")
	eventsPath := filepath.Join(t.TempDir(), "live.jsonl")
	marker := filepath.Join(t.TempDir(), "exited.marker")
	t.Setenv("ISSUE44_CLI_HELPER", "stream")
	t.Setenv("ISSUE44_CLI_MARKER", marker)
	args := []string{"gates", "--repo", repo, "--policy", policyPath, "--format", "json", "--stream-output", "--events-file", eventsPath}

	type outcome struct {
		code   int
		out    string
		errOut string
	}
	done := make(chan outcome, 1)
	go func() {
		var out, errOut bytes.Buffer
		code := run(args, &out, &errOut)
		done <- outcome{code: code, out: out.String(), errOut: errOut.String()}
	}()

	deadline := time.Now().Add(8 * time.Second)
	seenLive := false
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(eventsPath)
		if err == nil && strings.Contains(string(raw), "ISSUE44_LIVE_STDOUT") {
			seenLive = true
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Error("CLI emitted stdout chunk only after child exited")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	result := <-done
	if !seenLive {
		t.Fatalf("no live stdout in events file: result=%+v", result)
	}
	if result.code != exitPass {
		t.Fatalf("gates code=%d stdout=%s stderr=%s", result.code, result.out, result.errOut)
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(result.out), &final); err != nil || final["status"] != "PASS" {
		t.Fatalf("final stdout JSON changed: %v raw=%q", err, result.out)
	}
	if strings.Contains(result.out, "ISSUE44_LIVE_STDOUT") || strings.Contains(result.out, "ISSUE44_LIVE_STDERR") {
		t.Fatalf("raw child output leaked into stdout: %q", result.out)
	}
	records := issue44LiveRecords(t, result.errOut)
	types := map[string]int{}
	runIDs := map[string]bool{}
	for _, record := range records {
		typeValue, _ := record["type"].(string)
		types[typeValue]++
		runIDs[fmt.Sprint(record["run_id"])] = true
		if typeValue == "output" && record["data"] == nil {
			t.Fatalf("output event lacks data: %v", record)
		}
		if typeValue == "result" && record["id"] == nil {
			t.Fatalf("result event lacks gate identity: %v", record)
		}
	}
	if len(runIDs) != 1 || types["output"] == 0 || types["result"] == 0 || types["progress"] == 0 {
		t.Fatalf("missing live event types: types=%v runIDs=%v", types, runIDs)
	}
	last := records[len(records)-1]
	if last["event"] != "completed" || last["scope"] != "command" {
		t.Fatalf("final stderr record must remain command completion: %v", last)
	}
	stored, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(stored)), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil || record["type"] == "" {
			t.Fatalf("invalid events file line %q: %v", line, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(eventsPath)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("events file permissions=%v err=%v", info, err)
		}
	}
}

func TestIssue44StreamingDisabledPreservesOrdinaryOutput(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := issue44PolicyWithHelper(t, "stream")
	t.Setenv("ISSUE44_CLI_HELPER", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--format", "json"}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if strings.Contains(errOut.String(), `"type":"output"`) || strings.Contains(errOut.String(), "POLIS LIVE") {
		t.Fatalf("streaming disabled but live events emitted: %s", errOut.String())
	}
	for _, record := range decodeJSONRecords(t, errOut.String()) {
		if record["schema_version"] == "polis-live-v1" {
			t.Fatalf("streaming disabled but live schema leaked: %v", record)
		}
	}
	var final map[string]any
	if err := json.Unmarshal(out.Bytes(), &final); err != nil || final["status"] != "PASS" {
		t.Fatalf("ordinary result changed: %v raw=%q", err, out.String())
	}
}

func TestIssue44TextLiveEventsSanitizeControlSequences(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := issue44PolicyWithHelper(t, "ansi")
	t.Setenv("ISSUE44_CLI_HELPER", "ansi")
	var out, errOut bytes.Buffer
	if code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--stream-output"}, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if strings.Contains(errOut.String(), "\x1b") || !strings.Contains(errOut.String(), "ISSUE44_ANSI_SAFE") {
		t.Fatalf("unsafe control sequence in text live output: %q", errOut.String())
	}
	for _, prefix := range []string{"POLIS STAGE:", "POLIS LIVE:", "POLIS RESULT:"} {
		if !strings.Contains(errOut.String(), prefix) {
			t.Fatalf("missing %q in text live output: %s", prefix, errOut.String())
		}
	}
}

func TestIssue44EventsFileRejectsUnsafePaths(t *testing.T) {
	repo := makeBuildRepo(t)
	policyPath := issue44PolicyWithHelper(t, "stream")
	t.Setenv("ISSUE44_CLI_HELPER", "stream")
	outside := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(outside, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "events-link.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"inside worktree": filepath.Join(repo, "live.jsonl"),
		"existing file":   outside,
		"symlink":         link,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{"gates", "--repo", repo, "--policy", policyPath, "--stream-output", "--events-file", path}, &out, &errOut)
			if code != exitUsage {
				t.Fatalf("unsafe events path accepted: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
			}
			if strings.Contains(errOut.String(), "ISSUE44_LIVE_STDOUT") {
				t.Fatalf("gates ran despite unsafe events path: %s", errOut.String())
			}
		})
	}
}

func TestIssue44WorkspaceValidateStreamsStagesAndResults(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("workspace target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	args := []string{"workspace", "validate", "--repo", repo, "--contract", contract, "--format", "json", "--stream-output"}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report["workspace_validated"] != true {
		t.Fatalf("workspace result changed: %v raw=%q", err, out.String())
	}
	records := issue44LiveRecords(t, errOut.String())
	stages := map[string]bool{}
	results := 0
	for _, record := range records {
		if record["type"] == "progress" && record["scope"] == "stage" {
			stages[fmt.Sprint(record["id"])] = true
		}
		if record["type"] == "result" {
			results++
		}
	}
	for _, stage := range []string{"red_green_regression", "isolated_target_preparation", "project_gate_execution", "source_state_recheck"} {
		if !stages[stage] {
			t.Fatalf("missing stage %q: stages=%v", stage, stages)
		}
	}
	if results == 0 {
		t.Fatalf("no incremental gate results: %s", errOut.String())
	}
}

func TestIssue44BuildStreamsStagesAndResults(t *testing.T) {
	repo := makeBuildRepo(t)
	contract := lockedCLIContract(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	var out, errOut bytes.Buffer
	args := []string{"build", "--repo", repo, "--project", "gitrex", "--change", "issue44-live", "--contract", contract, "--format", "json", "--stream-output", "--out", outDir}
	if code := run(args, &out, &errOut); code != exitPass {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil || payload["status"] != "PASS" {
		t.Fatalf("build result changed: %v raw=%q", err, out.String())
	}
	records := issue44LiveRecords(t, errOut.String())
	stages := map[string]bool{}
	for _, record := range records {
		if record["type"] == "progress" && record["scope"] == "stage" {
			stages[fmt.Sprint(record["id"])] = true
		}
	}
	for _, stage := range []string{"locked_baseline_proof", "artifact_packaging"} {
		if !stages[stage] {
			t.Fatalf("missing stage %q: stages=%v", stage, stages)
		}
	}
}
