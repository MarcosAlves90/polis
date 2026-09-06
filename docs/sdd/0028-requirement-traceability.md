# SDD-0028 — Requirement-to-Acceptance Proof Traceability

## Status

Accepted and implemented for strict Change Contract schema v3/v4.

## Problem

A structured Specification can contain requirements and acceptance criteria while still leaving requirements orphaned or leaving the relationship between requirement and executable proof implicit.

## Objective

Make every strict requirement trace to at least one acceptance criterion and make every acceptance criterion declare the executable proof gate that validates it.

## Contract

A strict `specification` MUST contain non-empty `requirements` and `acceptance_criteria`.

Each requirement contains a globally unique non-empty `id` and non-empty `statement`.

Each acceptance criterion MUST contain:

- non-empty unique `id` and `statement`;
- `requirements` with at least one unique requirement ID;
- `proof: "regression"`.

Every referenced requirement MUST exist and every requirement MUST be referenced by at least one acceptance criterion.

`TraceabilityLinks()` derives links from the validated Specification; no independent traceability table exists.

## Inspection

`polis inspect` exposes the derived links. Text output uses:

```text
Trace: REQ-001 -> AC-001 -> regression
```

JSON output uses the same `Inspection.Traceability` data.

## Acceptance criteria

1. Orphan requirements are rejected.
2. Unknown and duplicate requirement references are rejected.
3. Unsupported proof gates are rejected.
4. Valid links are returned deterministically.
5. Legacy contracts expose no traceability links.
6. `inspect` text and JSON derive traceability from the decoded Change Contract.

## Invariants

- The Change Contract Specification is the single authority for traceability.
- POLIS does not judge natural-language requirement quality.
- No new Evidence event is introduced solely to duplicate the existing regression proof.
