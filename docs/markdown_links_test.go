package docs

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

var (
	markdownInlineLink = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]+)\)`)
	markdownATXHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)
)

func TestUsageIndexLinksReferencePages(t *testing.T) {
	usage, err := os.ReadFile("usage.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{
		"[First change quickstart](quickstart.md)",
		"[Command reference](commands.md#agent-command-instructions)",
		"[Policy and validation](policy-and-validation.md)",
		"[Development proof](development-proof.md)",
		"[Repository retention](retention.md)",
		"[Gate execution and progress](gates-and-progress.md)",
		"[Artifact trust](artifact-trust.md)",
		"[local workspace guide](workflows/local-agent-workspace.md)",
		"[remote delivery guide](workflows/web-agent-delivery.md)",
	} {
		if !strings.Contains(string(usage), reference) {
			t.Fatalf("usage index missing page %s", reference)
		}
	}
	if lines := strings.Count(string(usage), "\n") + 1; lines > 180 {
		t.Fatalf("usage index has %d lines; maximum is 180", lines)
	}
	if strings.Contains(string(usage), "<!-- command-help:") {
		t.Fatal("usage index duplicates canonical command-help source")
	}
	commands, err := os.ReadFile("commands.md")
	if err != nil {
		t.Fatalf("canonical command source docs/commands.md: %v", err)
	}
	if got := strings.Count(string(commands), "<!-- command-help: "); got != 18 {
		t.Fatalf("canonical command-help block count=%d want=18", got)
	}
}

func TestAffectedMarkdownLinksResolve(t *testing.T) {
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		"README.md",
		"docs/usage.md",
		"docs/quickstart.md",
		"docs/commands.md",
		"docs/policy-and-validation.md",
		"docs/development-proof.md",
		"docs/retention.md",
		"docs/gates-and-progress.md",
		"docs/artifact-trust.md",
		"docs/workflows/local-agent-workspace.md",
		"docs/workflows/web-agent-delivery.md",
	}
	for _, relative := range files {
		filename := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("missing markdown page %s: %v", relative, err)
		}
		checkMarkdownLinks(t, repositoryRoot, relative, string(content))
	}
}

func checkMarkdownLinks(t *testing.T, repositoryRoot, relative, content string) {
	t.Helper()
	content = stripFencedCode(content)
	for _, match := range markdownInlineLink.FindAllStringSubmatch(content, -1) {
		destination := strings.TrimSpace(strings.Fields(match[1])[0])
		destination = strings.Trim(destination, "<>")
		parsed, err := url.Parse(destination)
		if err != nil {
			t.Errorf("%s has invalid Markdown link %q: %v", relative, destination, err)
			continue
		}
		if parsed.Scheme != "" || strings.HasPrefix(destination, "//") {
			continue
		}
		path := parsed.Path
		source := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
		target := source
		if path != "" {
			target = filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(path)))
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("%s link %q has no target %s: %v", relative, destination, target, err)
			continue
		}
		if parsed.Fragment != "" {
			fragment, err := url.PathUnescape(parsed.Fragment)
			if err != nil {
				t.Errorf("%s link %q has invalid fragment encoding: %v", relative, destination, err)
				continue
			}
			fragmentTarget := target
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				fragmentTarget = filepath.Join(target, "README.md")
			}
			fragmentContent, err := os.ReadFile(fragmentTarget)
			if err != nil || !markdownHasFragment(string(fragmentContent), fragment) {
				t.Errorf("%s link %q has no heading fragment #%s", relative, destination, fragment)
			}
		}
	}
}

func stripFencedCode(content string) string {
	var lines []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func markdownHasFragment(content, fragment string) bool {
	seen := make(map[string]int)
	for _, match := range markdownATXHeading.FindAllStringSubmatch(content, -1) {
		base := markdownHeadingSlug(match[1])
		if base == "" {
			continue
		}
		count := seen[base]
		seen[base]++
		id := base
		if count > 0 {
			id += "-" + strconv.Itoa(count)
		}
		if id == fragment {
			return true
		}
	}
	return false
}

func markdownHeadingSlug(heading string) string {
	heading = strings.ToLower(strings.TrimSpace(heading))
	var slug strings.Builder
	lastDash := false
	for _, r := range heading {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			slug.WriteRune(r)
			lastDash = false
		case r == '-' || unicode.IsSpace(r):
			if slug.Len() > 0 && !lastDash {
				slug.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(slug.String(), "-")
}
