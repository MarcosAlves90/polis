package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/gaterun"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

func runRecordedGates(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("gates", flag.ContinueOnError)
	fs.SetOutput(errOut)
	repo := fs.String("repo", ".", targetRepoHelp)
	policy := fs.String("policy", "", externalPolicyHelp)
	format := fs.String("format", "text", outputFormatHelp)
	contract := fs.String("contract", "", "optional external locked Change Contract")
	jobs := fs.Int("jobs", 1, "maximum concurrent gates (1..16); only parallel-safe gates overlap")
	environmentID := fs.String("environment-id", "", "nonsecret version of external resources and environment; required for reuse/replay")
	reuse := fs.String("reuse", "", "prior gate-run manifest to consider for reuse")
	replay := fs.String("replay", "", "reexecute a matching recorded run without reuse")
	inspect := fs.String("inspect-run", "", "inspect a recorded run without executing commands")
	outRun := fs.String("out-run", "", "new external gate-run manifest file (never overwritten)")
	affected := fs.Bool("affected", false, "derive gates from captured Git changes and policy input_paths")
	var selected argvFlag
	fs.Var(&selected, "gate", "select an enabled gate (repeatable); includes prerequisites")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 || !validFormat(*format) {
		fmt.Fprintln(errOut, "usage: polis gates [options]; see polis help gates")
		return exitUsage
	}
	if err := policyexec.ValidateJobs(*jobs); err != nil {
		return writeFailure(errOut, *format, "POLIS GATES", exitUsage, err)
	}
	if *inspect != "" {
		invalid := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "inspect-run" && f.Name != "format" {
				invalid = true
			}
		})
		if invalid {
			return writeFailure(errOut, *format, "POLIS GATES", exitUsage, errors.New("inspect-run only accepts format"))
		}
		m, err := gaterun.Load(*inspect)
		if err != nil {
			return writeFailure(errOut, *format, "POLIS GATES", exitValidationFailed, err)
		}
		if *format == "json" {
			writeJSON(out, m)
		} else {
			writeGateRunText(out, m)
		}
		return exitPass
	}
	if *affected && len(selected) != 0 {
		return writeFailure(errOut, *format, "POLIS GATES", exitUsage, errors.New("gate and affected are mutually exclusive"))
	}
	jobsExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "jobs" {
			jobsExplicit = true
		}
	})
	if *replay != "" && jobsExplicit {
		return writeFailure(errOut, *format, "POLIS GATES", exitUsage, errors.New("replay uses the recorded jobs bound; omit --jobs"))
	}
	ctx := context.Background()
	root, err := gitutil.ResolveRoot(ctx, *repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS GATES", exitValidationFailed, err)
	}
	plan, err := policyplan.Load(ctx, policyplan.Options{Repo: root, Policy: *policy})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS GATES", exitValidationFailed, err)
	}
	manifest, result, err := gaterun.Run(ctx, root, plan, gaterun.Options{Version: version, Contract: *contract,
		EnvironmentID: *environmentID, Jobs: *jobs, Selected: selected, Affected: *affected, Reuse: *reuse, Replay: *replay, Out: *outRun})
	if err != nil {
		return writeFailure(errOut, *format, "POLIS GATES", exitValidationFailed, err)
	}
	report := gatesResult(plan, result)
	report.Run = &manifest
	for i := range report.GateResults {
		for _, gate := range manifest.Gates {
			if gate.ID == report.GateResults[i].ID {
				report.GateResults[i].Action = gate.Action
				report.GateResults[i].StaleCategories = gate.StaleCategories
			}
		}
	}
	if *format == "json" {
		writeJSON(out, report)
	} else {
		writeGatesText(out, report)
	}
	switch result.Overall {
	case spec.StatusPass:
		return exitPass
	case spec.StatusBlocked:
		return exitBlocked
	default:
		return exitValidationFailed
	}
}

func writeGateRunText(out io.Writer, m gaterun.Manifest) {
	fmt.Fprintf(out, "POLIS GATE RUN: %s\nRun: %s\nRecorded current: %t (not a check against this worktree)\nSource: %s\nPolicy: %s\nSelected gates: %s\nJobs: %d\n", m.Status, m.RunID, m.Current, m.Inputs.SourceSHA256, m.Inputs.PolicySHA256, displayGateList(m.SelectedGates), m.Jobs)
	for _, gate := range m.Gates {
		fmt.Fprintf(out, "- Gate %s: %s (%s); reason=%s; stale inputs=%s\n", gate.ID, gate.Status, gate.Action, gate.Reason, strings.Join(gate.StaleCategories, ", "))
	}
	fmt.Fprintln(out, "Replay with: polis gates --replay <this-run.json> --repo <same-repo> [--policy <same-policy>] [--contract <same-contract>] --environment-id <same-nonsecret-version> --out-run <new-external.json>")
	fmt.Fprintln(out, deliveryArtifactNotice)
}
