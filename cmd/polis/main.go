package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/docs"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
)

const version = "6.11.1"

const captureRedCommand = "capture-red"
const checkRedScopeCommand = "check-red-scope"

const (
	outputFormatHelp       = "output format: text or json"
	externalPolicyHelp     = "external Project Policy schema-v3 JSON outside the worktree"
	repoHelp               = "Git worktree path"
	targetRepoHelp         = "target Git worktree path"
	preflightLabel         = "POLIS PREFLIGHT"
	applyLabel             = "POLIS APPLY"
	baselineModeHelp       = "consumer baseline mode: strict, compatible, or permissive"
	overrideBaselineHelp   = "explicitly waive unavailable baseline development proof; requires permissive mode"
	commitModeHelp         = "artifact-backed commit behavior: none, prompt, or auto"
	deliveryArtifactNotice = "Gate validation does not mean a delivery artifact was built or verified."
)

const (
	exitPass             = 0
	exitUsage            = 2
	exitInvalidArtifact  = 3
	exitBlocked          = 4
	exitBaselineMismatch = 5
	exitValidationFailed = 6
	exitApplyFailed      = 7
)

var commandHelpEntries = docs.CommandHelpEntries()

func rootUsageLine() string {
	names := make([]string, 0, len(commandHelpEntries))
	for _, command := range commandHelpEntries {
		names = append(names, command.Name)
	}
	return "usage: polis <" + strings.Join(names, "|") + ">"
}

func runHelp(args []string, out, errOut io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		args = nil
	}
	if len(args) > 1 {
		fmt.Fprintln(errOut, "usage: polis help [command]")
		return exitUsage
	}
	if len(args) == 1 {
		for _, command := range commandHelpEntries {
			if command.Name != args[0] {
				continue
			}
			fmt.Fprintf(out, "POLIS V6 %s\n\n%s", version, command.Instructions)
			return exitPass
		}
		fmt.Fprintf(errOut, "unknown command %q\n", args[0])
		return exitUsage
	}

	fmt.Fprintf(out, "POLIS V6 %s\n\nUsage:\n  polis <command> [options]\n\nCommands:\n", version)
	for _, command := range commandHelpEntries {
		fmt.Fprintf(out, "  %-12s %s\n", command.Name, command.Summary)
	}
	fmt.Fprintln(out, "\nUse `polis help <command>` for syntax and agent-oriented operational instructions.")
	fmt.Fprintln(out, "Command help: `polis <command> -h` or `polis <command> --help` (no other arguments).")
	fmt.Fprintln(out, "Aliases: `polis -h` and `polis --help`.")
	return exitPass
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) (exitCode int) {
	if len(args) == 0 {
		fmt.Fprintln(errOut, rootUsageLine())
		return exitUsage
	}
	if progress, id, ok := commandProgressDetails(args); ok {
		w := &commandProgressWriter{out: errOut, format: commandProgressFormat(args), id: id, progress: progress, start: time.Now()}
		progressLifecycleEvent(errOut, w.format, "started", "command", id, progress.action, progress.why, "", 0)
		errOut = w
		defer func() { w.finish(exitCode) }()
	}
	// Resolve standalone help before parsing required inputs or executing commands.
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		for _, command := range commandHelpEntries {
			if command.Name == args[0] {
				return runHelp([]string{command.Name}, out, errOut)
			}
		}
	}
	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(args[1:], out, errOut)
	case "doctor":
		return runDoctor(args[1:], out, errOut)
	case "init":
		return runInit(args[1:], out, errOut)
	case "plan":
		return runPlan(args[1:], out, errOut)
	case "gates":
		return runGates(args[1:], out, errOut)
	case "workspace":
		return runWorkspace(args[1:], out, errOut)
	case "start":
		return runStart(args[1:], out, errOut)
	case "implementation-plan":
		return runImplementationPlan(args[1:], out, errOut)
	case "status":
		return runStatus(args[1:], out, errOut)
	case checkRedScopeCommand:
		return runCheckRedScope(args[1:], out, errOut)
	case captureRedCommand:
		return runCaptureRed(args[1:], out, errOut)
	case "verify":
		return runVerify(args[1:], out, errOut)
	case "inspect":
		return runInspect(args[1:], out, errOut)
	case "preflight":
		return runPreflight(args[1:], out, errOut)
	case "build":
		return runBuild(args[1:], out, errOut)
	case "apply":
		return runApply(args[1:], out, errOut)
	case "sign":
		return runSign(args[1:], out, errOut)
	case "export":
		return runExport(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "unknown command %q\n", args[0])
		return exitUsage
	}
}

func displayGateList(gates []string) string {
	if len(gates) == 0 {
		return "none"
	}
	return strings.Join(gates, ", ")
}

func validFormat(format string) bool { return format == "text" || format == "json" }

func writeJSON(w io.Writer, value any) {
	var encoded strings.Builder
	enc := json.NewEncoder(&encoded)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return
	}
	_, _ = io.WriteString(w, escapeJSONC1Controls(encoded.String()))
}

func escapeJSONC1Controls(encoded string) string {
	var escaped strings.Builder
	for _, r := range encoded {
		if r >= 0x80 && r <= 0x9F {
			fmt.Fprintf(&escaped, `\u%04x`, r)
		} else {
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}

// classifyFailure provides a conservative category if the underlying error did
// not carry a more precise diagnostic. Exit categories, not message text, are
// the only source of fallback classification.
func classifyFailure(label string, exitCode int, report diagnostic.Report) diagnostic.Report {
	operation := strings.TrimPrefix(strings.ToLower(label), "polis ")
	if report.Stage == "" {
		report.Stage = strings.ReplaceAll(operation, "-", "_")
	}
	if len(report.AffectedOperations) == 0 {
		report.AffectedOperations = []string{operation}
	}
	if report.Code != "" {
		return report
	}
	switch exitCode {
	case exitUsage:
		report.Code, report.Category, report.ObservedCause = "POLIS_OPERATION_REJECTED", "usage", "invalid_operation_input"
	case exitInvalidArtifact:
		report.Code, report.Category, report.ObservedCause = "POLIS_INVALID_ARTIFACT", "artifact", "artifact_validation_failed"
	case exitBlocked:
		report.Code, report.Category, report.ObservedCause = "POLIS_OPERATION_BLOCKED", "blocked", "operation_blocked"
	case exitBaselineMismatch:
		report.Code, report.Category, report.ObservedCause = "POLIS_BASELINE_MISMATCH", "baseline", "baseline_validation_failed"
	case exitValidationFailed:
		report.Code, report.Category, report.ObservedCause = "POLIS_VALIDATION_FAILED", "validation", "operation_validation_failed"
	case exitApplyFailed:
		report.Code, report.Category, report.ObservedCause = "POLIS_APPLY_FAILED", "apply", "apply_not_completed"
	default:
		report.Code, report.Category, report.ObservedCause = "POLIS_OPERATION_FAILED", "operation", "operation_failed"
	}
	return report
}

func writeFailure(w io.Writer, format, label string, code int, err error) int {
	structured, hasDiagnostic := diagnostic.As(err)
	if format == "json" {
		if writer, ok := w.(interface{ finishBeforeFailure(int) }); ok {
			writer.finishBeforeFailure(code)
		}
		payload := map[string]any{"status": "FAIL", "exit_code": code, "error": err.Error()}
		var report diagnostic.Report
		if hasDiagnostic {
			if structured.Summary != "" {
				payload["error"] = structured.Summary
			}
			report = structured.Report
		}
		payload["diagnostic"] = classifyFailure(label, code, report)
		writeJSON(w, payload)
	} else if hasDiagnostic {
		summary := structured.Summary
		if summary == "" {
			summary = err.Error()
		}
		fmt.Fprintf(w, "%s: FAIL: %s\n", label, summary)
		fmt.Fprint(w, structured.Report.FormatText())
	} else {
		fmt.Fprintf(w, "%s: FAIL: %v\n", label, err)
	}
	return code
}
