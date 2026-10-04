# SDD-0043 — Agent Workspace Validation Without Packaging

## Status

Implemented against the clean `main` baseline `c675cf4`, with the locked Change
Contract binding this change to the same commit and policy.

## Objective

Let an agent validate a completed in-scope change in the checkout it already
owns, without requiring a `.polis` package solely to obtain final local
validation. Keep portable producer/consumer delivery unchanged.

## Workflow boundary

`polis workspace validate` accepts the same locked strict contract, effective
Project Policy, captured Red proof, and optional contract-bound Implementation
Plan used by the producer validation path. It must reuse the same locked
baseline admission, source-index, target-tree, changed-path scope, proof replay,
immutable Red-test, behavior/affected command, and complete non-deferred policy
gate checks as `polis build`.

The workspace path stops after successful isolated target validation. It does
not create an archive, package evidence, run package verification, sign, preflight,
apply, stage, or commit. `polis build -> verify -> inspect -> preflight -> apply`
remains the canonical portable delivery path. A workspace PASS is not a package
PASS, producer authentication, consumer validation, or apply authorization.

The shared producer validation implementation is used by both paths so
contract, baseline, proof, scope, and policy behavior cannot drift. Workspace
validation rechecks the real index and recomputes the locked-base target tree
after isolated validation; a changed source snapshot fails instead of emitting a
current result. This check detects a changed final snapshot, not transient edits
that are reverted between observations, and is not a filesystem lock.

## Machine-readable checkpoint

Successful output reports the schema version, workspace-only validation kind,
base commit, exact target tree, contract and policy SHA-256, validation level,
enabled/disabled gates, and producer gate outcomes. It explicitly sets
`workspace_validated=true`, `delivery_artifact_built=false`, and
`delivery_artifact_verified=false`.

The optional `--out-report` path must be outside the physical target worktree,
absent, and a regular-file destination whose parent already exists. On POSIX,
POLIS creates it without overwrite using mode 0600; Windows uses the destination
directory's inherited filesystem ACL. The versioned JSON schema is embedded in
the offline runtime. Reports contain no command output, environment values, or
secret data. They are unsigned historical checkpoints; readers must not treat
them as authenticated evidence or assume they remain current after another
workspace edit. Build, verify, inspect, preflight, and apply never consume them.

## Security and compatibility

The command runs locked project checks in the existing isolated-worktree
validation path. Isolation is not a sandbox: configured project commands can
access external services or caches. Invalid contract/policy/baseline/proof,
source-index mutation, scope failure, target-tree drift, incomplete gates, or an
unsafe report destination fail closed. A failed validation writes no report.

Existing `build` validation, package formats, evidence, signatures, artifact
retention, consumer baseline modes, and transactional apply semantics remain
unchanged. The new command is vendor-neutral; Claude Code/Codex-specific hooks or
installers are outside this change.

## Acceptance traceability

- REQ-001 / AC-001: add the CLI workspace-validation entry point under strict
  locked-contract inputs.
- REQ-002 / AC-002: share complete existing producer validation and never consume
  incremental gate reuse as proof.
- REQ-003 / AC-003: preserve source Git state and produce no delivery package.
- REQ-004 / AC-002: make the local-vs-delivery distinction explicit in the
  versioned report.
- REQ-005 / AC-004: write only new external reports with restrictive permissions.
- REQ-006 / AC-004: publish the report schema offline and state the trust limits.
- REQ-007 / AC-006: expose workspace operations in the embedded agent-help index.
- REQ-008 / AC-007: keep canonical help inventory tests aligned with added commands.
- AC-005: retain existing package-building and verification behavior.
