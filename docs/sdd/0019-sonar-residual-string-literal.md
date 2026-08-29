# SDD-0019 — Residual SonarQube string-literal remediation

Status: accepted for a behavior-preserving POLIS V4 maintenance delivery.

## Problem

A follow-up SonarQube export captured after SDD-0018 reports 25 findings in total. Of those, 24 are `FIXED/CLOSED` and one remains `OPEN`:

- `go:S1192` in `spec/policy.go`, reporting that the literal `"%s must be a string"` is duplicated three times.

The remaining finding is a maintainability issue only. No bug, vulnerability, security hotspot, or behavioral defect is asserted by the export.

## Scope

The production-code change is limited to `spec/policy.go`.

## Design

1. Introduce one private package constant for the shared error format.
2. Reuse that constant at the three call sites identified by SonarQube.
3. Preserve the exact rendered error text and all public API and validation behavior.

## Acceptance criteria

- the literal `"%s must be a string"` occurs only once in production source;
- focused `spec` tests pass;
- the complete project test suite passes;
- project-wide line coverage remains strictly greater than 80%;
- `gofmt`, `go vet ./...`, `go mod verify`, build, POLIS package verification, and consumer application pass;
- no claim is made that the remote SonarQube issue is closed until SonarQube reanalyzes the committed target.
