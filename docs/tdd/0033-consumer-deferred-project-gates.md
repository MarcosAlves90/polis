# TDD-0033 — Consumer-Deferred Project Gates

## Scope

Implement SDD-0038 and Issue #4: transfer selected enabled project-gate
execution from producer build to consumer preflight/apply while preserving the
gate requirement, existing policy schema, and historical package semantics.

## Red observed — historical v3 fixture used the new evidence wire format

The first full-suite run passed 460 tests and failed three legacy packageapply
tests. Their fixture changed a current format-v5 manifest to format v3 but left
the Evidence v3-only `deferred_gates` field in the evidence member. The v3 reader
correctly rejected that field under the historical Evidence v2 contract.

## Green — version-aware producer and consumer responsibility

- The shared execution plan now reports producer action and consumer requirement,
  with canonical deferral order and direct/transitive dependency validation.
- Producer execution emits Evidence v3 `DEFERRED` traces and skips deferred
  commands and coverage-report handling. Executed gate failures remain failures.
- New packages use format v5 and retain the existing eight-member embedded
  baseline layout. Formats v2-v4 retain Evidence v2 decoding and their existing
  package member inventories.
- Verification authenticates and semantically cross-checks deferred inventories
  and gate traces. Plan, build, verify, inspect, preflight, and apply expose the
  producer deferrals and consumer responsibility.
- Consumer validation compiles the packaged policy with no deferrals. Preflight
  remains read-only, and apply waits for all enabled consumer gates to pass
  before real mutation. Successful preflight/apply output distinguishes
  `producer_deferred_gates` from an empty `outstanding_deferred_gates` list and
  includes per-gate consumer statuses.
- The legacy v3 fixture now transcodes v5 evidence through the Evidence v2
  representation before changing the manifest version; v3 and v4 compatibility
  tests pass.

## Full validation

- `go test ./... -count=1` — PASS, 463 tests across 22 packages.
- `go test -race ./... -count=1` — PASS, 463 tests across 22 packages.
- CI project-wide line coverage — PASS, `4948 / 6071 = 81.502223686378%` against
  the unchanged strict `>80.0%` threshold.
- `go vet ./...`, `go mod verify`, `gofmt -l .`, and `git diff --check` — PASS.
- `go build -trimpath -o /tmp/polis-issue4 ./cmd/polis` and native doctor — PASS.
- Offline export inspection — PASS; format-v5 manifest and Evidence v3 schemas
  and the V6 specification are present in the archive.
- `npm run verify` was attempted but is unavailable because this Go repository
  has no `package.json`.

Validation ran on macOS 25.6.0 arm64 with Go 1.27.1 and Git 2.55.0. No Linux or
Windows run was performed locally.
