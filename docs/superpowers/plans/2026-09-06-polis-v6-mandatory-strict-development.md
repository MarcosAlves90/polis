# POLIS V6 Mandatory Strict Development Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make strict locked SDD/TDD mandatory for every new POLIS V6 build while retaining read compatibility for historical artifacts.

**Architecture:** Keep the existing package, policy, evidence, isolation, and consumer contracts. Narrow producer admission to locked Change Contract schema v4, narrow `capture-red` to locked v4, preserve legacy decoding on read paths, then move the Go module and CLI identity to V6.

**Tech Stack:** Go 1.23+, Git, POLIS package format v3, Project Policy v3, Evidence v2.

**Spec:** `docs/sdd/0030-polis-v6-mandatory-strict-development.md`

## Global Constraints

- New V6 builds accept only locked Change Contract schema v4.
- Historical schemas remain readable for migration.
- Package format stays v3; Project Policy stays v3; Evidence stays v2.
- Coverage remains strictly greater than 80.0%.
- No remote publication or GitHub mutation.

---

### Task 1: Mandatory v4 producer admission

**Files:**
- Modify: `internal/packagebuild/build.go`
- Test: `internal/packagebuild/build_test.go`

**Interfaces:**
- Consumes: `spec.ChangeContract.SchemaVersion`, `spec.LockedChangeContractSchemaVersion`, `devlock.Validate`.
- Produces: fail-closed V6 producer admission before target construction.

- [x] Add a failing test that builds with schema v2 and expects rejection mentioning `polis start`/schema v4.
- [x] Add a failing test that builds with strict schema v3 and expects the same rejection.
- [x] Run those tests and observe failure because the current producer still accepts v2/v3.
- [x] Change producer admission to accept only schema v4 locked input.
- [x] Run focused and package tests to Green.

### Task 2: Mandatory locked Red capture

**Files:**
- Modify: `internal/redcapture/capture.go`
- Test: `internal/redcapture/capture_test.go`

**Interfaces:**
- Consumes: locked schema-v4 Change Contract and existing Red-to-Green semantics.
- Produces: no regression patch can be captured from an unlocked schema-v3 contract.

- [x] Add a test with schema-v3 strict Red-to-Green and expect rejection before output creation.
- [x] Observe Red because schema v3 is currently accepted.
- [x] Require schema v4 in `loadRedGreenContract` before `devlock.Validate`/capture.
- [x] Verify schema-v4 Red capture still passes.

### Task 3: Legacy read compatibility

**Files:**
- Test: `internal/packageverify/verify_test.go`
- Test: `cmd/polis/main_test.go`

**Interfaces:**
- Consumes: historical valid package fixtures.
- Produces: evidence that producer break does not silently remove verify/inspect migration compatibility.

- [x] Add/strengthen tests that historical contract schemas remain accepted by verify/inspect.
- [x] Run them before further compatibility changes and keep them Green throughout.

### Task 4: V6 module and CLI identity

**Files:**
- Modify: `go.mod`
- Modify: Go imports repository-wide from `/v5` to `/v6`.
- Modify: `cmd/polis/main.go`
- Test: `cmd/polis/main_test.go`
- Test: `internal/cicontract/sonar_local_test.go` or a focused V6 repository-contract test.

**Interfaces:**
- Produces: module `github.com/MarcosAlves90/polis/v6` and CLI `6.0.0`.

- [x] Add failing contract assertions for module `/v6` and doctor `6.0.0`.
- [x] Observe Red on `/v5` and `5.1.0`.
- [x] Update module/import paths and CLI version.
- [x] Run `go mod tidy` only if required by the module-path change; no dependency upgrades.
- [x] Run focused tests and compile all packages.

### Task 5: Public V6 contract documentation

**Files:**
- Create: `spec/POLIS-SPEC-v6.md`
- Create: `docs/tdd/0030-polis-v6-mandatory-strict-development.md`
- Modify: `README.md`
- Create: `guide/Z_end-to-end-project-delivery-guide-v6.json`
- Modify: `CHANGELOG.md`
- Modify: `docs/releases.md`

**Interfaces:**
- Produces: one consistent statement that strict locked development is mandatory for new V6 builds while old artifacts remain readable.

- [x] Document producer/read compatibility split and major-version rationale.
- [x] Update command examples to require `polis start` before build.
- [x] Move release identity from planned 5.1.0 to 6.0.0 without claiming publication.
- [x] Record only observed Red/Green evidence in TDD-0030.

### Task 6: Complete validation and E2E

**Files:**
- No production files unless a new failing verification reveals a defect.

**Interfaces:**
- Consumes: complete V6 tree.
- Produces: fresh evidence for release readiness.

- [x] Run `gofmt -l .` and `git diff --check`.
- [x] Run affected tests and `go test ./... -count=1`.
- [x] Run `go test -race ./... -count=1` with explicit exit status.
- [x] Run `go vet ./...`, `go mod verify`, and `go build ./...`.
- [x] Generate canonical project coverage and prove line coverage `>80.0`.
- [x] Execute a fresh external strict V6 E2E: `start → capture-red → build → verify → inspect → preflight → apply → complete tests`.
- [x] Self-build and self-verify POLIS V6 from the exact baseline.
- [x] Generate the final patch and SHA-256; do not commit, push, tag, or release.
