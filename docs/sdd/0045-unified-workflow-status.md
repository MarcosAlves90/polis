# SDD-0045 — Unified workflow status projection

## Status

Accepted for implementation on 2026-10-06.

## Context

Repository retention lets top-level `polis status` reconstruct a workflow from
`.polis/artifacts/`, while the default external workflow leaves those artifacts
outside the repository. `polis workspace status` can compare an external
workspace-validation checkpoint, but it is a separate partial view. After an
agent loses context, the canonical external workflow therefore has no single
read-only projection of its existing Policy, Change Contract, plan, Red proof,
workspace report, and package.

## Decision

Top-level `polis status` is the single workflow projection surface. Repository
retention continues automatic content-addressed discovery. Explicit
`--policy`, `--contract`, `--implementation-plan`, `--regression-patch`,
`--report`, and `--package` inputs let external workflows reconstruct the same
state without adding a database, daemon, session manifest, or background
process. A verified package may supply its embedded locked contract when no
contract path is given.

The command reuses existing bounded external-input readers, strict schema
decoders, contract/policy binding, Red-proof validation, package verification,
and workspace checkpoint comparison. Explicit artifacts inside a repository are
accepted only through the existing managed-retention boundary; ordinary
in-worktree files remain invalid inputs.

## Projection semantics

The existing `state`, `consistent`, stage summaries, gates, problems, and
`next_action` remain authoritative. Status additionally emits three
agent-oriented lists:

- `proven`: evidence validated by the current command, such as a selected locked
  contract, resolvable baseline, matching policy, valid Red proof, or verified
  package;
- `stale_or_unproven`: checkpoint claims, invalid/incomplete artifacts, deferred
  consumer validation, or current mismatches;
- `missing`: prerequisites or delivery evidence still required for the selected
  workflow.

A workspace report remains unsigned historical data. Even when its recorded
source, contract, and policy identities match the current checkout, status does
not set current validation or implementation completion from that report. A
verified package remains the strongest completion evidence. Source changes and a
plan alone continue to mean `implementation_pending`.

## Compatibility

`polis workspace status` remains available as the focused checkpoint-inspection
surface. Existing repository-retention status behavior and exit-code semantics
remain unchanged. The change adds no artifact schema and intentionally creates
no new persistent state.
