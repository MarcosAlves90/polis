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

## POLIS V6 consumer-deferred project gates — Issue #4 — 2026-09-23

New producer artifacts use package format v5 and Evidence v3 to record enabled
project gates whose execution is deferred to the consumer. Project Policy schema
v3 is unchanged. Formats v2, v3, and v4 continue to select Evidence v2; formats
v4 and v5 retain the same eight-member package inventory and embedded-baseline
contract.

The implementation adds repeatable `--defer-gate` flags to `plan` and `build`,
rejects unknown, duplicate, not-applicable, and dependency-invalid requests
before project commands run, and emits an authenticated `DEFERRED` trace without
executing producer commands or touching coverage reports. Package verification
checks the inventory against the evidence trace. Consumer preflight and apply
execute every enabled packaged-policy gate in isolation before reporting success
or mutating the real target.

Observed validation on macOS 25.6.0 arm64 with Go 1.27.1 and Git 2.55.0:

- `go test ./... -count=1` — PASS, 463 tests across 22 packages.
- `go test -race ./... -count=1` — PASS, 463 tests across 22 packages.
- `go test -coverpkg=./... ./... -count=1 -coverprofile=/tmp/polis-issue4-coverage.out` — PASS.
- CI line-union metric: `4948 / 6071 = 81.502223686378%`, strictly greater than `80.0%` — PASS.
- `go vet ./...` — PASS.
- `go mod verify` — PASS (`all modules verified`).
- `go build -trimpath -o /tmp/polis-issue4 ./cmd/polis` — PASS.
- `/tmp/polis-issue4 doctor --format=json` — PASS; reports POLIS 6.7.0, Darwin arm64, Go 1.27.1, and Git 2.55.0.
- Offline export and archive inspection — PASS; the bundle contains format-v5 manifest schema, Evidence v3 schema with `DEFERRED` and `deferred_gates`, and the current V6 specification.
- `gofmt -l .`, `git diff --check`, and `go mod verify` — PASS.
- `npm run verify` — unavailable: the repository has no `package.json`.

No Linux or Windows run was performed in this local validation. No remote CI,
publication, commit, or issue mutation was triggered.
- `go vet ./...`, `go build ./cmd/polis`, `go mod verify`, `gofmt -l .`, and `git diff --check` — PASS.
- Go JSON Schema checks for strict legacy, standard, minimal, and invalid combinations — PASS.
- focused tests cover omitted-command behavior, disabled-command non-execution, enabled command execution, explicit evidence inventory, empty enabled inventories, invalid configurations, and strict compatibility — PASS.

The installed `/Users/marcos.lopes/go/bin/polis` v6 was also exercised in an isolated clone: `doctor`, `start`, `build`, and `verify` passed, and `apply` passed with its embedded fail-closed preflight for the generated update artifact. The clone received the intended payload; the main worktree was not used as the apply target.

## POLIS V6 consumer baseline compatibility modes — 2026-09-16

This section records validation for SDD-0036. The change keeps exact consumer
baseline identity as the default `strict` behavior and adds explicit
`compatible` and `permissive` admission modes to `preflight` and `apply`.
Package format v3, Project Policy schema v3, Change Contract schema v4, and
Evidence v2 remain unchanged.

The compatibility mechanism does not force-apply a payload. For a divergent
consumer `HEAD`, POLIS records the observed clean commit, checks the exact patch
against that tree, computes the deterministic consumer target tree in a
temporary index, repeats development proof on the artifact's locked base, then
runs target behavior, scope, Project Policy, and exact target-tree validation on
an isolated worktree based on the observed consumer commit. `apply` requires the
same `HEAD` and clean state immediately before real mutation.

Focused regression coverage establishes:

- strict mode still rejects a different consumer `HEAD`;
- compatible mode accepts a descendant with unrelated changes;
- compatible mode preserves a non-overlapping committed change in a payload
  file when the exact patch context still applies;
- compatible mode rejects conflicting payload context and non-descendant
  history;
- permissive mode can accept non-descendant but patch-compatible history only
  while the locked artifact base remains available for development proof;
- permissive mode still rejects actual patch conflicts;
- compatible preflight remains read-only;
- a consumer `HEAD` change after assessment invalidates the result before real
  mutation;
- invalid CLI modes are rejected and successful non-strict execution reports
  the admitted base, compatibility reason, and risk warnings.

Observed validation on Linux x86_64, Go 1.23.2, Git 2.47.3:

- `go test ./... -count=1` — PASS.
- `go test -coverpkg=./... ./... -coverprofile=/tmp/polis-coverage.out` — PASS.
- `go tool cover -func=/tmp/polis-coverage.out | tail -1` — 86.1% statements.
- `go vet ./...` — PASS.
- `go build ./...` — PASS.
- `go mod verify` — PASS (`all modules verified`).
- `git diff --check` — PASS.

The repository policy still requires coverage strictly greater than 80.0%; the
observed instrumented suite remains above that threshold. No policy threshold,
package schema, signature rule, dirty-worktree protection, scope check, or
development-proof requirement was weakened for this feature.

## POLIS V6 self-contained baseline proof and explicit missing-proof override — 2026-09-17

This section records validation for SDD-0037 and Issue #1. New producer builds use
package format v4 with an authenticated `polis/polis-baseline.tar`; historical
v2/v3 seven-member artifacts remain readable without redefining their package
semantics. The embedded member carries the locked producer commit plus its complete
tree/blob closure as canonical raw Git objects. Consumer target validation remains
separate from producer baseline-proof storage.

The representation design gate compared the selected raw-object TAR with an actual
Git-pack prototype on Linux amd64 / Git 2.47.3. Repeated `git pack-objects` runs were
byte-identical for SHA-1 and SHA-256 fixtures containing symlink, executable and
binary content. On the repository baseline sampled during implementation, 194
objects contained 3,216,899 raw payload bytes; the pack occupied 2,489,738 bytes
while the equivalent canonical TAR size was approximately 3,370,496 bytes. Pack
transport therefore demonstrated a real size advantage, but it would add compressed
and delta-object expansion as a separately bounded parser/resource surface. The
raw-object representation was selected to keep authenticated resource accounting
direct and semantics smaller; the 32 MiB embedded-baseline cap remains fail-closed.

Focused regression coverage establishes:

- a consumer in a genuinely independent Git object database can use permissive
  preflight/apply without possessing the producer baseline commit when v4 embedded
  proof is valid;
- embedded proof leaves consumer HEAD, real index, refs, Git configuration,
  persistent object database, linked-worktree administration and working tree
  unchanged during preflight;
- SHA-1 and SHA-256 embedded baselines preserve native tree identity, symlinks,
  executable bits and binary blobs;
- missing tree/blob closure, unrelated objects, truncation, object tampering, wrong
  commit/tree identity and malformed embedded material fail closed; native Git object
  validation rejects a hash-consistent but structurally invalid commit before `verify` PASS;
- producer build replays locked development proof from the materialized embedded
  snapshot itself; a baseline command that depends on omitted parent history (for
  example `HEAD^`) is rejected during build rather than creating a non-portable artifact;
- baseline TAR size is projected from `git cat-file -s` metadata before full object
  payloads are loaded, and exact encoded-size accounting is checked against that projection;
- the canonical manifest JSON Schema declares format v4 and mandatory
  `baseline_sha256`, matching the manifest emitted by current builds;
- v3 artifacts with a genuinely missing producer baseline still fail normally and
  can proceed only when `--allow-missing-baseline-proof` is explicitly supplied;
- apply does not inherit preflight override authorization;
- a requested override is not activated when local proof is available;
- malformed v4 embedded proof remains an invalid package even when the override
  flag is supplied;
- dirty consumers and payload conflicts remain fatal under override;
- strict and compatible modes reject the missing-proof override flag;
- strict, compatible and permissive successful results expose stable baseline
  source/ancestry/override reporting.

Observed validation on Linux amd64, Go 1.23.2, Git 2.47.3:

- `gofmt` check over tracked and newly added Go files — PASS, no files reported.
- `git diff --check` — PASS.
- `go test ./... -count=1` — PASS.
- `go vet ./...` — PASS.
- `go mod verify` — PASS (`all modules verified`).
- `go build -trimpath ./cmd/polis` — PASS.
- `./polis doctor` — PASS; reports POLIS 6.6.0, Linux amd64, Go 1.23.2 and Git 2.47.3.
- authoritative coverage command `go test -coverpkg=./... ./... -count=1 -coverprofile=/tmp/polis-issue1-coverage.out` — PASS.
- normative CI line-union metric: `5109 / 6216 = 82.191119691120%`, strictly greater than `80.0%` — PASS.
- race detector: `go test -race ./internal/packageapply -count=1` plus `go test -race` over every remaining `go list ./...` package — PASS. The single aggregated `go test -race ./...` invocation was interrupted by the external execution-time limit before completion and is therefore not itself claimed as a PASS; the two exhaustive package partitions completed successfully.

No macOS or Windows execution was performed for this uncommitted Issue #1 work in
this Linux workspace. The repository CI still defines those platform jobs, but a
fresh cross-platform result requires the change to run in those environments; no
remote workflow was triggered because publication/push/remote mutation was outside
the authorized scope.

## POLIS V6 artifact-backed commit — Issue #3 — 2026-09-23

This change carries optional commit intent in Change Contract schema v5/v6 and
adds consumer-authorized local commits to `apply`. The default remains
apply-only. It also removes unowned schema identifiers from the current schema
resources while retaining standard JSON Schema declarations.

Observed on Darwin arm64 with Go 1.27.1 and Git 2.55.0:

- `go test ./...` — PASS, 495 tests across 22 packages.
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` — PASS.
- Normative `go-coverprofile-v1` line-union metric: `5219 / 6503 = 80.255267%`, strictly greater than `80.0%` — PASS.
- `go vet ./...` — PASS.
- `go build ./...` — PASS.
- `go mod verify` — PASS (`all modules verified`).
- `gofmt -l .` and `git diff --check` — PASS, no output.
- Offline export — PASS; verified 14-member archive includes Change Contract v5/v6 schemas and declares `network_required: false`; `unzip -t` found no archive errors.
- Repository-wide search for the unowned schema host — PASS, no matches.
- `npm run verify` — unavailable because the repository has no `package.json`.

No cross-platform or remote CI result is claimed from this local run.
