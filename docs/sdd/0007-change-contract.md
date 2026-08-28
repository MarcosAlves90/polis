# SDD-0007 — Delivery Change Contract and Red/Green Regression Evidence

Status: accepted for 4.0.0-alpha.7

## Objective

Make delivery-specific `behavior`, `regression`, and `affected` evidence deterministic. These gates are not project policy: they describe one change and travel inside the `.polis` artifact.

## Change Contract v1

A Change Contract is strict JSON with `additionalProperties=false` semantics and fields:

- `schema_version`: exactly `1`.
- `kind`: `feature`, `defect`, or `behavior_preserving`.
- `behavior`: mandatory `CommandSpec`.
- `affected`: mandatory `CommandSpec`.
- `regression`: exactly one of:
  - `{"mode":"red_green","command":...,"baseline_exit_code":<1..255>,"baseline_output_contains":[...non-empty...]}` for `kind=defect`.
  - `{"mode":"not_applicable","reason_code":"not-a-defect"}` for every non-defect kind.

Free-form regression applicability reasons are forbidden.

## Package format v2

Every archive contains exactly seven regular members:

- `polis/polis-manifest.json`
- `polis/polis-policy.json`
- `polis/polis-change.json`
- `polis/polis-regression.patch`
- `polis/polis-payload.patch`
- `polis/polis-evidence.ndjson`
- `polis/polis-checksums.sha256`

The manifest binds policy, change contract, regression probe, and payload by SHA-256.

For non-defect changes `polis-regression.patch` is exactly zero bytes. For defects it MUST be non-empty.

## Defect validation

1. Create isolated worktree at `base_commit`.
2. Apply `polis-regression.patch` with `git apply --check` and `git apply --index`.
3. Every path changed by the regression patch MUST also be changed by the final payload.
4. Execute the regression command directly, without a shell.
5. Require exact `baseline_exit_code` and every declared token in combined stdout+stderr.
6. Create/prepare isolated target from the same baseline and final payload.
7. Run the same regression command and require exit zero.
8. Run behavior and affected commands and require PASS.
9. Run the complete project policy.
10. Only then may the package be emitted or applied to the real consumer worktree.

## Non-defect validation

Regression is deterministically `NOT_APPLICABLE` with reason `not-a-defect`; behavior and affected remain mandatory and must PASS before project policy execution.
