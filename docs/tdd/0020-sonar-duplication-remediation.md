# TDD-0020 — SonarQube duplication remediation

## Red evidence

The supplied SonarQube export reports all prior code-smell findings as closed, while duplicated-code density still exceeds the Quality Gate target. The remaining failure oracle is therefore structural duplication, not a functional defect.

A local exact-line duplication proxy over production Go files is used only as producer-side evidence to compare baseline and target. It does not replace SonarQube CPD.

## Green implementation

The target removes repeated infrastructure by extracting shared helpers for:

- Git subprocess execution and output normalization;
- temporary Git indexes and detached worktrees;
- changed-index and target-tree checks;
- build/apply isolated validation orchestration;
- external-file boundary validation and reading;
- strict JSON EOF validation.

Tests that directly exercised removed private helpers are updated to exercise the shared implementations instead.

The `.gitignore` also excludes disposable local Sonar analysis state: `.scannerwork/`, `.sonar/`, and `.sonarlint/`.

## Green evidence

Validation must demonstrate:

1. the production-source duplication proxy is materially lower than baseline and below 3%;
2. no GitHub Actions or SonarScanner/coverage configuration is part of the diff;
3. `go test ./...` passes;
4. `go test -race ./...` passes;
5. project-wide coverage remains strictly greater than 80%;
6. `gofmt -l .` produces no output;
7. `go vet ./...` passes;
8. `go mod verify` passes;
9. `go build -trimpath ./cmd/polis` and `polis doctor` pass;
10. POLIS V4 build, verify, and clean consumer apply pass against the exact baseline.
