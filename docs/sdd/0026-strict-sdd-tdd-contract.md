# SDD-0026 — Strict SDD/TDD Change Contract

## Status

Accepted for implementation as an opt-in strict Change Contract v3 on top of POLIS V5. Existing Change Contract v1/v2 artifacts remain readable and v2 remains accepted for current policy-v3 builds during migration. Promotion of v3 to mandatory is intentionally deferred to a later breaking release after the strict path has production evidence.

## Problem

POLIS V5 requires SDD/TDD in the engineering guide, but machine enforcement is asymmetric. Defects require reproducible Red -> Green evidence; features may use `regression.mode=not_applicable`, so a valid artifact can prove final behavior and project gates without proving that its new behavior was test-first. SDD intent is also documented outside the machine Change Contract, so the runtime cannot require the presence of acceptance criteria, invariants, forbidden states, inputs, outputs, or failure semantics.

## Objective

Add a strict Change Contract v3 mode that makes the existing SDD/TDD workflow machine-enforceable for behavior-changing feature and defect deliveries while preserving v1/v2 migration behavior.

## Contract

A schema-v3 Change Contract MUST contain:

- `development_method: "strict_sdd_tdd_v1"`;
- ordinary `scope.allowed_paths`;
- `test_scope.allowed_paths` limited to paths authorized by `scope`;
- `specification` containing non-empty `objective`, `acceptance_criteria`, `invariants`, `forbidden_states`, `inputs`, `outputs`, and `failure_semantics`;
- every structured specification clause has a non-empty unique `id` and non-empty `statement`;
- explicit command environments for behavior, affected, and every executable regression command.

For schema v3:

- `feature` and `defect` MUST use `regression.mode=red_green` with the existing non-zero baseline exit-code and output-oracle contract;
- `behavior_preserving` retains `regression.mode=not_applicable` in this first increment; Green -> Green characterization proof is a separate follow-up because it requires a distinct capture/state contract rather than pretending a refactor should manufacture Red.

## Capture contract

`polis capture-red` MUST accept:

- legacy/current defect contracts that already require Red -> Green;
- schema-v3 features that require Red -> Green.

For a schema-v3 strict contract, after applying the candidate Red probe in isolation, every changed index path MUST be authorized by `test_scope`. Any production or otherwise unauthorized path in the Red probe blocks capture.

The Red probe continues to require the declared failing exit code and every baseline output token. Capture MUST remain non-mutating to source HEAD, real index, and working tree.

## Build and consumer validation

Any Change Contract whose semantics require Red -> Green MUST:

1. provide a non-empty regression patch to build/preflight/apply;
2. reproduce the declared Red state on the immutable base;
3. require the same regression command to PASS on the final target;
4. retain every Red probe path in the final payload;
5. for schema-v3 strict contracts, retain the exact staged blob identity of every captured Red probe path in the final target, preventing a test from being weakened or rewritten after Red capture.

A strict Red probe path that is deleted by the probe is invalid because there is no final test blob to lock.

## Compatibility

- Change Contract v1 and v2 decoding semantics remain unchanged.
- V1/v2 features continue to use `not_applicable` regression semantics.
- Existing v1/v2 defect Red -> Green behavior remains unchanged.
- Current Project Policy schema v3 builds accept Change Contract v2 or strict v3 during migration.
- Existing package format v3, Evidence v2, signature, exact-tree, scope, policy, preflight, and apply contracts remain unchanged.

## Acceptance criteria

1. A well-formed schema-v3 feature with strict specification, scope/test_scope, environments, and Red -> Green regression validates.
2. A schema-v3 feature using `not_applicable` regression is rejected.
3. A schema-v3 contract missing any required specification section is rejected.
4. Duplicate or empty specification clause IDs/statements are rejected.
5. `test_scope` outside ordinary `scope` is rejected.
6. `capture-red` accepts a valid schema-v3 feature Red test and rejects a Red probe containing a path outside `test_scope`.
7. A strict feature build without a regression patch is rejected.
8. A strict feature build reproduces Red on base and Green on target.
9. A strict feature build is rejected if a captured Red test path is absent from the final payload.
10. A strict feature build is rejected if the final staged blob for a captured Red test path differs from the blob captured in the Red probe.
11. Evidence validation requires the Red and Green regression trace for a strict schema-v3 feature.
12. Legacy/current v1/v2 feature and defect contracts retain their previous validation and evidence semantics.
13. Existing complete repository validation, coverage threshold, vet, build, module verification, and exact-tree behavior remain green.

## Invariants

- POLIS never infers which files are tests from language, filename, or directory conventions; `test_scope` is explicit contract data.
- POLIS never asks an AI/model to judge whether specification prose is semantically good; runtime validation is structural and deterministic.
- No shell command synthesis is introduced.
- Raw stdout/stderr persistence is not reintroduced.
- No new executable archive member or package member is introduced.
- Existing consumer validation remains fresh; no PASS cache is added.
- Strict-mode failure is fail-closed.

## Forbidden states

- Schema-v3 feature accepted without observed Red -> Green semantics.
- Red probe containing production paths outside declared `test_scope`.
- Final payload that silently modifies a captured Red test after successful capture.
- Schema-v3 contract accepted with missing/empty SDD sections.
- Strict-mode validation delegated to free-form model judgement.
- Legacy v1/v2 artifacts becoming unreadable as a side effect of this increment.

## Inputs

- Base Git commit and exact worktree state.
- External Change Contract JSON.
- Candidate Red working-tree delta.
- External captured regression patch.
- Final target payload.
- Committed Project Policy.

## Outputs

- Validated strict Change Contract v3 or deterministic validation error.
- Immutable captured Red patch for strict feature/defect work.
- POLIS artifact whose evidence proves the required Red -> Green sequence.

## Failure semantics

Any malformed strict specification, invalid test scope, missing required regression patch, incorrect Red oracle, Red path outside test scope, missing captured test in target, changed captured test blob, or evidence-order mismatch returns a non-zero validation result and produces no accepted artifact.

## Non-goals

- Making Change Contract v3 mandatory for every V5 build in this increment.
- Green -> Green characterization capture for `behavior_preserving`.
- Multi-cycle hash-chained Development Ledger.
- Trusted timestamps or remote witnesses for historical ordering.
- Mutation-testing, property-testing, fuzzing, or model-based-testing adapters.
- Storing prompts, chat transcripts, or model chain-of-thought.
