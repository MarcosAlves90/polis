# SDD-0003 — Existing-project package build (alpha.3)

Status: Superseded for package-layout and delivery-specific validation semantics by SDD-0007; retained as historical alpha.3 design evidence

## Objective

Implement `polis build` for an agent-owned Git worktree without modifying the repository index, branch history, or source files. The output is one verified `.polis` package containing the exact delta from current `HEAD` to the current working-tree state.

## Inputs

CLI:

`polis build --repo <path> --project <slug> --change <slug> --out <directory>`

All flags are required in alpha.3.

## Preconditions

- `<repo>` resolves to a Git worktree.
- Git object format is explicitly detected with `git rev-parse --show-object-format` and MUST be `sha1` or `sha256`.
- `HEAD` resolves to one exact commit.
- The real Git index MUST equal `HEAD`; staged changes are rejected. Build never resets or mutates the real index.
- At least one tracked/untracked, non-ignored working-tree change exists.
- `.polis/policy.json` exists in `HEAD` and the working copy is byte-identical to the committed version. Policy changes are not permitted in the same alpha.3 delivery.
- Project and change slugs satisfy their manifest grammar.

## Target construction

- Runtime creates a temporary Git index.
- Temporary index is initialized from `HEAD`.
- Runtime adds the entire current non-ignored working-tree state to the temporary index with `git add -A -- .` using only the temporary index.
- `target_tree` is `git write-tree` from that temporary index.
- Patch payload is generated from `HEAD` to the temporary index using `git diff --cached --no-ext-diff --no-textconv --binary --full-index --find-renames HEAD --`.
- Empty patch is invalid.

## Isolated validation

- Runtime creates a detached temporary Git worktree at `base_commit`.
- Patch MUST pass `git apply --check` there.
- Runtime applies with `git apply --index` without `--unsafe-paths`.
- Resulting isolated `git write-tree` MUST equal `target_tree`.
- Runtime executes the canonical project policy in the isolated worktree and records NDJSON evidence.
- Overall project-policy status MUST be PASS; FAIL or BLOCKED prevents package creation.
- Temporary worktree is removed after validation. Failure to clean temporary resources is reported but MUST NOT mutate the source worktree.

## Manifest alpha.3

Manifest fields are exactly:

- `format_version`
- `project`
- `change`
- `git_object_format`: `sha1 | sha256`
- `base_commit`
- `target_tree`
- `policy_sha256`
- `payload_sha256`

Object IDs MUST match the declared Git object format: 40 lowercase hex for SHA-1 and 64 lowercase hex for SHA-256.

## Packaging

> Superseded by SDD-0007. Format v2 uses the seven-member inventory and mandatory Change Contract described there.

Historical alpha.3 rule: the package keeps the v1 five-member inventory. Runtime assembles candidate bytes, verifies them with the same package verifier used by `polis verify`, hashes the final archive bytes with SHA-256, and names the final artifact:

`polis-<project>-<change>-<archive-sha256-first12>.polis`

Existing output path MUST NOT be overwritten.

## Acceptance criteria

- Build includes untracked non-ignored files without staging them in the source index.
- Build rejects staged source changes.
- Build rejects a missing, uncommitted, or modified `.polis/policy.json`.
- Build rejects an unchanged worktree.
- Build validates the exact patch in isolation and verifies exact target tree.
- Build executes policy; package is not produced on FAIL/BLOCKED.
- Successful package passes `polis verify`.
- Source HEAD, index, and working-tree bytes are unchanged by build.
