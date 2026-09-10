# SDD-0033 — Execution Plan

## Status

Implemented in POLIS V6.

## Objective

Provide one explicit, read-only representation of what a Project Policy execution will do and which guarantees it provides. The representation is shared by the command-line projection and the policy executor so policy interpretation is not duplicated.

## Interface

The `internal/policyplan` module exposes two seams:

- `Compile(spec.Policy) (Plan, error)` validates a policy and compiles its ordered project gates, commands, mandatory invariants, and guarantee status without side effects;
- `Load(context.Context, Options) (Plan, error)` resolves the repository, loads either the committed policy or an external policy, records the effective policy digest and runtime, and delegates to `Compile`.

`Plan` contains:

- `plan_version: 1`, policy schema version, source class, SHA-256 digest, and runtime;
- the effective validation level and complete enabled/disabled gate inventories;
- ordered gate descriptions with argv, working directory, timeout, environment mode, coverage metadata, and explicit disabled-gate reasons;
- effective dependency edges and the deterministic topological execution order;
- mandatory invariants that remain outside validation-level reduction;
- guarantee status for every project gate (`provided` or `not_provided`).

The plan does not include the external policy pathname, execute commands, or mutate repository state. Gate policies returned for execution are deep copies so callers cannot mutate the compiled plan accidentally.

## CLI

```text
polis plan --repo <path> [--policy <external-policy>] [--format text|json]
```

The command is read-only. With no `--policy`, it loads the committed policy for compatibility. With `--policy`, it uses the existing external-policy boundary checks for that execution. Text output is intended for human review; JSON is intended for automation and inspection.

## Reuse by execution

`internal/policyexec` compiles the same `Plan` before emitting `validation_configured` and executes the gate policies returned by that plan in the plan's deterministic topological order. A gate represented as `not_applicable` is never passed to the command executor.

## Safety and compatibility

- Invalid policies fail closed during plan compilation.
- A policy dependency on an unknown, missing, self, cyclic, or disabled gate fails closed before project commands run.
- `polis plan` performs no project-command execution and no repository mutation.
- Policy, contract, command-safety, baseline, development-proof, package/evidence-integrity, signature, isolation, and transactional-apply invariants remain mandatory.
- The package schema remains unchanged; Project Policy schema v3 gains only the optional `depends_on` field, so existing policies without it remain valid. A missing `validation_level` continues to resolve to `strict`, and the built-in `coverage` dependency on `test.complete` remains effective.
- Disabled project gates are explicit and each missing project-quality guarantee is reported as `not_provided`.

## Validation

Tests cover strict defaulting, standard/minimal gate inventories, guarantee reporting, defensive copying, invalid policies, committed-policy loading and digest calculation, JSON/text CLI output, and executor reuse of the compiled plan.
