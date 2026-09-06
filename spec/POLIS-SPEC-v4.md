# POLIS Specification v4 — Locked strict development baseline

This specification extends POLIS Specification v3 with an opt-in baseline-lock transition. Unless explicitly changed here, POLIS Specification v3, POLIS Specification v2, and the unchanged V1 exact-tree and transactional rules remain normative.

This specification does not change package format v3, Project Policy schema v3, Evidence v2, detached signatures, coverage semantics, or the seven-member archive layout.

## Change Contract schema v4

Schema v4 uses `development_method: strict_sdd_tdd_v2` and retains all strict schema-v3 Specification, scope, test-scope, traceability, Red-to-Green, Green-to-Green, and captured-test immutability semantics.

A schema-v4 Change Contract MUST additionally contain `baseline_lock` with exactly:

- `git_object_format`: `sha1` or `sha256`;
- `base_commit`: exact lowercase Git object ID for the selected object format;
- `base_tree`: exact lowercase tree object ID for `base_commit^{tree}`;
- `policy_sha256`: SHA-256 of the exact committed `.polis/policy.json` bytes;
- `specification_sha256`: SHA-256 of the canonical JSON serialization produced from the machine `specification` object.

Schema v3 MUST NOT contain `baseline_lock`. Schema v4 MUST contain it.

The JSON Schema files provide structural validation. Cross-field invariants such as scope containment, requirement traceability, Git identity, and Specification-hash equality remain governed by the canonical POLIS decoder/runtime.

## `polis start`

`polis start --repo <repo> --contract <draft-v3.json> --out <locked-v4.json>` is the canonical local state transition for creating a locked contract.

Before producing output, POLIS MUST prove:

1. `repo` resolves to a Git worktree;
2. index and worktree are clean, including no untracked files;
3. `.polis/policy.json` is valid Project Policy schema v3;
4. working policy bytes exactly equal `HEAD:.polis/policy.json`;
5. the draft is a valid strict schema-v3 contract using `strict_sdd_tdd_v1`;
6. draft and output paths are outside the target repository;
7. output does not already exist.

POLIS then resolves object format, HEAD, and `HEAD^{tree}`, calculates policy and Specification digests, changes only schema version/development method/baseline lock, self-validates schema v4, and creates the external output exclusively.

`polis start` MUST NOT stage, commit, reset, checkout, stash, or modify target repository files.

## Producer lock validation

Before accepting a schema-v4 Red proof, `polis capture-red` MUST revalidate the full lock against the current source repository.

Before constructing a schema-v4 target, `polis build` MUST revalidate the full lock against the source repository.

Validation MUST fail if any of these differ from the lock:

- Git object format;
- HEAD commit;
- HEAD tree;
- committed/working Project Policy bytes;
- current machine Specification digest.

A revised Specification therefore requires a new `polis start` transition and new dependent development proof.

## Offline package verification

`polis verify` has artifact bytes but no source repository. For schema v4 it MUST therefore prove exactly the facts available offline:

- `baseline_lock.git_object_format == manifest.git_object_format`;
- `baseline_lock.base_commit == manifest.base_commit`;
- SHA-256 of packaged Project Policy equals `baseline_lock.policy_sha256`;
- SHA-256 of the packaged machine Specification equals `baseline_lock.specification_sha256`.

The verifier MUST NOT pretend to validate `base_tree` without repository access.

## Consumer validation

`polis preflight` and `polis apply` have the consumer repository and MUST revalidate the complete lock, including `base_tree`, against that repository.

The lock MUST be checked before isolated target validation and checked again before the final consumer transition. Any lock divergence is a baseline mismatch.

A previous preflight PASS is not cached or reused by apply.

## Strict test immutability

Captured Red-test staged blob identity applies to every strict development schema, including schema v4. Runtime decisions MUST use strict-development semantics rather than literal schema-number checks.

A target that changes, weakens, deletes, or otherwise replaces a captured strict Red path MUST fail even if its regression command returns zero.

## Compatibility

During this migration period, Project Policy schema v3 MAY build with:

- Change Contract v2 under existing V5 semantics;
- strict Change Contract v3 under `strict_sdd_tdd_v1`;
- locked strict Change Contract v4 under `strict_sdd_tdd_v2`.

Change Contract v1 remains readable according to existing migration rules. Making locked v4 mandatory requires a later explicit breaking-policy decision.

## Security and proof boundary

The local lock proves consistency among an exact Git baseline, policy, Specification, development proof, and artifact within the POLIS workflow. It is not a trusted timestamp or independent witness of wall-clock authorship order. An operator with control of all local state can reconstruct historical bytes.

Trusted remote witnesses, signed checkpoints, or a hash-chained Development Ledger require a separate specification and are not implied by schema v4.

## Fail-closed conditions

Schema-v4 processing MUST fail on at least:

- absent or malformed baseline lock;
- unsupported object format or malformed object IDs/digests;
- Specification digest mismatch;
- policy digest mismatch;
- source or consumer HEAD/tree mismatch;
- `capture-red` attempted after baseline drift;
- build attempted after baseline drift;
- captured strict Red path changed in the target;
- offline package facts inconsistent with the lock;
- output overwrite attempt in `polis start`;
- dirty source baseline at `polis start`.
