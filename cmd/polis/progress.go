package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/internal/policyexec"

	"github.com/MarcosAlves90/polis/v6/spec"
)

type commandProgress struct {
	action string
	why    string
}

var commandProgressByName = map[string]commandProgress{
	"doctor":              {action: "check POLIS runtime prerequisites", why: "later commands depend on a usable Git runtime and local cryptographic support"},
	"init":                {action: "prepare a Project Policy", why: "POLIS needs an explicit policy before it can plan or validate project gates"},
	"plan":                {action: "compile the effective Project Policy", why: "the operator needs the exact validation plan before execution"},
	"gates":               {action: "prepare configured gate execution", why: "the effective Project Policy determines which checks are required"},
	"workspace":           {action: "process a workspace operation", why: "workspace validation and status commands compare current source state with locked POLIS evidence"},
	"start":               {action: "lock the Change Contract baseline", why: "strict development requires an immutable baseline before implementation evidence is accepted"},
	"implementation-plan": {action: "generate the contract-bound implementation plan", why: "the plan must be tied to the locked change scope and baseline"},
	"status":              {action: "derive the current POLIS workflow status", why: "persisted evidence must be reconciled before recommending the next action"},
	checkRedScopeCommand:  {action: "check the proposed Red proof scope", why: "regression evidence must stay inside the locked Change Contract scope"},
	captureRedCommand:     {action: "capture the required Red proof", why: "strict Red-to-Green development needs reproducible failing-state evidence before implementation"},
	"build":               {action: "build the POLIS delivery artifact", why: "producer validation and packaging must prove the locked change before delivery"},
	"verify":              {action: "verify the POLIS delivery artifact", why: "artifact integrity and policy evidence must be checked before trusting its metadata"},
	"inspect":             {action: "inspect validated artifact metadata", why: "the operator requested traceability and delivery metadata from a verified artifact"},
	"preflight":           {action: "preflight the artifact against the target repository", why: "consumer compatibility and gates must pass before any repository mutation"},
	"apply":               {action: "apply the validated artifact", why: "the target repository should change only after artifact and consumer validation succeed"},
	"sign":                {action: "sign the delivery artifact", why: "a detached signature binds the artifact bytes to the selected signing key"},
	"export":              {action: "export a self-contained POLIS runtime bundle", why: "offline consumers need the runtime and required resources packaged together"},
}

func writeProgress(w io.Writer, format, action, why string) {
	if w == nil {
		return
	}
	if format == "json" {
		writeJSON(w, map[string]any{"type": "progress", "action": action, "why": why})
		return
	}
	fmt.Fprintf(w, "POLIS PROGRESS: action=%q why=%q\n", action, why)
}

// progressLifecycleEvent is presentation-only. It must not enter POLIS evidence.
func progressLifecycleEvent(w io.Writer, format, event, scope, id, action, why, status string, durationMS int64) {
	if w == nil {
		return
	}
	if format == "json" {
		record := map[string]any{"type": "progress", "event": event, "scope": scope, "id": id, "action": action, "why": why}
		if event == "completed" {
			record["status"] = status
			record["duration_ms"] = durationMS
		}
		writeJSON(w, record)
		return
	}
	if event == "completed" {
		fmt.Fprintf(w, "POLIS PROGRESS: action=%q why=%q event=%q scope=%q id=%q status=%q duration_ms=%d\n", action, why, event, scope, id, status, durationMS)
	} else {
		fmt.Fprintf(w, "POLIS PROGRESS: action=%q why=%q event=%q scope=%q id=%q\n", action, why, event, scope, id)
	}
}

func commandProgressDetails(args []string) (commandProgress, string, bool) {
	if len(args) == 0 || args[0] == "help" || hasHelpFlag(args) {
		return commandProgress{}, "", false
	}
	name := args[0]
	progress, ok := commandProgressByName[name]
	if !ok {
		return commandProgress{}, "", false
	}
	if name == "workspace" && len(args) > 1 && (args[1] == "validate" || args[1] == "status") {
		name += "." + args[1]
		progress.action = "run workspace " + args[1]
		progress.why = "the operator requested a " + args[1] + " view of the current workspace against POLIS evidence"
	}
	return progress, name, true
}

func writeCommandStartProgress(w io.Writer, args []string) {
	progress, id, ok := commandProgressDetails(args)
	if !ok {
		return
	}
	progressLifecycleEvent(w, commandProgressFormat(args), "started", "command", id, progress.action, progress.why, "", 0)
}

// commandProgressWriter completes JSON failures before their final error record.
// This preserves the established JSON Lines contract on stderr.
type commandProgressWriter struct {
	out        io.Writer
	format, id string
	progress   commandProgress
	start      time.Time
	finished   bool
}

func (w *commandProgressWriter) Write(b []byte) (int, error) { return w.out.Write(b) }

func (w *commandProgressWriter) finish(code int) {
	if w.finished {
		return
	}
	w.finished = true
	status := "FAIL"
	if code == exitPass {
		status = "PASS"
	} else if code == exitBlocked {
		status = "BLOCKED"
	}
	progressLifecycleEvent(w.out, w.format, "completed", "command", w.id, w.progress.action, w.progress.why, status, time.Since(w.start).Milliseconds())
}

func (w *commandProgressWriter) finishBeforeFailure(code int) {
	if w.format == "json" {
		w.finish(code)
	}
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func commandProgressFormat(args []string) string {
	for i := 1; i < len(args); i++ {
		if args[i] == "--format" && i+1 < len(args) && args[i+1] == "json" {
			return "json"
		}
		if args[i] == "--format=json" {
			return "json"
		}
	}
	return "text"
}

func gateStartProgress(w io.Writer, format string) func(spec.GatePolicy) {
	return func(gate spec.GatePolicy) {
		progressLifecycleEvent(w, format, "started", "gate", strings.TrimSpace(gate.ID), "run configured gate "+strings.TrimSpace(gate.ID), "the effective Project Policy selected this gate and all required prerequisites are satisfied", "", 0)
	}
}

func gateCompleteProgress(w io.Writer, format string) func(spec.GatePolicy, policyexec.Outcome, int64) {
	return func(gate spec.GatePolicy, outcome policyexec.Outcome, durationMS int64) {
		id := strings.TrimSpace(gate.ID)
		progressLifecycleEvent(w, format, "completed", "gate", id, "run configured gate "+id, "the effective Project Policy selected this gate and all required prerequisites are satisfied", string(outcome.Status), durationMS)
	}
}
