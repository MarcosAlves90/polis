# POLIS V4 alpha.7

POLIS V4 separates authority into three modules:

1. `guide/` — End-to-End Guide: engineering workflow, SDD/TDD, scope, safety, and evidence obligations.
2. `spec/` — POLIS Specification: normative package bytes, schemas, statuses, Change Contract, policy, coverage, integrity, and evidence semantics.
3. `cmd/polis` + `internal/` — POLIS CLI: deterministic Go reference implementation.

## Development method

Accepted behavior is specified under `docs/sdd/` before implementation. Implementation slices use observed Red -> Green -> Refactor cycles recorded in `docs/tdd/iterations.md`.

## Commands

```bash
go build -o polis ./cmd/polis
./polis doctor
./polis init --repo /path/to/repo
./polis capture-red --repo /path/to/repo --contract /outside/change.json --out /outside/regression.patch
./polis build --repo /path/to/repo --project project-slug --change change-slug --contract /outside/change.json --regression-patch /outside/regression.patch --out /path/to/output
./polis verify artifact.polis
./polis apply --repo /path/to/repo artifact.polis
```

`--regression-patch` is required only when the Change Contract kind is `defect` and is forbidden for non-defect contracts.

## Project policy

`polis init` currently supports only root-level Go modules (`auto|go`). It creates `.polis/policy.json` exclusively and never overwrites, stages, or commits it. Review and commit the policy before delivery work.

The canonical Go profile fixes:

- complete tests: `go test ./...`
- POLIS coverage producer: `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out`
- lint: `go vet ./...`
- build: `go build ./...`
- dependency integrity: `go mod verify`
- non-inferable gates: explicit `NOT_APPLICABLE` values with profile-owned reasons.

POLIS itself parses the coverprofile and requires computed line coverage strictly greater than the configured threshold. Equality with 80.0 is FAIL.

## Change Contract

Every build requires an external Change Contract (`schema_version: 1`) with exactly:

- `kind`: `feature | defect | behavior_preserving`
- `behavior`: direct command specification
- `affected`: direct command specification
- `regression`

Feature and behavior-preserving changes require `regression.mode: not_applicable` with `reason_code: not-a-defect`.

Defects require `regression.mode: red_green`, an exact regression command, a non-zero expected baseline exit code, and one or more exact output tokens. The Red state is captured before the production fix with `polis capture-red`. Build and apply reproduce that Red state in an isolated worktree and then require the same regression command to pass on the final target.

Commands are arrays of argv values and are executed directly. POLIS does not synthesize shell command strings.

## Build

`polis build`:

- requires committed, unchanged `.polis/policy.json`;
- requires the real Git index to equal `HEAD`;
- reads Change Contract and regression probe only from outside the worktree;
- captures tracked plus untracked non-ignored changes through a temporary Git index;
- never stages the source worktree;
- generates binary/full-index patches;
- for defects, applies the Red probe to the immutable baseline and requires the declared failing oracle;
- requires every path modified by the Red probe to remain part of the final payload;
- applies the final payload to a fresh detached worktree and requires exact `target_tree`;
- requires target regression Green, behavior PASS, affected PASS, and every project-policy gate in exact order;
- assembles the exact format-v2 package and verifies it again before final copy.

## Verify

`polis verify` validates package paths, inventory, JSON contracts, SHA-256 bindings, checksum grammar, regression-patch mode, and the complete evidence trace. Schema-valid but missing, reordered, duplicated, arithmetically inconsistent, or policy-inconsistent PASS evidence is rejected.

## Apply

`polis apply`:

- requires exact package verification, Git object format, `base_commit`, and a clean consumer tree/index;
- reproduces defect Red when applicable;
- validates target Green, behavior, affected, complete project policy, coverage, and exact `target_tree` in isolation;
- stores fresh evidence under Git metadata rather than the working tree;
- rechecks the baseline immediately before mutation;
- applies the payload without staging it;
- leaves `HEAD` and the real index unchanged;
- verifies post-apply working-tree identity against `target_tree`.

## Format v2

A `.polis` artifact is a ZIP containing exactly seven regular files:

- `polis/polis-manifest.json`
- `polis/polis-policy.json`
- `polis/polis-change.json`
- `polis/polis-regression.patch`
- `polis/polis-payload.patch`
- `polis/polis-evidence.ndjson`
- `polis/polis-checksums.sha256`

For non-defect changes `polis-regression.patch` is exactly zero bytes. For defects it MUST be non-empty.

## Current alpha boundary

The delivery-specific behavior/regression/affected ambiguity from alpha.6 is closed by the Change Contract and Red probe. The remaining production blocker is platform evidence: this runtime is actually executed and validated on Linux only. Cross-compilation is not treated as proof of macOS or Windows runtime behavior.
