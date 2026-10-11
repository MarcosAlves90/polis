# Development proof and delivery

## Canonical V6 delivery flow

By default, the canonical producer path keeps Project Policy, Change Contract,
regression patch, package, signature, and caller-owned records outside the
target repo.

Run `polis start` on a clean baseline. It locks the policy, Specification,
Change Contract, test scope, and proof requirements for later producer steps.

Before preparing a Red probe, run `polis check-red-scope` with each proposed
repository-relative file path and the locked Red-to-Green contract. The files
do not need to exist yet. The command reports every path as accepted or
rejected, lists the effective `test_scope.allowed_paths` and
`scope.allowed_paths`, and names the rejecting rule for each failure. Text and
JSON contain the same decisions. Exit code 0 means all proposed paths fit;
exit code 6 means a scope or contract validation failed. This check runs no
regression command, writes no patch, and does not modify the source repository.
It checks the proposed list only; `capture-red` still validates every path in
the actual captured patch before executing the Red command.

Use `polis capture-red` for Red-to-Green work. Use the locked contract with
`polis build`; the resulting package can be inspected, verified, and applied by
the consumer workflow.

Run `polis status --repo <path>` when resuming any POLIS workflow. Repository
retention still discovers `.polis/artifacts/` automatically. In the default
external-retention workflow, pass the external Policy, locked Change Contract,
Implementation Plan, Red proof, workspace report, and package files that exist
for the task. A verified package can reconstruct its embedded locked contract
even when `--contract` is omitted. Status projects the selected contract,
baseline, evidence, gates, optional workspace checkpoint, and next action into
explicit `proven`, `stale_or_unproven`, and `missing` lists. A matching workspace
report remains unsigned historical data: it can show that source identities
still match the saved checkpoint, but it does not establish current validation
or implementation completion. Before a verified package exists, source changes,
a plan, or a checkpoint do not by themselves prove that implementation is
complete. In JSON, `next_action.action` names a workflow step and
`next_action.command` is present only when that step has a POLIS command.

## V6 contract summary

- Unplanned builds use package format v5 with eight regular members under `polis/`. Explicitly planned builds use format v6 with the exact plan as a ninth member; both include the authenticated `polis/polis-baseline.tar`. Valid historical v2-v5 formats remain readable.
- Project Policy uses schema v3 with command environments and gate configuration.
- New builds accept locked Change Contract schema v4 for compatibility and schema v6 for the new producer flow. Schema v5 is a draft accepted by `polis start`, which locks it as v6. Schemas v1-v4 retain their existing meaning.
- Formats v5 and v6 use Evidence v3 to record deferred gates as `DEFERRED`; formats v2-v4 retain Evidence v2 semantics. Both use bounded-output counts and digests, not raw streams.
- `polis inspect` reports plan presence, schema, strategy, step count, and requirement-to-plan-to-proof traceability after successful package verification. JSON separates test and implementation step IDs; `proof_plan_steps` lists each step declaring the linked contract proof with its ID and kind. Historical and unplanned artifacts report plan absence.
- `polis build --defer-gate <id>` may repeat the option. Deferred gates are skipped by the producer and run with the packaged policy during consumer `preflight` and `apply`.
- Detached Ed25519 signatures authenticate exact package bytes.
- Coverage adapters are Go coverprofile, LCOV, and Cobertura.
- Strict consumer mode keeps exact Git baseline identity; compatible retains ancestry proof; permissive can source locked development proof from local or embedded baseline state while retaining deterministic exact consumer target-tree validation. Missing proof can be waived only through the explicit permissive-only override.
- Consumer validation is isolated and `apply` is transactional. `--commit-mode none` preserves apply-only behavior; explicit prompt/auto modes create a local commit only after the post-apply target-tree check.

V6 can read supported historical V5 policies and Change Contracts for migration.
It does not create new legacy producer contracts.

## Strict SDD/TDD

Strict drafts contain the Specification, requirement-to-acceptance traceability,
test scope, and proof command.

Strict features and defects require Red-to-Green proof. Strict
`behavior_preserving` changes require the same characterization command to pass
on baseline and target without manufacturing a Red state.

Captured strict Red paths are immutable. Later steps revalidate the repository,
Specification, policy hash, baseline lock, and captured tests.
