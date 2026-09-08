# SDD-0031 — Descendant Producer HEAD Without Baseline Drift

## Status

Accepted for implementation.

## Problem

POLIS V6 locks an exact Git baseline before implementation, but `build` currently reuses the same exact-HEAD validation required by `capture-red`, `preflight`, and `apply`. As a result, any legitimate commit created after `polis start` advances producer `HEAD` and is treated as baseline drift, even when the locked base commit and tree still exist unchanged and remain ancestors of the target state.

This creates recurrent `baseline base_commit mismatch` failures during ordinary development workflows that commit Red tests or implementation slices before packaging.

## Objective

Keep the locked baseline exact while allowing `polis build` to package a target whose current producer `HEAD` is a descendant of the locked baseline.

## Required behavior

1. `polis start` semantics remain unchanged: it locks exact object format, base commit, base tree, policy digest, and Specification digest on a clean baseline.
2. `polis capture-red` continues to require the producer repository to be exactly at the locked baseline commit and tree.
3. `polis build` MUST use `baseline_lock.base_commit` as the artifact base commit and target-construction base.
4. `polis build` MAY run when current producer `HEAD` differs from the locked base commit only when the locked base commit is an ancestor of current `HEAD`.
5. `polis build` MUST resolve the tree of the locked base commit and require it to equal `baseline_lock.base_tree`.
6. `polis build` MUST continue to require matching Git object format, Specification digest, and effective Project Policy digest.
7. `polis build` MUST continue to reject staged changes in the real index.
8. A clean producer worktree is valid when descendant commits already contain the complete target; an empty base-to-target patch remains invalid.
9. `preflight` and `apply` continue to require the consumer `HEAD` to equal the artifact base commit exactly and the consumer worktree/index to be clean.
10. A producer `HEAD` that is not descended from the locked base commit MUST fail closed before target construction.

## Invariants

- The artifact manifest `base_commit` remains exactly equal to `baseline_lock.base_commit`.
- Consumer baseline validation is not relaxed.
- Red capture ordering is not relaxed.
- Scope, immutable captured-test, policy, target-tree, package verification, signature, and transactional apply semantics remain unchanged.
- No automatic reset, checkout, rebase, merge, commit, or history mutation is introduced.

## Failure semantics

- Missing/malformed locked baseline continues to fail.
- Object-format, base-tree, Specification, or policy digest mismatch continues to fail.
- Producer history that no longer contains the locked base commit as an ancestor fails as locked-baseline drift.
- Consumer `HEAD != manifest.base_commit` remains a baseline mismatch.
- Empty base-to-target payload remains a build failure.

## Compatibility

This changes only V6 producer `build` admission for schema-v4 locked contracts. Artifact format, schemas, reader compatibility, and consumer semantics are unchanged.

## Validation

Implementation is complete only when:

- a regression test reproduces the current baseline mismatch after a post-`start` descendant commit;
- the same case builds successfully after the fix and the artifact base commit remains the locked base;
- a fully committed descendant target can build with a clean worktree;
- a non-descendant producer `HEAD` is still rejected;
- `capture-red` still rejects post-lock HEAD drift;
- `preflight`/`apply` still reject a consumer on the wrong HEAD;
- focused tests and the complete Go suite pass.
