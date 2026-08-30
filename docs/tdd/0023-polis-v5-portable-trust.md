# TDD-0023 — POLIS V5 Portable Trust Boundary

## Planned Red → Green slices
1. Archive limits reject oversized declared members and oversized archive bytes.
2. Change Contract v2 rejects missing/invalid scope and scope matching handles exact paths, directory prefixes and global `.`.
3. Package build rejects an out-of-scope changed file; isolated consumer validation rechecks scope.
4. `clean` command environment prevents undeclared variables from reaching the child while allowlisted variables remain available.
5. Command evidence encodes digests/byte counts and no raw stdout/stderr; defect baseline emits oracle evidence.
6. LCOV and Cobertura parsers compute authoritative unique line coverage and reject malformed/empty reports.
7. Preflight validates without changing consumer working-tree/index/HEAD.
8. Inspect returns canonical identity and contract/policy metadata.
9. Detached Ed25519 signature accepts exact bytes and rejects tampered artifact/signature/untrusted key.
10. JSON CLI output is parseable and stable for doctor/verify/inspect/preflight.
11. Existing apply/build/verify invariants remain green.
12. A Go 1.27 consumer regression that measured 79.626% despite the producer passing at 81.191% is addressed by expanding behavior/error-path coverage while preserving the strict >80% policy; the producer target now maintains a materially larger coverage margin and documents `go-coverprofile-v1` toolchain sensitivity.

## Validation order
- New unit tests per slice.
- Affected package tests.
- Complete `go test ./...`.
- Project policy coverage command and strict >80% evaluation.
- `go vet ./...`.
- `go build ./...`.
- `go mod verify`.
- Build migration package with external POLIS V4 binary and verify exact bytes with V4.
