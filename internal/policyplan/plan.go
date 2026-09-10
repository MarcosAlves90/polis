package policyplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	PlanVersion = 1

	PolicySourceCommitted = "committed"
	PolicySourceExternal  = "external"

	GateStateEnabled  = "enabled"
	GateStateDisabled = "disabled"

	GuaranteeProvided    = "provided"
	GuaranteeNotProvided = "not_provided"
)

type Options struct {
	Repo   string
	Policy string
}

type Plan struct {
	PlanVersion         int                     `json:"plan_version"`
	PolicySchemaVersion int                     `json:"policy_schema_version"`
	PolicySource        string                  `json:"policy_source"`
	PolicySHA256        string                  `json:"policy_sha256"`
	Runtime             Runtime                 `json:"runtime"`
	ValidationLevel     string                  `json:"validation_level"`
	EnabledGates        []string                `json:"enabled_gates"`
	DisabledGates       []string                `json:"disabled_gates"`
	DependencyEdges     []spec.PolicyDependency `json:"dependency_edges"`
	ExecutionOrder      []string                `json:"execution_order"`
	Gates               []Gate                  `json:"gates"`
	MandatoryInvariants []Invariant             `json:"mandatory_invariants"`
	Guarantees          []Guarantee             `json:"guarantees"`
	gatePolicies        []spec.GatePolicy
}

type Runtime struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	GoVersion    string `json:"go_version"`
}

type Gate struct {
	ID               string            `json:"id"`
	State            string            `json:"state"`
	Mode             string            `json:"mode"`
	Command          *spec.CommandSpec `json:"command,omitempty"`
	Reason           *string           `json:"reason,omitempty"`
	DependsOn        []string          `json:"depends_on,omitempty"`
	Adapter          string            `json:"adapter,omitempty"`
	Report           string            `json:"report,omitempty"`
	Operator         string            `json:"operator,omitempty"`
	ThresholdPercent *float64          `json:"threshold_percent,omitempty"`
}

type Invariant struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Guarantee struct {
	Gate        string `json:"gate"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

func Load(ctx context.Context, opts Options) (Plan, error) {
	repo := opts.Repo
	if repo == "" {
		repo = "."
	}
	root, err := gitutil.ResolveRoot(ctx, repo, gitutil.ResolveRootOptions{EmptyAsDot: true, GitError: "not a Git worktree"})
	if err != nil {
		return Plan{}, err
	}
	policyRaw, policy, source, err := loadPolicy(ctx, root, opts.Policy)
	if err != nil {
		return Plan{}, err
	}
	plan, err := Compile(policy)
	if err != nil {
		return Plan{}, err
	}
	digest := sha256.Sum256(policyRaw)
	plan.PolicySource = source
	plan.PolicySHA256 = hex.EncodeToString(digest[:])
	plan.Runtime = Runtime{OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version()}
	return plan, nil
}

func Compile(policy spec.Policy) (Plan, error) {
	if err := policy.Validate(); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	lint := spec.LintPolicy(policy)
	if err := lint.Err(); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	summary := policy.ValidationSummary()
	dependencies := make(map[string][]string, len(policy.Gates))
	for _, edge := range lint.Dependencies {
		dependencies[edge.Gate] = append(dependencies[edge.Gate], edge.DependsOn)
	}
	plan := Plan{
		PlanVersion:         PlanVersion,
		PolicySchemaVersion: policy.SchemaVersion,
		ValidationLevel:     summary.Level,
		EnabledGates:        append([]string{}, summary.EnabledGates...),
		DisabledGates:       append([]string{}, summary.DisabledGates...),
		DependencyEdges:     append([]spec.PolicyDependency{}, lint.Dependencies...),
		ExecutionOrder:      append([]string{}, lint.ExecutionOrder...),
		Gates:               make([]Gate, 0, len(policy.Gates)),
		MandatoryInvariants: mandatoryInvariants(),
		Guarantees:          make([]Guarantee, 0, len(policy.Gates)),
		gatePolicies:        make([]spec.GatePolicy, 0, len(policy.Gates)),
	}
	for _, policyGate := range policy.Gates {
		plan.Gates = append(plan.Gates, describeGate(policyGate, dependencies[policyGate.ID]))
		plan.Guarantees = append(plan.Guarantees, describeGuarantee(policyGate))
	}
	gateByID := make(map[string]spec.GatePolicy, len(policy.Gates))
	for _, policyGate := range policy.Gates {
		gateByID[policyGate.ID] = policyGate
	}
	for _, gateID := range lint.ExecutionOrder {
		gate := cloneGatePolicy(gateByID[gateID])
		gate.DependsOn = append([]string{}, dependencies[gateID]...)
		plan.gatePolicies = append(plan.gatePolicies, gate)
	}
	return plan, nil
}

func (p Plan) GatePolicies() []spec.GatePolicy {
	policies := make([]spec.GatePolicy, 0, len(p.gatePolicies))
	for _, gate := range p.gatePolicies {
		policies = append(policies, cloneGatePolicy(gate))
	}
	return policies
}

func loadPolicy(ctx context.Context, root, policyPath string) ([]byte, spec.Policy, string, error) {
	if policyPath != "" {
		raw, policy, err := policyload.LoadExternal(root, policyPath)
		return raw, policy, PolicySourceExternal, err
	}
	raw, policy, err := policyload.LoadCommitted(ctx, root)
	return raw, policy, PolicySourceCommitted, err
}

func describeGate(policyGate spec.GatePolicy, dependencies []string) Gate {
	state := GateStateEnabled
	if policyGate.Mode == spec.GateModeNotApplicable {
		state = GateStateDisabled
	}
	return Gate{
		ID:               policyGate.ID,
		State:            state,
		Mode:             policyGate.Mode,
		Command:          cloneCommand(policyGate.Command),
		Reason:           cloneString(policyGate.Reason),
		DependsOn:        append([]string{}, dependencies...),
		Adapter:          policyGate.Adapter,
		Report:           policyGate.Report,
		Operator:         policyGate.Operator,
		ThresholdPercent: cloneFloat(policyGate.ThresholdPercent),
	}
}

func describeGuarantee(policyGate spec.GatePolicy) Guarantee {
	if policyGate.Mode == spec.GateModeNotApplicable {
		return Guarantee{
			Gate:        policyGate.ID,
			Status:      GuaranteeNotProvided,
			Description: "this gate is not scheduled; its project-quality guarantee is absent",
		}
	}
	description := "the declared command is scheduled and its pass condition is required"
	if policyGate.Mode == spec.GateModeCoverage {
		description = "the declared coverage command and threshold are scheduled and their pass condition is required"
	}
	return Guarantee{Gate: policyGate.ID, Status: GuaranteeProvided, Description: description}
}

func mandatoryInvariants() []Invariant {
	return []Invariant{
		{ID: "policy-contract-validation", Description: "Project Policy and Change Contract schemas are validated before execution"},
		{ID: "command-safety", Description: "Declared argv, working directory, environment, timeout, and resource limits remain enforced"},
		{ID: "scope-and-baseline", Description: "Change scope, test scope, Git baseline, and exact target-tree checks remain enforced"},
		{ID: "development-proof", Description: "Required Red-to-Green or Green-to-Green development proof remains enforced"},
		{ID: "artifact-integrity", Description: "Package members, evidence, digests, and manifest consistency remain enforced; detached signatures are checked when supplied"},
		{ID: "isolated-transactional-apply", Description: "Consumer validation remains isolated and apply preserves HEAD and the real index"},
	}
}

func cloneGatePolicy(gate spec.GatePolicy) spec.GatePolicy {
	copy := gate
	copy.Command = cloneCommand(gate.Command)
	copy.Reason = cloneString(gate.Reason)
	copy.DependsOn = append([]string{}, gate.DependsOn...)
	copy.ThresholdPercent = cloneFloat(gate.ThresholdPercent)
	return copy
}

func cloneCommand(command *spec.CommandSpec) *spec.CommandSpec {
	if command == nil {
		return nil
	}
	copy := *command
	copy.Argv = append([]string{}, command.Argv...)
	if command.Environment != nil {
		environment := *command.Environment
		environment.Pass = append([]string{}, command.Environment.Pass...)
		copy.Environment = &environment
	}
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
