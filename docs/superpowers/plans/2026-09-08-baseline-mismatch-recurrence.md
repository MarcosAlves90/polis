# Baseline Mismatch Recurrence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate recurrent producer-side baseline mismatches caused by legitimate post-`start` commits while preserving exact consumer baseline validation.

**Architecture:** Keep `devlock.ValidateRepository` as the exact lock check used by `capture-red`, `preflight`, and `apply`. Add a build-specific repository validator that verifies the locked commit/tree directly and requires the current producer `HEAD` to descend from that commit. Make package construction use the locked base commit rather than current `HEAD`, so committed and uncommitted target state are both captured relative to the immutable baseline.

**Tech Stack:** Go 1.23+, Git CLI, existing POLIS V6 packagebuild/devlock/spec contracts.

**Spec:** `docs/sdd/0031-descendant-producer-head.md`

## Global Constraints

- Preserve fail-closed exact consumer baseline validation.
- Preserve exact Red-capture baseline validation.
- Do not mutate Git history, HEAD, or the real index.
- Keep package format v3, Project Policy v3, Change Contract v4, and Evidence v2 unchanged.
- Implement the smallest behavior change necessary.

---

### Task 1: Reproduce descendant-HEAD build mismatch

**Files:**
- Modify: `internal/packagebuild/build_test.go`

**Interfaces:**
- Consumes: existing `lockedDoubleFeatureFixture`, `Build`, and Git test helpers.
- Produces: regression tests defining descendant-HEAD build behavior.

- [ ] **Step 1: Write a failing regression test** that captures Red on the locked baseline, commits the captured Red test, leaves the production fix unstaged, and expects `Build` to succeed with `Result.BaseCommit == contract.baseline_lock.base_commit`.
- [ ] **Step 2: Run** `go test ./internal/packagebuild -run '^TestBuildLockedFeatureAcceptsDescendantHeadAfterRedCapture$' -count=1 -v`.
- [ ] **Step 3: Confirm RED** is the current `locked development baseline: baseline base_commit mismatch` failure.
- [ ] **Step 4: Add a second failing test** for a fully committed descendant target with a clean worktree.
- [ ] **Step 5: Run the focused tests and confirm the clean-target case currently fails because the build requires non-ignored working-tree changes.**

### Task 2: Add build-specific locked-baseline validation

**Files:**
- Modify: `internal/devlock/lock.go`
- Modify: `internal/devlock/lock_test.go`

**Interfaces:**
- Produces: `ValidateBuildRepository(ctx context.Context, repo string, change spec.ChangeContract) error`.
- Preserves: `ValidateRepository` exact equality semantics.

- [ ] **Step 1: Add tests** proving `ValidateBuildRepository` accepts an exact baseline and a descendant `HEAD`, while rejecting a non-descendant `HEAD` and a locked base-tree mismatch.
- [ ] **Step 2: Run** `go test ./internal/devlock -run 'BuildRepository' -count=1 -v` and confirm RED because the function does not exist.
- [ ] **Step 3: Implement** `ValidateBuildRepository` by checking object format, locked commit tree, Specification digest, and `git merge-base --is-ancestor <locked-base> HEAD`.
- [ ] **Step 4: Run the focused devlock tests and confirm GREEN.**

### Task 3: Build relative to the locked base

**Files:**
- Modify: `internal/packagebuild/build.go`

**Interfaces:**
- Consumes: `devlock.ValidateBuildRepository`.
- Produces: artifacts whose manifest base is always the locked base commit.

- [ ] **Step 1: Replace build-time exact `ValidateRepository` with `ValidateBuildRepository`.**
- [ ] **Step 2: Set build `baseCommit` from `changeContract.BaselineLock.BaseCommit`; retain runtime Git object-format detection.**
- [ ] **Step 3: Relax source-state admission to require only `index == HEAD`; let generated empty patch detection reject a no-change target.**
- [ ] **Step 4: Construct target tree and payload against the locked base commit so descendant committed state is included.**
- [ ] **Step 5: Run the two packagebuild regression tests and confirm GREEN.**

### Task 4: Protect fail-closed boundaries

**Files:**
- Test only unless a defect is found: `internal/redcapture/capture_test.go`, `internal/packageapply/apply_test.go`, `internal/packagebuild/build_test.go`

**Interfaces:**
- Preserves exact baseline semantics for Red capture and consumers.

- [ ] **Step 1: Run** `go test ./internal/redcapture -run 'RejectsBaselineDrift' -count=1 -v` and confirm post-lock drift remains rejected.
- [ ] **Step 2: Run** `go test ./internal/packageapply -run 'RejectsWrongHead' -count=1 -v` and confirm wrong consumer HEAD remains rejected.
- [ ] **Step 3: Run packagebuild non-descendant regression and confirm rejection before target acceptance.**

### Task 5: Align normative specification and record evidence

**Files:**
- Modify: `spec/POLIS-SPEC-v6.md`
- Create: `docs/tdd/0031-descendant-producer-head.md`

**Interfaces:**
- Documents the new distinction between immutable baseline identity and descendant producer target history.

- [ ] **Step 1: Update producer `build` rules** to define descendant `HEAD` as valid only when the locked base remains an ancestor and its tree matches exactly.
- [ ] **Step 2: Keep consumer exact-HEAD language unchanged.**
- [ ] **Step 3: Record observed Red/Green commands and results in the TDD evidence document.**

### Task 6: Verification

**Files:**
- No additional production files expected.

- [ ] **Step 1: Run** `gofmt` on changed Go files.
- [ ] **Step 2: Run focused packages:** `go test ./internal/devlock ./internal/packagebuild ./internal/redcapture ./internal/packageapply -count=1`.
- [ ] **Step 3: Run complete suite:** `go test ./... -count=1`.
- [ ] **Step 4: Run** `go vet ./...`.
- [ ] **Step 5: Run** `go build ./...`.
- [ ] **Step 6: Inspect `git diff --check` and final diff for unrelated changes.**
