# SDD-0029 — Locked Development Baseline

## Status

Accepted and implemented as Change Contract schema v4 with `strict_sdd_tdd_v2`.

## Problem

Schema v3 can prove Red-to-Green or Green-to-Green and requirement traceability, but the strict contract may still be authored after production work has begun. Without a baseline lock, the artifact proves an internally consistent proof but not that the Specification and Project Policy were frozen against the delivery baseline.

## Objective

Add an explicit `polis start` state transition that locks the strict Specification and committed Project Policy to an exact clean Git baseline before implementation. Build, capture, verification, preflight, and apply must fail closed when facts available at their trust boundary disagree with that lock.

## Change Contract schema v4

Schema v4 uses `development_method: strict_sdd_tdd_v2` and requires `baseline_lock`:

- `git_object_format` (`sha1` or `sha256`);
- `base_commit` exact object ID;
- `base_tree` exact `HEAD^{tree}` object ID;
- `policy_sha256` of exact committed `.polis/policy.json` bytes;
- `specification_sha256` of deterministic JSON serialization of the machine Specification.

All strict v3 SDD/TDD, Green-to-Green, traceability, test-scope, and captured-test immutability rules remain mandatory in v4.

## `polis start`

`polis start --repo <repo> --contract <draft-v3.json> --out <locked-v4.json>` MUST:

1. require a clean Git worktree and index, including no untracked files;
2. require valid Project Policy schema v3 at `.polis/policy.json` whose working bytes equal committed bytes;
3. require a valid strict schema-v3 draft using `strict_sdd_tdd_v1`;
4. require the input and output contract files outside the repository;
5. resolve Git object format, `HEAD`, and `HEAD^{tree}`;
6. calculate policy and Specification SHA-256 values;
7. change only schema version, development method, and baseline lock;
8. self-validate the resulting schema-v4 contract;
9. create the output exclusively and never overwrite it;
10. never stage, commit, reset, checkout, or mutate repository files.

## Producer enforcement

`capture-red` and `build` MUST revalidate the locked baseline before accepting proof or constructing a target. A changed HEAD, base tree, object format, committed/working policy, or Specification digest invalidates the lock.

Strict captured Red test blob immutability applies to all strict schemas, including v4.

## Offline verification

Without repository access, `polis verify` MUST cross-check all lock facts available from artifact bytes:

- lock object format equals manifest object format;
- lock base commit equals manifest base commit;
- packaged policy SHA-256 equals lock policy SHA-256;
- packaged Specification SHA-256 equals lock Specification SHA-256.

`base_tree` requires Git repository access and is not guessed by the offline verifier.

## Consumer enforcement

`preflight` and `apply` MUST validate the complete baseline lock against the consumer repository before isolated target validation and validate it again before any real apply transition. Lock drift is categorized as baseline mismatch.

## Compatibility

- v1/v2 semantics remain unchanged;
- v3 `strict_sdd_tdd_v1` remains readable;
- v4 is opt-in during the V5 migration period;
- package format remains v3 and Evidence remains v2.

## Security boundary

A local baseline lock proves consistency with an exact Git state inside the POLIS workflow. It is not a trusted timestamp and cannot prove wall-clock authorship order against an operator who controls and can reconstruct all local state. Trusted remote witnesses or hash-chained Development Ledger semantics are separate future work.

## Non-goals

- trusted timestamps or remote CI witnesses;
- Development Ledger or multi-slice hash chains;
- prompt/chat storage;
- new package members;
- automatic inference of specification, tests, commands, or scope.
