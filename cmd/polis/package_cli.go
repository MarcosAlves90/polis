package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
)

func runVerify(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || !validFormat(*format) || !validSignaturePair(*signaturePath, *trustedKey) {
		fmt.Fprintln(errOut, "usage: polis verify [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return exitUsage
	}
	artifact := fs.Arg(0)
	writeProgress(errOut, *format, "verify the detached artifact signature when configured", "a supplied signature must authenticate the exact artifact bytes before package verification")
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, "POLIS VERIFY", exitInvalidArtifact, err)
	}
	writeProgress(errOut, *format, "verify artifact structure and evidence", "POLIS must reject malformed, tampered, or internally inconsistent delivery packages")
	r, err := packageverify.Verify(artifact)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS VERIFY", exitInvalidArtifact, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "project": r.Project, "change": r.Change, "base_commit": r.BaseCommit, "target_tree": r.TargetTree, "validation_level": r.ValidationLevel, "enabled_gates": r.EnabledGates, "disabled_gates": r.DisabledGates, "deferred_gates": r.DeferredGates, "consumer_validation_required": r.ConsumerValidationRequired})
	} else {
		fmt.Fprintf(out, "POLIS VERIFY: PASS\nProject: %s\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nDeferred gates: %s\nConsumer validation required: %t\n", r.Project, r.BaseCommit, r.TargetTree, r.ValidationLevel, strings.Join(r.EnabledGates, ", "), strings.Join(r.DisabledGates, ", "), strings.Join(r.DeferredGates, ", "), r.ConsumerValidationRequired)
	}
	return exitPass
}

func runInspect(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || !validFormat(*format) || !validSignaturePair(*signaturePath, *trustedKey) {
		fmt.Fprintln(errOut, "usage: polis inspect [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return exitUsage
	}
	artifact := fs.Arg(0)
	writeProgress(errOut, *format, "verify the detached artifact signature when configured", "inspection must not trust metadata from an artifact whose configured signature fails")
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, "POLIS INSPECT", exitInvalidArtifact, err)
	}
	writeProgress(errOut, *format, "validate and decode artifact metadata", "traceability is meaningful only after package integrity and schema validation succeed")
	inspection, err := packageverify.Inspect(artifact)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS INSPECT", exitInvalidArtifact, err)
	}
	if *format == "json" {
		writeJSON(out, inspection)
	} else {
		writeInspectionText(out, inspection)
	}
	return exitPass
}

func writeInspectionText(out io.Writer, inspection packageverify.Inspection) {
	fmt.Fprintf(out, "POLIS INSPECT: PASS\nProject: %s\nChange: %s\nFormat: %d\nPolicy schema: %d\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nDeferred gates: %s\nConsumer validation required: %t\nChange schema: %d\nKind: %s\nBase: %s\nTarget: %s\nScope: %s\nEvidence events: %d\n",
		inspection.Project, inspection.Change, inspection.FormatVersion, inspection.PolicySchemaVersion,
		inspection.ValidationLevel, strings.Join(inspection.EnabledGates, ", "), strings.Join(inspection.DisabledGates, ", "), strings.Join(inspection.DeferredGates, ", "), inspection.ConsumerValidationRequired, inspection.ChangeContractSchemaVersion,
		inspection.Kind, inspection.BaseCommit, inspection.TargetTree, strings.Join(inspection.AllowedPaths, ", "), inspection.EvidenceEvents)
	fmt.Fprintf(out, "Implementation plan: %t\n", inspection.ImplementationPlanPresent)
	if inspection.ImplementationPlanPresent {
		fmt.Fprintf(out, "Plan schema: %d\nPlan strategy: %s\nPlan steps: %d\n", inspection.ImplementationPlanSchemaVersion, inspection.ImplementationPlanStrategy, inspection.ImplementationPlanStepCount)
		for _, trace := range inspection.ImplementationPlanTraceability {
			proofSteps := make([]string, 0, len(trace.ProofPlanSteps))
			for _, step := range trace.ProofPlanSteps {
				proofSteps = append(proofSteps, fmt.Sprintf("%s (%s)", step.ID, step.Kind))
			}
			fmt.Fprintf(out, "Plan trace: %s -> %s; proof=%s; test steps=%s; implementation steps=%s; proof steps=%s\n",
				trace.RequirementID, trace.AcceptanceCriterionID, trace.Proof,
				strings.Join(trace.TestPlanStepIDs, ", "), strings.Join(trace.ImplementationPlanStepIDs, ", "), strings.Join(proofSteps, ", "))
		}
	}
	if inspection.Commit != nil {
		fmt.Fprintf(out, "Commit message:\n%s", escapeCommitMessageForDisplay(inspection.Commit.Message))
		fmt.Fprintln(out)
	}
	for _, link := range inspection.Traceability {
		fmt.Fprintf(out, "Trace: %s -> %s -> %s\n", link.RequirementID, link.AcceptanceCriterionID, link.Proof)
	}
}

func runPreflight(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	baselineModeValue := fs.String("baseline-mode", string(packageapply.BaselineModeStrict), baselineModeHelp)
	allowMissingBaselineProof := fs.Bool("allow-missing-baseline-proof", false, overrideBaselineHelp)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	baselineMode, modeErr := packageapply.ParseBaselineMode(*baselineModeValue)
	if fs.NArg() != 1 || !validFormat(*format) || !validSignaturePair(*signaturePath, *trustedKey) || modeErr != nil || (*allowMissingBaselineProof && baselineMode != packageapply.BaselineModePermissive) {
		if modeErr != nil {
			fmt.Fprintln(errOut, modeErr)
		}
		if *allowMissingBaselineProof && baselineMode != packageapply.BaselineModePermissive {
			fmt.Fprintln(errOut, "--allow-missing-baseline-proof requires --baseline-mode permissive")
		}
		fmt.Fprintln(errOut, "usage: polis preflight [--repo <path>] [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return exitUsage
	}
	artifact := fs.Arg(0)
	writeProgress(errOut, *format, "verify the detached artifact signature when configured", "consumer preflight must authenticate the artifact before evaluating it against the target repository")
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, preflightLabel, exitInvalidArtifact, err)
	}
	writeProgress(errOut, *format, "verify the delivery artifact", "preflight must start from a structurally valid package with trustworthy producer evidence")
	if _, err := packageverify.Verify(artifact); err != nil {
		return writeFailure(errOut, *format, preflightLabel, exitInvalidArtifact, err)
	}
	writeProgress(errOut, *format, "validate the artifact against the target repository", "consumer baseline compatibility, change scope, and required gates must pass before apply is allowed")
	result, err := packageapply.PreflightWithOptions(context.Background(), artifact, *repo, packageapply.Options{BaselineMode: baselineMode, AllowMissingBaselineProof: *allowMissingBaselineProof, OnGateStart: gateStartProgress(errOut, *format), OnGateComplete: gateCompleteProgress(errOut, *format)})
	if err != nil {
		code := exitValidationFailed
		if errors.Is(err, packageapply.ErrBaselineMismatch) {
			code = exitBaselineMismatch
		}
		return writeFailure(errOut, *format, preflightLabel, code, err)
	}
	if *format == "json" {
		payload := map[string]any{"status": "PASS", "safe_to_apply": true, "project": result.Project, "change": result.Change, "target_tree": result.TargetTree, "validation_level": result.ValidationLevel, "enabled_gates": result.EnabledGates, "disabled_gates": result.DisabledGates, "producer_deferred_gates": result.ProducerDeferredGates, "outstanding_deferred_gates": result.OutstandingDeferredGates, "consumer_validation_required": result.ConsumerValidationRequired, "consumer_validation_status": result.ConsumerValidationStatus, "consumer_gate_statuses": result.ConsumerGateStatuses}
		addBaselineResultFields(payload, result)
		writeJSON(out, payload)
	} else {
		fmt.Fprintf(out, preflightLabel+": PASS\nSafe to apply: yes\nProject: %s\nChange: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nProducer deferred gates: %s\nOutstanding deferred gates: %s\nConsumer validation required: %t\nConsumer validation status: %s\nConsumer gate statuses: %v\n", result.Project, result.Change, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), displayGateList(result.ProducerDeferredGates), displayGateList(result.OutstandingDeferredGates), result.ConsumerValidationRequired, result.ConsumerValidationStatus, result.ConsumerGateStatuses)
		writeBaselineResultText(out, result)
	}
	return exitPass
}

func runBuild(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	project := fs.String("project", "", "canonical project slug")
	change := fs.String("change", "", "canonical change slug")
	outDir := fs.String("out", "", "output directory")
	contract := fs.String("contract", "", "delivery Change Contract JSON outside the worktree or retained by repository mode")
	regressionPatch := fs.String("regression-patch", "", "validated Red-state patch outside the worktree or retained by repository mode")
	implementationPlanPath := fs.String("implementation-plan", "", "optional contract-bound Implementation Plan JSON outside the worktree or retained by repository mode")
	jobs := fs.Int("jobs", 1, "maximum concurrent parallel-safe gates (1..16)")
	format := fs.String("format", "text", outputFormatHelp)
	var deferredGates argvFlag
	fs.Var(&deferredGates, "defer-gate", "defer enabled gate validation to the consumer; repeat for each gate")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if err := policyexec.ValidateJobs(*jobs); err != nil {
		fmt.Fprintln(errOut, err)
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) || *repo == "" || *project == "" || *change == "" || *outDir == "" || *contract == "" {
		fmt.Fprintln(errOut, "usage: polis build --repo <path> [--policy <policy-v3.json>] --project <slug> --change <slug> --contract <change.json> [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--defer-gate <id> ...] [--jobs <1..16>] [--format text|json] --out <directory>")
		return exitUsage
	}
	writeProgress(errOut, *format, "validate the locked change and assemble the delivery package", "producer gates and package integrity must pass before a .polis artifact is emitted")
	result, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: *repo, Policy: *policy, Project: *project, Change: *change, Out: *outDir, Contract: *contract, RegressionPatch: *regressionPatch, ImplementationPlan: *implementationPlanPath, DeferredGates: deferredGates, Jobs: *jobs,
		OnGateStart: gateStartProgress(errOut, *format), OnGateComplete: gateCompleteProgress(errOut, *format),
	})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS BUILD", exitUsage, err)
	}
	if *format == "json" {
		payload := map[string]any{"status": "PASS", "artifact": result.Path, "sha256": result.SHA256, "base_commit": result.BaseCommit, "target_tree": result.TargetTree, "validation_level": result.ValidationLevel, "enabled_gates": result.EnabledGates, "disabled_gates": result.DisabledGates, "deferred_gates": result.DeferredGates, "consumer_validation_required": result.ConsumerValidationRequired, "producer_gate_statuses": result.ProducerGateStatuses}
		if len(result.RetainedPaths) > 0 {
			payload["retained_paths"] = result.RetainedPaths
		}
		writeJSON(out, payload)
	} else {
		fmt.Fprintf(out, "POLIS BUILD: PASS\nArtifact: %s\nSHA256: %s\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nDeferred gates: %s\nConsumer validation required: %t\nProducer gate statuses: %v\n", result.Path, result.SHA256, result.BaseCommit, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), strings.Join(result.DeferredGates, ", "), result.ConsumerValidationRequired, result.ProducerGateStatuses)
		if len(result.RetainedPaths) > 0 {
			fmt.Fprintf(out, "Retained artifacts: %s\n", strings.Join(result.RetainedPaths, ", "))
		}
	}
	return exitPass
}
