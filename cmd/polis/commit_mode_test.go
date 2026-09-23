package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
)

func TestParseApplyCommitModes(t *testing.T) {
	for _, mode := range []string{"none", "prompt", "auto"} {
		t.Run(mode, func(t *testing.T) {
			options, ok := parseApplyCLIOptions([]string{"--commit-mode", mode, "artifact.polis"}, &bytes.Buffer{})
			if !ok || options.commitMode != packageapply.CommitMode(mode) {
				t.Fatalf("options=%+v ok=%t", options, ok)
			}
		})
	}
	if _, ok := parseApplyCLIOptions([]string{"--commit-mode", "sometimes", "artifact.polis"}, &bytes.Buffer{}); ok {
		t.Fatal("invalid commit mode was accepted")
	}
}

func TestConfirmArtifactCommitRequiresTTYAndAffirmativeAnswer(t *testing.T) {
	message := "Feature: exact message\nsecond line  \n"
	targetTree := "0123456789abcdef"
	var output bytes.Buffer
	if approved, err := confirmArtifactCommit(strings.NewReader("yes\n"), &output, message, targetTree, false); err == nil || approved {
		t.Fatalf("non-terminal confirmation approved=%t err=%v", approved, err)
	}

	output.Reset()
	approved, err := confirmArtifactCommit(strings.NewReader("no\n"), &output, message, targetTree, true)
	if err != nil || approved {
		t.Fatalf("declined confirmation approved=%t err=%v", approved, err)
	}
	if !strings.Contains(output.String(), message) {
		t.Fatalf("prompt did not display exact artifact message: %q", output.String())
	}
	if !strings.Contains(output.String(), targetTree) {
		t.Fatalf("prompt did not display validated target tree %q: %q", targetTree, output.String())
	}

	approved, err = confirmArtifactCommit(strings.NewReader("YES\n"), &output, message, targetTree, true)
	if err != nil || !approved {
		t.Fatalf("affirmative confirmation approved=%t err=%v", approved, err)
	}
}

func TestApplyJSONIncludesCreatedCommit(t *testing.T) {
	var output bytes.Buffer
	message := "feat: exact artifact intent\n"
	writeApplySuccess(&output, "json", packageapply.Result{Committed: true, CommitSHA: "0123456789abcdef", CommitMessage: &message})
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["commit_sha"] != "0123456789abcdef" {
		t.Fatalf("commit_sha=%v", payload["commit_sha"])
	}
	if payload["committed"] != true || payload["commit_message"] != message {
		t.Fatalf("commit result fields=%v", payload)
	}
	if got := applyExitCode(packageapply.ErrCommitBlocked); got != exitBlocked {
		t.Fatalf("blocked commit exit=%d want %d", got, exitBlocked)
	}
}

func TestApplyJSONIncludesUncommittedIntentAndNullSHA(t *testing.T) {
	message := "feat: suggested artifact intent\n"
	var output bytes.Buffer
	writeApplySuccess(&output, "json", packageapply.Result{CommitMessage: &message})
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["committed"] != false || payload["commit_sha"] != nil || payload["commit_message"] != message {
		t.Fatalf("uncommitted result fields=%v", payload)
	}

	output.Reset()
	writeApplySuccess(&output, "json", packageapply.Result{})
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["committed"] != false || payload["commit_sha"] != nil || payload["commit_message"] != nil {
		t.Fatalf("no-intent result fields=%v", payload)
	}
}
