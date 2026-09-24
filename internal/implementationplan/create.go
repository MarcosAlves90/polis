package implementationplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MarcosAlves90/polis/v6/internal/devlock"
	"github.com/MarcosAlves90/polis/v6/internal/fileutil"
	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/pathguard"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/internal/policyplan"
	"github.com/MarcosAlves90/polis/v6/spec"
)

type Options struct {
	Repo     string
	Policy   string
	Contract string
	Out      string
}

type Result struct {
	Path   string                  `json:"path"`
	SHA256 string                  `json:"sha256"`
	Plan   spec.ImplementationPlan `json:"plan"`
}

// Create writes a generated plan only to an absent external output path. It
// performs read-only Git checks and never creates repository state.
func Create(ctx context.Context, opts Options) (Result, error) {
	if opts.Repo == "" || opts.Contract == "" || opts.Out == "" {
		return Result{}, errors.New("repo, contract, and out are required")
	}
	root, err := gitutil.ResolveRoot(ctx, opts.Repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return Result{}, err
	}
	if err := requireCleanRepository(ctx, root); err != nil {
		return Result{}, err
	}
	contractRaw, err := fileutil.ReadOutside(root, opts.Contract, fileutil.OutsideReadOptions{Max: int64(spec.MaxContractMemberBytes), OversizeMessage: "Change Contract exceeds maximum size"})
	if err != nil {
		return Result{}, fmt.Errorf("load Change Contract: %w", err)
	}
	contract, err := spec.DecodeChangeContract(contractRaw)
	if err != nil {
		return Result{}, fmt.Errorf("invalid Change Contract: %w", err)
	}
	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 {
		return Result{}, errors.New("polis implementation-plan requires locked Change Contract schema v4 or v6 produced by polis start")
	}
	if err := contract.Validate(); err != nil {
		return Result{}, fmt.Errorf("invalid locked Change Contract: %w", err)
	}
	policyRaw, policy, err := loadPolicy(ctx, root, opts.Policy)
	if err != nil {
		return Result{}, err
	}
	if policy.SchemaVersion != spec.PolicySchemaVersion {
		return Result{}, fmt.Errorf("implementation planning requires Project Policy schema v%d", spec.PolicySchemaVersion)
	}
	if err := devlock.ValidateRepository(ctx, root, contract); err != nil {
		return Result{}, fmt.Errorf("locked development baseline: %w", err)
	}
	if err := devlock.ValidatePolicy(contract, policyRaw); err != nil {
		return Result{}, fmt.Errorf("locked development baseline: %w", err)
	}
	execution, err := policyplan.Compile(policy)
	if err != nil {
		return Result{}, err
	}
	effectiveGateOrder, err := EffectiveExecutionOrder(execution)
	if err != nil {
		return Result{}, err
	}
	plan, err := Generate(contract, contractRaw, effectiveGateOrder)
	if err != nil {
		return Result{}, err
	}
	outAbs, err := validateOutputPath(root, opts.Out)
	if err != nil {
		return Result{}, err
	}
	if err := requireCleanRepository(ctx, root); err != nil {
		return Result{}, err
	}
	if err := devlock.ValidateRepository(ctx, root, contract); err != nil {
		return Result{}, fmt.Errorf("locked development baseline changed during planning: %w", err)
	}
	if err := devlock.ValidatePolicy(contract, policyRaw); err != nil {
		return Result{}, fmt.Errorf("locked development policy changed during planning: %w", err)
	}
	raw, err := encodePlan(plan)
	if err != nil {
		return Result{}, err
	}
	if err := writePlan(outAbs, raw); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(raw)
	return Result{Path: outAbs, SHA256: hex.EncodeToString(sum[:]), Plan: plan}, nil
}

// Load reads and validates an optional plan supplied by its caller. An empty
// filename means the caller selected the unplanned workflow.
func Load(repo, filename string, contract spec.ChangeContract, contractRaw []byte) ([]byte, *spec.ImplementationPlan, error) {
	if filename == "" {
		return nil, nil, nil
	}
	raw, err := fileutil.ReadOutside(repo, filename, fileutil.OutsideReadOptions{Max: int64(spec.MaxImplementationPlanBytes), OversizeMessage: "implementation plan exceeds maximum size"})
	if err != nil {
		return nil, nil, fmt.Errorf("load implementation plan: %w", err)
	}
	plan, err := spec.DecodeImplementationPlan(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := plan.ValidateAgainst(contract, contractRaw); err != nil {
		return nil, nil, err
	}
	return raw, &plan, nil
}

func loadPolicy(ctx context.Context, root, filename string) ([]byte, spec.Policy, error) {
	if filename != "" {
		return policyload.LoadExternal(root, filename)
	}
	return policyload.LoadCommitted(ctx, root)
}

// EffectiveExecutionOrder returns enabled gates in the order compiled by
// policyplan, excluding disabled and not-applicable gates.
func EffectiveExecutionOrder(plan policyplan.Plan) ([]string, error) {
	enabled := make(map[string]struct{}, len(plan.EnabledGates))
	for _, gate := range plan.EnabledGates {
		enabled[gate] = struct{}{}
	}
	order := make([]string, 0, len(enabled))
	seen := make(map[string]struct{}, len(plan.ExecutionOrder))
	for _, gate := range plan.ExecutionOrder {
		if _, duplicate := seen[gate]; duplicate {
			return nil, fmt.Errorf("policyplan execution order repeats project gate %q", gate)
		}
		seen[gate] = struct{}{}
		if _, ok := enabled[gate]; ok {
			order = append(order, gate)
		}
	}
	if len(order) != len(enabled) {
		return nil, errors.New("policyplan execution order does not cover every enabled Project Policy gate")
	}
	return order, nil
}

func requireCleanRepository(ctx context.Context, root string) error {
	status, err := gitutil.Output(ctx, root, nil, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect source state: %w", err)
	}
	if status != "" {
		return errors.New("polis implementation-plan requires a clean HEAD, index, worktree, and no untracked files")
	}
	return nil
}

func validateOutputPath(root, filename string) (string, error) {
	if filename == "" {
		return "", errors.New("output path is required")
	}
	outAbs, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	contained, err := pathguard.Contains(root, outAbs)
	if err != nil {
		return "", fmt.Errorf("resolve output boundary: %w", err)
	}
	if contained {
		return "", errors.New("implementation plan output must be outside target worktree")
	}
	if err := requireAbsentOutput(outAbs); err != nil {
		return "", err
	}
	return outAbs, nil
}

func requireAbsentOutput(filename string) error {
	_, err := os.Lstat(filename)
	if err == nil {
		return fmt.Errorf("output already exists: %s", filename)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func encodePlan(plan spec.ImplementationPlan) ([]byte, error) {
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode implementation plan: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > spec.MaxImplementationPlanBytes {
		return nil, fmt.Errorf("generated implementation plan exceeds maximum size of %d bytes", spec.MaxImplementationPlanBytes)
	}
	return raw, nil
}

func writePlan(filename string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create implementation plan: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return fmt.Errorf("write implementation plan: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return fmt.Errorf("sync implementation plan: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filename)
		return fmt.Errorf("close implementation plan: %w", err)
	}
	return nil
}
