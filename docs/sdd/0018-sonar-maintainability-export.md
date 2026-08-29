# SDD-0018 — SonarQube maintainability export remediation

Status: accepted for a behavior-preserving POLIS V4 maintenance delivery.

## Problem

The SonarQube export captured on 2026-08-29 reports 24 open code smells in POLIS:

- 15 `go:S3776` findings where cognitive complexity exceeds the configured limit of 15;
- 8 `go:S1192` findings for duplicated string literals;
- 1 `go:S107` finding for a function with 9 parameters where at most 7 are allowed.

The export reports no bugs, vulnerabilities, or security hotspots. The requested change is therefore a maintainability refactor and must not change POLIS package bytes, validation semantics, CLI behavior, failure behavior, or Git safety boundaries.

## Scope

The affected source is limited to the files identified by the export:

- `internal/packagebuild/build.go`;
- `internal/packagebuild/coverage_margin_test.go`;
- `internal/redcapture/capture.go`;
- `internal/packageapply/apply.go`;
- `internal/packageapply/defect_test.go`;
- `internal/packageverify/verify.go`;
- `spec/change.go`;
- `spec/coverage.go`;
- `spec/evidence.go`;
- `spec/evidence_contract.go`;
- `spec/policy.go`.

Documentation for this decision and its observed validation evidence is also in scope.

## Design

1. Replace the duplicated package-member and Git argument literals identified by `S1192` with named constants.
2. Replace the 9-parameter isolated-apply function identified by `S107` with a typed validation input structure.
3. Split each function identified by `S3776` into focused helpers whose individual control-flow responsibilities remain bounded.
4. Keep public types, exported function signatures, error text relied on by tests, manifest grammar, policy grammar, evidence grammar, package inventory, and Git mutation ordering unchanged.
5. Refactor tests that were themselves reported for cognitive complexity without reducing their assertions.

## Acceptance criteria

- All 24 source patterns reported in the supplied SonarQube export are structurally removed from the target source.
- No new helper introduced by the refactor has more than 7 parameters.
- The refactored routines remain at or below the configured cognitive-complexity threshold under the local structural check used for producer evidence.
- `go test ./...` passes.
- `go test -race ./...` passes.
- authoritative project-wide line coverage remains strictly greater than 80%.
- `gofmt`, `go vet ./...`, `go mod verify`, build, and `polis doctor` pass.
- POLIS package verification and consumer-side isolated application pass against the exact baseline and target tree.

## Non-goals

- No functional feature or defect behavior is changed.
- No SonarQube server configuration, quality profile, issue status, or quality gate is changed.
- Producer-side structural evidence does not claim that SonarQube has reanalyzed the target; only a new SonarQube analysis can close the remote findings.
