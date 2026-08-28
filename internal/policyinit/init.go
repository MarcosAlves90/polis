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

	"github.com/polis-dev/polis-v4/spec"
)

const (
	ProfileAuto = "auto"
	ProfileGo   = "go"
)

type Options struct {
	Repo    string
	Profile string
}

type Result struct {
	Profile    string
	PolicyPath string
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
	root, err := resolveRepo(ctx, repo)
	if err != nil {
		return Result{}, err
	}
	resolvedProfile, err := resolveProfile(root, profile)
	if err != nil {
		return Result{}, err
	}
	var policy spec.Policy
	switch resolvedProfile {
	case ProfileGo:
		policy = goPolicy()
	default:
		return Result{}, fmt.Errorf("unsupported profile %q", resolvedProfile)
	}
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode policy: %w", err)
	}
	encoded = append(encoded, '\n')
	if _, err := spec.DecodePolicy(encoded); err != nil {
		return Result{}, fmt.Errorf("generated policy failed self-validation: %w", err)
	}
	policyDir := filepath.Join(root, ".polis")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create .polis directory: %w", err)
	}
	policyPath := filepath.Join(policyDir, "policy.json")
	f, err := os.OpenFile(policyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return Result{}, errors.New(".polis/policy.json already exists; POLIS init never overwrites policy")
		}
		return Result{}, fmt.Errorf("create .polis/policy.json: %w", err)
	}
	if _, err := f.Write(encoded); err != nil {
		_ = f.Close()
		_ = os.Remove(policyPath)
		return Result{}, fmt.Errorf("write .polis/policy.json: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(policyPath)
		return Result{}, fmt.Errorf("close .polis/policy.json: %w", err)
	}
	return Result{Profile: resolvedProfile, PolicyPath: policyPath}, nil
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
	case ProfileAuto:
		if regularFile(filepath.Join(root, "go.mod")) {
			return ProfileGo, nil
		}
		return "", errors.New("no supported POLIS init profile detected; alpha.7 auto-detection supports only root-level go.mod")
	default:
		return "", fmt.Errorf("unknown init profile %q; supported profiles: auto, go", requested)
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
			{ID: "security", Mode: spec.GateModeNotApplicable, Reason: reason("alpha.7 does not assume an external Go vulnerability scanner is installed")},
			{ID: "platform", Mode: spec.GateModeNotApplicable, Reason: reason("local policy bootstrap does not establish cross-platform runtime evidence")},
		},
	}
}

func command(timeout int, argv ...string) *spec.CommandSpec {
	return &spec.CommandSpec{Argv: argv, Cwd: ".", TimeoutSeconds: timeout}
}
