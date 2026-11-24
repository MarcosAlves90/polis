package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/MarcosAlves90/polis/v6/internal/changeexec"
	"github.com/MarcosAlves90/polis/v6/internal/commandexec"
	"github.com/MarcosAlves90/polis/v6/internal/diagnostic"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyexec"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

// StageStart reports a validation stage actually being entered. The returned
// function completes that stage exactly once with PASS or FAIL.
type StageStart = func(id, action, why string) func(string)

type Validation struct {
	BaselineRepo          string
	BaselineCommit        string
	TargetRepo            string
	TargetBaseCommit      string
	SkipBaselineProof     bool
	TargetTree            string
	Patch                 []byte
	RegressionPatch       []byte
	Change                spec.ChangeContract
	Policy                spec.Policy
	ExecutionPlan         *policyplan.Plan
	PolicyResult          *policyexec.Result
	Evidence              io.Writer
	RedWorktreePattern    string
	TargetWorktreePattern string
	CreateWorktreeError   string
	TargetApplyCheckError string
	TargetApplyError      string
	PolicyFailureLabel    string
	PolicyFailureStage    string
	PolicyFailureNotRun   []string
	Jobs                  int
	OnStageStart          StageStart
	OnGateStart           func(spec.GatePolicy)
	OnGateComplete        func(spec.GatePolicy, policyexec.Outcome, int64)
	OnGateResult          func(spec.GatePolicy, policyexec.Outcome, int64)
	OnOutput              commandexec.OutputObserver
	OnChangeStart         func(string, spec.CommandSpec)
	OnChangeResult        func(string, spec.CommandSpec, spec.Status, int64)
}

type stageTracker struct {
	end    func(string)
	status string
}

func startStage(onStart StageStart, id, action, why string) *stageTracker {
	tracker := &stageTracker{status: "FAIL", end: func(string) {}}
	if onStart == nil {
		return tracker
	}
	if end := onStart(id, action, why); end != nil {
		tracker.end = end
	}
	return tracker
}

func (s *stageTracker) pass() {
	if s != nil {
		s.status = "PASS"
	}
}

func (s *stageTracker) finish() {
	if s != nil {
		s.end(s.status)
	}
}

func Validate(ctx context.Context, validation Validation) error {
	var redPaths map[string]string
	if !validation.SkipBaselineProof && validation.Change.RequiresBaselineProof() {
		stage := startStage(validation.OnStageStart, "red_green_regression",
			"replay the captured Red proof and target change gates",
			"the locked contract must pass in an isolated target worktree")
		var err error
		redPaths, err = validateRegression(ctx, validation)
		stage.finishWith(err)
		if err != nil {
			return err
		}
	}
	return validateTarget(ctx, validation, redPaths)
}

func (s *stageTracker) finishWith(err error) {
	if err == nil {
		s.pass()
	}
	s.finish()
}

func (v Validation) changeHooks(ctx context.Context) changeexec.Hooks {
	return changeexec.Hooks{Context: ctx, OnOutput: v.OnOutput, OnStart: v.OnChangeStart, OnResult: v.OnChangeResult}
}

func validateRegression(ctx context.Context, validation Validation) (map[string]string, error) {
	if validation.SkipBaselineProof || !validation.Change.RequiresBaselineProof() {
		return nil, nil
	}
	worktree, cleanup, err := gitutil.DetachedWorktree(
		ctx,
		validation.BaselineRepo,
		validation.BaselineCommit,
		validation.RedWorktreePattern,
		"create isolated worktree staging",
		validation.CreateWorktreeError,
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	redProof, err := prepareRegressionProof(ctx, validation, worktree)
	if err != nil {
		return nil, err
	}
	if err := changeexec.ExecuteBaselineWithHooks(validation.Change, worktree, validation.Evidence, validation.changeHooks(ctx)); err != nil {
		return nil, fmt.Errorf("regression baseline validation: %w", err)
	}
	return redProof, nil
}

func prepareRegressionProof(ctx context.Context, validation Validation, worktree string) (map[string]string, error) {
	if !validation.Change.RequiresRegressionPatch() {
		return nil, nil
	}
	if err := applyRegressionPatch(ctx, worktree, validation.RegressionPatch); err != nil {
		return nil, err
	}
	return captureRegressionProof(ctx, worktree, validation.Change.IsStrictDevelopment())
}

func applyRegressionPatch(ctx context.Context, worktree string, patch []byte) error {
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--check", "-"); err != nil {
		return fmt.Errorf("regression probe apply check failed: %w", err)
	}
	if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(patch), "apply", "--index", "-"); err != nil {
		return fmt.Errorf("regression probe apply failed: %w", err)
	}
	return nil
}

func captureRegressionProof(ctx context.Context, worktree string, strict bool) (map[string]string, error) {
	changedPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return nil, err
	}
	redProof := make(map[string]string, len(changedPaths))
	for path := range changedPaths {
		blob, err := regressionBlobID(ctx, worktree, path, strict)
		if err != nil {
			return nil, err
		}
		redProof[path] = blob
	}
	return redProof, nil
}

func regressionBlobID(ctx context.Context, worktree, path string, strict bool) (string, error) {
	if !strict {
		return "", nil
	}
	blob, err := indexBlobID(ctx, worktree, path)
	if err != nil {
		return "", fmt.Errorf("capture strict Red proof path %q: %w", path, err)
	}
	return blob, nil
}

func validateTarget(ctx context.Context, validation Validation, redProof map[string]string) error {
	targetBaseCommit := validation.TargetBaseCommit
	if targetBaseCommit == "" {
		targetBaseCommit = validation.BaselineCommit
	}
	var worktree string
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	preparationStage := startStage(validation.OnStageStart, "isolated_target_preparation",
		"prepare the isolated target worktree",
		"validation must run against the exact locked target tree")
	preparationErr := func() error {
		var err error
		worktree, cleanup, err = gitutil.DetachedWorktree(
			ctx,
			validation.TargetRepo,
			targetBaseCommit,
			validation.TargetWorktreePattern,
			"create isolated worktree staging",
			validation.CreateWorktreeError,
		)
		if err != nil {
			return err
		}
		if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.Patch), "apply", "--check", "-"); err != nil {
			return gitutil.Wrap(validation.TargetApplyCheckError, err)
		}
		if _, err := gitutil.Bytes(ctx, worktree, nil, bytes.NewReader(validation.Patch), "apply", "--index", "-"); err != nil {
			return gitutil.Wrap(validation.TargetApplyError, err)
		}
		if err := requireRegressionPaths(ctx, worktree, redProof); err != nil {
			return err
		}
		if err := gitutil.RequireTargetTree(ctx, worktree, validation.TargetTree); err != nil {
			return err
		}
		changedPaths, err := gitutil.ChangedTreePaths(ctx, worktree, targetBaseCommit, validation.TargetTree)
		if err != nil {
			return err
		}
		if err := validation.Change.ValidateChangedPaths(changedPaths); err != nil {
			return fmt.Errorf("consumer change scope validation: %w", err)
		}
		if err := changeexec.ExecuteTargetWithHooks(validation.Change, worktree, validation.Evidence, validation.changeHooks(ctx)); err != nil {
			return err
		}
		return nil
	}()
	preparationStage.finishWith(preparationErr)
	if preparationErr != nil {
		return preparationErr
	}

	gateStage := startStage(validation.OnStageStart, "project_gate_execution",
		"execute project gates",
		"the effective strict plan must pass against the isolated target")
	gateErr := func() error {
		jobs := validation.Jobs
		if jobs == 0 {
			jobs = 1
		}
		var result policyexec.Result
		if validation.ExecutionPlan != nil {
			result = policyexec.ExecutePlanWithOptions(*validation.ExecutionPlan, worktree, validation.Evidence, policyexec.Options{Jobs: jobs, Context: ctx, OnOutput: validation.OnOutput, OnGateStart: validation.OnGateStart, OnGateComplete: validation.OnGateComplete, OnGateResult: validation.OnGateResult})
		} else {
			result = policyexec.ExecuteWithOptions(validation.Policy, worktree, validation.Evidence, policyexec.Options{Jobs: jobs, Context: ctx, OnOutput: validation.OnOutput, OnGateStart: validation.OnGateStart, OnGateComplete: validation.OnGateComplete, OnGateResult: validation.OnGateResult})
		}
		if validation.PolicyResult != nil {
			*validation.PolicyResult = result
		}
		if result.Overall != spec.StatusPass {
			stage := validation.PolicyFailureStage
			if stage == "" {
				stage = validation.PolicyFailureLabel
			}
			gateStatuses := make(map[string]string, len(result.Gates))
			gateIDs := make([]string, 0, len(result.Gates))
			for gate, status := range result.Gates {
				gateStatuses[gate] = string(status)
				gateIDs = append(gateIDs, gate)
			}
			sort.Strings(gateIDs)
			notRun := append([]string(nil), validation.PolicyFailureNotRun...)
			for _, gate := range gateIDs {
				status := result.Gates[gate]
				if status == spec.StatusBlocked || status == spec.StatusDeferred || status == spec.StatusNotApplicable {
					notRun = append(notRun, fmt.Sprintf("gate %s (%s)", gate, status))
				}
			}
			commandIDs := make([]string, 0, len(result.CommandFailures))
			for gate := range result.CommandFailures {
				commandIDs = append(commandIDs, gate)
			}
			sort.Strings(commandIDs)
			commands := make([]diagnostic.Command, 0, len(commandIDs))
			for _, gate := range commandIDs {
				execution := result.CommandFailures[gate]
				observation := execution.Observation
				stdout, stdoutPreviewTruncated := diagnostic.BoundProcessOutput(observation.Stdout)
				stderr, stderrPreviewTruncated := diagnostic.BoundProcessOutput(observation.Stderr)
				commands = append(commands, diagnostic.Command{
					Gate: gate, Argv: append([]string(nil), execution.Argv...), Cwd: execution.Cwd,
					Status: string(observation.Status), ExitCode: observation.ExitCode, DurationMS: observation.DurationMS, TimedOut: observation.TimedOut,
					Prerequisite:  observation.Prerequisite,
					StdoutContext: stdout, StderrContext: stderr, StdoutBytes: observation.StdoutBytes, StderrBytes: observation.StderrBytes,
					StdoutSHA256: observation.StdoutSHA256, StderrSHA256: observation.StderrSHA256,
					StdoutTruncated: observation.StdoutTruncated || stdoutPreviewTruncated,
					StderrTruncated: observation.StderrTruncated || stderrPreviewTruncated,
				})
			}
			report := diagnostic.Report{
				Stage: stage, Condition: "one or more configured project gates did not pass",
				Expected:     map[string]any{"overall_gate_status": string(spec.StatusPass)},
				Actual:       map[string]any{"overall_gate_status": string(result.Overall)},
				GateStatuses: gateStatuses, Commands: commands, NotRun: notRun,
			}
			if len(commands) == 1 {
				report.Command = &commands[0]
			}
			report = diagnostic.ClassifyGateFailure(report)
			return &diagnostic.Error{Summary: fmt.Sprintf("%s %s", validation.PolicyFailureLabel, result.Overall), Report: report}
		}
		return nil
	}()
	gateStage.finishWith(gateErr)
	return gateErr
}

func requireRegressionPaths(ctx context.Context, worktree string, redProof map[string]string) error {
	targetPaths, err := gitutil.ChangedIndexPaths(ctx, worktree, "--cached")
	if err != nil {
		return err
	}
	for path, capturedBlob := range redProof {
		if _, ok := targetPaths[path]; !ok {
			return fmt.Errorf("regression probe path %q is absent from final payload", path)
		}
		if capturedBlob == "" {
			continue
		}
		targetBlob, err := indexBlobID(ctx, worktree, path)
		if err != nil {
			return fmt.Errorf("read final Red proof path %q: %w", path, err)
		}
		if targetBlob != capturedBlob {
			return fmt.Errorf("regression probe path %q differs from captured Red proof", path)
		}
	}
	return nil
}

func indexBlobID(ctx context.Context, worktree, path string) (string, error) {
	blob, err := gitutil.Output(ctx, worktree, nil, nil, "rev-parse", "--verify", ":"+path)
	if err != nil {
		return "", err
	}
	if blob == "" {
		return "", fmt.Errorf("empty index blob id")
	}
	return blob, nil
}
