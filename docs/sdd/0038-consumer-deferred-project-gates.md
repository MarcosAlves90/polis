# SDD-0038 — Consumer-Deferred Project Gates

## Status

Accepted for implementation of GitHub issue #4.

## Objective

Allow a producer to omit execution of selected enabled Project Policy gates while preserving each gate as an authenticated, mandatory consumer obligation. Deferral transfers execution responsibility; it never removes the validation requirement.

## Execution-plan contract

`policyplan` remains the single interpreter of Project Policy and its effective dependency graph. Add an execution-options entry point that accepts a deferred gate set; retain the existing `Compile(policy)` and `Load` behavior as no-deferral wrappers. `plan` and producer execution consume the same compiled plan.

The plan reports canonical `deferred_gates` and `consumer_validation_required`, plus producer action and consumer requirement for each gate. The Project Policy schema, gate mode, command, coverage configuration, validation level, dependencies, and policy digest do not change.

Only enabled IDs from `spec.ProjectGateOrder` are eligible. Unknown, duplicate, and `not_applicable` IDs fail before any project command. Deferred IDs are ordered canonically. A producer-executed gate is invalid when any direct or transitive prerequisite is deferred; a deferred gate may depend on producer-executed or deferred prerequisites. `spec.LintPolicy` dependencies and topological order remain authoritative.

## Evidence versioning

Evidence v2 semantics are frozen. Add Evidence v3 decoding/validation without broadening the historical `DecodeEvidence` contract. V3 `validation_configured` requires `deferred_gates`, including an explicit empty array; it must be an ordered, unique subset of `enabled_gates`, disjoint from `disabled_gates`.

Evidence v3 permits `DEFERRED` only for a project-gate `gate_finished` event with a stable non-empty reason. It does not permit `DEFERRED` for `command_finished` or `coverage_measured`. A deferred producer gate has the ordinary `gate_started` event followed by `gate_finished: DEFERRED`, with no command or coverage result. Every non-deferred enabled producer gate must still pass. A producer failure or blocked command remains a failure and cannot be converted to deferral.

## Package compatibility and integrity

New artifacts use package format v5, which selects Evidence v3. Formats v2, v3, and v4 select Evidence v2 and have an empty deferred inventory. Formats v2/v3 retain their exact seven-member layout. Formats v4/v5 retain the exact eight-member layout; both require and verify `polis/polis-baseline.tar` and `baseline_sha256`. No ninth package member is added.

Package verification validates the versioned evidence against the packaged Project Policy, including canonical inventories, producer gate trace, absence of execution results for deferred gates, and PASS evidence for every enabled non-deferred gate. Checksums and optional detached signatures continue to authenticate exact artifact bytes. Deferred metadata is exposed only after package semantic verification succeeds.

## Producer and consumer behavior

`polis build` accepts repeatable `--defer-gate <id>` values. With no values, producer execution remains unchanged. Build output in text and JSON reports the deferred inventory and whether consumer validation is required.

`polis plan` accepts the same option, remains read-only, and reports producer actions, consumer requirements, dependency edges, and execution order from the same compiled plan used by build.

`verify` and `inspect` expose verified deferred obligations in deterministic order. `preflight` and `apply` compile the packaged policy with an empty execution-deferral set and execute every enabled gate, including producer-deferred gates. Preflight remains read-only. Apply retains isolated consumer validation, target-tree checks, `git apply --check`, real-state revalidation, transactional mutation, and post-apply verification. Any consumer FAIL/BLOCKED result stops before real mutation; no consumer option may waive deferred gates.

Successful preflight/apply reports producer-deferred gates, consumer PASS results for those obligations, and no outstanding deferred gates. Historical formats report an empty deferred inventory.

## Non-deferrable invariants

`--defer-gate` accepts only `spec.ProjectGateOrder` project-quality gate IDs. Policy/Change Contract decoding, dependency validation, command safety, scope, locked development proof, baseline proof, package inventory/checksums, signature verification when requested, bounded parsing, Git object validation, exact target-tree construction, consumer baseline checks, isolation, `git apply --check`, race detection, transactional apply, and post-apply tree verification remain mandatory at their existing boundaries.

## Compatibility

- Project Policy schema v3 remains unchanged.
- Plan JSON advances to plan version 2 because responsibility fields are added; existing no-deferral callers retain empty deferral state.
- Package v2/v3/v4 readers retain Evidence v2 semantics; v4 embedded-baseline validation remains active after v5 becomes current.
- New package v5 writers use Evidence v3 even when the deferred inventory is empty.
- No-flag builds continue to execute every enabled producer gate.

## Acceptance and validation

The implementation maps all 28 acceptance criteria and 35 required issue scenarios to focused/unit/integration tests. Validation includes historical v2/v3/v4 fixtures, no-deferral end-to-end behavior, producer execution omission, evidence tampering, direct/transitive dependency closure, read-only preflight, failure-atomic apply, deterministic text/JSON reporting, offline resource export, the full Go suite, race, vet, build, formatting, module integrity, diff checks, and strict project-wide line coverage above 80%.

No validation result is recorded here until observed.
