# TDD-0026 — Strict SDD/TDD Change Contract

## Scope

Observed implementation evidence for SDD-0026. This delivery adds the opt-in strict Change Contract schema v3 path while preserving v1/v2 behavior.

The implementation itself is delivered through the current POLIS V5 Change Contract schema v2 because strict schema v3 does not exist on the immutable baseline. Consequently, the current POLIS runtime cannot encode this feature's own Red proof as a feature regression patch. The Red/Green slices below are recorded explicitly, and the final current-POLIS build/verify evidence remains authoritative for the resulting target state.

## SDD baseline

Before production implementation, `docs/sdd/0026-strict-sdd-tdd-contract.md` and the external implementation Change Contract `/mnt/data/polis-change-strict-sdd-tdd-v2.json` were created. The implementation scope was revised when tests exposed a real dependency on `internal/packageverify`; the external contract was expanded before changing that component. The normative machine-level delta is consolidated in `spec/POLIS-SPEC-v3.md` rather than redefining historical V2 semantics.

Baseline affected packages were green before implementation:

```text
go test ./spec ./internal/changeexec ./internal/redcapture ./internal/packagebuild ./internal/isolation -count=1
```

Observed result: PASS.

## Slice 1 — Change Contract schema v3

### Red

Command:

```text
go test ./spec -run 'TestChangeContractV3' -count=1
```

Observed exit code: `1`.

Behavior-specific failure began with missing strict schema types:

```text
spec/v5_contract_test.go:76:36: undefined: DevelopmentSpecification
spec/v5_contract_test.go:77:39: undefined: SpecificationClause
FAIL github.com/MarcosAlves90/polis/v5/spec [build failed]
```

### Green

After the minimum schema/validation implementation, strict schema tests and the complete `spec` package passed.

The resulting contract requires `strict_sdd_tdd_v1`, explicit `scope` and `test_scope`, a structured specification, unique non-empty clause IDs/statements, explicit command environments, and Red-to-Green regression for strict features/defects.

## Slice 2 — Strict feature Red/Green execution

### Red

Command targeted the new feature baseline and target behavior.

Observed failures:

```text
strict feature baseline: baseline regression execution is only valid for defects
strict feature target skipped regression Green
```

### Green

The execution decision was moved from `kind == defect` to `ChangeContract.RequiresRedGreen()`.

Current Green verification:

```text
go test ./internal/changeexec -run 'TestExecuteBaselineAcceptsStrictFeatureRed|TestExecuteTargetRequiresStrictFeatureRegressionGreen' -count=1
ok github.com/MarcosAlves90/polis/v5/internal/changeexec
```

Legacy/current feature contracts still return false from `RequiresRedGreen()` and retain NOT_APPLICABLE regression behavior.

## Slice 3 — Evidence v2 Red/Green trace

### Red

Observed failure:

```text
strict feature evidence rejected: event 1: gate_finished mismatch for regression
```

The validator still expected NOT_APPLICABLE regression evidence for all non-defects.

### Green

Evidence validation now uses the same `RequiresRedGreen()` semantic predicate as execution.

Current Green verification:

```text
go test ./spec -run 'TestChangeContractV3|TestValidatePassEvidenceRequiresStrictFeatureRedGreen' -count=1
ok github.com/MarcosAlves90/polis/v5/spec
```

Evidence v2 format and its no-raw-output contract were not changed.

## Slice 4 — Feature-compatible, test-only Red capture

### Red

Observed failures:

```text
strict feature capture: capture-red requires a defect change contract
expected test-scope rejection, got capture-red requires a defect change contract
```

### Green

`capture-red` now accepts contracts whose semantics require Red-to-Green. For strict schema v3 it applies the probe in isolation, enumerates staged changed paths, validates all of them against explicit `test_scope`, and then proves the existing Red exit-code/output oracle.

Current Green verification:

```text
go test ./internal/redcapture -run 'TestCaptureStrictFeature|TestSortedPathKeysReturnsStableLexicographicOrder' -count=1
ok github.com/MarcosAlves90/polis/v5/internal/redcapture
```

### Deterministic ordering sub-slice

Architecture review found that `ChangedIndexPaths` returns a map. A new test was added before the ordering helper existed.

Observed Red:

```text
internal/redcapture/capture_test.go:258:9: undefined: sortedPathKeys
FAIL github.com/MarcosAlves90/polis/v5/internal/redcapture [build failed]
```

The minimum implementation sorts changed paths lexicographically before `test_scope` validation. The targeted test and complete `redcapture` package then passed.

## Slice 5 — Regression-patch requirement follows semantics

### Red — build

A strict feature with no regression patch did not fail at the input boundary. It progressed until candidate verification and failed for the wrong reason:

```text
expected strict regression-patch requirement, got verify candidate POLIS package: validate evidence contract: event 1: command evidence does not match contract for regression
```

### Green — build input

`packagebuild` now requires a non-empty regression patch iff `RequiresRedGreen()` is true. Existing defect-specific error wording remains preserved for defects.

### Red — package verifier

The subsequent end-to-end strict build exposed another defect-only branch:

```text
strict feature proof rejected: non-defect package requires empty regression patch
```

This was treated as a plan/scope discovery. `internal/packageverify` was added to the implementation Change Contract before its production code changed.

### Green — package verifier

`packageverify` now uses the same semantic predicate to require or forbid regression-patch bytes.

Current Green verification:

```text
go test ./internal/packageverify -run 'TestValidateRegressionPatchAllowsStrictFeatureProof' -count=1
ok github.com/MarcosAlves90/polis/v5/internal/packageverify
```

## Slice 6 — Isolated Red reproduction for strict feature

### Red

The first complete strict feature fixture still failed because target isolation retained the old non-defect branch:

```text
strict feature build: verify candidate POLIS package: validate evidence contract: event 1: command evidence does not match contract for regression
```

### Green

`internal/isolation` now performs baseline Red and target Green whenever `RequiresRedGreen()` is true.

The integration fixture then successfully captured a strict feature Red probe, reproduced Red on the immutable base, applied the final production change, and proved Green on the target.

## Slice 7 — Prevent post-Red test laundering

### Red

The fixture captured a failing test, then replaced the captured test with a weakened empty test before fixing production.

Observed failure of the implementation to protect TDD integrity:

```text
expected captured Red proof immutability rejection, got <nil>
```

### Green

For strict schema v3, isolated Red validation now records the staged Git blob identity of every captured Red path. Target validation requires that path to remain in the payload and have exactly the same staged blob identity. Deleted or rewritten captured Red tests therefore fail closed.

Current Green verification includes both the legitimate and laundering paths:

```text
go test ./internal/packagebuild -run 'TestBuildStrictFeature|TestBuildPolicyV3AcceptsStrictChangeContractV3' -count=1
ok github.com/MarcosAlves90/polis/v5/internal/packagebuild
```

Legacy v1/v2 defect validation keeps its previous path-presence semantics; blob locking is not retroactively imposed on old contracts.

### Captured Red path-presence acceptance coverage

An explicit strict-feature acceptance test removes the captured Red test from the final target and requires build rejection with `is absent from final payload`. The test passed immediately because the pre-existing regression-path-presence invariant is now reached through the generalized strict Red/Green path; no additional production change was made for this case.

```text
go test ./internal/packagebuild -run '^TestBuildStrictFeatureRejectsCapturedRedPathMissingFromTarget$' -count=1
ok github.com/MarcosAlves90/polis/v5/internal/packagebuild
```

## Slice 8 — Project Policy v3 migration compatibility

### Red

A valid strict Change Contract v3 under the current Project Policy v3 was rejected before validation:

```text
policy v3 strict contract v3: policy schema v3 requires Change Contract schema v2 for new builds
```

### Green

During the opt-in migration period, Project Policy v3 build accepts either current Change Contract v2 or strict Change Contract v3. V1 remains governed by the existing migration rules.

## Slice 9 — Schema-specific diagnostics

### Red

A strict schema-v3 contract missing a regression command environment produced a stale schema-v2 message:

```text
expected schema-v3 environment error, got regression: change contract schema v2 requires explicit command environment
```

### Green

The validation error now uses the actual contract schema version. Existing schema-v2 wording remains unchanged when validating v2.

## Strict feature end-to-end consumer proof

A separate minimal Go repository exercised the new protocol using a schema-v3 strict feature contract. The source baseline committed the Project Policy and buggy production behavior, then added only the failing feature test before capture.

Observed strict Red capture:

```text
POLIS CAPTURE-RED: PASS
SHA256: 17c9831795e249ece15a5b2c06a1af2b5fe06aca3444ab3ad040d8576a8e48a3
```

After the production change, with the captured test bytes untouched, strict build and verify passed:

```text
POLIS BUILD: PASS
POLIS VERIFY: PASS
```

The exact artifact was then checked against a clean consumer at the captured baseline:

```text
POLIS PREFLIGHT: PASS
Safe to apply: yes
POLIS APPLY: PASS
```

The first auxiliary post-apply test invocation used an invalid absolute `...` package pattern from outside the module and failed before testing the program. It was corrected rather than interpreted as a product failure. From the consumer module root:

```text
go test ./... -count=1
ok example.com/strictfixture
```

Strict fixture artifact SHA-256:

```text
f1adaf1554fe79f1b747d18ea8ba4690eec1b1e29bcf1f8716c5695f4dfa6d0e
```

## Current affected Green

The complete affected set, including the package verifier dependency discovered during TDD, is green:

```text
go test ./spec ./internal/changeexec ./internal/redcapture ./internal/packageverify ./internal/packagebuild ./internal/isolation -count=1
```

Observed result:

```text
?  github.com/MarcosAlves90/polis/v5/internal/isolation [no test files]
ok github.com/MarcosAlves90/polis/v5/spec
ok github.com/MarcosAlves90/polis/v5/internal/changeexec
ok github.com/MarcosAlves90/polis/v5/internal/redcapture
ok github.com/MarcosAlves90/polis/v5/internal/packageverify
ok github.com/MarcosAlves90/polis/v5/internal/packagebuild
```

## Protected behavior

The implementation deliberately retains:

- Change Contract v1/v2 decode and regression semantics;
- v1/v2 feature regression NOT_APPLICABLE behavior;
- existing defect Red-to-Green behavior;
- package format v3 with exactly seven members;
- Evidence v2 without raw stdout/stderr;
- direct argv execution;
- exact baseline/target-tree and transactional consumer validation;
- existing Project Policy and coverage semantics.

## Deferred work

This increment does not claim implementation of:

- mandatory schema v3 for all new deliveries;
- Green-to-Green characterization proof for `behavior_preserving`;
- multi-cycle Development Ledger;
- trusted timestamps/remote witnesses;
- mutation, property, fuzz, model-based, or metamorphic proof adapters;
- prompt/chat transcript provenance.

Those require separate accepted specifications and Red/Green evidence.
