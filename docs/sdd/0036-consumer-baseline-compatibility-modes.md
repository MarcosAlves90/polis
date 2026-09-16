# SDD-0036 — Consumer Baseline Compatibility Modes

## Status

Implemented in POLIS V6.

## Objective

Allow `polis preflight` and `polis apply` to consume a valid `.polis` artifact
when the consumer `HEAD` differs from the artifact's locked base, without turning
the existing baseline gate into an unconditional bypass.

## Decision

Add an explicit consumer-only `--baseline-mode strict|compatible|permissive`
option to `preflight` and `apply`. The default is `strict`, preserving the
existing behavior and automation contract.

The three modes are:

- `strict`: require consumer `HEAD` to equal `manifest.base_commit` exactly and
  revalidate the locked development baseline exactly as before.
- `compatible`: allow a different clean consumer `HEAD` only when the artifact
  base is an ancestor of that `HEAD`. The exact payload must apply cleanly to the
  consumer tree, and the resulting target must pass complete isolated consumer
  validation.
- `permissive`: allow a clean consumer `HEAD` that is not proven to descend from
  the artifact base. The locked artifact base must still be resolvable locally
  so required baseline development proof can be repeated. The exact payload must
  apply cleanly, and the resulting target must pass the same isolated validation
  as `compatible`.

`compatible` and `permissive` do not use the artifact's static `target_tree` as
the consumer result when `HEAD` differs. POLIS instead computes the exact tree
produced by applying the artifact payload to the observed consumer `HEAD` in an
isolated temporary index. That computed tree becomes the required target for the
consumer validation and the post-apply check.

## Compatibility decision

Baseline admission is separated from mutation. Before isolated validation, POLIS
records an assessment containing the artifact base, observed consumer `HEAD`,
mode, ancestry result, and any risk notice.

All modes require:

- matching Git object format;
- clean consumer index and worktree;
- a structurally valid and already verified package;
- locked baseline facts that remain provable from the artifact/repository;
- an exact payload preflight against the observed consumer tree.

Additional mode rules:

- `strict` requires exact commit identity.
- `compatible` requires `manifest.base_commit` to be an ancestor of consumer
  `HEAD`.
- `permissive` does not require ancestry, but reports that ancestry was not
  established as an explicit risk when applicable.

A missing artifact base commit is a hard failure even in `permissive`, because
POLIS cannot repeat required Red/Green or Green/Green baseline proof without the
locked baseline object.

## Validation model

For a non-exact consumer baseline:

1. repeat baseline development proof on the artifact's locked base commit;
2. create the target validation worktree from the observed consumer `HEAD`;
3. run `git apply --check` and apply the exact payload there;
4. require captured strict regression paths to retain their required final blob
   identity;
5. require the isolated index tree to equal the dynamically computed consumer
   target tree;
6. validate payload changed paths against the Change Contract scope relative to
   the observed consumer `HEAD`;
7. run target behavior, affected checks, and the packaged Project Policy.

Immediately before real mutation, `apply` requires that the consumer `HEAD` and
clean status still equal the assessed state. It does not accept a newly changed
but independently compatible `HEAD` without repeating isolation.

## Safety invariants

- The default remains strict and existing callers receive unchanged behavior.
- A mode never permits a dirty consumer tree or index.
- `compatible` never accepts a non-descendant consumer history.
- `permissive` never skips package verification, baseline development proof,
  patch conflict detection, Change Contract scope, project validation, or
  transactional post-apply tree verification.
- Patch failures and objective incompatibilities remain hard failures.
- Risk notices are explicit in command output for non-strict admission.
- No compatibility result is cached across `preflight` and `apply`.
- Consumer `HEAD` and real index remain unchanged by a successful apply.

## Compatibility

The `.polis` package format, manifest schema, Change Contract schema, and
historical artifact readers do not change. The new behavior is selected only at
consumer execution time. Existing `Apply` and `Preflight` library entry points
remain strict wrappers; option-aware entry points expose the new modes.

## Alternatives considered

### Disable the base-commit check

Rejected. This removes the only admission boundary without replacing the guarantees it provided. A patch that happens to parse could be applied to a semantically unrelated tree without ancestry, proof, scope, or target-state evidence.

### Require full affected-file hash equality

Rejected as the canonical `compatible` rule. It is safe but unnecessarily strict: a descendant may contain non-overlapping edits in a payload file while the exact patch context, final regression-path identity, target validation, and policy checks still prove compatibility. The implementation therefore uses the patch's real context plus complete isolated validation rather than whole-file equality alone.

### Always use Git three-way merge

Rejected for this change. Three-way fallback can synthesize merged content outside the exact payload/context contract and materially expands merge semantics, conflict handling, and audit surface. The selected design remains deterministic: the exact payload must apply as-is.

### Keep only strict and force modes

Rejected. A binary exact-or-force choice cannot distinguish a provably safe descendant from an unproven divergent history. The three modes make that distinction explicit without weakening the default.

## Chosen approach

The selected design changes only consumer baseline admission and target-base selection. It preserves the package format and existing strict entry points, reuses the current isolated validation pipeline, and computes one exact consumer target tree from the observed `HEAD` plus the exact payload. This is the smallest change that accepts safe divergence while retaining deterministic failure on real incompatibility.

## Validation

Tests cover:

- exact baseline under strict mode;
- strict rejection of a descendant commit;
- compatible acceptance of a descendant with unrelated changes;
- compatible acceptance when the same file has a non-conflicting contextual
  change and the patch still applies;
- compatible rejection when payload context conflicts;
- compatible rejection of non-descendant history;
- permissive controlled acceptance of a non-descendant but patch-compatible
  history;
- permissive rejection of an actual patch conflict;
- read-only compatible preflight;
- invalid mode rejection and CLI reporting;
- revalidation of the assessed consumer `HEAD` before real mutation.
