package policyinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MarcosAlves90/polis/v5/spec"
)

const (
	ProfileAuto   = "auto"
	ProfileGo     = "go"
	ProfileCustom = "custom"
)

type Options struct {
	Repo              string
	Profile           string
	TestArgv          []string
	CoverageArgv      []string
	CoverageAdapter   string
	CoverageReport    string
	CoverageThreshold *float64
	DryRun            bool
}

type Result struct {
	Profile    string
	PolicyPath string
	Policy     []byte
}

func Init(ctx context.Context, opts Options) (Result, error) {
	repo := opts.Repo
	if repo == "" {
		repo = "."
	}
	profile := opts.Profile
	if profile == "" {
		profile = ProfileAuto
	}
	if err := validateProfileOptions(profile, opts); err != nil {
		return Result{}, err
	}
	root, err := resolveRepo(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	resolvedProfile, err := resolveProfile(root, profile)
	if err != nil {
		return Result{}, err
	}
	policy, err := policyForProfile(resolvedProfile, opts)
	if err != nil {
		return Result{}, err
	}
	encoded, err := encodePolicy(policy)
	if err != nil {
		return Result{}, err
	}
	policyPath := filepath.Join(root, ".polis", "policy.json")
	result := Result{Profile: resolvedProfile, PolicyPath: policyPath, Policy: encoded}
	if opts.DryRun {
		return result, nil
	}
	if err := writePolicy(policyPath, encoded); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateProfileOptions(profile string, opts Options) error {
	if profile == ProfileCustom {
		return nil
	}
	if len(opts.TestArgv) != 0 || len(opts.CoverageArgv) != 0 || opts.CoverageAdapter != "" || opts.CoverageReport != "" || opts.CoverageThreshold != nil {
		return errors.New("custom init options require --profile custom")
	}
	return nil
}

func policyForProfile(profile string, opts Options) (spec.Policy, error) {
	switch profile {
	case ProfileGo:
		return goPolicy(), nil
	case ProfileCustom:
		return customPolicy(opts), nil
	default:
		return spec.Policy{}, fmt.Errorf("unsupported profile %q", profile)
	}
}

func encodePolicy(policy spec.Policy) ([]byte, error) {
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode policy: %w", err)
	}
	encoded = append(encoded, '\n')
	if _, err := spec.DecodePolicy(encoded); err != nil {
		return nil, fmt.Errorf("generated policy failed self-validation: %w", err)
	}
	return encoded, nil
}

func writePolicy(policyPath string, encoded []byte) error {
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		return fmt.Errorf("create .polis directory: %w", err)
	}
	f, err := os.OpenFile(policyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New(".polis/policy.json already exists; POLIS init never overwrites policy")
		}
		return fmt.Errorf("create .polis/policy.json: %w", err)
	}
	if _, err := f.Write(encoded); err != nil {
		_ = f.Close()
		_ = os.Remove(policyPath)
		return fmt.Errorf("write .polis/policy.json: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(policyPath)
		return fmt.Errorf("close .polis/policy.json: %w", err)
	}
	return nil
}

func resolveRepo(ctx context.Context, repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", abs, "rev-parse", "--show-toplevel")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("target is not a Git worktree: %w: %s", err, strings.TrimSpace(string(b)))
	}
	root := strings.TrimSpace(string(b))
	if root == "" {
		return "", errors.New("Git returned an empty worktree root")
	}
	return filepath.Abs(root)
}

func resolveProfile(root, requested string) (string, error) {
	switch requested {
	case ProfileGo:
		if !regularFile(filepath.Join(root, "go.mod")) {
			return "", errors.New("Go profile requires a root-level go.mod")
		}
		return ProfileGo, nil
	case ProfileCustom:
		return ProfileCustom, nil
	case ProfileAuto:
		if regularFile(filepath.Join(root, "go.mod")) {
			return ProfileGo, nil
		}
		return "", errors.New("no supported POLIS init profile detected; V5 auto-detection supports only root-level go.mod; use --profile custom with explicit test and coverage commands")
	default:
		return "", fmt.Errorf("unknown init profile %q; supported profiles: auto, go, custom", requested)
	}
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func goPolicy() spec.Policy {
	reason := func(v string) *string { return &v }
	threshold := 80.0
	return spec.Policy{
		SchemaVersion: spec.PolicySchemaVersion,
		Gates: []spec.GatePolicy{
			{ID: "test.complete", Mode: spec.GateModeCommand, Command: command(1200, "go", "test", "./...")},
			{ID: "coverage", Mode: spec.GateModeCoverage, Command: command(1200, "go", "test", "-coverpkg=./...", "./...", "-coverprofile=.polis/coverage.out"), Adapter: spec.CoverageAdapterGoCoverProfileV1, Report: ".polis/coverage.out", Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold},
			{ID: "lint", Mode: spec.GateModeCommand, Command: command(600, "go", "vet", "./...")},
			{ID: "typecheck", Mode: spec.GateModeNotApplicable, Reason: reason("Go test/build perform compile checks; the canonical Go profile defines no independent typecheck command")},
			{ID: "build", Mode: spec.GateModeCommand, Command: command(1200, "go", "build", "./...")},
			{ID: "smoke", Mode: spec.GateModeNotApplicable, Reason: reason("the canonical Go profile cannot infer a project-specific runtime smoke command")},
			{ID: "compatibility", Mode: spec.GateModeNotApplicable, Reason: reason("the canonical Go profile cannot infer a project-specific compatibility surface")},
			{ID: "dependency", Mode: spec.GateModeCommand, Command: command(600, "go", "mod", "verify")},
			{ID: "migration", Mode: spec.GateModeNotApplicable, Reason: reason("the canonical Go profile cannot infer a persisted-state migration contract")},
			{ID: "security", Mode: spec.GateModeNotApplicable, Reason: reason("the canonical Go profile does not assume an external vulnerability scanner is installed")},
			{ID: "platform", Mode: spec.GateModeNotApplicable, Reason: reason("local policy bootstrap does not establish cross-platform runtime evidence")},
		},
	}
}

func customPolicy(opts Options) spec.Policy {
	threshold := spec.MinimumCoverageThreshold
	if opts.CoverageThreshold != nil {
		threshold = *opts.CoverageThreshold
	}
	notApplicable := func(id string) spec.GatePolicy {
		reason := fmt.Sprintf("custom profile has no explicit command for %s", id)
		return spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason}
	}
	return spec.Policy{
		SchemaVersion: spec.PolicySchemaVersion,
		Gates: []spec.GatePolicy{
			{ID: "test.complete", Mode: spec.GateModeCommand, Command: commandFromArgv(1200, opts.TestArgv)},
			{ID: "coverage", Mode: spec.GateModeCoverage, Command: commandFromArgv(1200, opts.CoverageArgv), Adapter: opts.CoverageAdapter, Report: opts.CoverageReport, Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: &threshold},
			notApplicable("lint"),
			notApplicable("typecheck"),
			notApplicable("build"),
			notApplicable("smoke"),
			notApplicable("compatibility"),
			notApplicable("dependency"),
			notApplicable("migration"),
			notApplicable("security"),
			notApplicable("platform"),
		},
	}
}

func command(timeout int, argv ...string) *spec.CommandSpec {
	return commandFromArgv(timeout, argv)
}

func commandFromArgv(timeout int, argv []string) *spec.CommandSpec {
	return &spec.CommandSpec{
		Argv: append([]string(nil), argv...), Cwd: ".", TimeoutSeconds: timeout,
		Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: defaultEnvironmentPass()},
	}
}

func defaultEnvironmentPass() []string {
	return []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SystemRoot", "COMSPEC", "PATHEXT", "USERPROFILE", "LOCALAPPDATA"}
}
