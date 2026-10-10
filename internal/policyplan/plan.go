package policyplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/MarcosAlves90/polis/v6/internal/gitutil"
	"github.com/MarcosAlves90/polis/v6/internal/policyload"
	"github.com/MarcosAlves90/polis/v6/internal/policyorigin"
	"github.com/MarcosAlves90/polis/v6/spec"
)

const (
	PlanVersion = 4

	PolicySourceCommitted = "committed"
	PolicySourceExternal  = "external"

	GateStateEnabled  = "enabled"
	GateStateDisabled = "disabled"

	ProducerActionExecute       = "execute"
	ProducerActionDeferred      = "deferred"
	ProducerActionNotApplicable = "not_applicable"

	ConsumerRequirementRequired    = "required"
	ConsumerRequirementNotRequired = "not_required"

	GuaranteeProvided    = "provided"
	GuaranteeNotProvided = "not_provided"

	DecisionSourcePolicyExplicit      = "policy_explicit"
	DecisionSourcePolicyDefault       = "policy_default"
	DecisionSourceMandatoryDependency = "mandatory_dependency"
	DecisionSourceExecutorDeferral    = "executor_deferral"
	DecisionSourceNotApplicable       = "not_applicable"
	DecisionSourceGateState           = "gate_state"
	DecisionSourceProfileDefault      = policyorigin.SourceProfileDefault
)

type Options struct {
	Repo          string
	Policy        string
	DeferredGates []string
}

type CompileOptions struct {
	DeferredGates []string
	// Origin is optional persisted init evidence. Only matching gate digests
	// can contribute provenance to the compiled plan.
	Origin *policyorigin.Manifest
}

type Plan struct {
	PlanVersion                int              `json:"plan_version"`
	PolicySchemaVersion        int              `json:"policy_schema_version"`
	PolicySource               string           `json:"policy_source"`
	PolicySHA256               string           `json:"policy_sha256"`
	Runtime                    Runtime          `json:"runtime"`
	ValidationLevel            string           `json:"validation_level"`
	ValidationLevelProvenance  Decision         `json:"validation_level_provenance"`
	EnabledGates               []string         `json:"enabled_gates"`
	DisabledGates              []string         `json:"disabled_gates"`
	DeferredGates              []string         `json:"deferred_gates"`
	ConsumerValidationRequired bool             `json:"consumer_validation_required"`
	DependencyEdges            []DependencyEdge `json:"dependency_edges"`
	ExecutionOrder             []string         `json:"execution_order"`
	Gates                      []Gate           `json:"gates"`
	MandatoryInvariants        []Invariant      `json:"mandatory_invariants"`
	Guarantees                 []Guarantee      `json:"guarantees"`
	gatePolicies               []spec.GatePolicy
}

type Runtime struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	GoVersion    string `json:"go_version"`
}

// Decision identifies the rule that selected an already validated plan value.
// Reason is a stable rule code, distinct from a project-authored gate reason.
type Decision struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type GateProvenance struct {
	Configuration       Decision `json:"configuration"`
	State               Decision `json:"state"`
	ProducerAction      Decision `json:"producer_action"`
	ConsumerRequirement Decision `json:"consumer_requirement"`
}

type DependencyEdge struct {
	Gate       string   `json:"gate"`
	DependsOn  string   `json:"depends_on"`
	Provenance Decision `json:"provenance"`
}

type Gate struct {
	ID                  string            `json:"id"`
	InputPaths          []string          `json:"input_paths,omitempty"`
	InputPathsComplete  bool              `json:"input_paths_complete,omitempty"`
	ParallelSafe        bool              `json:"parallel_safe,omitempty"`
	State               string            `json:"state"`
	Mode                string            `json:"mode"`
	ProducerAction      string            `json:"producer_action"`
	ConsumerRequirement string            `json:"consumer_requirement"`
	Provenance          GateProvenance    `json:"provenance"`
	Command             *spec.CommandSpec `json:"command,omitempty"`
	Reason              *string           `json:"reason,omitempty"`
	DependsOn           []string          `json:"depends_on,omitempty"`
	Adapter             string            `json:"adapter,omitempty"`
	Report              string            `json:"report,omitempty"`
	Operator            string            `json:"operator,omitempty"`
	ThresholdPercent    *float64          `json:"threshold_percent,omitempty"`
}

type Invariant struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Guarantee struct {
	Gate        string   `json:"gate"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Provenance  Decision `json:"provenance"`
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
	plan, err := CompileForRepository(ctx, root, policy, source == PolicySourceCommitted, opts.DeferredGates)
	if err != nil {
		return Plan{}, err
	}
	digest := sha256.Sum256(policyRaw)
	plan.PolicySource = source
	plan.PolicySHA256 = hex.EncodeToString(digest[:])
	plan.Runtime = Runtime{OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version()}
	return plan, nil
}

// CompileForRepository shares the provenance boundary for command planning and
// producer execution. An external policy never inherits repository metadata.
func CompileForRepository(ctx context.Context, root string, policy spec.Policy, committed bool, deferred []string) (Plan, error) {
	var origin *policyorigin.Manifest
	if committed {
		origin = readCommittedOrigin(ctx, root)
	}
	return CompileWithOptions(policy, CompileOptions{DeferredGates: deferred, Origin: origin})
}

func Compile(policy spec.Policy) (Plan, error) {
	return CompileWithOptions(policy, CompileOptions{})
}

func CompileWithOptions(policy spec.Policy, opts CompileOptions) (Plan, error) {
	if err := policy.Validate(); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	if err := validateCommandPortability(policy); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	lint := spec.LintPolicy(policy)
	if err := lint.Err(); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	summary := policy.ValidationSummary()
	origins := map[string]policyorigin.Decision{}
	if opts.Origin != nil {
		origins = opts.Origin.Match(policy)
	}
	validationLevelProvenance := Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_validation_level"}
	if policy.ValidationLevel == "" {
		validationLevelProvenance = Decision{Source: DecisionSourcePolicyDefault, Reason: "omitted_validation_level_defaults_to_strict"}
	}
	if opts.Origin != nil {
		if recorded, ok := opts.Origin.MatchValidationLevel(policy); ok {
			validationLevelProvenance = Decision(recorded)
		}
	}
	dependencies := make(map[string][]string, len(policy.Gates))
	dependencyEdges := make([]DependencyEdge, 0, len(lint.Dependencies))
	for _, edge := range lint.Dependencies {
		dependencies[edge.Gate] = append(dependencies[edge.Gate], edge.DependsOn)
		provenance := Decision{Source: DecisionSourcePolicyExplicit, Reason: "declared_prerequisite"}
		if spec.IsBuiltInPolicyDependency(edge.Gate, edge.DependsOn) {
			provenance = Decision{Source: DecisionSourceMandatoryDependency, Reason: "built_in_prerequisite"}
		} else if origin, ok := origins[edge.Gate]; ok && origin.Source == DecisionSourceProfileDefault {
			provenance = Decision{Source: origin.Source, Reason: "generated_profile_prerequisite"}
		}
		dependencyEdges = append(dependencyEdges, DependencyEdge{Gate: edge.Gate, DependsOn: edge.DependsOn, Provenance: provenance})
	}
	deferredGates, deferredSet, err := canonicalDeferredGates(policy, opts.DeferredGates)
	if err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	if err := validateDeferredDependencies(policy, lint.ExecutionOrder, dependencies, deferredSet); err != nil {
		return Plan{}, fmt.Errorf("compile execution plan: %w", err)
	}
	plan := Plan{
		PlanVersion:                PlanVersion,
		PolicySchemaVersion:        policy.SchemaVersion,
		ValidationLevel:            summary.Level,
		ValidationLevelProvenance:  validationLevelProvenance,
		EnabledGates:               append([]string{}, summary.EnabledGates...),
		DisabledGates:              append([]string{}, summary.DisabledGates...),
		DeferredGates:              deferredGates,
		ConsumerValidationRequired: len(deferredGates) > 0,
		DependencyEdges:            dependencyEdges,
		ExecutionOrder:             append([]string{}, lint.ExecutionOrder...),
		Gates:                      make([]Gate, 0, len(policy.Gates)),
		MandatoryInvariants:        mandatoryInvariants(),
		Guarantees:                 make([]Guarantee, 0, len(policy.Gates)),
		gatePolicies:               make([]spec.GatePolicy, 0, len(policy.Gates)),
	}
	for _, policyGate := range policy.Gates {
		gate := describeGate(policyGate, dependencies[policyGate.ID], deferredSet, origins[policyGate.ID])
		plan.Gates = append(plan.Gates, gate)
		plan.Guarantees = append(plan.Guarantees, describeGuarantee(gate))
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

func canonicalDeferredGates(policy spec.Policy, requested []string) ([]string, map[string]struct{}, error) {
	gateByID := make(map[string]spec.GatePolicy, len(policy.Gates))
	for _, gate := range policy.Gates {
		gateByID[gate.ID] = gate
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if !spec.IsProjectGate(id) {
			return nil, nil, fmt.Errorf("unknown project gate %q cannot be deferred", id)
		}
		if _, exists := requestedSet[id]; exists {
			return nil, nil, fmt.Errorf("project gate %q was requested for deferral more than once", id)
		}
		requestedSet[id] = struct{}{}
		gate := gateByID[id]
		if gate.Mode == spec.GateModeNotApplicable {
			return nil, nil, fmt.Errorf("project gate %q cannot be deferred because it is not applicable", id)
		}
	}
	ordered := make([]string, 0, len(requestedSet))
	for _, id := range spec.ProjectGateOrder {
		if _, ok := requestedSet[id]; ok {
			ordered = append(ordered, id)
		}
	}
	return ordered, requestedSet, nil
}

func validateDeferredDependencies(policy spec.Policy, executionOrder []string, dependencies map[string][]string, deferred map[string]struct{}) error {
	gateByID := make(map[string]spec.GatePolicy, len(policy.Gates))
	for _, gate := range policy.Gates {
		gateByID[gate.ID] = gate
	}
	for _, gateID := range executionOrder {
		gate := gateByID[gateID]
		if gate.Mode == spec.GateModeNotApplicable {
			continue
		}
		if _, isDeferred := deferred[gateID]; isDeferred {
			continue
		}
		stack := append([]string(nil), dependencies[gateID]...)
		visited := make(map[string]struct{}, len(stack))
		for len(stack) > 0 {
			last := len(stack) - 1
			dependency := stack[last]
			stack = stack[:last]
			if _, seen := visited[dependency]; seen {
				continue
			}
			visited[dependency] = struct{}{}
			if _, isDeferred := deferred[dependency]; isDeferred {
				return fmt.Errorf("producer gate %q depends on deferred prerequisite %q", gateID, dependency)
			}
			stack = append(stack, dependencies[dependency]...)
		}
	}
	return nil
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

// The provenance sidecar must be committed unchanged. Missing, modified, or
// malformed optional evidence is ignored, keeping policy interpretation intact.
const maxProvenanceBytes = 1 << 20

func readCommittedOrigin(ctx context.Context, root string) *policyorigin.Manifest {
	path := filepath.Join(root, ".polis", policyorigin.Filename)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxProvenanceBytes {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	openInfo, err := f.Stat()
	if err != nil || !openInfo.Mode().IsRegular() || openInfo.Size() > maxProvenanceBytes {
		return nil
	}
	working, err := io.ReadAll(io.LimitReader(f, maxProvenanceBytes+1))
	if err != nil || len(working) > maxProvenanceBytes {
		return nil
	}
	// Resolve a fixed blob identity, then inspect its size before reading its
	// bytes. gitutil.Bytes buffers all output, so never feed it an unchecked blob.
	objectID, err := gitutil.Output(ctx, root, nil, nil, "rev-parse", "--verify", "HEAD:.polis/"+policyorigin.Filename)
	if err != nil || (len(objectID) != 40 && len(objectID) != 64) {
		return nil
	}
	if _, err := hex.DecodeString(objectID); err != nil {
		return nil
	}
	sizeText, err := gitutil.Output(ctx, root, nil, nil, "cat-file", "-s", objectID)
	if err != nil {
		return nil
	}
	size, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
	if err != nil || size < 0 || size > maxProvenanceBytes || size != int64(len(working)) {
		return nil
	}
	committed, err := gitutil.Bytes(ctx, root, nil, nil, "cat-file", "blob", objectID)
	if err != nil || int64(len(committed)) != size || !bytes.Equal(working, committed) {
		return nil
	}
	manifest, err := policyorigin.Decode(committed)
	if err != nil {
		return nil
	}
	return &manifest
}

func describeGate(policyGate spec.GatePolicy, dependencies []string, deferred map[string]struct{}, origin policyorigin.Decision) Gate {
	state := GateStateEnabled
	producerAction := ProducerActionExecute
	consumerRequirement := ConsumerRequirementRequired
	configuration := Decision{Source: DecisionSourcePolicyExplicit, Reason: "policy_gate_declaration"}
	if origin.Source != "" {
		configuration = Decision{Source: origin.Source, Reason: origin.Reason}
	}
	provenance := GateProvenance{
		Configuration:       configuration,
		State:               Decision{Source: configuration.Source, Reason: configuration.Reason},
		ProducerAction:      Decision{Source: configuration.Source, Reason: configuration.Reason},
		ConsumerRequirement: Decision{Source: DecisionSourceGateState, Reason: "enabled_gate_required"},
	}
	if origin.Source == "" {
		provenance.State.Reason = "declared_gate_mode"
		provenance.ProducerAction.Reason = "enabled_gate_scheduled"
	}
	if policyGate.Mode == spec.GateModeNotApplicable {
		state = GateStateDisabled
		producerAction = ProducerActionNotApplicable
		consumerRequirement = ConsumerRequirementNotRequired
		provenance = GateProvenance{
			Configuration:       configuration,
			State:               Decision{Source: DecisionSourceNotApplicable, Reason: "declared_not_applicable"},
			ProducerAction:      Decision{Source: DecisionSourceNotApplicable, Reason: "gate_not_applicable"},
			ConsumerRequirement: Decision{Source: DecisionSourceNotApplicable, Reason: "gate_not_applicable"},
		}
	} else if _, ok := deferred[policyGate.ID]; ok {
		producerAction = ProducerActionDeferred
		provenance.ProducerAction = Decision{Source: DecisionSourceExecutorDeferral, Reason: "requested_by_executor"}
		provenance.ConsumerRequirement = Decision{Source: DecisionSourceExecutorDeferral, Reason: "deferred_gate_requires_consumer_validation"}
	}
	return Gate{
		ID:                  policyGate.ID,
		InputPaths:          append([]string{}, policyGate.InputPaths...),
		InputPathsComplete:  policyGate.InputPathsComplete,
		ParallelSafe:        policyGate.ParallelSafe,
		State:               state,
		Mode:                policyGate.Mode,
		ProducerAction:      producerAction,
		ConsumerRequirement: consumerRequirement,
		Provenance:          provenance,
		Command:             cloneCommand(policyGate.Command),
		Reason:              cloneString(policyGate.Reason),
		DependsOn:           append([]string{}, dependencies...),
		Adapter:             policyGate.Adapter,
		Report:              policyGate.Report,
		Operator:            policyGate.Operator,
		ThresholdPercent:    cloneFloat(policyGate.ThresholdPercent),
	}
}

func describeGuarantee(gate Gate) Guarantee {
	if gate.State == GateStateDisabled {
		return Guarantee{
			Gate:        gate.ID,
			Status:      GuaranteeNotProvided,
			Description: "this gate is not scheduled; its project-quality guarantee is absent",
			Provenance:  gate.Provenance.ConsumerRequirement,
		}
	}
	description := "the declared command is scheduled and its pass condition is required"
	if gate.Mode == spec.GateModeCoverage {
		description = "the declared coverage command and threshold are scheduled and their pass condition is required"
	}
	if gate.ProducerAction == ProducerActionDeferred {
		description = "this gate is deferred by the producer; consumer validation must execute it and require its pass condition"
	}
	return Guarantee{Gate: gate.ID, Status: GuaranteeProvided, Description: description, Provenance: gate.Provenance.ConsumerRequirement}
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
	copy.InputPaths = append([]string{}, gate.InputPaths...)
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
