# Strict SDD/TDD Change Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add an opt-in Change Contract v3 that structurally requires SDD intent and machine-enforces test-only Red capture plus immutable Red-test bytes for feature/defect Red -> Green deliveries.

**Architecture:** Extend `spec.ChangeContract` compatibly so v1/v2 retain existing semantics and v3 activates `strict_sdd_tdd_v1`. Reuse the existing regression patch and Evidence v2 pipeline rather than adding package members. Generalize Red execution from `defect` to a contract-level `RequiresRedGreen()` predicate, validate strict Red paths against explicit `test_scope`, and compare staged blob IDs between Red and target isolation to prevent post-Red test laundering.

**Tech Stack:** Go 1.23+, Git plumbing used by existing POLIS isolation, POLIS package format v3, Project Policy v3, Evidence v2.

**Spec:** `docs/sdd/0026-strict-sdd-tdd-contract.md`

## Global Constraints

- Preserve Change Contract v1/v2 decoding and semantics.
- Keep package format v3 at exactly seven members.
- Keep Evidence v2 free of raw stdout/stderr.
- Keep direct argv execution and explicit command environments.
- Do not infer test paths; use explicit `test_scope`.
- Do not make v3 mandatory for current V5 builds in this increment.
- Coverage remains strictly greater than 80%.

---

### Task 1: Strict Change Contract v3 schema

**Files:**
- Modify: `spec/change.go`
- Modify: `spec/v5_contract_test.go`

**Interfaces:**
- Produces: `StrictChangeContractSchemaVersion`, `DevelopmentMethodStrictSDDTDDV1`, `DevelopmentSpecification`, `SpecificationClause`, `ChangeContract.RequiresRedGreen()`, `ChangeContract.ValidateTestPaths([]string)`.
- Preserves: existing v1/v2 decoding and regression semantics.

- [x] **Step 1: Write failing schema tests** for valid strict feature, missing specification section, duplicate clause IDs, feature `not_applicable`, and test scope outside ordinary scope.
- [x] **Step 2: Run** `go test ./spec -run 'TestStrict|TestChangeContractV3' -count=1` and verify failures are caused by absent v3 support.
- [x] **Step 3: Implement minimal v3 structs and validation** while leaving v1/v2 paths unchanged.
- [x] **Step 4: Run** `go test ./spec -count=1` and require PASS.

### Task 2: Generalize Red execution semantics

**Files:**
- Modify: `internal/changeexec/execute.go`
- Modify: `internal/changeexec/execute_test.go`
- Modify: `spec/evidence_contract.go`
- Modify: `spec/evidence_contract_test.go`

**Interfaces:**
- Consumes: `ChangeContract.RequiresRedGreen()`.
- Produces: identical Red/evidence sequence for defects and strict-v3 features; legacy features remain NOT_APPLICABLE.

- [x] **Step 1: Write failing tests** proving strict feature baseline execution is accepted and strict feature Evidence requires Red then Green.
- [x] **Step 2: Run** `go test ./internal/changeexec ./spec -run 'Strict|Evidence' -count=1` and verify expected failures.
- [x] **Step 3: Replace defect-only branching with `RequiresRedGreen()`** in baseline, target, and Evidence validation.
- [x] **Step 4: Run** `go test ./internal/changeexec ./spec -count=1` and require PASS.

### Task 3: Enforce test-only Red capture

**Files:**
- Modify: `internal/redcapture/capture.go`
- Modify: `internal/redcapture/capture_test.go`

**Interfaces:**
- Consumes: `ChangeContract.RequiresRedGreen()` and `ValidateTestPaths`.
- Produces: feature-compatible strict Red capture with no production-path changes in the Red probe.

- [x] **Step 1: Write failing tests** for strict feature capture and rejection when the Red patch also changes `app.go` outside `test_scope`.
- [x] **Step 2: Run** `go test ./internal/redcapture -run 'StrictFeature' -count=1`; expected Red is current defect-only rejection or missing test-scope validation.
- [x] **Step 3: Generalize contract loading and validate staged Red paths** after isolated patch apply.
- [x] **Step 4: Add a Red test for deterministic changed-path ordering**, then sort staged Red paths lexicographically before `test_scope` validation.
- [x] **Step 5: Run** `go test ./internal/redcapture -count=1` and require PASS.

### Task 4: Require strict-feature regression patch in build

**Files:**
- Modify: `internal/packagebuild/build.go`
- Modify: `internal/packagebuild/build_test.go`
- Modify: `internal/packageverify/verify.go`
- Modify: `internal/packageverify/verify_test.go`

**Interfaces:**
- Consumes: `ChangeContract.RequiresRedGreen()`.
- Produces: build/package verification rules based on Red/Green semantics instead of `kind == defect`.

- [x] **Step 1: Write failing test** where a valid strict feature build omits `RegressionPatch` and must be rejected.
- [x] **Step 2: Run** targeted packagebuild test and observe current acceptance/failure for the wrong reason.
- [x] **Step 3: Change `loadRegressionPatch` to consume the full contract** and require a patch iff `RequiresRedGreen()`.
- [x] **Step 4: Run** `go test ./internal/packagebuild -count=1` and require PASS.

### Task 5: Lock captured Red test blobs

**Files:**
- Modify: `internal/isolation/validate.go`
- Modify: `internal/packagebuild/build_test.go`

**Interfaces:**
- Produces: `map[path]blobID` from strict Red isolation and exact comparison against the target index.
- Legacy defect behavior retains path-presence semantics; strict v3 adds byte identity.

- [x] **Step 1: Write failing integration test** that captures a failing test, changes the expected assertion after Red, fixes production, and expects build rejection. Add explicit acceptance coverage that removing the captured Red path from the target is rejected.
- [x] **Step 2: Run** targeted test and observe current build acceptance, proving the laundering gap.
- [x] **Step 3: Record strict Red staged blob IDs and compare them in target isolation** using Git index object IDs; reject deletions and mismatches.
- [x] **Step 4: Run** packagebuild and isolation-related tests and require PASS.

### Task 6: Publish the machine contract and observed TDD evidence

**Files:**
- Create: `spec/POLIS-SPEC-v3.md`
- Create: `docs/tdd/0026-strict-sdd-tdd-contract.md`

**Interfaces:**
- `spec/POLIS-SPEC-v3.md` is the normative machine-level delta for strict Change Contract schema v3 and references unchanged V5/v2 contracts rather than duplicating them.
- The TDD record captures exact observed Red/Green evidence and plan revisions.

- [x] **Step 1: Write `spec/POLIS-SPEC-v3.md`** from the accepted SDD contract, defining only the new strict machine semantics and compatibility boundary.
- [x] **Step 2: Record each observed failing command and behavior-specific failure.**
- [x] **Step 3: Record corresponding Green commands and results.**
- [x] **Step 4: State limitations explicitly: no Green→Green characterization, no Development Ledger, and v3 not yet mandatory.**

### Task 7: Current-POLIS delivery validation

**Files:**
- External: `/mnt/data/polis-change-strict-sdd-tdd-v2.json`
- Output: `/mnt/data/polis-strict-sdd-tdd-artifacts/`

**Interfaces:**
- Uses current POLIS V5 build/verify semantics against this feature delivery.

- [x] **Step 1: Run gofmt and affected tests.**
- [x] **Step 2: Run complete `go test ./...`, `go vet ./...`, `go mod verify`, and `go build ./...`.**
- [x] **Step 3: Run committed Project Policy coverage and require `> 80.0`.**
- [x] **Step 4: Run `git diff --check`.**
- [x] **Step 5: Run canonical `polis build` with the external schema-v2 feature Change Contract.**
- [x] **Step 6: Run canonical `polis verify` on the exact generated bytes.**
- [x] **Step 7: Exercise a schema-v3 strict feature end-to-end through capture-red, build, verify, preflight, apply, and consumer tests.**
- [x] **Step 8: Inspect final changed paths and confirm they are all authorized by the implementation Change Contract.**
