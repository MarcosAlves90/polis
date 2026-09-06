# TDD-0028 — Requirement-to-Acceptance Proof Traceability

## Scope

Observed Red/Green evidence for SDD-0028.

## Slice 1 — Specification model

### Red

Traceability tests initially failed to compile because `Requirements`, `AcceptanceCriterion`, `ProofGateRegression`, and `TraceabilityLinks` did not exist.

### Green

The strict Specification now validates requirement references, coverage, duplicate references, and the allowed proof gate.

```text
go test ./spec -run 'Traceability|Requirement' -count=1
```

Result: PASS.

## Slice 2 — Artifact inspection

### Red

`packageverify.Inspect` had no traceability representation.

### Green

`Inspection.Traceability` is derived from the already validated Change Contract Specification.

```text
go test ./internal/packageverify -run 'Traceability' -count=1
```

Result: PASS.

## Slice 3 — CLI inspection

### Red

The CLI had no traceability renderer.

### Green

Text inspection renders one deterministic `REQ -> AC -> regression` line per derived link; JSON continues to serialize the same Inspection object.

```text
go test ./cmd/polis -run 'InspectionTextIncludesTraceability' -count=1
```

Result: PASS.
