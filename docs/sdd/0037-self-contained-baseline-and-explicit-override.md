# SDD-0037 — Self-Contained Baseline Proof and Explicit Missing-Proof Override

## Status

Accepted for Issue #1 implementation in POLIS V6.

## Objective

Make new `.polis` artifacts self-contained for locked development-proof replay when a consumer does not possess the producer baseline commit, while preserving strict, compatible, and permissive admission semantics. Add one explicit user authorization that may waive only missing baseline-development proof when that proof genuinely cannot be established.

## Package format decision

POLIS package format v4 adds exactly one canonical member:

```text
polis/polis-baseline.tar
```

Formats v2 and v3 retain their exact seven-member inventories. Format v4 has the same seven members plus `polis/polis-baseline.tar`.

`polis-baseline.tar` is a deterministic, uncompressed tar stream containing native Git object payloads for exactly:

- the locked `base_commit` object;
- the locked root tree and every recursively reachable tree object;
- every recursively reachable blob object.

Parent commits are intentionally not included. Gitlink/submodule commit objects are not required because the superproject tree records their object IDs without owning their contents.
Refs and parent history are therefore not transported by this representation. Producer `build` MUST materialize the generated snapshot and replay the locked development proof against that exact embedded state before accepting the artifact. If a locked baseline command requires unavailable refs or ancestor history, the build MUST fail closed rather than publish a package that will only fail when consumed, synthesize history, or weaken that proof.

Tar entries are named `objects/<type>/<object-id>` where `<type>` is `commit`, `tree`, or `blob`. Entry order is lexical, metadata is canonicalized, duplicate entries are invalid, and all object IDs are recomputed from `"<type> <size>\\0" + payload` using the manifest Git object format.

This representation was selected over Git pack transport because it preserves native commit/tree/blob identity while keeping resource use directly bounded by raw authenticated bytes. It avoids a second compressed-object parser and pack/delta expansion amplification. It was selected over a filesystem-only snapshot because exact Git object identity, modes, symlinks, gitlinks, binary blobs, and the locked commit/tree relationship remain native rather than reinterpreted.

### Design-gate evidence

The representation decision was checked against a Git-native pack prototype rather than made from format preference alone. On Linux amd64 with Git 2.47.3, repeated `git pack-objects` runs over the same explicitly enumerated object set produced byte-identical packs for both SHA-1 and SHA-256 fixtures containing a symlink, executable file, binary blob, tree, and commit. The SHA-1 fixture packed six objects into 383 bytes and the SHA-256 fixture into 458 bytes. On the repository baseline sampled during implementation, 194 raw objects contained 3,216,899 payload bytes; the pack was 2,489,738 bytes, while the equivalent canonical raw-object tar was approximately 3,370,496 bytes.

That prototype demonstrates a real artifact-size advantage for pack transport and did not expose a determinism failure in the tested Git version. It does not remove the additional compressed/delta-object expansion surface: a safe pack implementation would need an independently enforced expanded-object budget and Git-pack-specific validation in addition to the ZIP/package limits. The raw-object tar therefore wins this design gate on simpler direct resource accounting and smaller semantic surface, accepting larger artifacts as the explicit trade-off. The decision can be revisited if the 32 MiB baseline cap proves operationally restrictive.

## Versioning and integrity

Package format v4 is a new contract. Format-v3 semantics are not redefined.

The v4 manifest adds mandatory `baseline_sha256`, the SHA-256 of `polis/polis-baseline.tar`. Formats v2/v3 MUST NOT contain this field or the baseline member.

The baseline member is also included in `polis/polis-checksums.sha256`. Candidate packages are verified through the normal verifier before publication.

The v4 baseline member is capped at 32 MiB, while the existing 64 MiB archive and aggregate-uncompressed caps remain unchanged. Producer snapshot construction first accounts for Git object sizes and canonical TAR framing, then reads object payloads only after the projected member fits the cap. A producer whose complete locked baseline exceeds the cap fails closed; POLIS does not silently increase limits, omit part of the baseline, or allocate the complete oversized member before rejecting it.

## Baseline proof validation

Verification of a v4 package MUST establish all of the following before the package is accepted:

- the baseline member digest matches `manifest.baseline_sha256` and the checksum inventory;
- every tar entry is canonical, regular, unique, bounded, and has a valid object type and object ID;
- object IDs recomputed from raw object bytes match entry names under the declared SHA-1 or SHA-256 Git object format;
- the packaged `base_commit` object exists;
- its root tree equals `baseline_lock.base_tree`;
- the complete reachable tree/blob closure is present;
- no unrelated extra objects are present;
- the manifest and `baseline_lock` object format/base commit remain identical.

Malformed or inconsistent embedded baseline material is an invalid artifact and is never eligible for override.

## Isolated baseline materialization

Consumer execution materializes verified baseline objects only into a temporary Git repository outside the consumer repository. It creates no consumer refs and writes no producer-only objects into the consumer object database.

The isolation contract separates proof state from target state:

```text
BaselineRepo / BaselineCommit -> development proof replay
TargetRepo / TargetBaseCommit -> consumer target validation
```

Producer build may use the same repository for both sides. Consumer execution may use an artifact-materialized baseline repository and the actual consumer repository.

Temporary baseline state is deleted after success or failure.

## Baseline source resolution

Result reporting uses:

- `baseline_source: local` — locked baseline proof was established from the consumer/local repository;
- `baseline_source: embedded` — locked baseline proof was established from v4 artifact-contained objects;
- `baseline_source: overridden` — baseline development proof was explicitly waived.

Resolution rules:

### strict

Consumer `HEAD` MUST equal artifact `base_commit`. Existing exact-baseline semantics remain unchanged. Embedded proof does not relax admission.

### compatible

Artifact `base_commit` MUST be provably an ancestor of consumer `HEAD` in the consumer repository. Embedded proof does not replace the ancestry requirement.

### permissive

If consumer `HEAD` differs:

1. use a locally resolvable locked baseline when available;
2. otherwise use a valid embedded baseline when present;
3. otherwise fail unless the caller explicitly authorizes the missing-baseline-proof override.

Lack of ancestry is allowed only under existing permissive semantics and remains visibly reported. A valid embedded baseline carries proof; it is not an override and does not reduce guarantees.

## Explicit override

The initial override capability is exactly:

```text
--allow-missing-baseline-proof
```

It is valid only with `--baseline-mode permissive` and only when locked baseline development proof cannot be established from local or embedded material.

The flag MUST be supplied independently to `preflight` and `apply`. Preflight authorization is not cached or inherited.

When active, POLIS skips only the baseline-development replay that requires the missing producer baseline. Depending on the Change Contract, bypassed guarantees are derived from the actual proof path and may include:

- locked baseline behavior replay;
- Red proof reconstruction;
- Red regression path reconstruction;
- strict Red blob identity reconstruction.

Still enforced:

- package structure and authenticated integrity;
- consumer clean worktree/index;
- matching Git object format;
- exact payload `git apply --check`;
- no hidden merge or payload rewriting;
- Change Contract scope validation;
- complete target behavior and Project Policy validation;
- deterministic consumer target tree;
- transactional post-apply verification and rollback behavior.

The override never converts package corruption, malformed embedded baseline, dirty state, object-format mismatch, payload conflict, scope failure, or target-validation failure into success.

## Reporting

Successful consumer results expose:

```text
baseline_mode: strict | compatible | permissive
baseline_source: local | embedded | overridden
consumer_base_commit: <observed HEAD>
baseline_ancestry: exact | proven_descendant | unproven
override_active: true | false
bypassed_guarantees: []
warnings: []
target_tree: <dynamically validated target>
```

Text output MUST prominently identify an active override and enumerate bypassed guarantees. JSON output MUST expose the same state as stable fields.

Successful embedded proof MUST NOT be labeled as reduced safety.

## Backward compatibility

Valid v2/v3 artifacts remain readable. They use the historical local-object-database baseline resolution path. If the locked baseline is absent, normal strict/compatible/permissive behavior remains fail-closed; only the explicit missing-proof override may authorize permissive progress.

Historical package readers are not removed merely because v4 exists.

## Safety and zero residue

Preflight MUST leave unchanged:

- consumer HEAD;
- real index;
- refs;
- Git configuration;
- consumer persistent object database;
- linked-worktree administration;
- working tree.

Apply may change only the exact intended payload through the existing validated transactional path. Failure leaves no baseline refs, baseline objects, temporary indexes, temporary repositories, or POLIS Git metadata in the consumer.

## Validation matrix

- exact consumer HEAD: strict/compatible/permissive PASS; override not needed;
- descendant consumer HEAD: strict FAIL; compatible/permissive PASS when all validation passes;
- independent history with local baseline available: permissive PASS without override;
- independent history with local baseline absent and valid v4 embedded baseline: permissive PASS with `baseline_source=embedded`;
- independent history with local baseline absent and v2/v3 artifact: permissive FAIL without override, MAY PASS with explicit override and remaining checks;
- malformed embedded baseline: FAIL in every mode including override;
- payload conflict: FAIL in every mode including override;
- dirty consumer: FAIL in every mode including override.

## Non-goals

- generic `--force` semantics;
- automatic three-way merge;
- accepting conflicting payloads;
- rewriting consumer history;
- consumer-ref baseline transport;
- network baseline retrieval;
- unrelated cleanup or abstraction work.
