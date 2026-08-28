# SDD-0001 — POLIS V4 foundation

Status: Accepted for implementation (alpha)

## Objective

Split POLIS V4 into three contracts with one-way responsibility:

1. End-to-End Guide: tells an AI/engineer when and why to use POLIS and which engineering evidence is required.
2. POLIS Specification: defines the exact machine contract for package structure, schemas, status semantics, and exit codes.
3. POLIS CLI: deterministic reference implementation of the specification.

## Invariants

- The Guide MUST NOT redefine package layout, schema fields, hashes, or exit-code semantics owned by the Specification.
- The Specification MUST NOT depend on a particular model/provider.
- The CLI MUST NOT infer success from prose; it computes results from bytes, schemas, Git state, commands, and exit codes.
- A `.polis` file is a ZIP container with exactly one root directory named `polis/`.
- Alpha package members are exactly:
  - `polis/polis-manifest.json`
  - `polis/polis-policy.json`
  - `polis/polis-payload.patch`
  - `polis/polis-evidence.ndjson`
  - `polis/polis-checksums.sha256`
- Unknown package members are invalid in format version 1.
- Paths MUST be relative, normalized, use `/`, and MUST NOT contain `..`, absolute paths, drive-letter paths, or links.
- `polis-checksums.sha256` hashes every other regular member and MUST NOT hash itself.
- Manifest JSON rejects unknown properties.
- Gate status enum is exactly: `PASS | FAIL | BLOCKED | NOT_APPLICABLE`.
- Overall PASS is computed by the runtime, never authored as trusted input.

## CLI alpha acceptance criteria

### `polis doctor`
- Prints CLI version, OS/architecture, Go runtime, and Git availability/version.
- Exits 0 only when mandatory local prerequisites for read-only package verification are available.

### `polis verify <artifact.polis>`
- Rejects a non-ZIP input.
- Rejects archives with entries outside `polis/`.
- Rejects missing or extra members.
- Rejects duplicate member names.
- Rejects directory traversal or absolute paths.
- Rejects malformed manifest JSON and unknown manifest fields.
- Rejects unsupported `format_version`.
- Rejects malformed checksum lines.
- Rejects checksum mismatches.
- On success prints `POLIS VERIFY: PASS` and exits 0.
- Verification MUST NOT modify a target repository.

### `polis build` and `polis apply`
- Command names are reserved in alpha.
- Until their complete contracts are implemented, they MUST fail closed with an explicit `NOT_IMPLEMENTED` diagnostic and non-zero exit.

## Exit codes (alpha)

- 0: PASS
- 2: INVALID_INPUT_OR_PACKAGE
- 3: NOT_IMPLEMENTED
- 4: BLOCKED_ENVIRONMENT
- 1: unexpected internal failure

## TDD slices

1. Manifest strict decoding and semantic validation.
2. Archive inventory/path/checksum verification.
3. CLI doctor and verify behavior.
4. Reserved build/apply fail-closed behavior.
5. Later: canonical policy execution.
6. Later: Git baseline isolation/build.
7. Later: transactional apply.
