package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/policyinit"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
)

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
	writeProgress(errOut, "text", "resolve and validate the requested Project Policy", "the generated or previewed policy must be canonical and valid before it is reported")
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
	fmt.Fprintf(out, "POLIS INIT: PASS\nProfile: %s\nValidation level: %s\nEnabled gates: %s\nDisabled gates: %s\nPolicy: %s\nProvenance: %s\nNext: review and commit .polis/policy.json and .polis/policy-provenance.json before polis build\n", result.Profile, result.ValidationLevel, strings.Join(result.EnabledGates, ", "), strings.Join(result.DisabledGates, ", "), result.PolicyPath, result.ProvenancePath)
	return exitPass
}

func runPlan(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	format := fs.String("format", "text", outputFormatHelp)
	var deferredGates argvFlag
	fs.Var(&deferredGates, "defer-gate", "defer enabled gate validation to the consumer; repeat for each gate")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis plan [--repo <path>] [--policy <policy-v3.json>] [--defer-gate <id> ...] [--format text|json]")
		return exitUsage
	}
	writeProgress(errOut, *format, "load and compile the effective Project Policy", "the plan must resolve gate modes, dependencies, deferrals, and validation guarantees deterministically")
	plan, err := policyplan.Load(context.Background(), policyplan.Options{Repo: *repo, Policy: *policy, DeferredGates: deferredGates})
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

func writePlanDependencies(out io.Writer, dependencies []policyplan.DependencyEdge, executionOrder []string) {
	fmt.Fprintln(out, "Dependency edges:")
	for _, dependency := range dependencies {
		fmt.Fprintf(out, "- %s -> %s source=%s reason=%s\n", dependency.DependsOn, dependency.Gate, dependency.Provenance.Source, dependency.Provenance.Reason)
	}
	fmt.Fprintf(out, "Execution order: %s\n", strings.Join(executionOrder, ", "))
}

func writePlanSummary(out io.Writer, plan policyplan.Plan) {
	fmt.Fprintf(out, "POLIS PLAN: PASS\nPlan version: %d\nPolicy source: %s\nPolicy SHA256: %s\nPolicy schema: %d\nRuntime: %s/%s (%s)\nValidation level: %s (source=%s reason=%s)\nEnabled gates: %s\nDisabled gates: %s\nDeferred gates: %s\nConsumer validation required: %t\n",
		plan.PlanVersion, plan.PolicySource, plan.PolicySHA256, plan.PolicySchemaVersion, plan.Runtime.OS, plan.Runtime.Architecture, plan.Runtime.GoVersion,
		plan.ValidationLevel, plan.ValidationLevelProvenance.Source, plan.ValidationLevelProvenance.Reason, strings.Join(plan.EnabledGates, ", "), strings.Join(plan.DisabledGates, ", "), strings.Join(plan.DeferredGates, ", "), plan.ConsumerValidationRequired)
}

func writePlanGates(out io.Writer, gates []policyplan.Gate) {
	fmt.Fprintln(out, "Gates:")
	for _, gate := range gates {
		writePlanGate(out, gate)
	}
}

func writePlanGate(out io.Writer, gate policyplan.Gate) {
	fmt.Fprintf(out, "- %s %s (%s) producer_action=%s consumer_requirement=%s", strings.ToUpper(gate.State), gate.ID, gate.Mode, gate.ProducerAction, gate.ConsumerRequirement)
	fmt.Fprintf(out, " configuration_source=%s configuration_reason=%s state_source=%s state_reason=%s producer_source=%s producer_reason=%s consumer_source=%s consumer_reason=%s",
		gate.Provenance.Configuration.Source, gate.Provenance.Configuration.Reason,
		gate.Provenance.State.Source, gate.Provenance.State.Reason, gate.Provenance.ProducerAction.Source, gate.Provenance.ProducerAction.Reason,
		gate.Provenance.ConsumerRequirement.Source, gate.Provenance.ConsumerRequirement.Reason)
	if gate.Command != nil {
		fmt.Fprintf(out, " parallel_safe=%t", gate.ParallelSafe)
	}
	if len(gate.InputPaths) != 0 {
		fmt.Fprintf(out, " input_paths=%q", gate.InputPaths)
	}
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
		fmt.Fprintf(out, "- %s [%s] source=%s reason=%s: %s\n", guarantee.Gate, guarantee.Status, guarantee.Provenance.Source, guarantee.Provenance.Reason, guarantee.Description)
	}
}

func writeInitUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: polis init [--repo <path>] [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--test-argv <arg> ... --coverage-argv <arg> ... --coverage-adapter <adapter> --coverage-report <path> [--coverage-threshold <percent>]] [--dry-run]")
}
