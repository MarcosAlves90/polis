# POLIS Package Specification v1 — 4.0.0

This specification is normative for `.polis` package bytes and machine-level runtime semantics. The End-to-End Guide references this contract and MUST NOT redefine it.

## 1. Container — format version 2

A `.polis` artifact is a ZIP archive with exactly seven regular files:

1. `polis/polis-manifest.json`
2. `polis/polis-policy.json`
3. `polis/polis-change.json`
4. `polis/polis-regression.patch`
5. `polis/polis-payload.patch`
6. `polis/polis-evidence.ndjson`
7. `polis/polis-checksums.sha256`

No additional members are valid. Paths MUST be normalized relative POSIX-style paths under `polis/`. Absolute paths, backslashes, drive-letter paths, empty segments, `.`/`..` traversal segments, duplicate names, directories, symbolic links, hard links, devices, sockets, and FIFOs are invalid.

For `kind: defect`, `polis-regression.patch` MUST be non-empty. For every other kind it MUST be exactly zero bytes.

## 2. Checksums

Every non-checksum member appears exactly once in `polis/polis-checksums.sha256`:

`<64 lowercase SHA-256 hex><two spaces><relative archive member path>\n`

Entries are sorted by member path ascending. The checksum file does not hash itself.

## 3. Manifest

`polis-manifest.json` conforms to `schemas/manifest.schema.json`, rejects unknown properties, and contains exactly:

- `format_version: 2`
- `project`
- `change`
- `git_object_format: sha1 | sha256`
- `base_commit`
- `target_tree`
- `policy_sha256`
- `change_contract_sha256`
- `regression_patch_sha256`
- `payload_sha256`

Git object IDs are 40 lowercase hexadecimal characters for SHA-1 repositories and 64 for SHA-256 repositories. The four SHA-256 fields bind the exact embedded policy, Change Contract, regression patch, and payload bytes.

## 4. Change Contract v1

`polis-change.json` conforms to `schemas/change-contract.schema.json`, has `schema_version: 1`, rejects unknown properties, and defines delivery-specific validation independently from the project policy.

Fields:

- `kind: feature | defect | behavior_preserving`
- `behavior`: command
- `affected`: command
- `regression`

A command has exactly `argv`, `cwd`, and `timeout_seconds`. The runtime invokes `argv[0]` directly; no shell is inserted.

### 4.1 Non-defect regression

Feature and behavior-preserving changes require exactly:

```json
{"mode":"not_applicable","reason_code":"not-a-defect"}
```

The package regression patch is zero bytes.

### 4.2 Defect regression

Defects require:

- `mode: red_green`
- `command`
- `baseline_exit_code` in `1..255`
- non-empty, unique `baseline_output_contains`

Before the production fix, `polis capture-red` captures the complete non-ignored worktree delta with a temporary Git index and proves in a detached baseline worktree that the captured patch applies and the regression command exits with exactly `baseline_exit_code` while combined stdout/stderr contains every declared token.

During build and apply the runtime MUST independently reproduce:

1. baseline + regression probe -> declared Red oracle;
2. baseline + final payload -> exact `target_tree`;
3. the regression command on final target -> PASS;
4. `behavior` -> PASS;
5. `affected` -> PASS.

Every path modified by the regression probe MUST also be modified by the final payload. This prevents a disposable Red-only helper from disappearing from the delivered target.

## 5. Project policy v2

`polis-policy.json` conforms to `schemas/policy.schema.json` with all eleven project gates present exactly once and in canonical order:

1. `test.complete`
2. `coverage`
3. `lint`
4. `typecheck`
5. `build`
6. `smoke`
7. `compatibility`
8. `dependency`
9. `migration`
10. `security`
11. `platform`

`test.complete` is always `command`; `coverage` is always `coverage`; remaining gates explicitly choose `command` or `not_applicable`.

Command execution status is runtime-owned:

- exit 0 -> PASS
- non-zero -> FAIL
- timeout -> FAIL
- process cannot start -> BLOCKED
- declared policy `not_applicable` -> NOT_APPLICABLE

The runtime never interprets shell syntax in argv elements.

## 6. Normative line coverage

Coverage contains an exact producer command, adapter, repository-relative report, operator `>`, and threshold in `80..100`.

Alpha.7 supports `go-coverprofile-v1`. The canonical Go bootstrap command is:

`go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out`

`-coverpkg=./...` is required by the canonical profile so code exercised through cross-package integration tests is instrumented as part of project-wide measurement.

Before the command, the runtime removes an old report. A new report MUST be a regular file resolving within the repository and no larger than 16 MiB.

POLIS computes:

1. each inclusive line spanned by a profile record is executable;
2. identity is exact profile filename plus line number;
3. a line is covered if any spanning record has count > 0;
4. `total_lines` is the number of distinct executable identities;
5. `covered_lines` is the number of distinct covered identities;
6. `line_coverage_percent = covered_lines / total_lines * 100`;
7. zero executable lines is invalid;
8. PASS requires `line_coverage_percent > threshold_percent`; equality FAILS.

The runtime also validates that stored coverage evidence is arithmetically consistent with covered and total lines.

## 7. Evidence

`polis-evidence.ndjson` is UTF-8 NDJSON. Each non-empty line satisfies `schemas/evidence-event.schema.json`.

For a package claiming PASS, schema validity is necessary but insufficient. `verify` reconstructs the exact expected trace from the embedded Change Contract and project policy.

Canonical order is:

- regression baseline Red trace for defects, or canonical regression `NOT_APPLICABLE` for non-defects;
- target regression Green for defects;
- behavior PASS;
- affected PASS;
- every project-policy gate in canonical order, including coverage measurement.

Missing, extra, reordered, duplicated, command-mismatched, reason-mismatched, threshold-mismatched, or arithmetically inconsistent events invalidate the package.

## 8. `polis init`

`polis init [--repo <path>] [--profile auto|go]` currently recognizes only a root-level Go module. It exclusively creates `.polis/policy.json`; it never overwrites, stages, commits, resets, or cleans.

The Go profile defines:

- `go test ./...`
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out`
- `go vet ./...`
- `go build ./...`
- `go mod verify`
- explicit profile-owned `NOT_APPLICABLE` values for gates that cannot be inferred safely.

The generated policy MUST be reviewed and committed before `polis build`.

## 9. `polis capture-red`

`polis capture-red --repo <path> --contract <change.json> --out <regression.patch>` is valid only for a defect Change Contract.

Preconditions include a Git worktree with HEAD, real index equal to HEAD, non-empty non-ignored working changes, external regular Change Contract, and a non-existing output path outside the worktree.

The runtime captures through a temporary index, writes no source staging, and validates the exact Red oracle in an isolated worktree before exclusively publishing the patch. Source HEAD, index, and status MUST remain unchanged.

## 10. Build

`polis build` accepts an external Change Contract and, for defects, an external regression patch. Both must be outside the worktree. The committed project policy is immutable input to the delivery.

Build captures the final source delta through a temporary index, validates regression Red when applicable, validates target Green/behavior/affected, validates every project-policy gate, verifies exact target tree, packages format-v2 bytes, and runs canonical package verification on the candidate before final copy.

Evidence contains runtime output and duration, so logically identical source changes are not required to produce byte-identical archives. The SHA-256 filename suffix identifies exact archive bytes, not a semantic change identity.

## 11. Verify

`polis verify` validates archive structure and member types, schemas, manifest digests, checksum grammar, regression-patch mode, and exact PASS evidence trace. It does not trust a status merely because JSON is syntactically valid.

## 12. Apply

Before consumer mutation, `polis apply` requires package verification, matching Git object format, exact `HEAD == base_commit`, and a clean worktree/index. It then independently re-executes the defect Red probe when applicable, final regression Green, behavior, affected, complete project policy, coverage calculation, and target-tree verification in isolated worktrees.

Fresh evidence is written under Git metadata. The baseline is rechecked before real mutation. The payload is applied without staging; HEAD and the real index remain unchanged. A temporary index computes post-apply tree identity, which MUST equal `target_tree`.

## 13. Platform evidence boundary

Alpha.7 is runtime-validated on Linux in the current development environment. Producing binaries for other GOOS/GOARCH targets is not evidence that build/verify/apply behavior has been executed successfully there.
