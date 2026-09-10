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

Linux amd64 is the producer runtime environment fully executed. Native macOS runtime evidence is also established on the exact host that successfully applies the POLIS V3.1 evidence-closing delivery for this checkpoint. That launcher is fail-closed: this documentation patch is applied only after native Darwin detection, exact baseline and target-tree checks, complete Go tests, race detection, authoritative project-wide coverage strictly greater than 80%, formatting/static/dependency checks, JSON validation, native build, and `polis doctor` all PASS in isolation.

For that historical V3.1 checkpoint, the complete native macOS run log was preserved under Git metadata at `.git/polis/results/` and the launcher printed the exact results path. Current V6 canonical external-policy apply semantics supersede that persistence behavior: default apply evidence is ephemeral and must not leave `.git/polis` in the target repository. Architecture and runtime/tool versions for the historical claim were recorded in that run log. The macOS claim remains evidence-scoped to the native host and architecture that produced the successful run; it does not imply runtime validation of every macOS architecture.

Windows native runtime validation remains outstanding. Cross-compilation alone is not treated as runtime evidence.

## Cross-toolchain coverage hardening after V013 consumer evidence

A native macOS consumer execution of the V013 POLIS V3.1 delivery (Go 1.27) passed bundle integrity, baseline, isolated patch identity, changed-behavior tests, and affected tests, then correctly stopped before real application because project-wide line coverage was `1723 / 2177 = 79.145613229215%`, below the unchanged strict `>80.0` requirement.

The follow-up did not weaken the gate. Deterministic tests were added for previously uncovered production failure paths in package construction, package application, and policy validation. The evidence-closing delivery reruns the canonical project-wide coverage producer natively on macOS and refuses to apply this documentation update unless the computed line coverage is strictly greater than 80.0%.


## POLIS V6 zero-residue target validation — 2026-09-08

This section records the validation contract for the V6 external-policy zero-residue change. The implementation adds explicit external Project Policy input for `start`/`build`, separates repository baseline validation from policy-byte validation, consumes the packaged policy at consumer boundaries, makes default apply evidence ephemeral outside the target, and isolates temporary Git object/index/worktree activity from the target repository. Package format v3, Project Policy schema v3, Change Contract schema v4, and Evidence v2 remain the active versions; their V6 semantics are extended by the configurable validation-reinforcement evidence described below.

The end-to-end regression test `TestZeroResidueExternalPolicyWorkflow` exercises:

```text
start --policy -> capture-red -> build --policy -> verify -> inspect -> preflight -> apply
```

Its target fixture never contains `.polis`. The test compares HEAD, real index tree, refs, `.git/config`, linked-worktree administration, persistent `.git/objects` files, final status, and payload contents. It permits only the intended product payload and rejects tool-owned residue or payload references introduced merely by the delivery mechanism.

Observed V6 zero-residue gate results on Linux amd64 with Go 1.23.2 and Git 2.47.3:

- `go test ./cmd/polis -run TestZeroResidueExternalPolicyWorkflow -count=1 -v` — PASS.
- `go test ./...` — PASS.
- `go test -race ./...` — PASS.
- `go vet ./...` — PASS.
- `go build ./cmd/polis` — PASS; the local ignored build output was removed after validation.
- `go mod verify` — PASS.
- `gofmt -l .` — PASS, no files reported.
- `git diff --check` — PASS.
- Guide JSON parsing with Go-compatible JSON syntax — PASS.
- authoritative policy coverage command `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` — PASS.
- normative `go-coverprofile-v1` line metric: `3599 / 4354 = 82.659623334864%`, strictly greater than the unchanged `80.0%` threshold — PASS.

`go tool cover -func` reported 85.9% statements for the same profile; that value is not used as the normative POLIS line-coverage gate.


## POLIS V6 configurable validation reinforcement — 2026-09-09

This section records the validation of the explicit validation-level model. The default remains legacy-compatible `strict`; `standard` and `minimal` reduce only project-quality gates, while policy decoding, contract/evidence integrity, scope, baseline/exact-tree checks, development proof, package verification, signatures, resource limits, isolation, and transactional application remain mandatory and fail closed.

The supported project-gate inventory is `test.complete`, `coverage`, `lint`, `typecheck`, `build`, `smoke`, `compatibility`, `dependency`, `migration`, `security`, and `platform`. Each execution exposes the effective level plus the enabled and disabled gate lists through command results and a `validation_configured` evidence event. Disabled gates require an explicit non-empty reason; incompatible combinations are rejected before project commands execute.

Observed repository validation on the current macOS arm64 host with Go 1.27.1 and Git 2.55.0:

- `go test ./... -count=1` — PASS.
- `go test -race ./... -count=1` — PASS.
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` — PASS.
- normative union line coverage: `3514 / 4304 = 81.6%`, strictly greater than `80.0%` — PASS.
- `go vet ./...`, `go build ./cmd/polis`, `go mod verify`, `gofmt -l .`, and `git diff --check` — PASS.
- Go JSON Schema checks for strict legacy, standard, minimal, and invalid combinations — PASS.
- focused tests cover omitted-command behavior, disabled-command non-execution, enabled command execution, explicit evidence inventory, empty enabled inventories, invalid configurations, and strict compatibility — PASS.

The installed `/Users/marcos.lopes/go/bin/polis` v6 was also exercised in an isolated clone: `doctor`, `start`, `build`, and `verify` passed, and `apply` passed with its embedded fail-closed preflight for the generated update artifact. The clone received the intended payload; the main worktree was not used as the apply target.
