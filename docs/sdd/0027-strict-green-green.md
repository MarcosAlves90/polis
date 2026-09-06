# SDD-0027 — Strict Green-to-Green Characterization Proof

## Status

Accepted and implemented as an opt-in extension of strict Change Contract schema v3/v4.

## Problem

A behavior-preserving change should not manufacture a failing Red state. The strict schema initially treated `behavior_preserving` regression as `not_applicable`, which proved the target gates but did not prove that explicitly protected behavior passed on both the immutable baseline and the target.

## Objective

Require an explicit characterization command to pass on the baseline and pass again on the target for strict `behavior_preserving` deliveries.

## Contract

For strict schema v3/v4 and `kind=behavior_preserving`:

- `regression.mode` MUST be `green_green`;
- `regression.command` MUST be present and valid;
- Red-only fields `baseline_exit_code`, `baseline_output_contains`, and `reason_code` MUST be absent;
- no regression patch is accepted;
- the regression command MUST PASS against the immutable baseline before target validation;
- the same command MUST PASS against the target before behavior, affected, and Project Policy gates continue.

Legacy v1/v2 behavior remains unchanged.

## Acceptance criteria

1. Strict `behavior_preserving` rejects `not_applicable`.
2. Green-to-Green rejects Red-oracle fields.
3. Baseline execution emits a regression PASS rather than NOT_APPLICABLE.
4. Target execution repeats the same regression command and requires PASS.
5. A build whose baseline characterization passes and target characterization fails is rejected.
6. No regression patch is required or accepted for Green-to-Green.
7. Evidence validation requires baseline Green followed by target Green.

## Invariants

- No artificial Red is manufactured for a behavior-preserving change.
- The characterization command and environment are explicit Change Contract data.
- Package format v3 and Evidence v2 remain unchanged.
- Existing feature/defect Red-to-Green semantics remain unchanged.

## Non-goals

- Inferring characterization tests from filenames or frameworks.
- Proving semantic equivalence of arbitrary programs.
- Mutation testing or property testing.
