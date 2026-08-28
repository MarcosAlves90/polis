# Validation evidence — POLIS V4.0.0

Validation date: 2026-08-28 UTC

## Scope

This checkpoint finalizes POLIS V4.0.0 from the published baseline commit `78cd2278d7eea7d9e3059b233dc4671d0b0f5de0` (tree `90a59de901f04c5fd783d37229748a6c03265049`). SDD-0009 replaces lexical repository-boundary decisions with a shared canonical physical-path guard. SDD-0011 corrects a non-portable test oracle discovered by native macOS consumer execution of POLIS V3.1 revision V005.

Package format remains v2. Project Policy remains schema v2. Change Contract remains schema v1.

## Producer environment actually executed

- OS/architecture: Linux x86_64
- Go: go1.23.2 linux/amd64
- Git: 2.47.3

## Observed TDD evidence

### Physical-path boundary

Before `internal/pathguard` existed, `go test ./internal/pathguard` failed to compile because `Contains` and canonical path resolution were undefined. After implementation and integration into packagebuild, redcapture, and policy coverage-report containment, the targeted suite returned Green.

Regression coverage includes physical aliases produced through symlinks, missing output paths below aliased roots, external siblings, root equality, and fail-closed unresolvable symlink paths. This models the macOS class where `/var/...` and `/private/var/...` can identify the same physical location.

### Portable absolute-path error validation

POLIS V3.1 revision V005 was executed by the consumer on macOS. Bundle inventory/checksums, consumer baseline validation, isolated worktree creation, and exact patch identity passed. Isolated validation then failed at `TestCanonicalPropagatesAbsolutePathResolutionFailure`: the test expected `filepath.Abs` to fail after its current working directory was removed. macOS did not exhibit that Linux-observed `Getwd` behavior.

The test was replaced by deterministic dependency injection: production `canonical` still supplies `filepath.Abs`, while the test injects a resolver returning a sentinel error and asserts exact propagation. The test no longer mutates the process current working directory and has no platform-specific skip.

## Final producer gates

- `gofmt -l .` — PASS, no files reported.
- `go vet ./...` — PASS.
- every Go package test — PASS. Long packages were also executed separately because the execution wrapper can terminate a monolithic command before all package results are emitted.
- race detector — PASS for every package. `internal/packageapply` was split into exhaustive named-test groups so each bounded process completes within the execution wrapper limit.
- `go mod verify` — PASS.
- JSON syntax for Guide and all schemas — PASS using Go `encoding/json`.
- native Linux amd64 build with `-trimpath` — PASS.
- native `polis doctor` — PASS and reports `POLIS doctor 4.0.0`.

## Normative project-wide line coverage

Coverage was measured conservatively by running every production package under Go coverage instrumentation and unioning the resulting profiles. `internal/packageapply` was divided into three exhaustive named-test groups because its single instrumented process can exceed the runner time budget. The same line-union algorithm used by the POLIS `go-coverprofile-v1` contract deduplicated executable source lines across the profiles.

Observed result after SDD-0011:

- covered lines: 1853
- executable lines: 2314
- line coverage: 80.077787381158%
- policy threshold: strictly greater than 80.0%
- result: PASS

This measurement is conservative relative to `-coverpkg=./...` because it does not credit cross-package execution to another package's production lines. No exclusions or threshold changes were introduced to obtain PASS.

## Security properties affected

- Change Contracts physically inside the target worktree are rejected even when referenced through an alias/symlink path.
- `capture-red` output paths physically inside the target worktree are rejected before creation, including currently non-existent descendants.
- coverage reports that resolve outside the repository remain rejected.
- containment uses one shared implementation instead of separate lexical checks.
- unresolved path conditions fail closed at the caller boundary.
- the SDD-0011 correction changes only testability of `filepath.Abs` error propagation; containment semantics are unchanged.

## Platform evidence boundary

Linux amd64 is the producer runtime environment fully executed. Native macOS consumer execution of V005 proved bundle/checksum verification, Git baseline/worktree setup, exact patch identity, and Go test execution on that platform, but validation stopped at the non-portable test oracle before PASS. Therefore macOS is not yet claimed fully validated. Revision V006 must rerun the complete consumer launcher on macOS; real worktree mutation still occurs only after every isolated gate passes.

## Cross-toolchain coverage hardening after V013 consumer evidence

A native macOS consumer execution of the V013 POLIS V3.1 delivery (Go 1.27) passed bundle integrity, baseline, isolated patch identity, changed-behavior tests, and affected tests, then correctly stopped before real application because project-wide line coverage was `1723 / 2177 = 79.145613229215%`, below the unchanged strict `>80.0` requirement.

The follow-up does not weaken the gate. Deterministic tests were added for previously uncovered production failure paths in package construction, package application, and policy validation. On the Linux/Go 1.23.2 producer, conservative merged line evidence now reaches `1900 / 2314 = 82.10890233362143%`. A V014 consumer run must still independently establish native coverage `>80.0`; this producer result is not substituted for native evidence.
