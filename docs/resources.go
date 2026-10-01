// Package docs exposes canonical operational documentation embedded in the CLI.
package docs

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed usage.md
var usageGuide string

const helpStart = "<!-- command-help: "
const helpEnd = "```\n<!-- /command-help -->"

// CommandHelpEntry derives both the command index and full instructions from
// the usage guide. No separately maintained agent-facing copy is needed.
type CommandHelpEntry struct {
	Name         string
	Usage        string
	Summary      string
	Instructions string
}

// CommandHelpEntries returns fresh entries from the immutable embedded guide.
// Invalid source documentation is a build-time programming error, not user input.
func CommandHelpEntries() []CommandHelpEntry {
	entries, err := parseCommandHelp(usageGuide)
	if err != nil {
		panic(err)
	}
	return entries
}

func parseCommandHelp(guide string) ([]CommandHelpEntry, error) {
	// Git may check out the embedded Markdown with CRLF on Windows.
	guide = strings.ReplaceAll(guide, "\r\n", "\n")
	var entries []CommandHelpEntry
	seen := make(map[string]bool)
	for {
		_, rest, found := strings.Cut(guide, helpStart)
		if !found {
			break
		}
		name, rest, found := strings.Cut(rest, " -->\n```text\n")
		if !found {
			return nil, fmt.Errorf("invalid canonical command help block header")
		}
		if name == "" || strings.ContainsAny(name, " 	\r\n<>") || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate canonical command help name %q", name)
		}
		instructions, remaining, found := strings.Cut(rest, helpEnd)
		if !found || strings.Contains(instructions, helpStart) {
			return nil, fmt.Errorf("unterminated canonical command help for %s", name)
		}
		for _, section := range []string{"Usage", "Purpose", "When to use", "Prerequisites", "Required inputs", "Workflow", "Reads/writes", "Options/defaults", "Outcomes", "Examples", "Do not use"} {
			_, value, ok := strings.Cut(instructions, section+":\n  ")
			if !ok || strings.TrimSpace(strings.SplitN(value, "\n", 2)[0]) == "" {
				return nil, fmt.Errorf("canonical command help for %s lacks %s", name, section)
			}
		}
		_, usage, _ := strings.Cut(instructions, "Usage:\n  ")
		usage = strings.SplitN(usage, "\n", 2)[0]
		if !strings.HasPrefix(usage, "polis "+name+" ") {
			return nil, fmt.Errorf("canonical command help usage does not match %s", name)
		}
		_, summary, _ := strings.Cut(instructions, "Purpose:\n  ")
		summary = strings.SplitN(summary, "\n", 2)[0]
		entries = append(entries, CommandHelpEntry{Name: name, Usage: usage, Summary: summary, Instructions: instructions})
		seen[name] = true
		guide = remaining
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("canonical command help is empty")
	}
	return entries, nil
}
