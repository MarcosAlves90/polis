# SDD-0030 — POLIS V6 Mandatory Strict Development

## Status

Accepted for implementation.

## Problem

POLIS 5.1 introduces strict SDD/TDD as an opt-in workflow through Change Contract schemas v3/v4 and `polis start`, but the producer still accepts legacy schema-v2 and unlocked schema-v3 contracts. A caller can therefore create a new delivery without the locked specification-first workflow. That contradicts the intended V6 contract: using the V6 producer must require machine-verifiable SDD/TDD evidence appropriate to the change kind.

## Objective

Make strict locked development mandatory for every new artifact produced by POLIS V6 while preserving read/verification compatibility for older artifacts.

## Required behavior

1. `polis build` in V6 MUST accept only Change Contract schema v4 with `development_method=strict_sdd_tdd_v2` and a valid `baseline_lock`.
2. Schema-v1, schema-v2, and schema-v3 contracts remain decodable for migration/inspection but MUST be rejected as producer input.
3. `polis capture-red` in V6 MUST require a schema-v4 locked contract whose regression semantics require Red-to-Green.
4. `polis start` remains the canonical CLI transition from an accepted strict schema-v3 draft to a schema-v4 locked contract.
5. A feature or defect produced by V6 MUST prove Red on the locked baseline and Green on the target using the immutable captured test scope.
6. A `behavior_preserving` change produced by V6 MUST prove Green on the locked baseline and Green on the target using the same characterization command.
7. `polis verify` and `polis inspect` MUST retain read compatibility for valid historical POLIS artifacts and Change Contract schemas already supported by V5.
8. `polis preflight` and `polis apply` MAY consume valid historical artifacts for migration compatibility; accepting an old artifact does not authorize V6 to produce a new legacy artifact.
9. CLI version MUST be `6.0.0`.
10. The Go semantic import path MUST be `github.com/MarcosAlves90/polis/v6`.
11. V6 documentation MUST describe strict locked development as mandatory for new builds, not opt-in.

## Invariants

- Package format remains v3 unless a separate specification proves a format change is required.
- Project Policy schema remains v3.
- Evidence schema remains v2.
- Existing detached-signature, hashing, path, environment, resource-limit, and transactional-apply semantics remain unchanged.
- `polis start` does not mutate HEAD, index, existing worktree files, or committed Project Policy.
- Legacy decoding is migration compatibility only and MUST NOT become a producer bypass.
- No plugin system, remote witness, timestamp authority, AI evaluator, validation cache, or new distributed component is introduced.

## Failure semantics

- `build` with Change Contract schema v1/v2/v3 fails before target construction or policy gate execution and directs the caller to `polis start`/schema v4.
- `capture-red` with an unlocked strict schema-v3 contract fails before reading or emitting a regression patch.
- Invalid or drifted schema-v4 baseline locks continue to fail closed at `capture-red`, `build`, `preflight`, and `apply` according to their existing boundaries.
- Historical valid artifacts remain verifiable even when their Change Contract is not schema v4.

## Compatibility

V6 intentionally breaks the producer workflow: commands that could build new V5 artifacts from schema-v2 or schema-v3 Change Contracts are no longer valid producer operations. This is the major-version justification. Read-side compatibility is preserved where it does not weaken the V6 producer contract.

## Validation

Implementation is complete only when:

- focused Red/Green tests prove legacy producer inputs are rejected and schema-v4 locked input succeeds;
- `capture-red` rejects schema v3 and accepts locked v4 Red-to-Green;
- historical-artifact verify/inspect tests remain green;
- module/version contract tests prove `/v6` and `6.0.0`;
- complete tests, race detector, `go vet`, `go mod verify`, `go build ./...`, formatting, diff checks, and strict project line coverage `>80%` pass;
- a fresh external repository completes `polis start → capture-red/Green → build → verify → inspect → preflight → apply` using V6;
- POLIS V6 can self-build and verify this change without remote publication.
