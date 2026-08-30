# POLIS Specification v2 — V5 contracts

This specification defines the machine-level changes introduced by POLIS V5. The unchanged transactional and exact-tree rules from POLIS Specification v1 remain normative unless explicitly superseded here.

## Package format v3

A `.polis` archive still contains exactly seven regular files under `polis/`: manifest, policy, Change Contract, regression patch, payload patch, evidence NDJSON, and checksums. Format v3 adds bounded parsing requirements; it does not add executable members.

A verifier MUST reject an archive larger than 64 MiB, a canonical member above its member limit, or aggregate declared uncompressed canonical content above 64 MiB before an unbounded read. Contract members are limited to 1 MiB each, evidence to 16 MiB, payload/regression patches to 32 MiB each, and checksums to 64 KiB. Evidence lines are limited to 1 MiB.

## Project Policy schema v3

Policy v3 retains the canonical eleven gates and strict coverage threshold semantics. Every executable command MUST declare an `environment` object. `mode=inherit` passes the process environment explicitly. `mode=clean` passes only variables named in `pass`; environment values are runtime inputs and MUST NOT be serialized into policy or evidence.

Coverage adapters are format contracts: `go-coverprofile-v1`, `lcov-v1`, and `cobertura-v1`.

`go-coverprofile-v1` computes its executable-line universe from the exact Go coverprofile bytes. Go toolchain releases may change instrumentation boundaries for unchanged source, so a PASS produced by one Go version is not evidence that another Go version will produce the same coverage percentage. Producer and consumer still evaluate their own fresh report and fail closed. Portable release claims therefore require validation on the claimed Go toolchain(s) or sufficient project coverage margin; the runtime MUST NOT lower the configured threshold to compensate for toolchain drift.

Policy schema v2 remains readable for V4→V5 migration.

## Change Contract schema v2

V2 adds `scope.allowed_paths` and explicit command environments. `.` authorizes the repository; a value ending in `/` authorizes that normalized directory prefix; any other value authorizes only the exact repository-relative path. Build and consumer validation MUST calculate the base-to-target Git changed path set with rename detection disabled and MUST reject any changed source or destination outside scope.

Change Contract v1 remains readable for migration packages. A policy-v3 build MUST use Change Contract v2.

## Evidence v2

V5-generated `command_finished` evidence MUST NOT contain raw stdout or stderr. It records byte counts, SHA-256 digests, truncation flags, exit code, duration, argv, cwd, and the declared environment mode/pass names. Runtime capture retained for diagnostics/oracles is bounded to 1 MiB per stream while hashes and byte counts cover all bytes received.

For defect Red evidence, each successfully proven `baseline_output_contains` oracle emits an `oracle_checked` event containing only the oracle type and index. If output was truncated before an oracle can be proven, validation fails closed.

## Detached signatures v1

Signatures are outside the `.polis` archive. A signature document uses `ed25519-sha256-v1`: Ed25519 signs the 32-byte SHA-256 digest of the exact artifact bytes. The document records the artifact digest and SHA-256 fingerprint of the PKIX-encoded public key. The consumer supplies the trusted public key; artifact contents never define trust roots.

## Consumer preflight

`polis preflight` performs package/signature verification when requested, exact baseline checks, isolated regression/target validation, policy gates, exact target-tree validation, scope validation, a second baseline check, and a real `git apply --check`. It MUST NOT apply the payload to consumer files. A later `apply` MUST repeat validation rather than reuse a preflight PASS.
