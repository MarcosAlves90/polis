# SDD-0020 — SonarQube duplication remediation

Status: accepted for a behavior-preserving POLIS V4 maintenance delivery.

## Problem

The SonarQube analysis after SDD-0019 reports no remaining open code-smell findings, but the Quality Gate still reports duplicated-code density above the configured target. The remaining problem is CPD-style block duplication rather than repeated string literals.

The duplicated implementation is concentrated in repository infrastructure shared by build, apply, red-capture, and strict JSON decoding paths. Maintaining independent copies of these flows increases drift risk and causes SonarQube to count substantially duplicated lines.

## Scope

This delivery is limited to duplication remediation and local cleanup rules:

- shared Git execution and temporary-worktree helpers;
- shared isolated validation orchestration used by producer and consumer flows;
- shared external-file validation and reading;
- shared strict JSON end-of-input validation;
- test updates required by the helper extraction;
- `.gitignore` entries for disposable local SonarQube/SonarScanner state.

Coverage reporting, SonarScanner configuration, GitHub Actions integration, package format, CLI behavior, and public protocol semantics are out of scope.

## Design

1. Add `internal/gitutil` as the single owner of Git command execution, repository-root resolution, detached worktrees, changed-index discovery, target-tree checks, and temporary indexes.
2. Add `internal/isolation` as the shared owner of isolated regression/target validation used by package build and package apply, with caller-specific error labels supplied as configuration.
3. Add `internal/fileutil` as the shared owner of reading validated files outside the target worktree.
4. Add `spec/json.go` as the shared strict JSON EOF validator used by manifest, policy, and change-contract decoding.
5. Preserve caller-visible error text and transactional behavior at the existing package boundaries.
6. Ignore `.scannerwork/`, `.sonar/`, and `.sonarlint/` as disposable local analysis/tool state.

## Acceptance criteria

- duplicated production implementation is materially reduced without using Sonar CPD exclusions;
- the local exact-line duplication proxy for production Go source is below 3%;
- no SonarScanner or coverage configuration is added;
- GitHub Actions is unchanged;
- `go test ./...` passes;
- `go test -race ./...` passes;
- project-wide line coverage remains strictly greater than 80% under the existing POLIS policy;
- `gofmt`, `go vet ./...`, `go mod verify`, build, and `polis doctor` pass;
- POLIS build, verify, and clean consumer apply pass against the exact baseline;
- final SonarQube duplication values are not claimed until a new local scan is performed.
