# SDD-0021 — SonarQube gitutil error-format remediation

Status: accepted for a behavior-preserving POLIS V4 maintenance delivery.

## Problem

The SonarQube analysis after SDD-0020 reports one new open maintainability finding:

- `go:S1192` in `internal/gitutil/gitutil.go` for the literal `"%s: %w"` appearing three times.

The finding was introduced when duplicated Git infrastructure was consolidated into `internal/gitutil`. It does not represent a behavioral defect.

## Design

Introduce one private constant for the shared wrapped-error format and reuse it at all three reported call sites.

## Acceptance criteria

- `"%s: %w"` occurs once in production source;
- rendered error text remains unchanged;
- `go test ./...` and `go test -race ./...` pass;
- project-wide coverage remains strictly greater than 80%;
- `gofmt`, `go vet ./...`, `go mod verify`, build, and `polis doctor` pass;
- POLIS build, verify, and clean consumer apply pass against the exact baseline.
