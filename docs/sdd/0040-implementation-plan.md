# SDD-0040 — Optional Contract-Bound Implementation Plans

## Status

Accepted for implementation. This SDD resolves Issue #5 against repository baseline `2054768cc43f48892145545a525296669056595d`.

## Objective

Add an optional deterministic Implementation Plan v1 between a locked Change Contract and implementation. The plan improves requirement traceability and resumability while staying below the contract's authority. Preserve the existing unplanned workflow and all proof, package, signature, and consumer guarantees.

## Authority and compatibility

The Change Contract remains normative. The Implementation Plan may order and reference existing requirements, acceptance criteria, proof obligations, allowed path entries, and Project Policy gate IDs. It MUST NOT add or change requirements, acceptance criteria, invariants, forbidden states, commands, policy, baseline, or scope. A plan describes intended work; it is not evidence that an unobservable human or editor action occurred.

Current `main` already uses package format v5 for the exact eight-member unplanned package and supports locked Change Contract schemas v4 and v6. Accordingly:

- Unplanned builds continue to emit format v5, unchanged.
- Planned builds emit format v6 with a required ninth member. Format v5 MUST NOT acquire a conditional inventory.
- Readers retain exact historical v2-v5 behavior and add exact v6 support.
- Planning accepts locked strict Change Contract v4 and v6 through `IsLockedStrictDevelopment`; draft/legacy contracts remain rejected.
- v6 uses Evidence v3. No Evidence v4 is introduced absent a demonstrated observable event need.

The package-version examples in Issue #5 describe its earlier v4 baseline; this SDD supersedes only those examples, not the feature acceptance criteria.

## Implementation Plan v1 model

The plan is a separate strict JSON file with:

- `schema_version: 1`;
- `change_contract_sha256`, computed over the exact locked Change Contract input bytes;
- `git_object_format`, `base_commit`, and `base_tree`, equal to the contract's `baseline_lock`;
- `strategy`, derived as `red_green` or `green_green` from existing contract predicates;
- ordered `steps`, each with deterministic unique `id`, `kind` (`test`, `proof`, `implementation`, or `validation`), non-empty objective, known `requirements` and `acceptance_criteria` references, optional `allowed_paths`, and `depends_on`;
- validation references to contract-owned proof names and Project Policy gate IDs, never copied command bodies.

Unknown fields, trailing JSON values, invalid schema/strategy, malformed IDs or object identities, unknown references, duplicate step IDs/dependencies, self-dependencies, unknown dependencies, graph cycles, and unbounded input MUST fail closed. Plan input is bounded to the existing 1 MiB contract-member limit; step and reference counts have explicit implementation limits.

Every contract requirement MUST occur on an implementation step. Every acceptance criterion MUST occur on a relevant test, proof, or validation step. Step order MUST be deterministic; dependencies MUST form a DAG whose deterministic topological order agrees with the serialized order. Project-gate references MUST use the effective policy's canonical `policyplan` execution order.

`allowed_paths` are repo-relative file paths or directory-prefix entries. Every implementation path MUST be equal to or beneath a Change Contract scope entry. Every test path MUST be equal to or beneath a `test_scope` entry. Plan generation copies only contract-provided path boundaries and MUST NOT invent files, modules, APIs, or architecture. Existing build scope and test-scope checks remain authoritative for actual changed paths.

## Strategy ordering

For feature/defect Red-to-Green work, test steps precede the capture-Red proof step; production implementation follows that proof; Green and final validation follow implementation. `capture-red` retains its existing evidence semantics.

For `behavior_preserving` Green-to-Green work, baseline characterization precedes implementation, target characterization follows implementation, and complete validation follows. The plan MUST NOT create a Red step or require a regression patch.

Validation steps reference existing contract proof commands (`regression`, `behavior`, `affected`) and enabled Project Policy gate IDs/order. They MUST NOT contain executable commands or replace Project Policy authority.

## Creation contract

`polis implementation-plan --repo <path> [--policy <policy-v3.json>] --contract <locked-v4-or-v6.json> --out <external-plan.json> [--format text|json]` is distinct from `polis plan`.

Creation requires a valid locked strict contract and specification, the exact locked repository baseline, clean HEAD/index/worktree with no untracked implementation changes, and the matching effective policy when supplied. The output MUST be absent and physically outside the target repository. Validate all inputs and paths before writing; never overwrite. Successful or failed creation MUST leave the target worktree, index, HEAD, refs, branches, tags, configuration, and persistent Git objects unchanged. The generated plan is written only to caller-owned external output; the plan is never discovered in or persisted to the repository.

If path discovery exposes insufficient contract scope, planning fails. The workflow is revise draft contract, run `polis start` again from a valid baseline, and generate a new plan.

## Producer and package behavior

`capture-red` and `build` accept an optional explicit `--implementation-plan`. When supplied, each validates the exact contract digest, baseline, kind/strategy, references, path bounds, graph, and required ordering before accepting proof or producing an artifact. Invalid plans fail before accepting Red proof or creating a package. Omitting the option preserves current producer behavior.

Planned build validates final observable conformance, including actual changed-path scope, existing Red/Green behavior, affected proof, enabled project gates, and exact target tree. It MUST NOT claim that a passing final tree proves the order of every implementation action. Build packages the exact plan bytes provided to that invocation.

Format v6 has exactly nine regular canonical members under `polis/`: the exact v5 eight members plus `polis/polis-implementation-plan.json`. Its manifest requires `implementation_plan_sha256`, the SHA-256 of the exact plan member bytes; checksums include the plan member. v2/v3 retain seven members; v4/v5 retain their exact eight-member inventory. Format v5 rejects the plan member/digest; format v6 requires both. Existing archive/member limits apply, with the plan capped at 1 MiB.

## Reader, signature, and consumer behavior

`verify` validates v6 exact inventory and size bounds, checksum and manifest digest, strict plan schema, exact packaged contract digest, baseline/strategy/reference/path/graph semantics, and consistency with actual proof/evidence before exposing the plan. `inspect` reports `present`, schema version, strategy, step count, and requirement-to-plan-to-proof traceability for v6; historical and unplanned packages report plan absence.

Preflight/apply continue to require successful package verification before consumer mutation. Detached signatures continue to cover exact artifact bytes, so plan changes invalidate an existing signature. No new trust root or special consumer plan authority is introduced.

## Failure semantics

Unknown references, out-of-scope paths, invalid baseline/policy, dirty planning state, existing/in-repository output, mismatched contract, invalid strategy/order/graph, malformed v6 inventory, checksum/digest mismatch, or proof/evidence inconsistency produce a failure before the relevant output or repository/consumer mutation. No invalid planned build may publish a `.polis` package.

## Acceptance traceability

This SDD covers Issue #5 acceptance criteria: schema and optional mode; distinct command; locked clean-baseline, zero-residue, external absent output; exact contract and baseline binding; derived strategy and deterministic steps; complete known requirement/acceptance coverage; contained paths; graph validation; Red-to-Green and Green-to-Green order; optional producer validation; unchanged unplanned behavior; observable-only claims; exact plan authentication; versioned planned package; verify/inspect traceability; historical compatibility; detached signature coverage; and no relaxation of contract or POLIS invariants.
