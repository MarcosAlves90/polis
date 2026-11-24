package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func runWorkspace(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: polis workspace validate|status [options]; see polis help workspace")
		return exitUsage
	}
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		return runHelp([]string{"workspace"}, out, errOut)
	}
	switch args[0] {
	case "validate":
		return runWorkspaceValidate(args[1:], out, errOut)
	case "status":
		return runWorkspaceStatus(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "unknown workspace operation %q\n", args[0])
		return exitUsage
	}
}

func runWorkspaceValidate(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("workspace validate", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	contract := fs.String("contract", "", "locked strict Change Contract JSON")
	regressionPatch := fs.String("regression-patch", "", "required captured Red proof for Red-to-Green contracts")
	implementationPlan := fs.String("implementation-plan", "", "optional contract-bound Implementation Plan JSON")
	outReport := fs.String("out-report", "", "new external workspace validation report; never overwritten")
	jobs := fs.Int("jobs", 1, "maximum concurrent parallel-safe gates (1..16)")
	format := fs.String("format", "text", outputFormatHelp)
	streamOutput := fs.Bool("stream-output", false, "stream live gate output and incremental results to stderr")
	eventsFile := fs.String("events-file", "", "new external JSON Lines file for live events (requires --stream-output)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := policyexec.ValidateJobs(*jobs); err != nil {
		fmt.Fprintln(errOut, err)
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) || *contract == "" {
		fmt.Fprintln(errOut, "usage: polis workspace validate --repo <path> [--policy <policy-v3.json>] --contract <locked-v4-or-v6.json> [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--out-report <new-external.json>] [--jobs <1..16>] [--format text|json]")
		return exitUsage
	}
	ctx := context.Background()
	writeProgress(errOut, *format, "resolve the workspace Git root", "workspace evidence and external inputs must be checked against the canonical repository boundary")
	root, err := gitutil.ResolveRoot(ctx, *repo, gitutil.ResolveRootOptions{PathError: "resolve repo path", GitError: "not a Git worktree", RootError: "resolve Git root"})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
	}
	reporter, err := newLiveReporter(*streamOutput, *format, *eventsFile, root, errOut)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitUsage, err)
	}
	defer reporter.Close()
	stageEnds := []func(string){
		reporter.Stage("red_green_regression", "replay the captured Red proof and target change gates", "the locked contract must pass in an isolated target worktree"),
		reporter.Stage("isolated_target_preparation", "prepare the isolated target worktree", "validation must run against the exact locked target tree"),
		reporter.Stage("project_gate_execution", "execute project gates", "the effective strict plan must pass against the isolated target"),
	}
	defer func() {
		for _, end := range stageEnds {
			end("FAIL")
		}
	}()
	recheckStage := reporter.Stage("source_state_recheck", "recheck the workspace source state", "the validated tree must still match the current worktree")
	defer recheckStage("FAIL")
	writeProgress(errOut, *format, "validate the current workspace against the locked contract", "the current source tree and producer gates must match the contract without creating a delivery artifact")
	result, err := packagebuild.ValidateWorkspace(ctx, packagebuild.WorkspaceOptions{
		Repo: root, Policy: *policy, Contract: *contract,
		RegressionPatch: *regressionPatch, ImplementationPlan: *implementationPlan, Jobs: *jobs,
		OnGateStart:    liveGateStart(reporter, gateStartProgress(errOut, *format), *format, errOut),
		OnGateComplete: gateCompleteProgress(errOut, *format),
		OnGateResult:   reporter.GateResult,
		OnOutput:       liveOutput(reporter),
		OnChangeStart:  liveChangeStart(reporter),
		OnChangeResult: reporter.ChangeResult,
	})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
	}
	for _, end := range stageEnds {
		end("PASS")
	}
	recheckStage("PASS")
	report := spec.WorkspaceValidationReport{
		SchemaVersion: 1, Status: "PASS", ValidationKind: "workspace_validation",
		WorkspaceValidated: true, DeliveryArtifactBuilt: false, DeliveryArtifactVerified: false,
		BaseCommit: result.BaseCommit, TargetTree: result.TargetTree,
		ContractSHA256: result.ContractSHA256, PolicySHA256: result.PolicySHA256,
		ValidationLevel: result.ValidationLevel,
		EnabledGates:    result.EnabledGates, DisabledGates: result.DisabledGates,
		ProducerGateStatuses: result.ProducerGateStatuses,
		Concurrency:          &result.Concurrency,
	}
	reportBytes, err := json.Marshal(report)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, fmt.Errorf("encode workspace report: %w", err))
	}
	reportBytes = append(reportBytes, '\n')
	if *outReport != "" {
		if err := fileutil.WriteOutsideExclusive(root, *outReport, reportBytes); err != nil {
			return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
		}
	}
	if *format == "json" {
		// The external checkpoint schema stays closed; the command result may
		// add a presentation-only concurrency report beside those fields.
		payload := map[string]any{}
		if err := json.Unmarshal(reportBytes, &payload); err != nil {
			return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
		}
		payload["concurrency"] = result.Concurrency
		writeJSON(out, payload)
		return exitPass
	}
	fmt.Fprintf(out, "POLIS WORKSPACE VALIDATE: PASS\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nProducer gate statuses: %v\nWorkspace validated: yes\nDelivery artifact built: no\nDelivery artifact verified: no\n",
		result.BaseCommit, result.TargetTree, result.ValidationLevel,
		strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), result.ProducerGateStatuses)
	writeConcurrencyText(out, result.Concurrency)
	if *outReport != "" {
		fmt.Fprintf(out, "Report: %s\n", *outReport)
	}
	return exitPass
}
