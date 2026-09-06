# TDD-0029 — Locked Development Baseline

## Scope

Observed Red/Green evidence for SDD-0029 on the exact repository baseline `00860d977abc9e36db88089d73d465e11ff2e6c3`.

## Slice 1 — Schema v4 model

### Red

Tests initially failed to compile because schema v4, `strict_sdd_tdd_v2`, `BaselineLock`, Specification SHA-256, and strict-development predicates did not exist.

### Green

The domain model gained schema v4 and exact lowercase-hex lock validation while retaining v1-v3 behavior.

```text
go test ./spec -run 'Locked|ChangeContractV4' -count=1
```

Result: PASS.

## Slice 2 — Git lock service

### Red

`devlock.Snapshot` and `devlock.Validate` were absent.

### Green

The service captures Git object format, HEAD, HEAD tree, committed policy bytes, and Specification digest and rejects policy or commit drift.

```text
go test ./internal/devlock -count=1
```

Result: PASS.

## Slice 3 — `polis start`

### Red

CLI acceptance initially returned:

```text
unknown command "start"
```

### Green

`devstart.Start` and the CLI command now require a clean baseline, valid committed policy v3, external draft/output paths, and exclusive output creation without source mutation.

```text
go test ./internal/devstart ./cmd/polis -run 'Start|RunStart' -count=1
```

Result: PASS.

## Slice 4 — Producer baseline enforcement

### Red

A valid locked contract was rejected by the old producer compatibility rule because Project Policy v3 accepted only Change Contract v2/v3. After compatibility was opened intentionally, a committed HEAD drift had no v4-specific lock guard.

### Green

Build accepts schema v4 and calls `devlock.Validate` before target construction. `capture-red` also validates the lock before accepting a Red proof.

Targeted build and capture tests: PASS.

## Slice 5 — Offline verification and consumer boundary

### Red

The verifier and consumer had no locked-baseline validation helpers.

### Green

Offline verification checks manifest/policy/Specification facts that do not require a repository. `preflight` and `apply` reuse `devlock.Validate` against the real consumer before and after isolated validation and map drift to baseline mismatch.

```text
go test ./internal/packageverify ./internal/packageapply -run 'Locked' -count=1
```

Result: PASS.

## Slice 6 — Schema-v4 test laundering regression

### Red

A schema-v4 feature captured a failing Red test, then replaced it with an empty test. Build incorrectly accepted the target:

```text
expected locked test immutability rejection, got <nil>
```

Root cause: strict staged-blob capture was guarded by the literal condition `schema_version == 3`.

### Green

The guard now uses `IsStrictDevelopment()`, so v3 and v4 share the same captured-test immutability contract.

```text
go test ./internal/packagebuild -run '^TestBuildLockedFeatureRejectsRedTestLaundering$' -count=1
```

Result: PASS.

## Slice 7 — Versioned JSON Schemas

### Red

The repository contained only `schemas/change-contract.schema.json`, whose title and structure described v2. A new contract test observed no aggregate references for v1-v4.

### Green

The repository now carries versioned v1-v4 Change Contract schema files and the generic file is an aggregate `oneOf`. Semantic invariants that JSON Schema cannot express remain enforced by `DecodeChangeContract`.

```text
go test ./spec -run '^TestChangeContractJSONSchemasCoverSupportedVersions$' -count=1
```

Result: PASS.
