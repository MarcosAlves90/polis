# SDD-0013 — Sonar maintainability cleanup

## Scope

Resolve the user-observed Sonar findings without changing POLIS behavior, package semantics, Git safety, path-boundary behavior, or public APIs.

Observed findings:

- `go:S1192` in `internal/packagebuild/build.go` for repeated `"rev-parse"`;
- `go:S1192` in `internal/packagebuild/build.go` for repeated `"--cached"`;
- `go:S107` in `internal/packagebuild/build.go` because `validateIsolated` accepted nine parameters;
- `go:S1192` in `internal/redcapture/capture.go` for repeated `"rev-parse"`.

## Design

- define package-local constants for repeated Git argv literals;
- group `validateIsolated` inputs into one private `isolatedValidation` value;
- keep `context.Context` explicit as the execution/cancellation boundary;
- do not add dependencies, public abstractions, or behavior changes.

## Acceptance

- each reported repeated literal appears as a string literal only once in its file;
- `validateIsolated` accepts only `context.Context` plus one private validation input;
- affected tests and the complete package set remain Green;
- `gofmt`, `go vet`, dependency integrity, race checks on changed packages, and project-wide line coverage remain PASS;
- strict coverage remains `>80.0%`.
