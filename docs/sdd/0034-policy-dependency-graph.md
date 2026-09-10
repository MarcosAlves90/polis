# SDD-0034 — Policy Dependency Graph and Linter

## Status

Implemented in POLIS V6.

## Objective

Make relationships between project-validation gates explicit, validate incompatible combinations before execution, and ensure an enabled gate cannot silently rely on a disabled prerequisite.

## Model

Project Policy schema v3 accepts an optional `depends_on` array on each project gate. Dependencies declared by a policy are additive to POLIS's built-in essential dependencies. The current built-in relationship is that `coverage` depends on `test.complete`; the execution edge therefore runs `test.complete` before `coverage`:

```text
test.complete -> coverage
```

The JSON representation stores the dependent gate in `gate` and its prerequisite in `depends_on`. Existing policies that omit the field retain the same effective behavior.

Built-in dependencies are authoritative: a policy cannot remove them. This keeps essential execution semantics in the implementation rather than making them dependent on a caller remembering a hidden prerequisite.

## Linter seam

`spec.LintPolicy(policy)` returns a `PolicyLintReport` containing:

- the effective dependency edges;
- the deterministic topological execution order;
- structured issues with stable codes and human-readable messages.

`Policy.Validate` invokes the linter, so decoding, policy planning, producer validation, package verification, and execution all share the same fail-closed validation boundary.

The linter rejects:

- unknown or missing dependency IDs;
- self-dependencies;
- cycles;
- an enabled gate depending on a `not_applicable` gate.

Duplicate dependencies declared by one gate are rejected structurally. A duplicate between a built-in edge and an explicit declaration is coalesced into one effective edge.

Dependencies of a disabled gate are still structurally validated, but disabling a gate is not an implicit request to disable its dependents. If an enabled gate depends on that disabled gate, validation fails and no project command is executed.

## Plan and execution

`internal/policyplan` exposes effective dependency edges and execution order in the read-only plan. `polis plan` prints both fields in text and JSON formats. `internal/policyexec` consumes the same compiled plan, so commands run in topological order and cannot diverge from the inspected plan.

The canonical gate inventory remains in `spec.ProjectGateOrder`; only execution order is topologically arranged. This preserves inventory compatibility while making prerequisites observable.

## Safety and compatibility

- Strict mode remains the default and continues to include the built-in coverage prerequisite.
- No validation is disabled implicitly.
- Structural, integrity, security, baseline, development-proof, packaging, signature, isolation, and transactional-apply invariants remain mandatory.
- Policies without `depends_on` remain backward-compatible.
- Invalid dependency graphs fail before project commands execute.

## Validation

Tests cover disabled prerequisites, unknown/self/cyclic dependencies, effective-edge reporting, deterministic ordering, JSON round trips, plan defensive copies, and CLI plan output.
