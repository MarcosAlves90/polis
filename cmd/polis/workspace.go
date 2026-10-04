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
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) || *contract == "" {
		fmt.Fprintln(errOut, "usage: polis workspace validate --repo <path> [--policy <policy-v3.json>] --contract <locked-v4-or-v6.json> [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--out-report <new-external.json>] [--format text|json]")
		return exitUsage
	}
	ctx := context.Background()
	root, err := gitutil.ResolveRoot(ctx, *repo, gitutil.ResolveRootOptions{PathError: "resolve repo path", GitError: "not a Git worktree", RootError: "resolve Git root"})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
	}
	result, err := packagebuild.ValidateWorkspace(ctx, packagebuild.WorkspaceOptions{
		Repo: root, Policy: *policy, Contract: *contract,
		RegressionPatch: *regressionPatch, ImplementationPlan: *implementationPlan,
	})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS WORKSPACE VALIDATE", exitValidationFailed, err)
	}
	report := spec.WorkspaceValidationReport{
		SchemaVersion: 1, Status: "PASS", ValidationKind: "workspace_validation",
		WorkspaceValidated: true, DeliveryArtifactBuilt: false, DeliveryArtifactVerified: false,
		BaseCommit: result.BaseCommit, TargetTree: result.TargetTree,
		ContractSHA256: result.ContractSHA256, PolicySHA256: result.PolicySHA256,
		ValidationLevel: result.ValidationLevel,
		EnabledGates:    result.EnabledGates, DisabledGates: result.DisabledGates,
		ProducerGateStatuses: result.ProducerGateStatuses,
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
		_, _ = out.Write(reportBytes)
		return exitPass
	}
	fmt.Fprintf(out, "POLIS WORKSPACE VALIDATE: PASS\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nProducer gate statuses: %v\nWorkspace validated: yes\nDelivery artifact built: no\nDelivery artifact verified: no\n",
		result.BaseCommit, result.TargetTree, result.ValidationLevel,
		strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), result.ProducerGateStatuses)
	if *outReport != "" {
		fmt.Fprintf(out, "Report: %s\n", *outReport)
	}
	return exitPass
}
