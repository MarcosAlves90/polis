package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/gaterun"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type gateCLIResult struct {
	ID              string      `json:"id"`
	Status          spec.Status `json:"status"`
	Executed        bool        `json:"executed"`
	Reason          string      `json:"reason,omitempty"`
	Action          string      `json:"action,omitempty"`
	StaleCategories []string    `json:"stale_categories,omitempty"`
}

type gatesCLIResult struct {
	Status                   spec.Status        `json:"status"`
	ValidationOnly           bool               `json:"validation_only"`
	ValidationLevel          string             `json:"validation_level"`
	EnabledGates             []string           `json:"enabled_gates"`
	DisabledGates            []string           `json:"disabled_gates"`
	ExecutedGates            []string           `json:"executed_gates"`
	GateResults              []gateCLIResult    `json:"gate_results"`
	DeliveryArtifactBuilt    bool               `json:"delivery_artifact_built"`
	DeliveryArtifactVerified bool               `json:"delivery_artifact_verified"`
	DeliveryArtifactNotice   string             `json:"delivery_artifact_notice"`
	Run                      *gaterun.Manifest  `json:"run,omitempty"`
	Diagnostic               *diagnostic.Report `json:"diagnostic,omitempty"`
}

func runGates(args []string, out, errOut io.Writer) int {
	return runRecordedGates(args, out, errOut)
}

func gatesResult(plan policyplan.Plan, execution policyexec.Result) gatesCLIResult {
	gateResults := make([]gateCLIResult, 0, len(plan.Gates))
	for _, gate := range plan.GatePolicies() {
		status, ok := execution.Gates[gate.ID]
		if !ok {
			status = spec.StatusBlocked
		}
		result := gateCLIResult{ID: gate.ID, Status: status, Executed: gateWasRun(gate.ID, status, execution)}
		if outcome, ok := execution.Outcomes[gate.ID]; ok {
			result.Reason = outcome.Reason
		}
		if gate.Reason != nil {
			result.Reason = *gate.Reason
		}
		gateResults = append(gateResults, result)
	}
	executedGates := make([]string, 0, len(plan.EnabledGates))
	for _, gateID := range plan.ExecutionOrder {
		if status, ok := execution.Gates[gateID]; ok && gateWasRun(gateID, status, execution) {
			executedGates = append(executedGates, gateID)
		}
	}
	var gateDiagnostic *diagnostic.Report
	if execution.Overall != spec.StatusPass {
		report := diagnostic.Report{
			Stage:        "project gate validation",
			Condition:    "one or more configured project gates did not pass",
			GateStatuses: make(map[string]string, len(execution.Gates)),
		}
		for gate, status := range execution.Gates {
			report.GateStatuses[gate] = string(status)
			if status == spec.StatusBlocked || status == spec.StatusDeferred {
				report.NotRun = append(report.NotRun, "gate "+gate)
			}
		}
		sort.Strings(report.NotRun)
		for gate, exec := range execution.CommandFailures {
			report.Commands = append(report.Commands, diagnostic.Command{Gate: gate, Status: string(exec.Observation.Status), Prerequisite: safeGatePrerequisite(exec.Observation.Prerequisite)})
		}
		sort.Slice(report.Commands, func(i, j int) bool { return report.Commands[i].Gate < report.Commands[j].Gate })
		report = diagnostic.ClassifyGateFailure(report)
		gateDiagnostic = &report
	}
	return gatesCLIResult{
		Diagnostic:               gateDiagnostic,
		Status:                   execution.Overall,
		ValidationOnly:           true,
		ValidationLevel:          plan.ValidationLevel,
		EnabledGates:             append([]string{}, plan.EnabledGates...),
		DisabledGates:            append([]string{}, plan.DisabledGates...),
		ExecutedGates:            executedGates,
		GateResults:              gateResults,
		DeliveryArtifactBuilt:    false,
		DeliveryArtifactVerified: false,
		DeliveryArtifactNotice:   deliveryArtifactNotice,
	}
}

// Startup diagnostics may contain environment-derived values. Expose only a
// POLIS-owned category; never copy the untrusted prerequisite detail into JSON.
func safeGatePrerequisite(raw string) string {
	for _, category := range []string{"missing executable", "missing dependency", "missing environment condition"} {
		if strings.HasPrefix(raw, category+" ") {
			return category + " (details omitted)"
		}
	}
	return ""
}

func gateWasRun(gateID string, status spec.Status, execution policyexec.Result) bool {
	if outcome, ok := execution.Outcomes[gateID]; ok {
		return outcome.Action == "executed" && outcome.Command != nil && outcome.Command.Observation.Status != spec.StatusBlocked
	}
	switch status {
	case spec.StatusPass:
		return true
	case spec.StatusFail:
		_, commandRan := execution.CommandFailures[gateID]
		return commandRan
	default:
		return false
	}
}

func writeGatesText(out io.Writer, report gatesCLIResult) {
	fmt.Fprintf(out, "POLIS GATES: %s\nValidation only: yes\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nGates executed: %s\nGate results:\n",
		report.Status, report.ValidationLevel, displayGateList(report.EnabledGates), displayGateList(report.DisabledGates), displayGateList(report.ExecutedGates))
	if len(report.GateResults) == 0 {
		fmt.Fprintln(out, "- none")
	}
	for _, gate := range report.GateResults {
		action := "executed"
		if !gate.Executed {
			action = "not executed"
		}
		if gate.Action == "reused" {
			action = "reused"
		}
		if gate.Action == "omitted" && gate.Status != spec.StatusNotApplicable {
			action = "omitted"
		}
		if gate.Action == "blocked" {
			action = "blocked"
		}
		fmt.Fprintf(out, "- Gate %s: %s (%s)", gate.ID, gate.Status, action)
		if gate.Reason != "" {
			fmt.Fprintf(out, "; reason=%s", gate.Reason)
		}
		if len(gate.StaleCategories) > 0 {
			fmt.Fprintf(out, "; stale inputs=%s", strings.Join(gate.StaleCategories, ", "))
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Delivery artifact: not built or verified")
	fmt.Fprintf(out, "%s\n", report.DeliveryArtifactNotice)
	if report.Run != nil {
		fmt.Fprintf(out, "Run: %s (current=%t, jobs=%d)\nSelection: %s\n", report.Run.RunID, report.Run.Current, report.Run.Jobs, report.Run.SelectionReason)
		writeGateEnvironmentText(out, report.Run.Inputs)
	}
}
