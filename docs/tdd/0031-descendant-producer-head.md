# TDD-0031 — Descendant Producer HEAD Without Baseline Drift

## Scope

Observed investigation and Red/Green evidence for SDD-0031 on repository baseline `79fc022` (`feat(workflow): support zero-residue target repositories`).

## Root cause investigation

The recurrent mismatch was reproduced in `internal/packagebuild` after the mandatory V6 sequence `polis start -> capture-red` when development created a normal Git commit before packaging.

Observed mechanism:

1. `polis start` correctly locked the original `HEAD` and `HEAD^{tree}`.
2. `capture-red` correctly required that exact baseline before accepting the Red proof.
3. `packagebuild.Build` then resolved current producer `HEAD` as its `baseCommit`.
4. The same exact `devlock.ValidateRepository` used by Red capture and consumer validation compared current producer `HEAD` to `baseline_lock.base_commit`.
5. A legitimate descendant commit therefore became indistinguishable from a lost/rebased baseline and failed as `baseline base_commit mismatch`.
6. When all target changes were committed, build failed even earlier because `requireBuildSourceState` required non-ignored working-tree changes, despite a non-empty locked-base-to-current-target delta.

The lock itself was not corrupt. Artifact verification, consumer apply logic, and policy hashing were not the source of this recurrence. The defect was the build-time coupling of immutable baseline identity to current producer `HEAD`.

## Red

Two packagebuild regression tests were added before production changes.

Command:

```text
go test ./internal/packagebuild -run '^TestBuildLockedFeatureAccepts(DescendantHeadAfterRedCapture|FullyCommittedDescendantTarget)$' -count=1 -v
```

Observed failures:

```text
TestBuildLockedFeatureAcceptsDescendantHeadAfterRedCapture:
locked development baseline: baseline base_commit mismatch: got <descendant> want <locked-base>

TestBuildLockedFeatureAcceptsFullyCommittedDescendantTarget:
working tree has no non-ignored changes
```

A devlock behavior test was then added for the required build-specific validation API.

Command:

```text
go test ./internal/devlock -run 'BuildRepository' -count=1 -v
```

After correcting a missing test import, the expected Red was:

```text
undefined: ValidateBuildRepository
```

## Green implementation

The smallest production change was:

- keep `devlock.ValidateRepository` unchanged for exact Red-capture and consumer semantics;
- add `devlock.ValidateBuildRepository` for producer build;
- validate the locked object format and resolve `baseline_lock.base_commit^{tree}` directly;
- require the locked base commit to be an ancestor of current producer `HEAD` with `git merge-base --is-ancestor`;
- use `baseline_lock.base_commit` as the build/artifact base rather than current `HEAD`;
- keep the real index equal to current `HEAD`, but permit a clean worktree because descendant commits may already contain the target;
- retain the existing empty-patch rejection after locked-base-to-target construction.

Focused Green command:

```text
go test ./internal/packagebuild -run '^TestBuildLockedFeature(AcceptsDescendantHeadAfterRedCapture|AcceptsFullyCommittedDescendantTarget|RejectsNonDescendantProducerHead)$' -count=1 -v
```

Result: PASS for all three scenarios.

Build-lock unit command:

```text
go test ./internal/devlock -run 'BuildRepository' -count=1 -v
```

Result: PASS for descendant acceptance, non-descendant rejection, and locked base-tree mismatch rejection.

## Fail-closed counterevidence

The correction was explicitly checked against boundaries that must remain strict.

```text
go test ./internal/redcapture -run 'RejectsBaselineDrift' -count=1 -v
```

Result: PASS. Red capture still rejects post-lock HEAD drift.

```text
go test ./internal/packageapply -run 'RejectsWrongHead' -count=1 -v
```

Result: PASS. Consumer apply still rejects `HEAD != artifact base_commit`.

A packagebuild regression also creates an unrelated root commit with the same tree shape and confirms that build rejects it because the locked base is not an ancestor.

## Repository validation

Focused affected packages:

```text
go test ./internal/devlock ./internal/packagebuild ./internal/redcapture ./internal/packageapply ./internal/packageverify -count=1
```

Result: PASS.

Complete suite:

```text
go test ./... -count=1
```

Result: PASS.

Static/dependency checks:

```text
go vet ./...
go mod verify
```

Results: PASS; `go mod verify` reported `all modules verified`.

Build:

```text
go build ./...
```

The first sandbox execution was blocked by Git's `dubious ownership` check on the extracted uploaded repository. Re-running the same Go build with a process-local `safe.directory=/mnt/data/polis-work/polis` Git configuration passed. No repository or global Git configuration was modified.

Race detector:

```text
go test -race ./... -count=1
```

Result: PASS.

Coverage policy command:

```text
go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out
```

The POLIS `go-coverprofile-v1` parser measured:

```text
covered_lines=3621
total_lines=4387
line_coverage_percent=82.539320720310
threshold=>80.0
result=PASS
```

The generated coverage report was removed after measurement because it is execution output, not part of this change.
