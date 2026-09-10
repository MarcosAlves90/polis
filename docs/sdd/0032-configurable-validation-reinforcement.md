# SDD-0032 — Configurable Validation Reinforcement

## Status

Accepted for implementation in POLIS V6.

## Objective

Allow a project or a caller-owned execution policy to choose an explicit validation reinforcement level and to disable non-essential project gates without scattering bypasses through the executors.

## Model

Project Policy schema v3 gains the optional `validation_level` field:

- `strict` (default when the field is absent): `test.complete` and `coverage` are required, preserving the current V6 behavior;
- `standard`: `test.complete` remains required and `coverage` may be explicitly `not_applicable`;
- `minimal`: both `test.complete` and `coverage` may be explicitly `not_applicable`.

Every project gate remains explicit. An enabled gate has `command` or `coverage` mode. A disabled or non-applicable gate has `not_applicable` mode and a non-empty reason. The policy's gate inventory and order remain unchanged. `polis init` exposes the levels and repeated `--disable-gate` options for generated policies; an external policy passed to `start`/`build` is the per-execution configuration.

The level is a minimum assurance profile, not a claim that all higher-level gates are disabled. The generated Go `standard` profile disables coverage by default and the generated `minimal` profile disables all project-quality gates; callers may keep additional gates enabled in a supplied policy. The emitted configuration evidence lists the actual enabled and disabled gates.

## Non-optional invariants

Reinforcement levels never disable:

- policy and contract decoding/invariant validation;
- declared command argv, path, environment, timeout, and resource limits;
- change scope and test-scope enforcement;
- baseline identity and exact target-tree validation;
- required Red-to-Green or Green-to-Green development proof;
- package member, digest, checksum, evidence, signature, and locked-baseline verification;
- consumer clean-baseline, isolated validation, `git apply --check`, transactional apply, and HEAD/index preservation.

These checks are structural or security guarantees rather than project-quality gates.

## Evidence and failure semantics

Each policy execution emits a `validation_configured` evidence event containing the effective level and complete enabled/disabled project-gate inventory. Disabled gates are also represented by `gate_finished` with `NOT_APPLICABLE` and their explicit reason. Missing, unknown, contradictory, or incompatible configuration fails closed before command execution. A reduced level is never presented as equivalent to `strict`; inspection and evidence retain the selected level and actual gate inventory.

Historical policies without `validation_level` and historical evidence without `validation_configured` remain readable and mean `strict` for compatibility.

## Validation

Tests cover strict compatibility, standard and minimal policies, selective disabling, no execution of disabled commands, enabled command behavior, invalid levels and incompatible required gates, evidence/configuration consistency, and the CLI initialization surface.
