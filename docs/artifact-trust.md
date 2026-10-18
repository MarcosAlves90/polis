# Artifact trust and offline runtime

## Artifact and trust boundaries

The verifier treats `.polis` bytes as untrusted input and enforces bounded
archive, contract, evidence, patch, and event sizes.

Change Contract scopes are checked against the Git base-to-target path set.
Rename sources and destinations are both checked.

`polis sign` creates a detached Ed25519 signature over the artifact SHA-256.
Consumers must supply both `--signature` and `--trusted-key`; the package never
chooses its own trust root.

## Offline runtime

`polis export` creates a deterministic, checksummed ZIP containing the V6 CLI,
specification, schemas, manifest, and usage guide. It never reads target
repository data or project policy and never overwrites an existing output.

The bundle is not a delivery `.polis` package and must not be passed to
`polis verify`. It requires Git and a matching OS/CPU runtime; configured project
commands may have their own dependencies.

See the [offline runtime guide](../spec/POLIS-OFFLINE.md) and the
[release process](releases.md) for distribution details.

## Exit categories

- `0` PASS
- `2` usage error
- `3` invalid artifact or signature
- `4` blocked environment or commit authorization
- `5` baseline mismatch
- `6` validation failure
- `7` apply failure
