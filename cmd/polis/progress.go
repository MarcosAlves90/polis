package main

import (
	"fmt"
	"io"
	"strings"

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

func writeCommandStartProgress(w io.Writer, args []string) {
	if len(args) == 0 {
		return
	}
	name := args[0]
	if name == "help" || hasHelpFlag(args) {
		return
	}
	progress, ok := commandProgressByName[name]
	if !ok {
		return
	}
	if name == "workspace" && len(args) > 1 && (args[1] == "validate" || args[1] == "status") {
		progress.action = "run workspace " + args[1]
		progress.why = "the operator requested a " + args[1] + " view of the current workspace against POLIS evidence"
	}
	writeProgress(w, commandProgressFormat(args), progress.action, progress.why)
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
		writeProgress(w, format, "run configured gate "+strings.TrimSpace(gate.ID), "the effective Project Policy selected this gate and all required prerequisites are satisfied")
	}
}
