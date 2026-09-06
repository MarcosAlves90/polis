# TDD-0027 — Strict Green-to-Green Characterization Proof

## Scope

Observed Red/Green evidence for SDD-0027.

## Slice 1 — Contract semantics

### Red

The first strict behavior-preserving tests did not compile because `green_green`, `RequiresGreenGreen`, `RequiresBaselineProof`, and `RequiresRegressionPatch` did not exist.

### Green

`spec/change.go` gained explicit Green-to-Green semantics. Verification:

```text
go test ./spec -run 'GreenGreen|BehaviorPreserving' -count=1
```

Result: PASS.

## Slice 2 — Baseline and target execution

### Red

The existing executor rejected a non-defect baseline and emitted NOT_APPLICABLE on the target.

### Green

Execution now follows `RequiresBaselineProof()` and distinguishes Red-to-Green from Green-to-Green. Verification:

```text
go test ./internal/changeexec -run 'GreenGreen' -count=1
```

Result: PASS.

## Slice 3 — Evidence ordering

### Red

Evidence validation expected NOT_APPLICABLE for strict behavior-preserving work.

### Green

Evidence validation now requires two regression PASS gates in canonical order: baseline Green and target Green.

## Slice 4 — Build integration

### Red

A real strict behavior-preserving build reached target validation without baseline Green evidence and failed the evidence contract.

### Green

`internal/isolation` now runs baseline validation whenever the Change Contract requires baseline proof and applies a regression patch only for Red-to-Green.

Verification:

```text
go test ./internal/packagebuild -run 'StrictBehaviorPreserving' -count=1
```

Result: PASS, including rejection of a target that changes the characterized result.
