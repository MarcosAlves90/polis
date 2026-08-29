# TDD-0019 — Residual SonarQube string-literal remediation

## Red evidence

The follow-up SonarQube export contains one open finding after the previous maintainability refactor:

- rule `go:S1192`;
- file `spec/policy.go`;
- message: define a constant instead of duplicating `"%s must be a string"` three times.

The failure oracle is maintainability-only; production behavior is not expected to change.

## Green implementation

The target introduces a private constant for the shared format string and uses it at each reported call site.

## Green evidence

Validation must demonstrate:

1. `go test ./spec` passes;
2. `go test ./...` passes;
3. `go test -race ./...` passes;
4. project-wide line coverage remains strictly greater than 80%;
5. `gofmt -l .` produces no output;
6. `go vet ./...` passes;
7. `go mod verify` passes;
8. `go build -trimpath ./cmd/polis` and `polis doctor` pass;
9. the duplicated error literal occurs once in `spec/policy.go`;
10. POLIS V4 build, verify, and clean consumer apply pass against the exact baseline.
