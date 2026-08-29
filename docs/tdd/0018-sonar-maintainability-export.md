# TDD-0018 — SonarQube maintainability export remediation

## Red evidence

The supplied SonarQube export is the failing maintainability oracle for this behavior-preserving change. It contains 24 open code smells:

- 15 `go:S3776` cognitive-complexity findings;
- 8 `go:S1192` duplicated-literal findings;
- 1 `go:S107` excessive-parameter finding.

The findings identify the exact source files, rules, lines, and configured limits. No production behavior failure is claimed.

## Green implementation

The target source removes the reported patterns through behavior-preserving decomposition:

- duplicated package-member and Git argument strings are named constants;
- isolated apply validation receives a typed structure instead of 9 parameters;
- the reported high-complexity functions are split into smaller validation, parsing, IO, and orchestration helpers;
- the two reported test functions are split into focused fixtures/assertion helpers while preserving their coverage.

## Green evidence

Producer validation must include:

1. focused tests for each affected package;
2. `go test ./...`;
3. `go test -race ./...`;
4. `go test -coverpkg=./... ./... -coverprofile=<report>` with POLIS line-union arithmetic and strict `>80%` threshold;
5. `gofmt -l .` with no output;
6. `go vet ./...`;
7. `go mod verify`;
8. `go build -trimpath ./cmd/polis` and `polis doctor`;
9. a source-level structural check confirming the reported duplicated literals, excessive parameter signature, and high-complexity source shapes are absent;
10. POLIS V4 verification and consumer application against the exact baseline.

A SonarQube CLI/server rerun is not available in the producer environment. Therefore the delivery records structural remediation and local validation, but does not claim that the remote SonarQube issue state has already changed.
