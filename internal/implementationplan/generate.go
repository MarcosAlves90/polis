package implementationplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/MarcosAlves90/polis/v6/spec"
)

// Generate derives an Implementation Plan from a locked Change Contract. It
// copies only contract-owned scope entries and references and records no
// executable command bodies.
func Generate(contract spec.ChangeContract, contractRaw []byte, effectiveGateOrder []string) (spec.ImplementationPlan, error) {
	if !contract.IsLockedStrictDevelopment() || contract.DevelopmentMethod != spec.DevelopmentMethodStrictSDDTDDV2 {
		return spec.ImplementationPlan{}, errors.New("implementation planning requires a locked strict Change Contract schema v4 or v6")
	}
	if contract.Specification == nil || contract.BaselineLock == nil {
		return spec.ImplementationPlan{}, errors.New("locked Change Contract requires specification and baseline_lock")
	}
	if err := contract.Validate(); err != nil {
		return spec.ImplementationPlan{}, fmt.Errorf("invalid locked Change Contract: %w", err)
	}
	sum := sha256.Sum256(contractRaw)
	strategy := ""
	switch {
	case contract.RequiresRedGreen():
		strategy = spec.ImplementationPlanStrategyRedGreen
	case contract.RequiresGreenGreen():
		strategy = spec.ImplementationPlanStrategyGreenGreen
	default:
		return spec.ImplementationPlan{}, errors.New("locked Change Contract has no supported development proof strategy")
	}
	plan := spec.ImplementationPlan{
		SchemaVersion:        spec.ImplementationPlanSchemaVersion,
		ChangeContractSHA256: hex.EncodeToString(sum[:]),
		GitObjectFormat:      contract.BaselineLock.GitObjectFormat,
		BaseCommit:           contract.BaselineLock.BaseCommit,
		BaseTree:             contract.BaselineLock.BaseTree,
		Strategy:             strategy,
	}
	addStep := func(step spec.ImplementationPlanStep) {
		step.ID = fmt.Sprintf("PLAN-%03d", len(plan.Steps)+1)
		if len(plan.Steps) > 0 {
			step.DependsOn = []string{plan.Steps[len(plan.Steps)-1].ID}
		}
		plan.Steps = append(plan.Steps, step)
	}
	addCharacterizationSteps := func(phase string, withRegressionProof bool) {
		for _, criterion := range contract.Specification.AcceptanceCriteria {
			objective := fmt.Sprintf("%s characterization for %s", phase, criterion.ID)
			step := spec.ImplementationPlanStep{
				Kind:               spec.ImplementationPlanStepTest,
				Objective:          objective,
				Requirements:       append([]string(nil), criterion.Requirements...),
				AcceptanceCriteria: []string{criterion.ID},
				AllowedPaths:       append([]string(nil), contract.TestScope.AllowedPaths...),
			}
			if withRegressionProof {
				step.ContractProofs = []string{"regression"}
			}
			addStep(step)
		}
	}
	if contract.RequiresGreenGreen() {
		addCharacterizationSteps("Baseline", true)
	} else {
		for _, criterion := range contract.Specification.AcceptanceCriteria {
			addStep(spec.ImplementationPlanStep{
				Kind:               spec.ImplementationPlanStepTest,
				Objective:          fmt.Sprintf("Add or update tests for %s", criterion.ID),
				Requirements:       append([]string(nil), criterion.Requirements...),
				AcceptanceCriteria: []string{criterion.ID},
				AllowedPaths:       append([]string(nil), contract.TestScope.AllowedPaths...),
			})
		}
		addStep(spec.ImplementationPlanStep{
			Kind:           spec.ImplementationPlanStepProof,
			Objective:      "Capture the contract's Red regression proof",
			ContractProofs: []string{"regression"},
		})
	}
	for _, requirement := range contract.Specification.Requirements {
		criteria := make([]string, 0)
		for _, criterion := range contract.Specification.AcceptanceCriteria {
			for _, requirementID := range criterion.Requirements {
				if requirementID == requirement.ID {
					criteria = append(criteria, criterion.ID)
					break
				}
			}
		}
		addStep(spec.ImplementationPlanStep{
			Kind:               spec.ImplementationPlanStepImplementation,
			Objective:          fmt.Sprintf("Implement %s", requirement.ID),
			Requirements:       []string{requirement.ID},
			AcceptanceCriteria: criteria,
			AllowedPaths:       append([]string(nil), contract.Scope.AllowedPaths...),
		})
	}
	if contract.RequiresGreenGreen() {
		addCharacterizationSteps("Target", true)
	}
	proofs := []string{"regression", "behavior", "affected"}
	if contract.RequiresGreenGreen() {
		proofs = []string{"behavior", "affected"}
	}
	addStep(spec.ImplementationPlanStep{
		Kind:           spec.ImplementationPlanStepValidation,
		Objective:      "Run the locked proof commands and enabled Project Policy gates",
		ContractProofs: proofs,
		ProjectGates:   append([]string(nil), effectiveGateOrder...),
	})
	if err := plan.ValidateAgainst(contract, contractRaw); err != nil {
		return spec.ImplementationPlan{}, fmt.Errorf("generated implementation plan is invalid: %w", err)
	}
	if err := plan.ValidateProjectGates(effectiveGateOrder); err != nil {
		return spec.ImplementationPlan{}, fmt.Errorf("generated implementation plan project gates: %w", err)
	}
	return plan, nil
}
