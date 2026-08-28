# SDD-0015 — Deterministic Git EOL fixtures on Windows

Status: accepted for POLIS V4.0.0 cross-platform validation.

## Problem

The first GitHub Actions Windows run for the cross-platform CI contract executed POLIS successfully through build/apply paths, but four tests failed because temporary Git repositories inherited the runner's line-ending conversion policy. Tracked files restored by Git were materialized as CRLF (`\r\n`) while the fixtures compared them against hard-coded LF (`\n`) bytes.

Observed failures were:

- `cmd/polis.TestRunApplyAppliesBuiltPackage`;
- `internal/packageapply.TestApplyExactBaselinePreservesIndexAndWritesEvidenceOutsideWorktree`;
- `internal/packageapply.TestApplyRejectsWrongHead`;
- `internal/packageapply.TestReversePatchRestoresAppliedChange`.

The failures occurred after the relevant POLIS operations completed; the reported mismatch was line-ending representation in test fixture files, not a target-tree or transaction failure.

## Decision

Tests that create temporary Git repositories MUST NOT inherit host-specific `core.autocrlf` behavior. The `cmd/polis` and `internal/packageapply` test processes set Git's process-local configuration environment to `core.autocrlf=false` before any tests run.

The override MUST:

- apply only to Git subprocesses launched by the test process;
- avoid modifying system, global, or repository Git configuration;
- preserve the production POLIS runtime unchanged;
- keep Windows in the CI matrix;
- make fixture bytes deterministic across Linux, macOS, and Windows.

Each affected package MUST include an executable assertion that its Git subprocess observes `core.autocrlf=false`.

## Acceptance

- the deterministic Git-config assertion passes in both affected packages;
- the four previously failing Windows tests no longer depend on runner/global EOL configuration;
- Linux and macOS behavior remains unchanged;
- no production source file changes;
- complete suite, coverage, build, and CI gates remain authoritative.
