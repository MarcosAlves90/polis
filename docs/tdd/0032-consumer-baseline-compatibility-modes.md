# TDD-0032 — Consumer Baseline Compatibility Modes

## Scope

Implement SDD-0036 without weakening the default strict consumer baseline or
changing `.polis` package bytes/schemas.

## Red 1 — packageapply had no compatibility-mode contract

Tests were added first for `strict`, `compatible`, and `permissive` consumer
behavior. The targeted test command failed at compile time because
`ApplyWithOptions`, `PreflightWithOptions`, `Options`, `BaselineMode`, and the
mode constants did not exist. This established that the requested consumer
compatibility surface was absent rather than accidentally already supported.

## Green 1 — explicit baseline assessment and dynamic consumer target

The implementation added:

- strict-compatible public wrappers plus option-aware apply/preflight entry
  points;
- explicit parsing for `strict`, `compatible`, and `permissive`;
- a baseline assessment separated from mutation;
- descendant ancestry proof for `compatible`;
- explicit non-ancestry risk reporting for `permissive`;
- exact `git apply --check` against the observed consumer tree;
- a temporary-index consumer target tree derived from observed `HEAD` + exact
  payload;
- isolated target validation based on the admitted consumer `HEAD`, while
  baseline development proof still runs on the artifact's locked base;
- a second exact consumer-HEAD/clean-state check before real mutation.

Targeted packageapply tests then passed, including descendant acceptance,
conflict rejection, non-descendant compatible rejection, controlled permissive
acceptance, permissive conflict rejection, read-only compatible preflight, a
non-overlapping edit in a payload file, and assessed-HEAD drift rejection.

## Red 2 — CLI did not expose the modes

CLI tests were added for `--baseline-mode compatible` and invalid-mode handling.
They failed because `preflight` and `apply` did not define the flag.

## Green 2 — CLI admission and diagnostics

`preflight` and `apply` now accept
`--baseline-mode strict|compatible|permissive`, defaulting to `strict`.
Non-strict successful output reports the mode, observed consumer base,
compatibility reason, and risk warnings when present. Strict output keeps the
historical surface unchanged.

The targeted CLI tests passed after the implementation.

## Full validation

Observed validation after implementation:

```text
go test ./... -count=1
PASS (all packages)

go test -coverpkg=./... ./... -coverprofile=/tmp/polis-coverage.out
go tool cover -func=/tmp/polis-coverage.out | tail -1
total: (statements) 86.1%

go vet ./...
PASS

go build ./...
PASS

go mod verify
all modules verified

git diff --check
PASS
```

The repository's strict coverage contract remains `> 80.0%`; the observed
86.1% result preserves that margin.
