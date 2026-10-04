# SDD-0044: Workspace checkpoint status

Status: Accepted for the `workspace status` slice.

## Context

`polis workspace validate` runs locked producer validation directly against the agent's checkout and may save an external version-1 JSON checkpoint. It deliberately does not create a `.polis` delivery package. The existing top-level `polis status` derives persisted workflow state from repository-retained artifacts; in external-retention mode it correctly reports that persisted state is unavailable. After context loss, an agent therefore needs a read-only way to compare a saved checkpoint with the checkout without rerunning expensive project gates.

The v1 report is unsigned, historical data. It records the base commit, target tree, contract digest, policy digest, and gate outcomes, but does not bind the exact Red-patch or implementation-plan digests. A report can also be edited or fabricated. The new operation must expose these limits rather than upgrade the report into validation evidence.

## Decision

Add `polis workspace status --repo <path> --contract <external-locked-contract> --report <external-report> [--policy <policy>] [--format text|json]`.

The command strictly parses a bounded version-1 report, reads the external locked contract and any explicit policy through the same bounded descriptor-based reader, and loads the default committed policy through a bounded read plus HEAD comparison. It validates the contract-policy binding and compares the report's recorded base/tree/contract/policy identities with the lock and a freshly computed source target tree. It reuses the producer's temporary-index snapshot helper, so source-tree identity uses the same Git semantics as producer validation. A staged index or an unresolvable baseline makes the snapshot unavailable rather than matching.

The command does not run Project Policy gates, replay Red proof, or alter HEAD, refs, the real index, or source files intentionally. Git may invoke repository-configured clean filters while creating the temporary-index snapshot; this is not a sandbox.

## Result semantics

- `source_snapshot_matches`: recorded base, target tree, contract digest, and policy digest agree with the selected lock and current source snapshot.
- `source_snapshot_differs`: the current source or selected input identities differ from the checkpoint.
- `checkpoint_unavailable`: POLIS cannot safely compute a comparable source snapshot, for example because the real index contains staged changes.

A JSON `status: PASS` means the comparison command completed, not that the saved checkpoint is authentic or that current validation has passed. Every result explicitly reports `report_authenticated=false`, `current_validation_established=false`, `proof_input_digests_bound=false`, and `delivery_artifact_verified=false`; it points to `polis workspace validate` when fresh gates are needed. The result includes both recorded and selected base/contract/policy identities to make mismatches observable.

## Input boundary and parsing

The report, external contract, and any explicit policy must be bounded regular non-symlink files outside the resolved Git worktree. For each external input, POLIS checks containment before and after opening, compares `Lstat` metadata with the opened descriptor, rechecks the path still names that descriptor, and reads at most the size limit plus one byte. The default committed policy is read through a bounded descriptor; POLIS pins one HEAD object ID for both Git blob-size and content reads and fails if HEAD moves during the operation. Parsing rejects duplicate or unknown JSON keys, missing/null report fields, invalid UTF-8, malformed identities, unsupported report versions, and artifact-verification claims. Input contents are never executed. A matching source snapshot does not authenticate the report.

The output is a closed, versioned workspace-status-v1 JSON object embedded in the offline resource kit. A stale or unavailable comparison is a successful inspection (exit 0); malformed input or invalid locked prerequisites fails closed.

## Alternatives rejected

- Re-running `workspace validate` from `status` would be expensive and would conflate inspection with gate execution.
- Reusing top-level `polis status` cannot discover arbitrary external reports and is intentionally limited to repository-retained state.
- Calling a matching report `current` or `verified` would overstate an unsigned v1 report that lacks proof-input digests.
- Provider-specific Claude/Codex logic is out of scope; the CLI remains agent-agnostic.

## Consequences and limits

The command can detect source, contract, policy, and baseline drift, but cannot authenticate the report or prove which Red patch/plan was used. It performs Git snapshot operations and does not isolate configured filters. Call `workspace validate` for fresh proof/gates; use `build` and `verify` when a portable artifact or consumer handoff is required. No package, signature, stage, commit, or push is produced by this command.