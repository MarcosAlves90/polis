package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/devstart"
	"github.com/MarcosAlves90/polis/v6/internal/packageapply"
	"github.com/MarcosAlves90/polis/v6/internal/packagebuild"
	"github.com/MarcosAlves90/polis/v6/internal/packageverify"
	"github.com/MarcosAlves90/polis/v6/internal/policyinit"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/internal/redcapture"
	artifactsig "github.com/MarcosAlves90/polis/v6/internal/signature"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const version = "6.2.0"

const (
	outputFormatHelp   = "output format: text or json"
	externalPolicyHelp = "external Project Policy schema-v3 JSON outside the worktree"
	repoHelp           = "Git worktree path"
	targetRepoHelp     = "target Git worktree path"
	preflightLabel     = "POLIS PREFLIGHT"
	applyLabel         = "POLIS APPLY"
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

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: polis <doctor|init|plan|start|capture-red|verify|inspect|preflight|build|apply|sign>")
		return exitUsage
	}
	switch args[0] {
	case "doctor":
		return runDoctor(args[1:], out, errOut)
	case "init":
		return runInit(args[1:], out, errOut)
	case "plan":
		return runPlan(args[1:], out, errOut)
	case "start":
		return runStart(args[1:], out, errOut)
	case "capture-red":
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
	default:
		fmt.Fprintf(errOut, "unknown command %q\n", args[0])
		return exitUsage
	}
}

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
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, "POLIS VERIFY", exitInvalidArtifact, err)
	}
	r, err := packageverify.Verify(artifact)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS VERIFY", exitInvalidArtifact, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "project": r.Project, "change": r.Change, "base_commit": r.BaseCommit, "target_tree": r.TargetTree, "validation_level": r.ValidationLevel, "enabled_gates": r.EnabledGates, "disabled_gates": r.DisabledGates})
	} else {
		fmt.Fprintf(out, "POLIS VERIFY: PASS\nProject: %s\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", r.Project, r.BaseCommit, r.TargetTree, r.ValidationLevel, strings.Join(r.EnabledGates, ", "), strings.Join(r.DisabledGates, ", "))
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
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, "POLIS INSPECT", exitInvalidArtifact, err)
	}
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
	fmt.Fprintf(out, "POLIS INSPECT: PASS\nProject: %s\nChange: %s\nFormat: %d\nPolicy schema: %d\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nChange schema: %d\nKind: %s\nBase: %s\nTarget: %s\nScope: %s\nEvidence events: %d\n",
		inspection.Project, inspection.Change, inspection.FormatVersion, inspection.PolicySchemaVersion,
		inspection.ValidationLevel, strings.Join(inspection.EnabledGates, ", "), strings.Join(inspection.DisabledGates, ", "), inspection.ChangeContractSchemaVersion,
		inspection.Kind, inspection.BaseCommit, inspection.TargetTree, strings.Join(inspection.AllowedPaths, ", "), inspection.EvidenceEvents)
	for _, link := range inspection.Traceability {
		fmt.Fprintf(out, "Trace: %s -> %s -> %s\n", link.RequirementID, link.AcceptanceCriterionID, link.Proof)
	}
}

func runPreflight(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || !validFormat(*format) || !validSignaturePair(*signaturePath, *trustedKey) {
		fmt.Fprintln(errOut, "usage: polis preflight [--repo <path>] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return exitUsage
	}
	artifact := fs.Arg(0)
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, preflightLabel, exitInvalidArtifact, err)
	}
	if _, err := packageverify.Verify(artifact); err != nil {
		return writeFailure(errOut, *format, preflightLabel, exitInvalidArtifact, err)
	}
	result, err := packageapply.Preflight(context.Background(), artifact, *repo)
	if err != nil {
		code := exitValidationFailed
		if errors.Is(err, packageapply.ErrBaselineMismatch) {
			code = exitBaselineMismatch
		}
		return writeFailure(errOut, *format, preflightLabel, code, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "safe_to_apply": true, "project": result.Project, "change": result.Change, "target_tree": result.TargetTree, "validation_level": result.ValidationLevel, "enabled_gates": result.EnabledGates, "disabled_gates": result.DisabledGates})
	} else {
		fmt.Fprintf(out, preflightLabel+": PASS\nSafe to apply: yes\nProject: %s\nChange: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", result.Project, result.Change, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "))
	}
	return exitPass
}

type argvFlag []string

func (v *argvFlag) String() string { return strings.Join(*v, " ") }

func (v *argvFlag) Set(value string) error {
	*v = append(*v, value)
	return nil
}

func runInit(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	profile := fs.String("profile", "auto", "policy profile: auto, go, or custom")
	validationLevel := fs.String("validation-level", "", "validation reinforcement level: strict, standard, or minimal")
	var testArgv argvFlag
	var coverageArgv argvFlag
	var disabledGates argvFlag
	fs.Var(&testArgv, "test-argv", "custom profile test argv element; repeat for each argument")
	fs.Var(&coverageArgv, "coverage-argv", "custom profile coverage argv element; repeat for each argument")
	fs.Var(&disabledGates, "disable-gate", "disable a project gate; repeat for each gate id")
	coverageAdapter := fs.String("coverage-adapter", "", "custom profile coverage adapter")
	coverageReport := fs.String("coverage-report", "", "custom profile coverage report path")
	coverageThreshold := fs.Float64("coverage-threshold", 80.0, "custom profile coverage threshold percent")
	dryRun := fs.Bool("dry-run", false, "generate and validate policy without filesystem mutation")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		writeInitUsage(errOut)
		return exitUsage
	}
	var threshold *float64
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "coverage-threshold" {
			threshold = coverageThreshold
		}
	})
	result, err := policyinit.Init(context.Background(), policyinit.Options{
		Repo: *repo, Profile: *profile, ValidationLevel: *validationLevel, DisabledGates: []string(disabledGates), TestArgv: []string(testArgv), CoverageArgv: []string(coverageArgv),
		CoverageAdapter: *coverageAdapter, CoverageReport: *coverageReport, CoverageThreshold: threshold, DryRun: *dryRun,
	})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS INIT: FAIL: %v\n", err)
		return exitUsage
	}
	if *dryRun {
		_, _ = out.Write(result.Policy)
		return exitPass
	}
	fmt.Fprintf(out, "POLIS INIT: PASS\nProfile: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nPolicy: %s\nNext: review and commit .polis/policy.json before polis build\n", result.Profile, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), result.PolicyPath)
	return exitPass
}

func runPlan(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis plan [--repo <path>] [--policy <policy-v3.json>] [--format text|json]")
		return exitUsage
	}
	plan, err := policyplan.Load(context.Background(), policyplan.Options{Repo: *repo, Policy: *policy})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS PLAN", exitValidationFailed, err)
	}
	if *format == "json" {
		writeJSON(out, plan)
	} else {
		writePlanText(out, plan)
	}
	return exitPass
}

func writePlanText(out io.Writer, plan policyplan.Plan) {
	writePlanSummary(out, plan)
	writePlanDependencies(out, plan.DependencyEdges, plan.ExecutionOrder)
	writePlanGates(out, plan.Gates)
	writePlanInvariants(out, plan.MandatoryInvariants)
	writePlanGuarantees(out, plan.Guarantees)
}

func writePlanDependencies(out io.Writer, dependencies []spec.PolicyDependency, executionOrder []string) {
	fmt.Fprintln(out, "Dependency edges:")
	for _, dependency := range dependencies {
		fmt.Fprintf(out, "- %s -> %s\n", dependency.DependsOn, dependency.Gate)
	}
	fmt.Fprintf(out, "Execution order: %s\n", strings.Join(executionOrder, ", "))
}

func writePlanSummary(out io.Writer, plan policyplan.Plan) {
	fmt.Fprintf(out, "POLIS PLAN: PASS\nPlan version: %d\nPolicy source: %s\nPolicy SHA256: %s\nPolicy schema: %d\nRuntime: %s/%s (%s)\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n",
		plan.PlanVersion, plan.PolicySource, plan.PolicySHA256, plan.PolicySchemaVersion, plan.Runtime.OS, plan.Runtime.Architecture, plan.Runtime.GoVersion,
		plan.ValidationLevel, strings.Join(plan.EnabledGates, ", "), strings.Join(plan.DisabledGates, ", "))
}

func writePlanGates(out io.Writer, gates []policyplan.Gate) {
	fmt.Fprintln(out, "Gates:")
	for _, gate := range gates {
		writePlanGate(out, gate)
	}
}

func writePlanGate(out io.Writer, gate policyplan.Gate) {
	fmt.Fprintf(out, "- %s %s (%s)", strings.ToUpper(gate.State), gate.ID, gate.Mode)
	writePlanCommand(out, gate)
	writePlanCoverage(out, gate)
	if gate.Reason != nil {
		fmt.Fprintf(out, " reason=%s", *gate.Reason)
	}
	fmt.Fprintln(out)
}

func writePlanCommand(out io.Writer, gate policyplan.Gate) {
	if gate.Command == nil {
		return
	}
	fmt.Fprintf(out, " argv=%q cwd=%s timeout=%ds", gate.Command.Argv, gate.Command.Cwd, gate.Command.TimeoutSeconds)
	if gate.Command.Environment != nil {
		fmt.Fprintf(out, " environment=%s", gate.Command.Environment.Mode)
	}
}

func writePlanCoverage(out io.Writer, gate policyplan.Gate) {
	if gate.Adapter == "" && gate.Report == "" && gate.Operator == "" && gate.ThresholdPercent == nil {
		return
	}
	fmt.Fprintf(out, " adapter=%s report=%s operator=%s", gate.Adapter, gate.Report, gate.Operator)
	if gate.ThresholdPercent != nil {
		fmt.Fprintf(out, " threshold=%.2f%%", *gate.ThresholdPercent)
	}
}

func writePlanInvariants(out io.Writer, invariants []policyplan.Invariant) {
	fmt.Fprintln(out, "Mandatory invariants:")
	for _, invariant := range invariants {
		fmt.Fprintf(out, "- %s: %s\n", invariant.ID, invariant.Description)
	}
}

func writePlanGuarantees(out io.Writer, guarantees []policyplan.Guarantee) {
	fmt.Fprintln(out, "Guarantees:")
	for _, guarantee := range guarantees {
		fmt.Fprintf(out, "- %s [%s]: %s\n", guarantee.Gate, guarantee.Status, guarantee.Description)
	}
}

func writeInitUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: polis init [--repo <path>] [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--test-argv <arg> ... --coverage-argv <arg> ... --coverage-adapter <adapter> --coverage-report <path> [--coverage-threshold <percent>]] [--dry-run]")
}

func runStart(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	contract := fs.String("contract", "", "strict schema-v3 draft Change Contract outside the worktree")
	outPath := fs.String("out", "", "locked schema-v4 Change Contract output outside the worktree")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" {
		fmt.Fprintln(errOut, "usage: polis start --repo <path> [--policy <policy-v3.json>] --contract <draft-v3.json> --out <locked-v4.json>")
		return exitUsage
	}
	result, err := devstart.Start(context.Background(), devstart.Options{Repo: *repo, Policy: *policy, Contract: *contract, Out: *outPath})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS START: FAIL: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(out, "POLIS START: PASS\nContract: %s\nSHA256: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", result.Path, result.SHA256, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "))
	return exitPass
}

func runCaptureRed(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("capture-red", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", "", repoHelp)
	contract := fs.String("contract", "", "defect Change Contract JSON outside the worktree")
	outPath := fs.String("out", "", "output regression patch outside the worktree")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *contract == "" || *outPath == "" {
		fmt.Fprintln(errOut, "usage: polis capture-red --repo <path> --contract <change.json> --out <regression.patch>")
		return exitUsage
	}
	result, err := redcapture.Capture(context.Background(), redcapture.Options{Repo: *repo, Contract: *contract, Out: *outPath})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS CAPTURE-RED: FAIL: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(out, "POLIS CAPTURE-RED: PASS\nPatch: %s\nSHA256: %s\n", result.Path, result.SHA256)
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
	contract := fs.String("contract", "", "delivery Change Contract JSON outside the worktree")
	regressionPatch := fs.String("regression-patch", "", "validated Red-state patch for defect contracts")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || *repo == "" || *project == "" || *change == "" || *outDir == "" || *contract == "" {
		fmt.Fprintln(errOut, "usage: polis build --repo <path> [--policy <policy-v3.json>] --project <slug> --change <slug> --contract <change.json> [--regression-patch <red.patch>] --out <directory>")
		return exitUsage
	}
	result, err := packagebuild.Build(context.Background(), packagebuild.Options{
		Repo: *repo, Policy: *policy, Project: *project, Change: *change, Out: *outDir, Contract: *contract, RegressionPatch: *regressionPatch,
	})
	if err != nil {
		fmt.Fprintf(errOut, "POLIS BUILD: FAIL: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(out, "POLIS BUILD: PASS\nArtifact: %s\nSHA256: %s\nBase: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", result.Path, result.SHA256, result.BaseCommit, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "))
	return exitPass
}

func runApply(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	format := fs.String("format", "text", outputFormatHelp)
	signaturePath, trustedKey := signatureFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || !validFormat(*format) || !validSignaturePair(*signaturePath, *trustedKey) {
		fmt.Fprintln(errOut, "usage: polis apply [--repo <path>] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>")
		return exitUsage
	}
	artifact := fs.Arg(0)
	if err := verifyDetached(artifact, *signaturePath, *trustedKey); err != nil {
		return writeFailure(errOut, *format, applyLabel, exitInvalidArtifact, err)
	}
	if _, err := packageverify.Verify(artifact); err != nil {
		return writeFailure(errOut, *format, applyLabel, exitInvalidArtifact, err)
	}
	result, err := packageapply.Apply(context.Background(), artifact, *repo)
	if err != nil {
		code := exitApplyFailed
		switch {
		case errors.Is(err, packageapply.ErrBaselineMismatch):
			code = exitBaselineMismatch
		case errors.Is(err, packageapply.ErrValidationFailed):
			code = exitValidationFailed
		}
		return writeFailure(errOut, *format, applyLabel, code, err)
	}
	if *format == "json" {
		payload := map[string]any{"status": "PASS", "project": result.Project, "change": result.Change, "target_tree": result.TargetTree, "validation_level": result.ValidationLevel, "enabled_gates": result.EnabledGates, "disabled_gates": result.DisabledGates}
		if result.EvidencePath != "" {
			payload["evidence"] = result.EvidencePath
		}
		writeJSON(out, payload)
	} else {
		fmt.Fprintf(out, applyLabel+": PASS\nProject: %s\nChange: %s\nTarget: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\n", result.Project, result.Change, result.TargetTree, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "))
		if result.EvidencePath != "" {
			fmt.Fprintf(out, "Evidence: %s\n", result.EvidencePath)
		}
	}
	return exitPass
}

func runSign(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(errOut)
	key := fs.String("key", "", "Ed25519 PKCS#8 private key PEM")
	outPath := fs.String("out", "", "detached signature output")
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 || *key == "" || *outPath == "" || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis sign --key <private.pem> --out <artifact.polis.sig> [--format text|json] <artifact.polis>")
		return exitUsage
	}
	result, err := artifactsig.SignFile(fs.Arg(0), *key, *outPath)
	if err != nil {
		return writeFailure(errOut, *format, "POLIS SIGN", exitValidationFailed, err)
	}
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "signature": result.SignaturePath, "artifact_sha256": result.ArtifactSHA256, "public_key_sha256": result.PublicKeySHA256})
	} else {
		fmt.Fprintf(out, "POLIS SIGN: PASS\nSignature: %s\nArtifact SHA256: %s\nPublic key SHA256: %s\n", result.SignaturePath, result.ArtifactSHA256, result.PublicKeySHA256)
	}
	return exitPass
}

func runDoctor(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", outputFormatHelp)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis doctor [--format text|json]")
		return exitUsage
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		if *format == "json" {
			writeJSON(errOut, map[string]any{"status": "BLOCKED", "version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_runtime": runtime.Version(), "error": "git not found"})
		} else {
			fmt.Fprintf(out, "POLIS doctor %s\nOS/Arch: %s/%s\nGo runtime: %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
			fmt.Fprintln(errOut, "Git: BLOCKED (not found)")
		}
		return exitBlocked
	}
	b, err := exec.Command(gitPath, "--version").CombinedOutput()
	if err != nil {
		return writeFailure(errOut, *format, "POLIS DOCTOR", exitBlocked, err)
	}
	gitVersion := strings.TrimSpace(string(b))
	if *format == "json" {
		writeJSON(out, map[string]any{"status": "PASS", "version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "go_runtime": runtime.Version(), "git": gitVersion})
	} else {
		fmt.Fprintf(out, "POLIS doctor %s\n", version)
		fmt.Fprintf(out, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(out, "Go runtime: %s\n", runtime.Version())
		fmt.Fprintf(out, "Git: %s\n", gitVersion)
	}
	return exitPass
}

func signatureFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("signature", "", "detached POLIS signature"), fs.String("trusted-key", "", "trusted Ed25519 public key PEM")
}

func validSignaturePair(signaturePath, trustedKey string) bool {
	return (signaturePath == "" && trustedKey == "") || (signaturePath != "" && trustedKey != "")
}

func verifyDetached(artifact, signaturePath, trustedKey string) error {
	if signaturePath == "" {
		return nil
	}
	return artifactsig.VerifyFile(artifact, signaturePath, trustedKey)
}

func validFormat(format string) bool { return format == "text" || format == "json" }

func writeJSON(w io.Writer, value any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func writeFailure(w io.Writer, format, label string, code int, err error) int {
	if format == "json" {
		writeJSON(w, map[string]any{"status": "FAIL", "exit_code": code, "error": err.Error()})
	} else {
		fmt.Fprintf(w, "%s: FAIL: %v\n", label, err)
	}
	return code
}
