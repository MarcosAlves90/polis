# TDD evidence

This file records externally observable Red/Green evidence for the alpha implementation. It is appended during development; it is not a substitute for test output.

## Iteration 1 — RED

Command: `go test ./...`

Exit: 1

```text
# github.com/polis-dev/polis-v4/cmd/polis [github.com/polis-dev/polis-v4/cmd/polis.test]
cmd/polis/main_test.go:11:13: undefined: run
cmd/polis/main_test.go:18:13: undefined: run
cmd/polis/main_test.go:25:13: undefined: run
# github.com/polis-dev/polis-v4/spec [github.com/polis-dev/polis-v4/spec.test]
spec/manifest_test.go:7:15: undefined: DecodeManifest
spec/manifest_test.go:14:18: undefined: DecodeManifest
spec/manifest_test.go:19:18: undefined: DecodeManifest
# github.com/polis-dev/polis-v4/internal/packageverify [github.com/polis-dev/polis-v4/internal/packageverify.test]
internal/packageverify/verify_test.go:63:15: undefined: Verify
internal/packageverify/verify_test.go:70:18: undefined: Verify
internal/packageverify/verify_test.go:75:18: undefined: Verify
internal/packageverify/verify_test.go:80:18: undefined: Verify
FAIL	github.com/polis-dev/polis-v4/cmd/polis [build failed]
FAIL	github.com/polis-dev/polis-v4/internal/packageverify [build failed]
FAIL	github.com/polis-dev/polis-v4/spec [build failed]
FAIL
```

## Iteration 1 — GREEN

Command: `go test ./...`

Exit: 0

```text
ok  	github.com/polis-dev/polis-v4/cmd/polis	0.015s
ok  	github.com/polis-dev/polis-v4/internal/packageverify	0.005s
ok  	github.com/polis-dev/polis-v4/spec	0.002s
```

## Iteration 2 — RED

Requirement: strict policy decoding and package policy validation.

Command: `go test ./...`

Exit: 1

```text
# github.com/polis-dev/polis-v4/spec [github.com/polis-dev/polis-v4/spec.test]
spec/policy_test.go:6:12: undefined: DecodePolicy
spec/policy_test.go:16:12: undefined: DecodePolicy
spec/policy_test.go:23:12: undefined: DecodePolicy
spec/policy_test.go:30:12: undefined: DecodePolicy
ok  	github.com/polis-dev/polis-v4/cmd/polis	(cached)
FAIL	github.com/polis-dev/polis-v4/spec [build failed]
--- FAIL: TestVerifyRejectsMalformedPolicy (0.00s)
    verify_test.go:122: expected malformed policy rejection
FAIL
FAIL	github.com/polis-dev/polis-v4/internal/packageverify	0.005s
FAIL
```

## Iteration 2 — GREEN

Command: `go test ./...`

Exit: 0

```text
ok  	github.com/polis-dev/polis-v4/cmd/polis	0.004s
ok  	github.com/polis-dev/polis-v4/internal/packageverify	0.008s
ok  	github.com/polis-dev/polis-v4/spec	0.008s
```

## Broader validation after Iterations 1-2

- `gofmt -l`: PASS (no files)
- `go vet ./...`: PASS
- `go test -race ./...`: PASS
- `go test ./... -coverprofile=coverage.out`: PASS
- Go statement coverage at that checkpoint: 91.5% (no normative POLIS line-coverage adapter existed yet)
- `go build -trimpath -o dist/polis ./cmd/polis`: PASS
- `polis doctor`: PASS
- `polis verify` on a freshly generated canonical smoke package: PASS
- Guide/schema JSON syntax validation: PASS

## SDD-0002 — canonical project policy execution

### Red

`go test ./...` failed to compile because `GatePolicy`, `CommandSpec`, `ProjectGateOrder`, `DecodeEvidence`, and `policyexec.Execute` did not exist. This was the expected absence of the specified behavior.

A second integration Red was observed after the executor was implemented: `TestVerifyRejectsMalformedEvidence` failed because package verification still accepted an evidence event with an unknown gate.

### Green

- strict canonical policy decoder implemented;
- direct argv executor implemented without shell invocation;
- PASS/FAIL/BLOCKED/timeout/NOT_APPLICABLE behavior covered;
- package verifier now validates NDJSON evidence;
- complete suite returned Green.

## SDD-0003 — existing-project package build

### Red

- manifest tests failed to compile because `Change` and `GitObjectFormat` did not exist;
- `internal/packagebuild` tests failed to compile because `Build` and `Options` did not exist;
- CLI build test observed exit code 3 / `NOT_IMPLEMENTED` instead of creating a package.

### Green

- manifest supports explicit SHA-1/SHA-256 repository identity;
- temporary-index target capture implemented without changing real staging;
- isolated apply and exact target-tree verification implemented;
- project policy runs before packaging;
- generated candidate is reverified before final output;
- `polis build` creates the verified package through the CLI.

## SDD-0004 — transactional consumer apply

### Red

- `internal/packageapply` tests failed to compile because `Apply` did not exist;
- CLI apply test observed exit code 3 / `NOT_IMPLEMENTED`.

### Green

- package loading reuses the canonical verifier and exposes verified patch/policy data;
- exact baseline and clean-tree preconditions implemented;
- fresh isolated consumer validation executes before real mutation;
- evidence is written beneath Git metadata;
- real application preserves HEAD and the real index and verifies exact post-apply tree through a temporary index;
- CLI `polis apply` executes the verified workflow.


## SDD-0005 — normative line coverage

### Red

- `go test ./spec ./internal/policyexec` failed to compile because `ParseGoCoverProfile` and `CoveragePass` did not exist.
- After policy schema v2 was introduced, the complete suite failed because alpha.4 fixtures still encoded `coverage` as a generic command. This was the expected fail-closed incompatibility.
- The first runtime-owned measurement of this project produced `76.267880%`, correctly failing the strict `>80` gate and exposing that prior Go statement coverage was not equivalent to the normative metric.

### Green

- policy schema v2 requires a dedicated coverage mode and `go-coverprofile-v1` adapter;
- runtime deletes stale reports, rejects path escape/non-regular/oversized/malformed reports, and calculates distinct covered/total source lines;
- exactly 80.0% fails threshold 80.0; greater values pass;
- `coverage_measured` evidence is schema-validated;
- additional behavior/error-path tests raised the runtime-owned project measurement above 80% without changing the threshold.

## SDD-0006 — canonical policy init

### Red

`go test ./internal/policyinit` failed to compile because `Init` and `Options` did not exist.

### Green

- `polis init` supports deterministic `auto|go` bootstrap;
- generated policy validates through the canonical spec parser;
- init refuses overwrite and unsupported/non-Git targets;
- HEAD and real index remain unchanged;
- CLI exposes the canonical bootstrap and tells the consumer to commit policy before `polis build`.

## SDD-0007 — delivery Change Contract and exact evidence trace

### Red

The reconstructed alpha.7 slice was re-executed from the frozen alpha.6 source because the prior intermediate alpha.7 source tree was not persisted. The following behavior-specific Reds were observed again rather than assumed from memory:

- `go test ./spec` failed to compile because `DecodeChangeContract`, `ChangeContractSchemaVersion`, change kinds, and regression modes did not exist.
- shared command-execution tests failed to compile because `commandexec.Run` did not exist.
- change-gate tests failed to compile because baseline/target Change Contract execution did not exist.
- package verification rejected the old five-member fixture after format-v2 inventory was required (`got 5, want 7`).
- package build/apply tests failed closed until an explicit Change Contract was supplied.
- CLI v1 fixtures failed once `build` required `--contract` and package verification required format v2.

### Green

- Change Contract schema v1 implemented with closed change kinds and regression modes;
- behavior and affected commands are mandatory for every delivery;
- defect contracts require a non-zero Red exit code plus explicit output oracle tokens;
- non-defect regression uses only canonical `not-a-defect` semantics;
- package format v2 embeds and hashes Change Contract plus regression probe;
- defect build/apply independently reproduce baseline Red and target Green;
- every Red-probe path must remain in the final target delta;
- verify reconstructs the complete PASS trace from Change Contract plus project policy and rejects omission, reordering, duplication, command mismatch, reason mismatch, coverage mismatch, or extra events;
- coverage evidence arithmetic is independently recomputed.

## SDD-0008 — canonical Red capture

### Red

`go test ./internal/redcapture` failed to compile because `Capture` and `Options` did not exist. CLI tests likewise had no `capture-red` command.

### Green

- `polis capture-red` captures the full non-ignored worktree delta through a temporary index;
- source HEAD/index/status are preserved;
- only defect/red_green contracts are accepted;
- contract and output are external to the worktree;
- output creation is exclusive and never overwrites;
- the exact captured patch is applied in an isolated baseline worktree and must reproduce the declared Red exit code and output tokens before publication;
- CLI returns patch path and SHA-256 only after validation PASS.

## Alpha.7 project-wide coverage correction

The first alpha.7 normative measurement remained below threshold even though per-package statement coverage was higher. Investigation established that the canonical Go coverage producer `go test ./... -coverprofile=...` does not instrument all production packages while cross-package integration tests execute them. The canonical Go profile was therefore changed to `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` so the project-wide suite instruments all project packages.

Even after that correction, the measured POLIS line coverage was initially `78.062804069%`, correctly FAIL under the unchanged strict `>80` gate. Additional tests were added for consumer-apply safety helpers and evidence-tampering failure classes. The next observed runtime-owned measurement was `1812/2261 = 80.141530296%`, PASS. The threshold and coverage algorithm were not weakened.


## SDD-0009 — physical path boundary

### Red

`go test ./internal/pathguard` failed to compile because `Contains` and canonical physical-path resolution did not exist. This represents the macOS-observed class where lexical aliases such as `/var/...` and `/private/var/...` can denote the same worktree while `filepath.Rel` alone classifies them differently.

### Green

- one shared `internal/pathguard` implementation canonicalizes existing symlinks and the nearest existing ancestor of missing output paths;
- packagebuild, redcapture, and coverage report containment use the shared physical boundary;
- alias regression tests pass;
- existing path-escape behavior remains fail-closed.

## SDD-0010 — V4.0.0 finalization

The CLI and Guide version were promoted to 4.0.0 without changing package format v2, Project Policy v2, or Change Contract v1. Platform claims remain limited to environments actually executed.

## SDD-0011 — portable pathguard absolute-path error validation

### Red

Consumer execution of POLIS V3.1 revision V005 on macOS reached isolated validation and failed `TestCanonicalPropagatesAbsolutePathResolutionFailure`: the test expected `filepath.Abs` to fail after deleting the process current directory, but macOS returned successfully. This demonstrated a non-portable test oracle rather than a proven production containment failure.

### Green

- `canonical` delegates only its absolute-path resolution dependency to `canonicalWithAbs`; production behavior still supplies `filepath.Abs`.
- the error branch is exercised with a deterministic injected resolver failure;
- the test no longer changes or deletes the process current working directory and requires no OS-specific skip.

## Iteration — cross-toolchain coverage margin

**Red observed:** V013 executed on macOS/Go 1.27 reached the project-wide coverage gate with `1723 / 2177 = 79.145613229215%`; no real patch application occurred, as required by failure atomicity.

**Green change:** Added deterministic tests covering previously unexercised production failure branches in `internal/packagebuild`, `internal/packageapply`, and `spec`. No coverage threshold, parser, exclusion, or production behavior was weakened.

**Producer evidence:** combining the complete prior suite evidence with the new focused executions covers `1900 / 2314 = 82.10890233362143%` of production lines on Linux/Go 1.23.2. The consumer remains authoritative for its own native toolchain and must independently pass `>80.0` before apply.

## SDD-0013 — Sonar maintainability cleanup

### Static-analysis Red

The user reported Sonar findings `go:S1192` for repeated `rev-parse`/`--cached` literals and `go:S107` for the nine-parameter `validateIsolated` helper.

The first refactor attempt also produced an executable Red in `internal/packagebuild`: the new `validation` input name collided with an existing local `validation := policyexec.Execute(...)`, causing compilation failure. This was not bypassed or weakened.

### Green

- package-local Git argv constants remove repeated literals;
- `validateIsolated` now accepts `context.Context` plus one private `isolatedValidation` value;
- the conflicting local policy result was renamed `policyValidation`;
- affected tests pass;
- all project packages pass when executed in bounded package runs;
- changed packages pass `go test -race`;
- `gofmt`, `go vet`, and `go mod verify` pass;
- conservative merged project-wide line coverage is `1921/2333 = 82.340334333476%`, above the unchanged strict `>80.0%` threshold.
