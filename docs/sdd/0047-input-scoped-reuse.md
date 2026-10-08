# SDD-0047 — Safe opt-in input-scoped incremental gate reuse

## Objective

Reduce redundant `polis gates --reuse` executions for command gates whose file
inputs are exhaustively declared by the project policy. This is **not** inferred
dependency analysis or hermetic execution.

## Behavior

- Policy schema v3 supports optional `input_paths_complete: true` on command gates
  with non-empty `input_paths`. It is an explicit policy-author assertion that
  the gate depends on no file outside those exact paths or directory prefixes.
  Without that assertion (including existing policies), retain the global
  tracked/nonignored snapshot and HEAD binding. Coverage gates cannot opt in. `input_paths: ["."]` keeps the global
  identity, since its scope cannot be narrowed.
- The asserted scope hashes worktree file bytes, modes, contained symlink
  targets, Git stage entries/index visibility and cached delta. Include untracked
  **and ignored** files inside declared paths. Refuse escaping/nonregular files,
  submodules and oversized/incomplete enumeration. Identity is version-tagged;
  old global identities cannot be accepted as scoped evidence.
- Gate identities still bind policy digest, command/definition, dependency
  result identities, Change Contract and baseline, runtime/POLIS version,
  environment declaration, and the caller-supplied `environment-id`.
- For safe reuse, a previous run must be current, PASS, have exit code 0 and
  complete observation, matching source scope and dependency identities.
  Dependency gates must pass before evaluating a dependent's reuse.
- `environment-id` is an explicit nonsecret version of every relevant external
  input, including toolchain, external resources, Git configuration and runtime
  environment, whether or not paths are complete. Changes must update it.
- Replay remains global and executes anew. Package build, verify, preflight and
  apply never use incremental gate-run caches.
- Recheck scoped inputs at completion, including ignored files. If source
  changes during execution, mark the run stale and block any passing guarantees.
  No filesystem lock is promised: transient restored mutations remain unprovable.

## Compatibility and security

This extension is additive to schema-v3 policy decoding; existing policies and
run-manifest schema v1 remain valid and conservative. Policies using the new
field require a reader supporting this extension (older strict decoders reject
it). A completeness claim is user-supplied trust, **not automatically verified**:
commands that read undeclared files, Git history, network state, or generated
outputs can produce stale reuse unless inputs/environment-id fully cover them.
Avoid opt-in for opaque commands; prefer global invalidation in doubt.

## Acceptance

- Unrelated changes to a distinct path and to HEAD do not invalidate a
  fully-scoped command gate; in-scope bytes/modes/staging/new files do.
- No explicit scope assertion means global invalidation remains.
- Unsafe scopes fail closed; previous failed/blocked/unversioned results cannot
  be reused, and dependency, policy, contract, command and environment changes
  invalidate affected identities.
- Existing package, evidence, replay, CLI output and gate exit semantics remain
  unchanged. No new dependencies or remote effects.
