# SDD-0033 — Execution Plan

## Status

Implemented in POLIS V6.

## Objective

Provide one explicit, read-only representation of what a Project Policy execution will do and which guarantees it provides. The representation is shared by the command-line projection and the policy executor so policy interpretation is not duplicated.

## Interface

The `internal/policyplan` module exposes these entry points:

- `Compile(spec.Policy) (Plan, error)` validates a policy and compiles its ordered project gates, commands, mandatory invariants, and guarantee status without side effects;
- `CompileWithOptions(spec.Policy, CompileOptions) (Plan, error)` additionally validates executor-requested deferred project gates and resolves producer/consumer responsibilities;
- `Load(context.Context, Options) (Plan, error)` resolves the repository, loads either the committed policy or an external policy, records the effective policy digest and runtime, and delegates to `CompileWithOptions` with the requested deferrals.

`Plan` contains:

- `plan_version: 4`, policy schema version, source class, SHA-256 digest, and runtime;
- the effective validation level and complete enabled/disabled gate inventories;
- ordered gate descriptions with argv, working directory, timeout, environment mode, coverage metadata, and explicit disabled-gate reasons;
- effective dependency edges and the deterministic topological execution order;
- mandatory invariants that remain outside validation-level reduction;
- guarantee status for every project gate (`provided` or `not_provided`).
- explicit `{source, reason}` decision provenance for validation level, gate configuration and state, producer action and consumer requirement, dependency edge, and guarantee.

The provenance codes are compiled from existing decisions: `policy_explicit` for declared validation levels, gate modes and dependency edges; `policy_default` when omitted `validation_level` resolves to `strict`; `mandatory_dependency` for built-in dependency edges; `not_applicable` for declared disabled gates; `executor_deferral` for requested producer deferral and its consumer obligation; and `gate_state` for requirements or guarantees derived from an enabled gate. Stable `reason` codes identify the actual rule (`declared_validation_level`, `omitted_validation_level_defaults_to_strict`, `declared_gate_mode`, `declared_not_applicable`, `enabled_gate_scheduled`, `enabled_gate_required`, `gate_not_applicable`, `requested_by_executor`, `deferred_gate_requires_consumer_validation`, `declared_prerequisite`, `built_in_prerequisite`). A deferred gate's policy-controlled enabled state is kept distinct from the executor-controlled producer action. The field `gates[].reason` remains the free-text reason supplied by the policy for `not_applicable`.

Profile defaults are materialized by `polis init` into the policy's explicit gate modes. Init records their origin when it selects those declarations, along with explicit `--disable-gate` overrides and custom commands, in the separately committed `.polis/policy-provenance.json` manifest (schema v1). The manifest records the resolved profile and SHA-256 of each canonical serialized gate. `polis plan` reads the optional sidecar only when unchanged in `HEAD` and classifies a gate as `profile_default` only when its saved declaration digest still matches. Changed gates, missing or uncommitted evidence, and external policies fall back to `policy_explicit`; no classification compares gate values with current profile templates. The `provenance.configuration` object exposes this attribution even for `not_applicable` gates, whose state/action/obligation origin remains `not_applicable`. The optional manifest does not alter policy schema v3, artifact digests or execution invariants.

The manifest also optionally records the original validation-level field representation and the init decision: `init_default_validation_level` (`profile_default`) when the CLI flag was omitted, or `init_explicit_validation_level` (`policy_explicit`) when explicitly requested. Both omitted and explicitly requested `strict` retain the same omitted policy field for compatibility. If the policy field changes or the optional manifest record is absent, the compiler falls back to its observable `policy_default` or `policy_explicit` level rule. The producer's build path uses the same repository-origin resolver as `polis plan`. The resolver pins the committed Git object identity and checks its size (maximum 1 MiB) before reading it; invalid or oversized optional metadata is ignored.

The plan does not include the external policy pathname, execute commands, or mutate repository state. Gate policies returned for execution are deep copies so callers cannot mutate the compiled plan accidentally.

## CLI

```text
polis plan --repo <path> [--policy <external-policy>] [--defer-gate <id> ...] [--format text|json]
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

Tests cover strict defaulting, standard/minimal gate inventories, guarantee reporting, defensive copying, invalid policies, committed-policy loading and digest calculation, JSON/text CLI output, executor reuse of the compiled plan, decision provenance, and mandatory-versus-declared dependencies.
