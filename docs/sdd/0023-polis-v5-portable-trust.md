# SDD-0023 — POLIS V5 Portable Trust Boundary

## Status
Accepted for implementation.

## Objective
Evolve POLIS V4 without relaxing exact-baseline or transactional-apply guarantees. V5 strengthens the consumer trust boundary with bounded artifact parsing, explicit change scope, bounded/digested command evidence, explicit environment policy, preflight inspection, machine-readable CLI output, detached Ed25519 signatures, and coverage-format adapters.

## Compatibility and migration
- Go module identity becomes `github.com/MarcosAlves90/polis/v5`.
- New packages emit POLIS format version 3.
- New `polis init` policies emit policy schema version 3.
- New Change Contracts use schema version 2.
- V5 continues to accept policy schema v2 and Change Contract schema v1 for migration, but new schema versions carry the stronger contracts below.
- The seven canonical members inside `.polis` remain unchanged.

## Required behavior
1. Package verification rejects archives or members that exceed normative byte limits before unbounded reads.
2. Change Contract v2 contains non-empty normalized `scope.allowed_paths`; build and consumer validation reject changes outside scope.
3. Policy v3 and Change Contract v2 executable commands must declare an environment contract. `clean` mode passes only named environment variables; values are never serialized into the contract/evidence.
4. V5-generated command evidence stores output byte counts and SHA-256 digests instead of complete stdout/stderr. Runtime capture is bounded. Defect baseline output oracles are recorded as semantic `oracle_checked` events.
5. `polis preflight` performs consumer validation and real `git apply --check` without applying the payload to consumer files.
6. `polis inspect` returns validated package identity, schemas, change kind/scope, gates and evidence count.
7. `doctor`, `verify`, `inspect`, and `preflight` support stable JSON output. CLI exit categories are stable for usage, invalid artifact, blocked environment, baseline mismatch, validation failure and apply failure.
8. Detached signatures use Ed25519 over SHA-256 of the exact `.polis` bytes. Trust roots are supplied by the consumer and never by the package. `sign`, `verify`, `preflight`, and `apply` support the detached signature path.
9. Coverage parsing supports `go-coverprofile-v1`, `lcov-v1`, and `cobertura-v1`. Adapter identity is format-based rather than language-based.
10. Existing exact-baseline, exact-target-tree, clean consumer, index preservation, HEAD preservation, Red→Green, checksums, and fail-closed behavior remain protected.

## Resource limits
- Archive bytes: 64 MiB maximum.
- Total uncompressed canonical members: 64 MiB maximum.
- Manifest, policy, Change Contract: 1 MiB each.
- Checksums: 64 KiB.
- Evidence: 16 MiB total, 1 MiB per NDJSON event line.
- Payload and regression patches: 32 MiB each.
- Runtime stdout/stderr capture: 1 MiB retained per stream; full-stream SHA-256 and byte count are still computed. If an oracle cannot be proven from bounded capture, the operation fails closed.

## Change scope semantics
- `.` allows the entire repository.
- Otherwise an allowed path is either an exact repository-relative path or a normalized directory prefix ending in `/`.
- Rename source and destination must both be within scope.
- Scope is enforced against the Git base-to-target changed path set, not only against patch text.

## Non-goals
- Applying to dirty worktrees.
- Automatic conflict resolution.
- Reusing cached PASS evidence.
- Parallel/DAG gates.
- Arbitrary executable plugins.
- Cloud registry/service.
- Claiming hermetic/reproducible builds from environment filtering alone.
- Automatically inventing multi-language init commands without repository evidence.

## Acceptance criteria
- Targeted tests cover each new contract and fail before its implementation.
- Complete repository tests, vet, build, mod verification, and project-wide coverage are attempted under repository policy.
- Coverage remains strictly greater than 80% for PASS.
- A POLIS V4 binary builds and verifies one `.polis` migration artifact whose baseline is the uploaded V4 ZIP HEAD.
