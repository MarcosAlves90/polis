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

	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	ProfileAuto        = "auto"
	ProfileGo          = "go"
	ProfileCustom      = "custom"
	testCompleteGateID = "test.complete"
)

type Options struct {
	Repo              string
	Profile           string
	ValidationLevel   string
	DisabledGates     []string
	TestArgv          []string
	CoverageArgv      []string
	CoverageAdapter   string
	CoverageReport    string
	CoverageThreshold *float64
	DryRun            bool
}

type Result struct {
	Profile         string
	ValidationLevel string
	EnabledGates    []string
	DisabledGates   []string
	PolicyPath      string
	Policy          []byte
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
	summary := policy.ValidationSummary()
	result := Result{
		Profile:         resolvedProfile,
		ValidationLevel: summary.Level,
		EnabledGates:    append([]string{}, summary.EnabledGates...),
		DisabledGates:   append([]string{}, summary.DisabledGates...),
		PolicyPath:      policyPath,
		Policy:          encoded,
	}
	if opts.DryRun {
		return result, nil
	}
	if err := writePolicy(policyPath, encoded); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateProfileOptions(profile string, opts Options) error {
	level := opts.ValidationLevel
	if level == "" {
		level = spec.ValidationLevelStrict
	}
	if err := spec.ValidateValidationLevel(level); err != nil {
		return err
	}
	if err := validateDisabledGates(level, opts.DisabledGates); err != nil {
		return err
	}
	if profile == ProfileCustom {
		return validateCustomOptions(level, opts)
	}
	if len(opts.TestArgv) != 0 || len(opts.CoverageArgv) != 0 || opts.CoverageAdapter != "" || opts.CoverageReport != "" || opts.CoverageThreshold != nil {
		return errors.New("custom init options require --profile custom")
	}
	return nil
}

func validateCustomOptions(level string, opts Options) error {
	if len(opts.TestArgv) == 0 && level != spec.ValidationLevelMinimal {
		return errors.New("custom profile requires test argv unless validation level is minimal")
	}
	coverageValues := len(opts.CoverageArgv) != 0 || opts.CoverageAdapter != "" || opts.CoverageReport != "" || opts.CoverageThreshold != nil
	if !coverageValues {
		if level == spec.ValidationLevelStrict {
			return errors.New("custom profile requires coverage argv, adapter, and report at strict validation level")
		}
		return nil
	}
	if len(opts.CoverageArgv) == 0 || opts.CoverageAdapter == "" || opts.CoverageReport == "" {
		return errors.New("custom coverage configuration requires coverage argv, adapter, and report")
	}
	return nil
}

func validateDisabledGates(level string, disabled []string) error {
	seen := make(map[string]struct{}, len(disabled))
	for _, id := range disabled {
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate disabled gate %q", id)
		}
		seen[id] = struct{}{}
		if !knownProjectGate(id) {
			return fmt.Errorf("unknown disabled gate %q", id)
		}
		if id == testCompleteGateID && level != spec.ValidationLevelMinimal {
			return fmt.Errorf("gate %q can be disabled only at validation level %q", id, spec.ValidationLevelMinimal)
		}
		if id == "coverage" && level == spec.ValidationLevelStrict {
			return fmt.Errorf("gate %q cannot be disabled at validation level %q", id, level)
		}
	}
	return nil
}

func knownProjectGate(id string) bool {
	for _, known := range spec.ProjectGateOrder {
		if id == known {
			return true
		}
	}
	return false
}

func policyForProfile(profile string, opts Options) (spec.Policy, error) {
	level := opts.ValidationLevel
	if level == "" {
		level = spec.ValidationLevelStrict
	}
	var policy spec.Policy
	switch profile {
	case ProfileGo:
		policy = goPolicy()
	case ProfileCustom:
		policy = customPolicy(opts)
	default:
		return spec.Policy{}, fmt.Errorf("unsupported profile %q", profile)
	}
	if level != spec.ValidationLevelStrict {
		policy.ValidationLevel = level
	}
	if profile != ProfileCustom {
		applyLevelDefaults(&policy, level)
	}
	applyDisabledGates(&policy, opts.DisabledGates, level)
	return policy, nil
}

func applyLevelDefaults(policy *spec.Policy, level string) {
	switch level {
	case spec.ValidationLevelStandard:
		disableGate(policy, "coverage", "disabled by the standard validation level; coverage is a non-structural project-quality gate")
	case spec.ValidationLevelMinimal:
		for _, id := range spec.ProjectGateOrder {
			disableGate(policy, id, "disabled by the minimal validation level; project-quality gates are not required in this execution context")
		}
	}
}

func applyDisabledGates(policy *spec.Policy, disabled []string, level string) {
	for _, id := range disabled {
		disableGate(policy, id, fmt.Sprintf("disabled by explicit validation configuration at level %q", level))
	}
}

func disableGate(policy *spec.Policy, id, reason string) {
	for i := range policy.Gates {
		if policy.Gates[i].ID != id {
			continue
		}
		policy.Gates[i].Mode = spec.GateModeNotApplicable
		policy.Gates[i].Command = nil
		policy.Gates[i].Reason = &reason
		policy.Gates[i].Adapter = ""
		policy.Gates[i].Report = ""
		policy.Gates[i].Operator = ""
		policy.Gates[i].ThresholdPercent = nil
		return
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
		return "", errors.New("no supported POLIS init profile detected; V6 auto-detection supports only root-level go.mod; use --profile custom with explicit test and coverage commands")
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
			{ID: testCompleteGateID, Mode: spec.GateModeCommand, Command: command(1200, "go", "test", "./...")},
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
	testGate := spec.GatePolicy{ID: testCompleteGateID, Mode: spec.GateModeCommand, Command: commandFromArgv(1200, opts.TestArgv)}
	if len(opts.TestArgv) == 0 {
		testGate = notApplicable(testCompleteGateID, "custom profile has no explicit test command for this validation level")
	}
	coverageGate := spec.GatePolicy{ID: "coverage", Mode: spec.GateModeCoverage, Command: commandFromArgv(1200, opts.CoverageArgv), Adapter: opts.CoverageAdapter, Report: opts.CoverageReport, Operator: spec.CoverageOperatorGreaterThan, ThresholdPercent: coverageThreshold(opts)}
	if len(opts.CoverageArgv) == 0 {
		coverageGate = notApplicable("coverage", "custom profile has no explicit coverage command for this validation level")
	}
	return spec.Policy{
		SchemaVersion: spec.PolicySchemaVersion,
		Gates: []spec.GatePolicy{
			testGate,
			coverageGate,
			notApplicable("lint", "custom profile has no explicit command for lint"),
			notApplicable("typecheck", "custom profile has no explicit command for typecheck"),
			notApplicable("build", "custom profile has no explicit command for build"),
			notApplicable("smoke", "custom profile has no explicit command for smoke"),
			notApplicable("compatibility", "custom profile has no explicit command for compatibility"),
			notApplicable("dependency", "custom profile has no explicit command for dependency"),
			notApplicable("migration", "custom profile has no explicit command for migration"),
			notApplicable("security", "custom profile has no explicit command for security"),
			notApplicable("platform", "custom profile has no explicit command for platform"),
		},
	}
}

func coverageThreshold(opts Options) *float64 {
	threshold := spec.MinimumCoverageThreshold
	if opts.CoverageThreshold != nil {
		threshold = *opts.CoverageThreshold
	}
	return &threshold
}

func notApplicable(id, reasonValue string) spec.GatePolicy {
	reason := reasonValue
	return spec.GatePolicy{ID: id, Mode: spec.GateModeNotApplicable, Reason: &reason}
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
