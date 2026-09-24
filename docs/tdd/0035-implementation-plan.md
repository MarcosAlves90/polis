# TDD 0035 — Optional Contract-Bound Implementation Plans

## Red

Reviewing the new plan validator against SDD-0040 exposed three missing
invariants. The focused regression probe failed for excessive JSON nesting,
serialized steps that differed from their canonical topological order, and a
missing final validation step for both Red-to-Green and Green-to-Green plans.
The observed command was:

```text
go test ./spec -run 'TestDecodeImplementationPlanRejectsExcessiveJSONNesting|TestImplementationPlanRequiresSerializedOrderToMatchTopologicalOrder|TestImplementationPlanValidatesAgainstContractSemantics/final_validation_missing' -count=1
```

## Green

The decoder now rejects nesting beyond 64 levels before recursive JSON
processing. Plan validation requires serialized order to match deterministic
topological order and requires a final validation step with the contract proof
references for its strategy. Regression coverage also exercises malformed
references, dependency graphs, path scope, locked baselines, output boundaries,
creation failures, and planned package integration.

The focused spec and generator/creator suite passed 79 selected tests. A
cross-package plan and package-build integration run passed 49 selected tests.
The broad project test run then exposed an obsolete package-version test that
still treated format v6 as unsupported; it was updated to reject v7 instead.
Review against the published JSON Schema also exposed `encoding/json` accepting
`null` for optional arrays and case-insensitive property aliases. The plan
decoder now rejects both forms so its accepted object shape matches the schema.
The v6-only manifest digest is also rejected by presence on earlier formats,
including a `null` value, and manifest property names are checked exactly.

Further boundary review found that repeating long requirement prose in every
generated objective could push an otherwise valid plan beyond the 1 MiB member
limit. Objectives now cite requirement and acceptance IDs, and the encoder
rejects oversized plans before writing. A 600 KiB criterion fixture confirms
the generated plan stays within the limit. End-to-end coverage now exercises
Red-to-Green capture/build/verify/inspect and Green-to-Green CLI create/build/
verify/inspect/preflight/apply flows. Integrity checks cover independent plan
digest and checksum mismatches, plan tampering, and detached-signature rejection
after a self-consistent package rewrite. Compatibility coverage includes
unplanned format v2, v4, and v5 artifacts.

## Validation

- `go test ./...` passed; the latest full run covered 646 tests in 23 packages.
- `go test -race ./...` passed; all 646 tests passed across 23 packages.
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` passed.
- Project line-union coverage: `6219 / 7740 = 80.348837%`, above the strict
  `>80%` gate. The threshold was not changed.
- `go vet ./...`, `go build ./...`, `go mod verify`, `gofmt -l .`, and
  `git diff --check` passed.
- Offline export succeeded and its ZIP CRC check passed; the exported archive
  includes both new schemas.
- `npm run verify` could not run because this Go repository has no
  `package.json`.
