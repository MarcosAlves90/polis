# SDD-0025 — Explicit Custom Policy Initialization

## Status

Accepted for implementation under POLIS V5.

## Problem

`polis init` currently bootstraps a Project Policy only when a root-level `go.mod` proves the canonical Go profile. Repositories that are not Go projects cannot create `.polis/policy.json`, so they cannot enter the normal `polis build` workflow even when the repository owner already knows the exact test and coverage commands.

## Objective

Add an explicit `custom` initialization profile that can bootstrap a valid Project Policy schema v3 for any Git worktree without inferring commands from language or ecosystem files.

## Observable CLI contract

The new profile is selected explicitly:

```text
polis init --repo . --profile custom \
  --test-argv npm \
  --test-argv test \
  --coverage-argv npm \
  --coverage-argv run \
  --coverage-argv coverage \
  --coverage-adapter lcov-v1 \
  --coverage-report coverage/lcov.info
```

Supported custom-only inputs:

- repeated `--test-argv <arg>`; every occurrence appends exactly one argv element;
- repeated `--coverage-argv <arg>`; every occurrence appends exactly one argv element;
- `--coverage-adapter <go-coverprofile-v1|lcov-v1|cobertura-v1>`;
- `--coverage-report <repo-relative-path>`;
- optional `--coverage-threshold <percent>`;
- optional `--dry-run`.

## Required behavior

1. `custom` works in any Git worktree, including repositories without `go.mod`.
2. Test and coverage commands are stored and later executed as direct argv. POLIS MUST NOT synthesize shell commands or split individual arguments.
3. Literal arguments containing whitespace or shell-like tokens such as `&&` remain single argv elements.
4. `--test-argv` and `--coverage-argv` each require at least one non-empty argument.
5. `--coverage-adapter` is mandatory for `custom` and must be one of the coverage adapters already supported by the POLIS Specification.
6. `--coverage-report` is mandatory for `custom` and must satisfy the existing repository-relative path contract.
7. `--coverage-threshold` defaults to `80.0`; an explicit value must satisfy the existing policy threshold contract. The coverage operator remains strict `>`.
8. The generated Project Policy uses schema version 3 and exactly the eleven canonical gates.
9. `test.complete` is a command gate using the supplied test argv.
10. `coverage` is a coverage gate using the supplied coverage argv, explicit adapter/report, strict `>` operator, and the requested/default threshold.
11. The remaining nine gates are `not_applicable` with non-empty reasons because no explicit command was supplied for them.
12. Custom-only flags are rejected unless `--profile custom` is selected.
13. `--profile auto` remains fail-closed and detects only canonical profiles that POLIS can prove. In V5 that remains the root-level Go profile. Its unsupported-repository error must recommend explicit `--profile custom`.
14. The existing explicit and auto-detected Go profile remains semantically unchanged.
15. `--dry-run` generates and self-validates the exact policy bytes but performs no filesystem or Git mutation. CLI dry-run writes only the generated policy JSON to stdout so it can be reviewed or redirected.
16. A non-dry-run invocation never overwrites an existing `.polis/policy.json`.
17. A dry-run may inspect a repository that already contains `.polis/policy.json`; it must not modify or replace that file.
18. All input validation and generated-policy self-validation complete before any `.polis` filesystem mutation.

## Invariants

- Minimum coverage threshold is not lowered below the POLIS Specification minimum (`80.0`).
- No dependencies are installed.
- No test or coverage command is inferred from `package.json`, `pyproject.toml`, `Cargo.toml`, `*.csproj`, or similar ecosystem metadata.
- No plugin architecture, automatic language profile registry, shell execution layer, or speculative profile is introduced.
- `init` does not stage, commit, reset, checkout, or otherwise mutate `HEAD`, index, or existing worktree files beyond creating a new `.polis/policy.json` in non-dry-run mode.
- Project Policy, gate registry, coverage adapter, path, and threshold validation remain governed by the existing POLIS V5 specification implementation.

## Failure semantics

`polis init` fails without writing a policy when any of the following is true:

- target is not a Git worktree;
- profile is unknown;
- explicit Go profile lacks root-level `go.mod`;
- custom-only input is used outside `custom`;
- required custom argv, adapter, or report input is missing;
- any argv element is empty;
- coverage adapter, report, or threshold violates the existing Project Policy contract;
- generated policy fails self-validation;
- `.polis/policy.json` already exists in non-dry-run mode.

## Protected behavior

The canonical Go profile retains its current gate commands, timeouts, clean-environment contract, reasons, coverage adapter/report, strict operator, and threshold. `auto` does not gain heuristic ecosystem detection.

## Validation

Implementation is complete only when:

- unit and CLI tests cover the custom profile, literal argv preservation, invalid inputs, auto fail-closed guidance, dry-run mutation guarantees, overwrite protection, and Go-profile regression;
- the repository-required Go formatting, affected tests, complete tests, coverage policy, vet, build, module verification, and `git diff --check` pass;
- a non-Go Git fixture can bootstrap a custom policy, commit it, execute `test.complete` and coverage, build a real `.polis`, and pass canonical `polis verify`.
