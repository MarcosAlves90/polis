# TDD-0030 — POLIS V6 Mandatory Strict Development

## Scope

Implementation evidence for SDD-0030. Only observed executions are recorded; no PASS is claimed for checks not executed.

## Red 1 — legacy producer admission remained open

Before changing production admission, two focused tests were added:

```text
TestBuildV6RejectsSchemaV2ProducerInput
TestBuildV6RejectsUnlockedSchemaV3ProducerInput
```

Both failed because `Build` returned `nil`: schema-v2 and unlocked schema-v3 contracts still produced new artifacts.

## Green 1 — producer requires locked schema v4

Producer admission was narrowed to Change Contract schema v4 with `strict_sdd_tdd_v2` and `baseline_lock`, before target construction or policy gate execution. The two rejection tests and `TestBuildLockedFeatureReproducesRedGreen` then passed. The complete `internal/packagebuild` suite was migrated to V6 locked fixtures and passed.

## Red 2 — capture-red accepted unlocked schema v3

`TestCaptureV6RejectsUnlockedSchemaV3Contract` initially failed with:

```text
expected V6 locked schema-v4 capture rejection, got <nil>
```

## Green 2 — capture-red requires locked schema v4

`loadRedGreenContract` now rejects anything other than schema v4 / `strict_sdd_tdd_v2` / non-nil `baseline_lock` before Red capture. Locked test-scope capture, production-before-Red rejection, and baseline-drift tests passed afterward.

## Red 3 — runtime identity was still V5

The module-identity contract and doctor test were changed first. Observed failures were:

```text
module declaration="module github.com/MarcosAlves90/polis/v5" want="module github.com/MarcosAlves90/polis/v6"

doctor version mismatch: stdout="POLIS doctor 5.1.0 ..."
```

## Green 3 — V6 module and CLI identity

The module/import path was migrated to `github.com/MarcosAlves90/polis/v6`, public installation commands were updated, and `doctor` now reports `6.0.0`. Focused module and doctor tests passed and `go mod verify` passed.

## Red 4 — changelog contract still described planned 5.1.0

The changelog contract was changed first to require:

```text
## [6.0.0] - 2026-09-06
compare/v6.0.0...HEAD
compare/v5.0.1...v6.0.0
```

The test failed because all three fragments were absent.

## Green 4 — public V6 contract aligned

The changelog, README, release examples, V6 Specification, and active End-to-End Guide were updated. Repository-wide validation was then executed on the resulting V6 tree and is recorded below.

## Repository-wide Green evidence

Fresh validation after V6 producer/CLI/fixture migration:

```text
go test ./... -count=1                  PASS
go test -race ./... -count=1            PASS
gofmt -l .                               no output
git diff --check                         PASS
go vet ./...                              PASS
go mod verify                             all modules verified
go build ./...                            PASS
```

Canonical project coverage was generated with the committed Project Policy command and parsed through `spec.ParseCoverage(go-coverprofile-v1)`:

```text
covered_lines=3507
total_lines=4220
line_coverage_percent=83.104265402844
threshold=>80.0
result=PASS
```

## External V6 E2E

A fresh external repository was created from a clean committed baseline. `polis start` ran before the test and implementation. Observed sequence:

```text
POLIS START: PASS
POLIS CAPTURE-RED: PASS
TestDouble: RED before production
TestDouble: GREEN after minimal production change
POLIS BUILD: PASS
POLIS VERIFY: PASS
POLIS INSPECT: PASS
Trace: REQ-001 -> AC-001 -> regression
POLIS PREFLIGHT: PASS
POLIS APPLY: PASS
go test ./...: PASS on consumer
```

Artifact SHA-256:

```text
5aca1fb3272d1d27c299455ec4662ff5ed195e79335c076c6ad91342f0c0b7a4
```

The artifact used Change Contract schema v4 and contained 41 Evidence events.


## Self-hosted V6 delivery

A fresh producer and consumer were both created from exact baseline:

```text
00860d977abc9e36db88089d73d465e11ff2e6c3
```

The producer executed `polis start` before the regression test or production change. Only the V6 module-identity test was introduced for the Red state, then captured with canonical `polis capture-red`. The full V6 payload was applied afterward. Observed results:

```text
POLIS START: PASS
POLIS CAPTURE-RED: PASS
POLIS BUILD: PASS
POLIS VERIFY: PASS
POLIS INSPECT: PASS
POLIS PREFLIGHT: PASS
POLIS APPLY: PASS
go test ./... -count=1: PASS on consumer
```

The self-hosted artifact before this evidence-document update had:

```text
artifact_sha256=98d53071e80b019f66b45570d8359dc1454af5bc09358e942c528bf2d8744654
base_commit=00860d977abc9e36db88089d73d465e11ff2e6c3
target_tree=bb0dad457e6de79582d3f7c46d9403e82fd4a84a
change_schema=4
evidence_events=41
red_patch_sha256=d50cfc188d476920c8748b08acfcef11949e54ed2b3a899e446efa9b9ba8dffd
```

Consumer invariants after `apply` were read directly:

```text
HEAD=00860d977abc9e36db88089d73d465e11ff2e6c3
index_tree=0d4b982152b69dae967ff66a41db2e74a2ee7217
applied_target_tree=bb0dad457e6de79582d3f7c46d9403e82fd4a84a
```

The real consumer working tree contained the payload while HEAD and the index remained unchanged. The complete Go test suite passed after application.

Two earlier grouped harness invocations expired while `apply` was still validating. Those timeouts were not counted as PASS. Individual Project Policy gates were then executed successfully in the isolated target, stale harness-killed worktrees were removed, and `polis apply` was rerun alone. That execution completed with exit code `0` in 25 seconds.

The final artifact is rebuilt after this section is recorded so the frozen delivery bytes include the complete TDD evidence.
