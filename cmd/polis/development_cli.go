package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/implementationplan"
	"github.com/MarcosAlves90/polis/v6/internal/redcapture"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func runStart(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	contract := fs.String("contract", "", "strict schema-v3 or v5 draft Change Contract outside the worktree")
	outPath := fs.String("out", "", "locked schema-v4 or v6 Change Contract output outside the worktree")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" {
		fmt.Fprintln(errOut, "usage: polis start --repo <path> [--policy <policy-v3.json>] --contract <draft-v3-or-v5.json> --out <locked-v4-or-v6.json>")
		return exitUsage
	}
	writeProgress(errOut, "text", "validate the draft contract and lock the repository baseline", "later Red and Green evidence must be bound to an exact source and policy state")
	result, err := devstart.Start(context.Background(), devstart.Options{Repo: *repo, Policy: *policy, Contract: *contract, Out: *outPath})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS START: FAIL: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(out, "POLIS START: PASS\nContract: %s\nSHA256: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", result.Path, result.SHA256, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "))
	if len(result.RetainedPaths) > 0 {
		fmt.Fprintf(out, "Retained artifacts: %s\n", strings.Join(result.RetainedPaths, ", "))
	}
	return exitPass
}

func runImplementationPlan(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("implementation-plan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	contract := fs.String("contract", "", "locked schema-v4 or v6 Change Contract outside the worktree or retained by repository mode")
	outPath := fs.String("out", "", "generated Implementation Plan JSON outside the worktree")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis implementation-plan --repo <path> [--policy <policy-v3.json>] --contract <locked-v4-or-v6.json> --out <external-plan.json> [--format text|json]")
		return exitUsage
	}
	writeProgress(errOut, *format, "derive the implementation plan from the locked contract", "each plan step must remain bound to approved scope, acceptance criteria, and baseline identity")
	result, err := implementationplan.Create(context.Background(), implementationplan.Options{Repo: *repo, Policy: *policy, Contract: *contract, Out: *outPath})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS IMPLEMENTATION-PLAN", exitValidationFailed, err)
	}
	if *format == "json" {
		payload := map[string]any{"status": "PASS", "plan_path": result.Path, "sha256": result.SHA256, "plan": result.Plan}
		if len(result.RetainedPaths) > 0 {
			payload["retained_paths"] = result.RetainedPaths
		}
		writeJSON(out, payload)
	} else {
		strategy := "Red -> Green"
		if result.Plan.Strategy == spec.ImplementationPlanStrategyGreenGreen {
			strategy = "Green -> Green"
		}
		requirements := make(map[string]struct{})
		criteria := make(map[string]struct{})
		for _, step := range result.Plan.Steps {
			for _, id := range step.Requirements {
				requirements[id] = struct{}{}
			}
			for _, id := range step.AcceptanceCriteria {
				criteria[id] = struct{}{}
			}
		}
		fmt.Fprintf(out, "POLIS IMPLEMENTATION PLAN: PASS\nStrategy: %s\nRequirements: %d\nAcceptance criteria: %d\nSteps: %d\n", strategy, len(requirements), len(criteria), len(result.Plan.Steps))
		for _, step := range result.Plan.Steps {
			fmt.Fprintf(out, "%s  %-14s %q\n", step.ID, strings.ToUpper(string(step.Kind)), step.Objective)
			if len(step.Requirements) > 0 || len(step.AcceptanceCriteria) > 0 || len(step.ContractProofs) > 0 || len(step.ProjectGates) > 0 {
				fmt.Fprintf(out, "  requirements: %s; acceptance criteria: %s; proofs: %s; project gates: %s\n", strings.Join(step.Requirements, ", "), strings.Join(step.AcceptanceCriteria, ", "), strings.Join(step.ContractProofs, ", "), strings.Join(step.ProjectGates, ", "))
			}
		}
		fmt.Fprintf(out, "Plan: %s\nSHA256: %s\n", result.Path, result.SHA256)
		if len(result.RetainedPaths) > 0 {
			fmt.Fprintf(out, "Retained artifacts: %s\n", strings.Join(result.RetainedPaths, ", "))
		}
	}
	return exitPass
}

func runCheckRedScope(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(checkRedScopeCommand, flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", repoHelp)
	contract := fs.String("contract", "", "locked Red-to-Green Change Contract outside the worktree or retained by repository mode")
	format := fs.String("format", "text", outputFormatHelp)
	var paths argvFlag
	fs.Var(&paths, "path", "proposed repository-relative Red probe file path; repeat for each file")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *contract == "" || len(paths) == 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis check-red-scope [--repo <path>] --contract <locked-v4-or-v6.json> --path <file> [--path <file> ...] [--format text|json]")
		return exitUsage
	}
	writeProgress(errOut, *format, "compare proposed Red paths with the locked change scope", "regression proof must not authorize files outside the Change Contract")
	result, err := redcapture.CheckScope(context.Background(), redcapture.ScopeOptions{Repo: *repo, Contract: *contract, Paths: paths})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS CHECK-RED-SCOPE", exitValidationFailed, err)
	}
	if *format == "json" {
		if result.Status != spec.StatusPass {
			report := diagnostic.Report{Code: "POLIS_RED_PROBE_SCOPE_VIOLATION", Category: "scope", Stage: "Red probe scope check", ObservedCause: "proposed_path_rejected", AffectedOperations: []string{"check-red-scope"}, NotRun: []string{"Red proof capture"}, Remediation: "review_change_and_test_scope"}
			writeJSON(out, struct {
				redcapture.ScopeResult
				Diagnostic diagnostic.Report `json:"diagnostic"`
			}{result, report})
		} else {
			writeJSON(out, result)
		}
	} else {
		writeScopeCheckText(out, result)
	}
	if result.Status == spec.StatusPass {
		return exitPass
	}
	return exitValidationFailed
}

func writeScopeCheckText(out io.Writer, result redcapture.ScopeResult) {
	fmt.Fprintf(out, "POLIS CHECK-RED-SCOPE: %s\n", result.Status)
	fmt.Fprintln(out, "Proposed path check only; capture-red validates the actual patch.")
	fmt.Fprintf(out, "Allowed test paths (test_scope.allowed_paths): %s\n", strings.Join(result.AllowedTestPaths, ", "))
	fmt.Fprintf(out, "Allowed change paths (scope.allowed_paths): %s\n", strings.Join(result.AllowedChangePaths, ", "))
	fmt.Fprintf(out, "Red probe executed: %t\nProof captured: %t\n", result.ProbeExecuted, result.ProofCaptured)
	for _, path := range result.Paths {
		fmt.Fprintf(out, "Path: %s | Status: %s", path.Path, path.Status)
		if path.Status == spec.StatusFail {
			fmt.Fprintf(out, " | Rule: %s | Reason: %s", path.Rule, path.Reason)
		}
		fmt.Fprintln(out)
	}
}

func runCaptureRed(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(captureRedCommand, flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	contract := fs.String("contract", "", "Change Contract JSON outside the worktree or retained by repository mode")
	implementationPlanPath := fs.String("implementation-plan", "", "optional contract-bound Implementation Plan JSON outside the worktree or retained by repository mode")
	outPath := fs.String("out", "", "output regression patch outside the worktree")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis capture-red --repo <path> --contract <change.json> [--implementation-plan <plan.json>] [--format text|json] --out <regression.patch>")
		return exitUsage
	}
	writeProgress(errOut, *format, "snapshot the failing source state as Red proof", "strict development requires reproducible pre-implementation evidence tied to the locked baseline")
	result, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: *repo, Contract: *contract, ImplementationPlan: *implementationPlanPath, Out: *outPath})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS CAPTURE-RED", exitUsage, err)
	}
	if *format == "json" {
		payload := map[string]any{"status": "PASS", "patch": result.Path, "sha256": result.SHA256}
		if len(result.RetainedPaths) > 0 {
			payload["retained_paths"] = result.RetainedPaths
		}
		writeJSON(out, payload)
	} else {
		fmt.Fprintf(out, "POLIS CAPTURE-RED: PASS\nPatch: %s\nSHA256: %s\n", result.Path, result.SHA256)
		if len(result.RetainedPaths) > 0 {
			fmt.Fprintf(out, "Retained artifacts: %s\n", strings.Join(result.RetainedPaths, ", "))
		}
	}
	return exitPass
}
