# SDD-0008 — Canonical Red-State Capture

Status: accepted for 4.0.0

## Objective

Make strict defect TDD easier to use without weakening the Red -> Green evidence contract. `polis capture-red` captures the exact current Red working-tree delta as the immutable regression probe later embedded by `polis build`.

## Command

```text
polis capture-red --repo <git-worktree> --contract <change.json> --out <regression.patch>
```

All three arguments are mandatory.

## Preconditions

1. `--repo` resolves to a Git worktree.
2. `HEAD` exists.
3. The real Git index equals `HEAD` exactly; staged changes are rejected.
4. `--contract` is a regular file outside the target worktree, <= 1 MiB, valid Change Contract schema v1, and `kind` is exactly `defect` with `regression.mode=red_green`.
5. The working tree contains at least one non-ignored change.
6. `--out` resolves outside the target worktree and does not already exist.

## Deterministic capture

1. Record source `HEAD`, index tree, and porcelain status.
2. Create a temporary Git index initialized from `HEAD`.
3. Add the complete current non-ignored working tree to the temporary index.
4. Generate one binary/full-index Git patch against `HEAD`.
5. Reject an empty patch.
6. Create an isolated detached worktree at `HEAD`.
7. Apply the candidate probe with `git apply --check` then `git apply --index`.
8. Execute only the Change Contract regression baseline command.
9. Require the baseline oracle to evaluate PASS: declared non-zero exit code plus every declared baseline output token.
10. Write the exact validated patch to `--out` with exclusive-create semantics.
11. Re-read the output and require byte equality with the validated candidate.
12. Require source `HEAD`, real index tree, and porcelain status to equal the values captured in step 1.

## Forbidden behavior

- No shell-generated command strings.
- No automatic staging, reset, clean, stash, commit, or source-worktree mutation.
- No acceptance of non-defect contracts.
- No overwrite of an existing output.
- No output file inside the target worktree.
- No successful capture when the Red oracle does not fail for the declared reason.

## Output

Success reports the output path and SHA-256 of the exact probe bytes. Failure writes no output and returns non-zero.
