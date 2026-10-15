# SDD-0041 — Repository-Declared POLIS Artifact Retention

## Status

Accepted for implementation. This SDD resolves Issue #16 against the clean
`main` baseline locked by the POLIS Change Contract for this change.

## Objective

Allow each repository to declare, in committed state, whether POLIS-generated
contracts, plans, proofs, and validation evidence should be retained inside the
repository. A `.polis` delivery package is a disposable transfer output and must
remain outside the target worktree; it is never retained in a repository.
Preserve the existing external-output workflow when the preference is absent or
explicitly external.

## Retention manifest

The optional `.polis/artifact-retention.json` file is strict JSON with exactly
these required fields:

```json
{
  "schema_version": 1,
  "mode": "repository"
}
```

`mode` is `external` or `repository`. The file must be a regular committed file
at `HEAD`; the working copy must match the committed bytes exactly. A valid
uncommitted file, a modified manifest, an unsupported version, an unknown
field, duplicate or non-canonical field names, or an invalid mode fails closed.
When neither `HEAD` nor the worktree contains the manifest, the mode is
`external`. The preference is independent of `.polis/policy.json` and applies
whether Project Policy is committed or supplied through `--policy`.

## Producer behavior

After successful generation, `repository` mode may store exact eligible
non-package output bytes under
`.polis/artifacts/<class>/sha256-<hex-digest>.<extension>`. The delivery package
must remain outside the target worktree: `polis build --out` must resolve
physically outside it and is rejected before
creating output directories or package bytes if it resolves inside.

| Producer | Class | Retained bytes |
|---|---|---|
| `polis start` | `contracts` | Locked Change Contract |
| `polis implementation-plan` | `plans` | Generated Implementation Plan |
| `polis capture-red` | `proofs` | Captured regression patch |
| `polis build` | `contracts`, `plans`, `proofs` | Exact contract and any supplied plan and regression patch |
| `polis build` | `evidence` | Exact bounded evidence member included in the package |

Existing identical content is idempotent. A path containing different bytes,
an unsafe path, or a write failure is an error; POLIS does not overwrite
different content, stage, commit, or delete retained files. The managed root is
ordinary visible Git content and remains manually reviewable and stageable.
Command output reports retained paths when copies are created or reused. Class
`packages` cannot be newly published by the retention API.

In `repository` mode, producer inputs may come from an appropriate class under
the managed root: contracts from `contracts`, plans from `plans`, and
regression patches from `proofs`. POLIS verifies the regular-file boundary,
canonical content-addressed filename, digest, configured class, and size
before parsing them. The `packages` class remains readable for historical
content, but no current producer creates a new retained package. Other
in-worktree inputs remain rejected; external input validation is unchanged.

## Git and package boundaries

Only `.polis/artifacts/` is omitted from producer clean-state and application
source-delta checks. The retention manifest itself is never omitted. Status
filtering handles staged, unstaged, untracked, and rename entries; a rename
crossing the managed-root boundary remains a source change. Temporary-index
capture restores the managed path from its locked base tree so retained copies
cannot enter a Red patch or delivery payload. POLIS leaves the real Git index,
`HEAD`, refs, and commits unchanged.

Committed retained files continue to belong to the exact authenticated Git
baseline. The existing embedded-baseline size limit, target-tree proof,
Change Contract scope, package inventory, and integrity checks do not change.
No output or other repository path is exempted.

## Compatibility and failure behavior

Repositories with no manifest retain the current external-output behavior.
`external` mode also preserves caller-selected external outputs and does not
remove them. Existing historical package files are never deleted or modified
automatically; they may remain readable for compatibility; contracts, plans, proofs, and evidence remain eligible for repository retention. Invalid or uncommitted configuration, a mismatch with `HEAD`, an unsafe retained input, a symlinked managed path, or a conflicting content address fails before a successful command result. POLIS does not stage or commit outputs in either mode.

## Acceptance traceability

- REQ-001 / AC-001: strict, committed, versioned repository preference.
- REQ-002 / AC-002: shared behavior across supported producers and retained
  inputs.
- REQ-003 / AC-003: retain non-package classes without implicit Git mutation;
  never create a new repository-retained `.polis` package.
- REQ-004 / AC-004: managed files do not enter source-change proofs; baseline
  and package guarantees remain exact.
- REQ-005 / AC-005: absent manifest preserves current external behavior.
