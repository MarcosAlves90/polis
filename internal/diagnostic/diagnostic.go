package diagnostic

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const MaxProcessOutputBytes = 4 << 10

type PathContributor struct {
	Path      string `json:"path"`
	BlobBytes uint64 `json:"blob_bytes"`
}

type Command struct {
	Gate            string   `json:"gate"`
	Argv            []string `json:"argv"`
	Cwd             string   `json:"cwd"`
	Status          string   `json:"status"`
	ExitCode        int      `json:"exit_code"`
	DurationMS      int64    `json:"duration_ms"`
	StdoutContext   string   `json:"stdout_context"`
	StderrContext   string   `json:"stderr_context"`
	StdoutBytes     int64    `json:"stdout_bytes"`
	StderrBytes     int64    `json:"stderr_bytes"`
	StdoutSHA256    string   `json:"stdout_sha256"`
	StderrSHA256    string   `json:"stderr_sha256"`
	StdoutTruncated bool     `json:"stdout_truncated"`
	StderrTruncated bool     `json:"stderr_truncated"`
}

type Report struct {
	Stage        string            `json:"stage"`
	Condition    string            `json:"condition"`
	Expected     map[string]any    `json:"expected,omitempty"`
	Actual       map[string]any    `json:"actual,omitempty"`
	Paths        []string          `json:"paths,omitempty"`
	Contributors []PathContributor `json:"contributors,omitempty"`
	GateStatuses map[string]string `json:"gate_statuses,omitempty"`
	Command      *Command          `json:"command,omitempty"`
	Commands     []Command         `json:"commands,omitempty"`
	NotRun       []string          `json:"not_run,omitempty"`
}

type Error struct {
	Summary string
	Report  Report
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	summary := strings.TrimSpace(e.Summary)
	if summary == "" && e.Cause != nil {
		summary = e.Cause.Error()
	}
	details := e.Report.Inline()
	if summary == "" {
		return details
	}
	if details == "" {
		return summary
	}
	return summary + ": " + details
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func As(err error) (*Error, bool) {
	var diagnostic *Error
	if !errors.As(err, &diagnostic) || diagnostic == nil {
		return nil, false
	}
	return diagnostic, true
}

func (r Report) FormatText() string {
	var out strings.Builder
	if r.Stage != "" {
		fmt.Fprintf(&out, "Stage: %s\n", r.Stage)
	}
	if r.Condition != "" {
		fmt.Fprintf(&out, "Condition: %s\n", r.Condition)
	}
	if len(r.Expected) > 0 {
		fmt.Fprintf(&out, "Expected: %s\n", formatValues(r.Expected))
	}
	if len(r.Actual) > 0 {
		fmt.Fprintf(&out, "Actual: %s\n", formatValues(r.Actual))
	}
	if len(r.Paths) > 0 {
		fmt.Fprintf(&out, "Paths: %s\n", strings.Join(r.Paths, ", "))
	}
	if len(r.Contributors) > 0 {
		out.WriteString("Contributing paths:\n")
		for _, contributor := range r.Contributors {
			fmt.Fprintf(&out, "  %s (%d blob bytes)\n", contributor.Path, contributor.BlobBytes)
		}
	}
	if len(r.GateStatuses) > 0 {
		ids := make([]string, 0, len(r.GateStatuses))
		for id := range r.GateStatuses {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, id+"="+r.GateStatuses[id])
		}
		fmt.Fprintf(&out, "Gate statuses: %s\n", strings.Join(parts, ", "))
	}
	commands := r.Commands
	if len(commands) == 0 && r.Command != nil {
		commands = []Command{*r.Command}
	}
	for _, command := range commands {
		if command.Gate != "" {
			fmt.Fprintf(&out, "Failed gate command: %s (%s)\n", command.Gate, command.Status)
		}
		fmt.Fprintf(&out, "Command: %s\n", formatArgv(command.Argv))
		fmt.Fprintf(&out, "Working directory: %s\n", command.Cwd)
		fmt.Fprintf(&out, "Process status: %s\nExit code: %d\nDuration: %d ms\n", command.Status, command.ExitCode, command.DurationMS)
		fmt.Fprintf(&out, "Process output bytes: stdout=%d stderr=%d\n", command.StdoutBytes, command.StderrBytes)
		fmt.Fprintf(&out, "Stdout context: %s\n", displayOutput(command.StdoutContext))
		fmt.Fprintf(&out, "Stderr context: %s\n", displayOutput(command.StderrContext))
		if command.StdoutTruncated || command.StderrTruncated {
			fmt.Fprintf(&out, "Process output truncated: stdout=%t stderr=%t\n", command.StdoutTruncated, command.StderrTruncated)
		}
	}
	if len(r.NotRun) > 0 {
		fmt.Fprintf(&out, "Not run: %s\n", strings.Join(r.NotRun, ", "))
	}
	return out.String()
}

func (r Report) Inline() string {
	parts := make([]string, 0, 5)
	if r.Stage != "" {
		parts = append(parts, "stage="+r.Stage)
	}
	if r.Condition != "" {
		parts = append(parts, "condition="+r.Condition)
	}
	if len(r.Expected) > 0 {
		parts = append(parts, "expected{"+formatValues(r.Expected)+"}")
	}
	if len(r.Actual) > 0 {
		parts = append(parts, "actual{"+formatValues(r.Actual)+"}")
	}
	if len(r.Paths) > 0 {
		parts = append(parts, "paths=["+strings.Join(r.Paths, ", ")+"]")
	}
	return strings.Join(parts, "; ")
}

func BoundProcessOutput(raw string) (string, bool) {
	raw = strings.ToValidUTF8(raw, "�")
	truncated := len(raw) > MaxProcessOutputBytes
	if truncated {
		limit := MaxProcessOutputBytes
		for limit > 0 && !isRuneBoundary(raw, limit) {
			limit--
		}
		raw = raw[:limit]
	}
	var safe strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			safe.WriteRune('�')
			continue
		}
		safe.WriteRune(r)
	}
	return safe.String(), truncated
}

func isRuneBoundary(value string, index int) bool {
	return index == len(value) || index == 0 || value[index]&0xc0 != 0x80
}

func formatValues(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		var rendered string
		switch typed := value.(type) {
		case string:
			rendered = typed
		case []string:
			rendered = "[" + strings.Join(typed, ", ") + "]"
		default:
			encoded, err := json.Marshal(value)
			if err != nil {
				rendered = fmt.Sprint(value)
			} else {
				rendered = string(encoded)
			}
		}
		parts = append(parts, key+"="+rendered)
	}
	return strings.Join(parts, ", ")
}

func formatArgv(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = fmt.Sprintf("%q", arg)
	}
	return strings.Join(quoted, " ")
}

func displayOutput(value string) string {
	if value == "" {
		return "(empty)"
	}
	return strings.TrimSuffix(value, "\n")
}
