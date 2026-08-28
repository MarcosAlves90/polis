# SDD-0004 — Transactional consumer apply (alpha.4)

Status: Accepted for implementation

## Objective

Make the normal consumer operation one command:

`polis apply [--repo <path>] <artifact.polis>`

`--repo` defaults to the current directory. The runtime MUST validate before changing consumer files and MUST preserve the real Git index.

## Preconditions

- Package passes complete POLIS structural/integrity verification.
- Target path resolves to a Git worktree.
- Repository Git object format equals manifest `git_object_format`.
- `HEAD` equals manifest `base_commit` exactly.
- Real working tree and index are completely clean, including untracked non-ignored files.
- Preconditions are checked again immediately before real application.

## Isolated consumer validation

- Create detached temporary worktree at `base_commit`.
- Apply package patch with `git apply --check` then `git apply --index` in isolation.
- Isolated tree MUST equal manifest `target_tree`.
- Execute the package policy in isolation and write fresh consumer evidence.
- FAIL or BLOCKED stops before real worktree mutation.

## Consumer evidence

- Evidence is written under Git metadata, never into the tracked/untracked worktree namespace.
- Canonical directory is resolved through `git rev-parse --git-path polis/results`.
- Each run creates a new non-overwriting `.ndjson` file and prints its path.

## Real apply

- Runtime performs `git apply --check` against the clean exact baseline.
- Runtime applies patch to the working tree without `--index`; consumer staging state therefore remains unchanged.
- Runtime computes resulting working-tree identity using a temporary index initialized from `HEAD` and populated from the post-apply worktree.
- Computed tree MUST equal manifest `target_tree`.
- `HEAD` MUST remain unchanged.

## Failure atomicity

- Any package, baseline, dirty-tree, isolated-apply, target-tree, or policy failure before real apply leaves consumer files unchanged.
- Real patch application uses Git's all-file apply operation after successful `--check` and isolated validation.
- A post-apply target-tree mismatch is a critical validation failure; runtime attempts reversal with the exact patch and reports whether rollback restored the baseline. It MUST NOT claim successful apply when rollback or verification is uncertain.

## Acceptance criteria

- Canonical package applies to an exact clean baseline with one command.
- Real Git index remains equal to its pre-apply tree.
- New/modified/deleted files produce exact manifest target tree when measured through a temporary index.
- Dirty worktree, wrong HEAD, wrong object format, malformed package, or consumer policy failure never reaches real apply.
- Evidence creation does not make `git status` dirty.
