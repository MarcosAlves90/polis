# SDD-0035 — Offline POLIS V6 Runtime Export

## Status

Implemented in POLIS V6.

## Objective

Allow an offline coding agent to use POLIS without downloading the source repository or installing a programming-language runtime.

## Decision

Add `polis export --out <bundle.zip>`. The command exports the executable that is currently running together with the minimum V6 documentation and machine-readable schemas required to create, validate, and understand POLIS inputs and outputs.

The export is a deterministic ZIP runtime bundle with this layout:

```text
polis-offline/
  bin/polis[.exe]
  POLIS-OFFLINE.md
  manifest.json
  SHA256SUMS
  spec/POLIS-SPEC-v6.md
  spec/schemas/
    change-contract.schema.json
    change-contract-v3.schema.json
    change-contract-v4.schema.json
    evidence-event.schema.json
    manifest.schema.json
    policy.schema.json
    signature.schema.json
```

`manifest.json` records the POLIS version, native runtime, executable member, Git prerequisite, and the explicit `network_required: false` guarantee. `SHA256SUMS` covers every other member, and the exporter reads the archive back before publishing it.

## Rationale

A compiled Go executable removes the need for Go, Python, Node.js, Ruby, or another language runtime at the consumer. Embedding the V6 specification and schemas preserves the contract needed by an AI to construct and inspect policy, Change Contract, package, evidence, and signature data. The bundle itself performs no network access, although a project command configured in a policy may have its own network dependency. Git remains outside the bundle because it is a repository-native executable rather than a language runtime and bundling it would be platform-specific and substantially enlarge the artifact.

The bundle is portable across hosts with the same operating system and CPU architecture. It is intentionally not a universal multi-platform binary; the manifest makes a mismatch explicit before use.

## Safety invariants

- Existing output is never overwritten.
- The target repository and project-specific policy are never read by `polis export`.
- No network access is performed by the exporter.
- Bundle members are checksummed and the generated archive is read back and compared before success.
- Temporary archive state is removed on both success and failure.
- The offline runtime bundle is not confused with or accepted as a delivery `.polis` package.

## Release integration

Every GitHub release prepared by `scripts/github-release.sh` includes a native
offline bundle named `polis-<tag>-offline-<GOOS>-<GOARCH>.zip` and a release-level
`SHA256SUMS` file. The script builds `./cmd/polis` from the exact clean `HEAD`,
runs that executable's `export` command, and completes the same local
self-validation before any tag or release mutation. A release runner must use a
host matching the bundle's target OS and architecture; cross-compiled binaries
cannot be executed by the exporter on the wrong host.

Additional `--asset` files are additive and cannot replace or disable the
required offline bundle. A basename collision with the generated bundle is a
hard preflight failure.
