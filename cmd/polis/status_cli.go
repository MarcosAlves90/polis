package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/MarcosAlves90/polis/v6/internal/changestatus"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
)

func runStatus(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	contract := fs.String("contract", "", "locked Change Contract; external or retained")
	implementationPlan := fs.String("implementation-plan", "", "external or retained contract-bound Implementation Plan JSON")
	regressionPatch := fs.String("regression-patch", "", "external or retained captured Red proof patch")
	report := fs.String("report", "", "external workspace-validation-v1 checkpoint report")
	packagePath := fs.String("package", "", "external or retained verified POLIS package")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis status [--repo <path>] [--policy <policy-v3.json>] [--contract <locked-contract.json>] [--implementation-plan <plan.json>] [--regression-patch <red.patch>] [--report <workspace-validation-v1.json>] [--package <artifact.polis>] [--format text|json]")
		return exitUsage
	}
	writeProgress(errOut, *format, "reconcile workflow evidence", "status must project retained or explicit contract, baseline, plan, proof, checkpoint, package, and gate evidence into one resumable workflow state")
	ctx := context.Background()
	result, err := changestatus.Derive(ctx, changestatus.Options{
		Repo: *repo, Policy: *policy, Contract: *contract, ImplementationPlan: *implementationPlan,
		RegressionPatch: *regressionPatch, Package: *packagePath,
	})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS STATUS", exitValidationFailed, err)
	}
	if *report != "" {
		contractRaw := result.SelectedContractRaw()
		if len(contractRaw) == 0 {
			return writeFailure(errOut, *format, "POLIS STATUS", exitValidationFailed, fmt.Errorf("inspect workspace checkpoint: a workspace report requires a selected locked Change Contract or verified package"))
		}
		checkpoint, err := inspectWorkspaceCheckpointWithContract(ctx, *repo, *policy, *report, contractRaw)
		if err != nil {
			return writeFailure(errOut, *format, "POLIS STATUS", exitValidationFailed, fmt.Errorf("inspect workspace checkpoint: %w", err))
		}
		result.Workspace = &changestatus.WorkspaceSummary{
			Path: *report, CheckpointState: checkpoint.CheckpointState,
			RecordedValidationStatus:     checkpoint.RecordedValidationStatus,
			ReportAuthenticated:          checkpoint.ReportAuthenticated,
			CurrentValidationEstablished: checkpoint.CurrentValidationEstablished,
			ProofInputDigestsBound:       checkpoint.ProofInputDigestsBound,
			DeliveryArtifactVerified:     checkpoint.DeliveryArtifactVerified,
			RecordedTargetTree:           checkpoint.RecordedTargetTree,
			CurrentTargetTree:            checkpoint.CurrentTargetTree,
			Differences:                  append([]string(nil), checkpoint.Differences...),
		}
	}
	result = changestatus.Project(result)
	if *format == "json" {
		if !result.Consistent {
			report := diagnostic.Report{Code: "POLIS_WORKFLOW_STATE_INCONSISTENT", Category: "workflow", Stage: "status", ObservedCause: "workflow_evidence_inconsistent", AffectedOperations: []string{"status"}, Remediation: "inspect_workflow_evidence"}
			writeJSON(out, struct {
				changestatus.Result
				Diagnostic diagnostic.Report `json:"diagnostic"`
			}{result, report})
		} else {
			writeJSON(out, result)
		}
	} else {
		writeStatusText(out, result)
	}
	if !result.Consistent {
		return exitValidationFailed
	}
	return exitPass
}

func writeStatusText(out io.Writer, result changestatus.Result) {
	fmt.Fprintf(out, "POLIS STATUS: %s\nWhere: %s\nRetention: %s\nConsistent: %t\n", result.State, result.State, result.RetentionMode, result.Consistent)
	if result.Contract == nil {
		fmt.Fprintln(out, "Contract: none")
	} else {
		fmt.Fprintf(out, "Contract: %s\nContract SHA256: %s\nContract schema: %d\nChange kind: %s\nRegression mode: %s\n",
			result.Contract.Path, result.Contract.SHA256, result.Contract.SchemaVersion, result.Contract.Kind, result.Contract.RegressionMode)
	}
	if result.Baseline != nil {
		fmt.Fprintf(out, "Baseline commit: %s\nBaseline tree: %s\nBaseline object format: %s\nBaseline resolvable: %t\nRepository relation: %s\nLocked policy: %s\n",
			result.Baseline.BaseCommit, result.Baseline.BaseTree, result.Baseline.GitObjectFormat, result.Baseline.Resolvable, result.Baseline.RepositoryRelation, result.Baseline.PolicyStatus)
	}
	writeStatusStage(out, "Implementation plan", result.ImplementationPlan)
	writeStatusStage(out, "Red proof", result.RedProof)
	writeStatusStage(out, "Package", changestatus.StageSummary{Status: result.Package.Status, Paths: result.Package.Paths})
	writeStatusStage(out, "Evidence", result.Evidence)
	for _, gate := range result.Gates {
		fmt.Fprintf(out, "Gate %s: %s\n", gate.ID, gate.Status)
	}
	if result.Workspace != nil {
		fmt.Fprintf(out, "Workspace checkpoint: %s\nWorkspace report: %s\n", result.Workspace.CheckpointState, result.Workspace.Path)
	}
	writeStatusFacts(out, "Proven", result.Proven)
	writeStatusFacts(out, "Stale/unproven", result.StaleOrUnproven)
	writeStatusFacts(out, "Missing", result.Missing)
	if result.NextAction != nil {
		fmt.Fprintf(out, "Next action: %s", result.NextAction.Action)
		if result.NextAction.Command != "" {
			fmt.Fprintf(out, " (%s)", result.NextAction.Command)
		}
		fmt.Fprintf(out, " — %s\n", result.NextAction.Reason)
	} else {
		fmt.Fprintln(out, "Next action: none")
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(out, "Problem: %s\n", problem)
	}
}

func writeStatusFacts(out io.Writer, label string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(out, "%s: none\n", label)
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, value := range values {
		fmt.Fprintf(out, "  - %s\n", value)
	}
}

func writeStatusStage(out io.Writer, name string, stage changestatus.StageSummary) {
	fmt.Fprintf(out, "%s: %s", name, stage.Status)
	if stage.Detail != "" {
		fmt.Fprintf(out, " — %s", stage.Detail)
	}
	fmt.Fprintln(out)
	for _, path := range stage.Paths {
		fmt.Fprintf(out, "  Evidence: %s\n", path)
	}
	for _, path := range stage.IncompletePaths {
		fmt.Fprintf(out, "  Incomplete evidence: %s\n", path)
	}
}
