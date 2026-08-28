# SDD-0005 — Normative line coverage adapter

Status: Accepted for implementation (alpha.5)

## Objective

Replace the generic `coverage` command gate with a deterministic runtime-owned line-coverage measurement. A project command may produce a report; only POLIS calculates and classifies the metric.

## Policy contract

Policy schema version becomes `2`.

The canonical `coverage` gate MUST be the second project gate and MUST use mode `coverage`. It contains exactly:

- `id`: `coverage`
- `mode`: `coverage`
- `command`: direct-process command contract
- `adapter`: versioned adapter identifier
- `report`: normalized repository-relative report path
- `operator`: exactly `>`
- `threshold_percent`: JSON number >= 80.0 and <= 100.0

Alpha.5 supports exactly adapter `go-coverprofile-v1`.

`test.complete` remains mandatory `command`. Other gates retain `command | not_applicable`.

## Go coverprofile line algorithm

Input MUST begin with `mode: set`, `mode: count`, or `mode: atomic` followed by one or more coverage records in canonical Go coverprofile syntax.

For each record `file:startLine.startCol,endLine.endCol numStatements count`:

1. `startLine` and `endLine` MUST be positive and `startLine <= endLine`.
2. `count` MUST be an integer >=0.
3. Every inclusive source line from `startLine` through `endLine` is executable for this metric.
4. A source line is covered when at least one record spanning that line has `count > 0`.
5. Source identity is the exact report file string plus line number; same line numbers in different files are distinct.
6. `total_lines` is the cardinality of executable source-line identities.
7. `covered_lines` is the cardinality of covered source-line identities.
8. `line_coverage_percent = covered_lines / total_lines * 100` using floating-point arithmetic.
9. A report with zero executable lines is invalid and the coverage gate is FAIL.

This is a POLIS metric; it MUST NOT be described as Go statement coverage.

## Runtime classification

- command cannot start -> `BLOCKED`
- command timeout -> `FAIL`
- command exits non-zero -> `FAIL`
- report missing, malformed, unsupported, outside repo, or with zero executable lines -> `FAIL`
- measured percentage satisfies `>` threshold -> `PASS`
- measured percentage is <= threshold -> `FAIL`

Exactly 80.0% against threshold 80.0 therefore FAILS.

## Evidence

After a successful coverage command, runtime emits exactly one `coverage_measured` event before `gate_finished` containing:

- `event`: `coverage_measured`
- `gate`: `coverage`
- `status`: `PASS | FAIL`
- `adapter`
- `report`
- `metric`: `line_coverage_percent`
- `covered_lines`
- `total_lines`
- `value_percent`
- `operator`: `>`
- `threshold_percent`

## Acceptance criteria

- alpha.4 policy with coverage mode `command` is rejected.
- unknown coverage adapter is rejected.
- threshold below 80, above 100, or operator other than `>` is rejected.
- malformed/empty coverage reports fail closed.
- 80.0% > 80.0 is FAIL.
- 80.01% > 80.0 is PASS.
- coverage metric evidence is schema-valid and derived by the runtime.
- command execution remains direct and shell-free.
