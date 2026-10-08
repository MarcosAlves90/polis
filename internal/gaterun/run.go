package gaterun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Options struct {
	Version        string
	Contract       string
	EnvironmentID  string
	Selected       []string
	Affected       bool
	Jobs           int
	Reuse          string
	Replay         string
	Out            string
	OnGateStart    func(spec.GatePolicy)
	OnGateComplete func(spec.GatePolicy, policyexec.Outcome, int64)
}

func Run(ctx context.Context, repo string, plan policyplan.Plan, opts Options) (Manifest, policyexec.Result, error) {
	var empty Manifest
	var noResult policyexec.Result
	if opts.Jobs == 0 {
		opts.Jobs = 1
	}
	if err := policyexec.ValidateJobs(opts.Jobs); err != nil {
		return empty, noResult, err
	}
	if opts.Version == "" {
		return empty, noResult, errors.New("POLIS version is required")
	}
	if !validEnvironmentID(opts.EnvironmentID) {
		return empty, noResult, errors.New("environment-id must be a nonsecret identifier of at most 128 ASCII letters, digits, '.', '_', ':', '-' ")
	}
	if len(plan.DeferredGates) != 0 {
		return empty, noResult, errors.New("incremental runs do not defer producer validation")
	}
	if opts.Replay != "" && (opts.Reuse != "" || opts.Affected || len(opts.Selected) != 0) {
		return empty, noResult, errors.New("replay cannot be combined with reuse or selection")
	}
	if opts.Out != "" {
		abs, err := filepath.Abs(opts.Out)
		if err != nil {
			return empty, noResult, err
		}
		contained, err := pathguard.Contains(repo, abs)
		if err != nil {
			return empty, noResult, err
		}
		if contained {
			return empty, noResult, errors.New("gate-run output must be outside target worktree")
		}
		if _, err := os.Lstat(abs); !errors.Is(err, os.ErrNotExist) {
			return empty, noResult, errors.New("gate-run output already exists or cannot be inspected")
		}
		parent, err := os.Stat(filepath.Dir(abs))
		if err != nil || !parent.IsDir() {
			return empty, noResult, errors.New("gate-run output parent directory is unavailable")
		}
	}
	var previous *Manifest
	inputRecord := opts.Reuse
	if opts.Replay != "" {
		inputRecord = opts.Replay
	}
	if inputRecord != "" {
		m, err := Load(inputRecord)
		if err != nil {
			return empty, noResult, fmt.Errorf("prior gate run: %w", err)
		}
		previous = &m
	}
	in, base, err := loadInputs(ctx, repo, plan, opts)
	if err != nil {
		return empty, noResult, err
	}
	paths := []string{}
	if opts.Affected {
		paths, err = changedPaths(ctx, repo, base)
		if err != nil {
			return empty, noResult, err
		}
	}
	if opts.Replay != "" {
		if opts.EnvironmentID == "" {
			return empty, noResult, errors.New("replay requires an explicit environment-id assertion")
		}
		if !previous.Current {
			return empty, noResult, errors.New("prior run is stale: source changed during validation")
		}
		for _, gate := range plan.GatePolicies() {
			old := priorGate(previous, gate.ID)
			if old == nil {
				return empty, noResult, errors.New("replay inputs unavailable: gate inventory")
			}
			changed := differences(identity(previous.Inputs, old.Definition, nil), identity(in, gate, nil))
			if len(changed) != 0 {
				return empty, noResult, fmt.Errorf("replay inputs missing or changed for %s: %s", gate.ID, strings.Join(changed, ", "))
			}
		}
		opts.Selected = previous.SelectedGates
		opts.Jobs = previous.Jobs
		if err := policyexec.ValidateJobs(opts.Jobs); err != nil {
			return empty, noResult, err
		}
	}
	selected, selectionReason, err := selectGates(plan, opts.Selected, opts.Affected, paths)
	if err != nil {
		return empty, noResult, err
	}
	if opts.Replay != "" {
		selectionReason = "replay: new execution of recorded selection"
		if len(previous.SelectedGates) == 0 {
			selected = map[string]bool{}
		}
	}
	// Scope assertions only apply to incremental reuse; replay remains bound to
	// the exact global source identity. Compute selected file snapshots before
	// launching any commands, and fail closed if a declared scope is unsafe.
	scopedSources := map[string]string{}
	if opts.Replay == "" {
		for _, gate := range plan.GatePolicies() {
			if !selected[gate.ID] || !gate.InputPathsComplete || containsRepoRootInput(gate.InputPaths) {
				continue
			}
			sha, snapshotErr := scopedSnapshot(ctx, repo, gate.InputPaths)
			if snapshotErr != nil {
				return empty, noResult, fmt.Errorf("gate %s input scope: %w", gate.ID, snapshotErr)
			}
			scopedSources[gate.ID] = sha
		}
	}
	manifest := Manifest{SchemaVersion: 1, Inputs: in, SelectedGates: []string{}, SelectionReason: selectionReason,
		ChangedPaths: paths, Jobs: opts.Jobs, Current: true, Gates: []Gate{}}
	if opts.Replay != "" {
		manifest.ReplayOf = previous.RunID
	}
	records := map[string]*Gate{}
	for _, gate := range plan.GatePolicies() {
		records[gate.ID] = &Gate{ID: gate.ID, Definition: gate}
		if selected[gate.ID] {
			for _, relative := range []string{gate.Command.Cwd, gate.Report} {
				if relative == "" {
					continue
				}
				contained, err := pathguard.Contains(repo, filepath.Join(repo, filepath.FromSlash(relative)))
				if err != nil || !contained {
					return empty, noResult, fmt.Errorf("gate %s input/output path resolves outside worktree", gate.ID)
				}
			}
			manifest.SelectedGates = append(manifest.SelectedGates, gate.ID)
		}
	}
	var reuse func(spec.GatePolicy, policyexec.Result) (policyexec.Outcome, bool)
	if previous != nil && opts.Replay == "" {
		reuse = func(gate spec.GatePolicy, result policyexec.Result) (policyexec.Outcome, bool) {
			record := records[gate.ID]
			head, source, err := snapshot(ctx, repo)
			if err != nil || head != in.Head || source != in.SourceSHA256 {
				manifest.Current = false
				return policyexec.Outcome{Status: spec.StatusBlocked, Action: "blocked", Reason: "source changed during validation; evidence is stale"}, true
			}
			dependencies := map[string]string{}
			for _, dep := range gate.DependsOn {
				dependencies[dep] = resultIdentity(records[dep].Identity, result.Outcomes[dep])
			}
			record.Identity = gateRunIdentity(in, gate, dependencies, scopedSources)
			old := priorGate(previous, gate.ID)
			if old == nil {
				record.StaleCategories = []string{"identity_missing_or_invalid"}
				return policyexec.Outcome{}, false
			}
			record.StaleCategories = differences(old.Identity, record.Identity)
			if !previous.Current {
				record.StaleCategories = append(record.StaleCategories, "source_changed_during_run")
			}
			if opts.EnvironmentID == "" {
				record.StaleCategories = append(record.StaleCategories, "environment_unversioned")
			}
			if len(record.StaleCategories) != 0 || old.Status != spec.StatusPass || (old.Action != "executed" && old.Action != "reused") || old.Observation == nil || old.Observation.ExitCode != 0 {
				return policyexec.Outcome{}, false
			}
			record.ReusedFrom = previous.RunID
			o := old.Observation
			return policyexec.Outcome{Status: spec.StatusPass, Action: "reused", Reason: "matching input identity and caller-versioned environment",
				Command: &policyexec.CommandExecution{Argv: append([]string{}, gate.Command.Argv...), Cwd: gate.Command.Cwd,
					Observation: commandexec.Observation{Status: spec.StatusPass, ExitCode: o.ExitCode, DurationMS: o.DurationMS, StdoutSHA256: o.StdoutSHA256, StderrSHA256: o.StderrSHA256}}}, true
		}
	}
	result := policyexec.ExecutePlanWithOptions(plan, repo, io.Discard, policyexec.Options{Jobs: opts.Jobs, Selected: selected, Reuse: reuse, OnGateStart: opts.OnGateStart, OnGateComplete: opts.OnGateComplete})
	for _, gate := range plan.GatePolicies() {
		record := records[gate.ID]
		o := result.Outcomes[gate.ID]
		record.Status, record.Action, record.Reason = o.Status, o.Action, o.Reason
		if o.Command != nil {
			obs := o.Command.Observation
			record.Observation = &Observation{ExitCode: obs.ExitCode, DurationMS: obs.DurationMS, StdoutSHA256: obs.StdoutSHA256, StderrSHA256: obs.StderrSHA256}
		}
		if record.Identity.SHA256 == "" {
			dependencies := map[string]string{}
			for _, dep := range gate.DependsOn {
				dependencies[dep] = resultIdentity(records[dep].Identity, result.Outcomes[dep])
			}
			record.Identity = gateRunIdentity(in, gate, dependencies, scopedSources)
		}
		manifest.Gates = append(manifest.Gates, *record)
	}
	// Commands are allowed to produce outputs, but a changed source state cannot
	// retain a current passing identity. Never reseal results to new inputs.
	head, source, err := snapshot(ctx, repo)
	// A gate may write an ignored file declared as its own input. The global
	// source snapshot omits ignored files, so recheck each scoped input set too.
	for _, gate := range plan.GatePolicies() {
		before, scoped := scopedSources[gate.ID]
		if !scoped {
			continue
		}
		after, scopeErr := scopedSnapshot(ctx, repo, gate.InputPaths)
		if scopeErr != nil || after != before {
			manifest.Current = false
		}
	}
	if err != nil || head != in.Head || source != in.SourceSHA256 || !manifest.Current {
		manifest.Current = false
		result.Overall = spec.StatusBlocked
		for i := range manifest.Gates {
			g := &manifest.Gates[i]
			if g.Status == spec.StatusPass {
				g.Status = spec.StatusBlocked
				g.Reason = "source changed during validation; evidence is stale"
				g.StaleCategories = append(g.StaleCategories, "source")
				result.Gates[g.ID] = spec.StatusBlocked
				o := result.Outcomes[g.ID]
				o.Status, o.Reason = g.Status, g.Reason
				result.Outcomes[g.ID] = o
			}
		}
	}
	manifest.Status = result.Overall
	if err := manifest.seal(); err != nil {
		return manifest, result, err
	}
	if opts.Out != "" {
		if err := Write(repo, opts.Out, manifest); err != nil {
			return manifest, result, err
		}
	}
	return manifest, result, nil
}

func priorGate(m *Manifest, id string) *Gate {
	for i := range m.Gates {
		if m.Gates[i].ID == id {
			return &m.Gates[i]
		}
	}
	return nil
}

func resultIdentity(id Identity, o policyexec.Outcome) string {
	var output any
	if o.Command != nil {
		obs := o.Command.Observation
		output = []any{obs.ExitCode, obs.StdoutSHA256, obs.StderrSHA256}
	}
	return digest([]any{id.SHA256, o.Status, output})
}

func validEnvironmentID(id string) bool {
	if len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return true
}

func gateRunIdentity(in Inputs, gate spec.GatePolicy, deps map[string]string, scopedSources map[string]string) Identity {
	if sha, ok := scopedSources[gate.ID]; ok && gate.InputPathsComplete {
		return scopedIdentity(in, gate, deps, sha)
	}
	return identity(in, gate, deps)
}

// The repository root is already covered by the existing global source identity.
// Its declaration cannot narrow the dependency closure.
func containsRepoRootInput(paths []string) bool {
	for _, path := range paths {
		if path == "." {
			return true
		}
	}
	return false
}
