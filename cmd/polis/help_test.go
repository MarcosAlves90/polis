package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var agentHelpCommands = []string{
	"help", "doctor", "init", "plan", "gates", "workspace", "start", "implementation-plan", "status",
	"check-red-scope", "capture-red", "build", "verify", "inspect", "preflight", "apply", "sign", "export",
}

func TestAgentHelpEveryCommand(t *testing.T) {
	for _, name := range agentHelpCommands {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run([]string{"help", name}, &out, &errOut); code != exitPass || errOut.Len() != 0 {
				t.Fatalf("help code=%d stderr=%q", code, errOut.String())
			}
			for _, section := range []string{
				"Usage:", "Purpose:", "When to use:", "Prerequisites:", "Required inputs:",
				"Workflow:", "Reads/writes:", "Options/defaults:", "Outcomes:", "Examples:", "Do not use:",
			} {
				if !strings.Contains(out.String(), section+"\n  ") {
					t.Errorf("agent help missing %s for %s", section, name)
				}
			}
			for _, alias := range []string{"-h", "--help"} {
				var aliasOut, aliasErr bytes.Buffer
				if code := run([]string{name, alias}, &aliasOut, &aliasErr); code != exitPass || aliasErr.Len() != 0 || aliasOut.String() != out.String() {
					t.Errorf("agent help missing identical %s %s: code=%d stdout=%q stderr=%q", name, alias, code, aliasOut.String(), aliasErr.String())
				}
			}
		})
	}
}

func TestAgentHelpCanonicalUsage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "usage.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got := strings.Count(guide, "<!-- command-help: "); got != len(agentHelpCommands) {
		t.Fatalf("agent help missing canonical command coverage: got=%d want=%d", got, len(agentHelpCommands))
	}
	for _, name := range agentHelpCommands {
		start := "<!-- command-help: " + name + " -->\n```text\n"
		_, rest, found := strings.Cut(guide, start)
		if !found {
			t.Fatalf("agent help missing canonical instructions for %s", name)
		}
		instructions, _, found := strings.Cut(rest, "```\n<!-- /command-help -->")
		if !found {
			t.Fatalf("unterminated canonical instructions for %s", name)
		}
		var out, errOut bytes.Buffer
		if code := run([]string{"help", name}, &out, &errOut); code != exitPass {
			t.Fatalf("help %s code=%d stderr=%s", name, code, errOut.String())
		}
		want := fmt.Sprintf("POLIS V6 %s\n\n%s", version, instructions)
		if out.String() != want {
			t.Errorf("canonical help drift for %s: got=%q want=%q", name, out.String(), want)
		}
	}
}

func TestAgentHelpSafetyAndOptions(t *testing.T) {
	fragments := map[string][]string{
		"help":                {"-h", "--help", "does not execute"},
		"doctor":              {"--format", "project dependencies", "4"},
		"init":                {"--repo", "--profile", "--validation-level", "--disable-gate", "--test-argv", "--coverage-argv", "--coverage-adapter", "--coverage-report", "--coverage-threshold", "80", "--dry-run", "no --format"},
		"plan":                {"--repo", "--policy", "--defer-gate", "--format", "does not execute"},
		"gates":               {"--repo", "--policy", "--contract", "--gate", "--affected", "--jobs", "--environment-id", "--reuse", "--replay", "--inspect-run", "--out-run", "--format", "not built or verified", "BLOCKED", "parallel_safe"},
		"workspace":           {"validate", "--contract", "--out-report", "workspace_validated=true", "delivery_artifact_built/verified=false", "without producing a delivery package"},
		"start":               {"--repo", "--policy", "--contract", "--out", "clean", "schema-v3", "schema-v5", "no --format"},
		"implementation-plan": {"--repo", "--policy", "--contract", "--out", "--format", "same plan bytes", "optional"},
		"status":              {"--repo", "--contract", "--format", "unavailable", "not implementation completion"},
		"check-red-scope":     {"--repo", "--contract", "--path", "--format", "actual patch", "no regression command"},
		"capture-red":         {"--repo", "--contract", "--implementation-plan", "--out", "--format", "immutable", "BLOCKED", "behavior_preserving"},
		"build":               {"--repo", "--policy", "--project", "--change", "--contract", "--regression-patch", "--implementation-plan", "--defer-gate", "--format", "--out", "consumer", "polis verify"},
		"verify":              {"--format", "--signature", "--trusted-key", "does not execute", "consumer"},
		"inspect":             {"--format", "--signature", "--trusted-key", "does not execute", "traceability"},
		"preflight":           {"--repo", "--baseline-mode", "--allow-missing-baseline-proof", "--format", "--signature", "--trusted-key", "strict", "permissive", "authorization"},
		"apply":               {"--repo", "--baseline-mode", "--allow-missing-baseline-proof", "--commit-mode", "--format", "--signature", "--trusted-key", "none", "prompt", "auto", "authorization", "rollback"},
		"sign":                {"--key", "--out", "--format", "does not validate", "private key"},
		"export":              {"--out", "--format", "--executable", "--runtime", "current executable", "polis verify"},
	}
	for _, name := range agentHelpCommands {
		var out, errOut bytes.Buffer
		if code := run([]string{"help", name}, &out, &errOut); code != exitPass {
			t.Fatalf("help %s code=%d stderr=%s", name, code, errOut.String())
		}
		for _, fragment := range fragments[name] {
			if !strings.Contains(out.String(), fragment) {
				t.Errorf("agent help missing safety/option %q for %s", fragment, name)
			}
		}
	}
}

func TestAgentHelpInvalidRequests(t *testing.T) {
	for _, args := range [][]string{{"help", "unknown"}, {"unknown", "--help"}, {"help", "apply", "extra"}, {"apply", "--help", "extra"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}
