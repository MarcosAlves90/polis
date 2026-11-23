# SDD-0049 — Safe parallel validation is explicit and diagnosable

## Problem

Issue #40 asked for bounded gate concurrency to be easy to enable, inspect, and
diagnose without weakening POLIS proof semantics. `--jobs` already existed on
`gates`, `workspace validate`, and `build`, but declarations were buried in
policy JSON, plans did not explain eligibility, and results did not distinguish
a requested worker bound from measured overlap. A higher `--jobs` value could
therefore look like a parallel run even when every gate stayed serial.

## Locked scope

The implementation uses strict SDD/TDD with the immutable schema-6 contract
`locked-v6.json`, base commit `42a22918ee17524bef0afd6bea257a7083016839`, base
tree `81e714a6ac564955b74745b5b56b2304cd3a3fae`, and external policy SHA-256
`2e744eb5a45b5237af4559084459e48ae6edd2011f0e6af73e08ff5e7aaa692c`. The
captured Red patch SHA-256 is
`dad95c06ab4c8786ac2719f9c49d3ac6cc26e2bbac335fe98a922fdc0bf60b3b`. The
committed `.polis/policy.json` and the contract bytes were not changed.

The genuine Red state failed with undefined concurrency/init APIs, and
`capture-red` preserved only the scoped failing tests. Production code was
added only after that proof.

## Design decisions

1. **One canonical vocabulary.** `spec/concurrency.go` owns the reason codes and
   report types. `policyplan` and `policyexec` alias them so every command
   surface serializes the same shape.
2. **Declaration and observation are separate.** `policyplan.ConcurrencySummary`
   is jobs-independent and declaration-only. `policyexec.ConcurrencyReport`
   combines a jobs-aware `ConcurrencyDiagnosis` with measured
   `ConcurrencyObservation`. `overlap_observed` requires an actual concurrent
   launch; `--jobs > 1` never implies it.
3. **Fail-closed opt-in.** Only enabled command gates with explicit
   `parallel_safe: true` are eligible. Coverage, disabled/deferred gates,
   prerequisites, missing opt-ins, selection, reuse, invalid jobs, and worker
   caps produce stable reason codes. No safety is inferred from a gate ID,
   name, command, category, or successful history.
4. **Reviewed init path.** `polis init --parallel-safe <id>` is repeatable and
   rejects unknown, coverage, disabled, and duplicate IDs before writing a
   policy. The resulting declaration is recorded as explicit policy-init
   provenance with the existing `init_explicit_command` reason.
5. **Additive legacy compatibility.** Schema-v2 policies may carry the additive
   `parallel_safe` declaration because it is fail-closed and does not alter
   validation guarantees. Schema-v3 `input_paths` fields remain rejected on
   v2 policies. Coverage declarations are rejected outright.
6. **Presentation-only reporting.** Concurrency is added to command result
   surfaces for `plan`, `gates`, `workspace validate`, and `build`. New
   gate-run manifests and external workspace checkpoints also carry the same
   additive optional field, so stored runs and checkpoints explain what the
   operator saw; older records without it remain valid. Evidence events,
   retained records, and package/artifact schemas remain digest-bound and
   compatibility-sensitive. The field is excluded from replay/reuse identity
   and does not change gate statuses, final result ordering, coverage
   exclusivity, source-state checks, or target-tree verification.
7. **Deterministic output.** Findings, launched gates, overlap groups, and text
   lines are sorted or derived from plan order. Completion timing never changes
   the report semantics.

## Rejected alternatives

- Inferring parallel safety from gate names, command categories, `input_paths`,
  or past success. This violates the fail-closed requirement.
- Treating coverage as an ordinary command gate because it owns report
  cleanup/measurement and must remain exclusive.
- Including concurrency in replay/reuse identity or immutable delivery evidence.
  The additive field is excluded from identity and checksum semantics, so saved
  runs and checkpoints remain explanatory without changing strict compatibility.
- Using a larger default jobs bound. Serial remains the default; concurrency is
  an explicit, reviewed project decision.

## Measured evidence

Benchmark fixture: two reviewed sleep-bound command gates with no shared
outputs, approximately three seconds of sleep each. Environment: Darwin arm64,
Apple M5, Go 1.27.3, `GOMAXPROCS=2`, `GOFLAGS=-p=1`, one benchmark at a time.

```text
BenchmarkSafeParallelJobsOne-2   1  6072323958 ns/op  1.000 max-concurrent
BenchmarkSafeParallelJobsTwo-2   1  3059266292 ns/op  2.000 max-concurrent
```

Real CLI gate run on the same host with the same two explicit declarations
(external policy declaring sleep-bound `lint`/`dependency` as
`parallel_safe`, executed on the actual repository via `polis gates`):

```text
--jobs 1: 6.30s, overlap_observed=false, max_concurrent=1
--jobs 2: 3.27s, overlap_observed=true,  max_concurrent=2
```

These numbers characterize sleep-bound fixtures only. CPU, disk, locks, cache,
network, and shared-tool contention can make concurrency slower. The guidance
is to measure the real project gates, not to promise universal speedup.

## Validation

The implementation preserves the strict full-suite and project-gate path. All
commands below ran on Darwin arm64 / Apple M5 with Go 1.27.3 and Git 2.56.0. Go
commands used a persistent `GOMAXPROCS=2 GOFLAGS=-p=1` wrapper and `nice(10)`;
only one heavy command ran at a time. Logs and reports are retained in the
persistent scratch directory, not system temporary storage:

```text
SCRATCH=/private/var/folders/yk/x0h9grg57_78plb5ght28hrh0000gn/T/opencode/polis-issue40-outside
R5_WORK="$HOME/.hermes/profiles/mariupol/cache/scratch/issue40-r5-work"
```

Observed results:

- Genuine scoped Red failed before production edits; `polis capture-red` then
  passed and preserved the immutable patch. Because CI enforces `gofmt -l .`,
  the captured proof test was formatted and the Red proof was legitimately
  re-captured on the locked base in a separate detached worktree; the new patch
  SHA-256 is `dad95c06ab4c8786ac2719f9c49d3ac6cc26e2bbac335fe98a922fdc0bf60b3b`.
  Replaying that patch on the locked base reproduced the declared nonzero Red
  oracle. All three captured test files remain byte-identical to the patch;
  Green-only tests and fixtures live in separate files.
- `polis doctor --repo . --format json` with the committed policy: PASS,
  `policy_source=committed`, policy SHA-256
  `2e744eb5a45b5237af4559084459e48ae6edd2011f0e6af73e08ff5e7aaa692c`
  (`$SCRATCH/final-doctor-committed.json`).
- `polis plan --repo . --format json` with the committed policy: PASS,
  `plan_version=4`, the strict enabled set
  `test.complete,coverage,lint,build,dependency`, and declaration-only
  concurrency blockers (`$SCRATCH/final-plan-committed.json`).
- `go test ./... -count=1` — PASS (`$SCRATCH/logs/final-test-all.stdout`).
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` — PASS;
  `go tool cover -func` reported 85.6% statements, while the POLIS coverage
  gate independently passed at the unchanged strict threshold.
- `go vet ./...` — PASS; `go mod verify` — PASS; `go build ./...` — PASS.
- `git diff --check` — PASS. `gofmt -l .` — PASS with no files reported; the
  formatted proof test was re-captured into the Red patch above, and the
  configured lint gate (`go vet ./...`) remains unchanged and passing.
- `polis workspace validate --repo . --contract "$SCRATCH/locked-v6.json"
  --regression-patch "$R5_WORK/regression-r5.patch" --out-report
  "$R5_WORK/final-workspace-report-r5.json" --jobs 1 --format json` — PASS.
  The enabled gates all returned PASS, the report contains `policy_sha256`
  `2e744eb5a45b5237af4559084459e48ae6edd2011f0e6af73e08ff5e7aaa692c`, and the
  command/external checkpoint include the additive optional concurrency report.
- The final report `target_tree` was recomputed independently with a temporary
  `GIT_INDEX_FILE`, `read-tree` of the locked base commit, `git add -A -- .
  ':(exclude).polis/artifacts/**'`, and `git write-tree`; both identities
  matched exactly, with the concrete value retained in
  `$R5_WORK/final-workspace-report-r5.json`.
- The external policy used for declaration-focused CLI checks canonicalizes on
  load, so its in-memory digest differs from the committed raw policy bytes.
  Full locked validation therefore uses the committed policy, preserving the
  locked baseline and invariant I1.

The locked schema-6 scope excludes `README.md` and `VALIDATION.md`, so those
files remain at their baseline bytes; this in-scope SDD and the persistent
scratch evidence carry the detailed validation record without changing the
pinned contract or weakening any gate.
