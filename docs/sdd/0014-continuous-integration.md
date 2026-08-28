# SDD-0014 — Continuous integration

Status: accepted for POLIS V4.0.0 repository integration.

## Objective

Add a deterministic GitHub Actions CI contract that continuously verifies POLIS on integration boundaries without creating a second release or delivery authority.

## Required behavior

- CI MUST run for pushes to `main`, pull requests, and explicit manual dispatch.
- The workflow MUST use read-only repository contents permission.
- Third-party GitHub Actions MUST be pinned to full commit SHAs.
- The ordinary test job MUST execute on GitHub-hosted Ubuntu, macOS, and Windows runners using the Go version declared by `go.mod`.
- Every platform job MUST execute the complete Go test suite, build the CLI with `-trimpath`, and execute `polis doctor` natively.
- A Linux quality job MUST enforce `gofmt`, `go vet`, `go mod verify`, the Go race detector, and authoritative project-wide line coverage strictly greater than 80.0%.
- Coverage MUST use `go test -coverpkg=./... ./...` and POLIS line-union arithmetic. Exactly 80.0% remains FAIL.
- CI MUST fail visibly; meaningful test or quality failures MUST NOT be hidden behind retries or continue-on-error behavior.
- CI MUST NOT publish releases, push commits/tags, deploy, or require write permissions.

## Test contract

`internal/cicontract/ci_test.go` protects the workflow's mandatory triggers, platform matrix, validation commands, strict coverage semantics, least-privilege permission, and full-SHA action pinning. It intentionally avoids a YAML library dependency: GitHub remains the authoritative workflow parser, while the repository test protects the project-specific CI contract.

## Evidence boundary

Local tests can prove the checked workflow contract and all repository behavior after the patch. Actual GitHub-hosted runner execution is established only after this workflow is committed/pushed and GitHub Actions reports the resulting run. A green local delivery MUST NOT be represented as proof that a future GitHub-hosted run occurred.
