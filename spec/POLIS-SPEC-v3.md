# POLIS Specification v3 — Strict SDD/TDD development proof

This specification defines the opt-in strict development-contract semantics introduced after POLIS V5.0.1. Unless explicitly changed here, POLIS Specification v2 and the unchanged V1 exact-tree and transactional rules remain normative.

This specification does not change POLIS package format v3, Project Policy schema v3, Evidence v2, detached signatures, exact-tree identity, or transactional preflight/apply semantics.

## Change Contract schema v3

Change Contract schema v3 adds machine-readable development intent and strict test-first proof while preserving schema v1/v2 read compatibility.

A schema-v3 contract MUST contain:

- `development_method` equal to `strict_sdd_tdd_v1`;
- ordinary `scope.allowed_paths`;
- `test_scope.allowed_paths`;
- `specification.objective` as a non-empty string;
- non-empty `specification.requirements`;
- non-empty `specification.acceptance_criteria`;
- non-empty `specification.invariants`;
- non-empty `specification.forbidden_states`;
- non-empty `specification.inputs`;
- non-empty `specification.outputs`;
- non-empty `specification.failure_semantics`.

Every specification clause MUST contain a non-empty `id` and a non-empty `statement`. Clause IDs MUST be unique across all specification sections.

Every acceptance criterion MUST reference at least one existing requirement through `requirements`, MUST NOT repeat a requirement reference, and MUST declare `proof: "regression"`. Every requirement MUST be covered by at least one acceptance criterion. POLIS derives `REQ -> AC -> proof` traceability from these fields; no independent traceability table is authoritative.

Every `test_scope.allowed_paths` entry MUST be authorized by ordinary `scope.allowed_paths`. POLIS MUST NOT infer test paths from filenames, directory names, languages, frameworks, or ecosystem metadata.

Executable behavior, affected, and Red/Green regression commands MUST declare the explicit environment contract already required by current V5 contracts.

Runtime validation of specification prose is structural only. POLIS MUST NOT use a language model or heuristic semantic judgement to decide whether objective or clause text is correct, sufficient, or well written.

## Strict development method

`strict_sdd_tdd_v1` has these change-kind semantics:

- `feature` MUST use `regression.mode=red_green`;
- `defect` MUST use `regression.mode=red_green`;
- `behavior_preserving` MUST use `regression.mode=green_green` with an explicit characterization command.

For Green-to-Green, the characterization command MUST PASS on the immutable baseline and MUST PASS again on the target. Red-only oracle fields and regression-patch bytes MUST be absent. POLIS MUST NOT simulate behavior preservation by manufacturing an artificial Red state.

The existing Red/Green command contract remains authoritative: the baseline command MUST exit with the declared non-zero `baseline_exit_code` and prove every declared `baseline_output_contains` oracle; the same regression command MUST exit zero on the final target.

## Red capture

`polis capture-red` MAY capture a regression probe only for a Change Contract whose semantics require Red-to-Green proof.

For schema-v3 strict contracts, after the candidate probe is applied to an isolated index:

1. POLIS MUST calculate every changed staged path.
2. Changed paths MUST be ordered deterministically before validation.
3. Every changed path MUST be authorized by `test_scope`.
4. The declared Red command and oracle MUST succeed as a Red proof.
5. Source `HEAD`, real index, and source worktree state MUST remain unchanged by capture.

A strict Red probe that changes a path outside `test_scope` MUST fail closed and MUST NOT produce an accepted captured patch.

## Regression patch requirement

The presence of a regression patch is determined by Change Contract semantics, not solely by `kind=defect`.

- A contract requiring Red-to-Green MUST provide a non-empty regression patch to build and consumer validation.
- A contract not requiring Red-to-Green MUST NOT provide a regression patch.

This rule applies to schema-v3 strict features as well as defects.

## Strict Red-test immutability

For a strict schema-v3 or schema-v4 Red probe, POLIS MUST record the Git staged blob identity for every changed Red-probe path after isolated application.

The final target MUST satisfy both conditions for every captured Red-probe path:

1. the path remains part of the final payload; and
2. its staged target blob identity exactly equals the captured Red-probe blob identity.

A strict Red probe that deletes a changed path is invalid because no target test blob exists to bind. A target that deletes, rewrites, weakens, or otherwise changes a captured Red-probe path MUST fail validation even when the regression command would otherwise pass.

Legacy/current v1/v2 defect packages retain their existing path-presence semantics and do not gain blob-identity locking retroactively. Schema-v4 strict packages inherit the same blob-identity lock as schema v3.

## Evidence v2 ordering

Evidence v2 remains the evidence format. No new package member or raw command output field is introduced.

For any Change Contract requiring Red-to-Green, accepted PASS evidence MUST include, in the existing canonical ordering:

1. baseline regression Red execution and successful oracle proof;
2. target regression Green execution;
3. behavior command PASS;
4. affected command PASS;
5. Project Policy gate evidence.

A schema-v3 strict feature MUST therefore produce the same Red/Green evidence shape already required for a defect, while v1/v2 features retain their existing NOT_APPLICABLE regression evidence. A strict behavior-preserving contract emits two regression PASS gates in order: baseline Green, then target Green.

## Project Policy v3 migration rule

During this opt-in migration period, a Project Policy schema-v3 build MAY consume:

- Change Contract schema v2 using existing V5 semantics; or
- Change Contract schema v3 using this strict specification; or
- Change Contract schema v4 when the locked-baseline extension in POLIS Specification v4 is used.

Change Contract schema v1 remains readable only according to the existing migration compatibility rules.

Making schema v3 mandatory for all new Project Policy v3 builds is a future breaking policy decision and is not authorized by this specification.

## Compatibility and unchanged contracts

The following remain unchanged:

- package format v3 and its exact seven-member layout;
- Project Policy schema v3 eleven-gate registry;
- coverage adapters and strict `> 80.0` minimum semantics;
- Evidence v2 bounded-output hashing and no-raw-output rule;
- direct argv execution;
- detached Ed25519 signature trust model;
- exact Git baseline and target-tree verification;
- scope validation for final changed paths;
- consumer preflight freshness;
- transactional apply behavior and preservation of consumer `HEAD` and real index.

Schema v1/v2 Change Contracts MUST retain their existing decoding and behavior. In particular, v1/v2 features continue to use `regression.mode=not_applicable`, and existing defect Red-to-Green packages remain valid under their original semantics.

## Fail-closed conditions

Strict schema-v3 validation MUST fail on at least:

- unknown or missing development method;
- missing `scope` or `test_scope`;
- `test_scope` entries outside ordinary scope;
- missing specification or any empty required specification section;
- orphan requirement, unknown/duplicate requirement reference, or unsupported acceptance proof gate;
- empty or duplicate specification clause IDs;
- empty specification clause statements;
- strict feature without Red-to-Green regression;
- missing regression patch for a Red-to-Green contract;
- regression patch supplied for a contract not requiring Red-to-Green;
- Red probe path outside `test_scope`;
- incorrect Red exit code or output oracle;
- missing captured Red path in the final payload;
- changed target blob for a strict captured Red path;
- malformed or out-of-order Red/Green evidence.
