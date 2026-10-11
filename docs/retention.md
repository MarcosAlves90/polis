# Artifact retention and apply behavior

## Repository artifact retention

A repository can commit `.polis/artifact-retention.json` to choose where POLIS
keeps generated artifacts. The optional strict manifest follows
[`artifact-retention-v1.schema.json`](../spec/schemas/artifact-retention-v1.schema.json):

```json
{
  "schema_version": 1,
  "mode": "repository"
}
```

`mode` is `repository` or `external`. If the manifest is absent, POLIS uses
`external` for compatibility. POLIS requires a present manifest to be committed
and byte-for-byte unchanged from `HEAD`; invalid or uncommitted preferences
fail closed. This setting is independent of Project Policy and works with
either committed or external `--policy` input.

In `repository` mode, `start`, `implementation-plan`, `capture-red`, and `build`
may publish exact, content-addressed contracts, plans, regression proofs, and
bounded validation evidence under `.polis/artifacts/`. **A delivery package is
never retained in the repository.** The `.polis` archive is a transferable,
disposable output; `polis build --out` must resolve physically outside the
target worktree. Later producer steps can read contracts, plans, and proofs
from their matching managed class. CLI output lists retained non-package
paths. Existing historical package copies are not deleted automatically and
may remain readable for compatibility.

Retained files remain ordinary visible Git files. POLIS never stages, commits,
or deletes them. Only `.polis/artifacts/` is omitted from producer cleanliness
and application-change delta checks; the retention manifest and all other
paths remain subject to the normal checks. Committed artifacts stay in the
authenticated baseline and count toward the existing baseline size limit. If
the repository later changes to `external`, existing retained files are left
untouched.

To carry producer-authored commit intent, put the optional field below in a
strict Change Contract schema-v5 draft before `polis start`:

```json
{
  "commit": {
    "message": "feat(apply): support artifact-backed commit creation\n\nPreserve the validated target tree."
  }
}
```

`polis start` locks schema v5 as schema v6 while preserving the message. The
existing schema-v3 draft to schema-v4 locked flow remains supported for
contracts without commit metadata. New packages keep format v5 and the existing
eight-member inventory; the message remains in the checksummed
`polis/polis-change.json` member. A trusted detached signature authenticates it
when supplied and verified; checksums alone do not identify the producer.

The message is limited to 16,384 Unicode scalar values and 64 KiB of UTF-8,
must be non-empty, valid UTF-8, and contain no NUL. POLIS preserves whitespace,
multiline content, and final-newline presence without imposing a universal
commit-message convention.

Consumers still apply only by default. Use `--commit-mode prompt` to review the
exact message and target tree and confirm before real mutation, or
`--commit-mode auto` to explicitly authorize a local commit without prompting.
Both modes require artifact commit metadata. A refusal or unavailable TTY is
blocked and leaves HEAD, index, and worktree unchanged. A created commit uses
the validated consumer HEAD as its parent and exactly the validated consumer
target tree; successful commit mode leaves the repository clean. POLIS does not
run commit hooks, request signing, push, or change remote refs. Apply JSON
reports `committed`, `commit_sha`, and `commit_message`.

Canonical external-policy execution is zero-residue during build, verify,
inspect, preflight, and `apply --commit-mode none`; these commands preserve
`HEAD`, the real index, refs, Git configuration, and persistent Git objects.
An explicitly authorized prompt/auto success intentionally creates a local
commit and ref. A failed commit attempt restores `HEAD`, index, and worktree
when rollback can be verified; an unreachable Git object may remain.

After applying a package, `apply` checks the exact target tree without changing
the real index. The temporary tree calculation includes tracked worktree changes
and eligible non-ignored untracked files, while excluding `.polis/artifacts/`.
Untracked names are treated as literal paths, including names that resemble Git
pathspec syntax. Ignore rules therefore do not hide tracked changes, and ignored
directories do not make tree verification fail merely because Git encounters
them. If target-tree verification fails, POLIS reverses the patch and verifies
`HEAD`, the index, the worktree tree, and repository status; it reports an
unverified rollback rather than claiming that restoration succeeded.
