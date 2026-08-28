# SDD-0017 — Go v4 module identity

Status: accepted for POLIS V4.0.0 release preparation.

## Problem

POLIS identifies the product and CLI as V4.0.0, but the Go module was declared as `github.com/MarcosAlves90/polis`. Under Go semantic import versioning, a module released at major version v2 or higher must include that major version in its module path. Without `/v4`, `go install ...@latest` cannot resolve a future `v4.0.0` tag as the canonical version of this module and instead resolves untagged commits through pseudo-versions.

## Decision

The canonical Go module identity is:

`github.com/MarcosAlves90/polis/v4`

All Go imports owned by this repository MUST use the `/v4` module path. Public installation documentation MUST install the CLI through:

`go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest`

The repository name and physical source layout remain unchanged; semantic import versioning changes the module/import identity only.

Historical SDD/TDD records that document the pre-migration state remain unchanged as historical evidence. Active documentation and executable contracts use only the canonical v4 identity.

## Compatibility

- CLI command name remains `polis`.
- Runtime behavior and package format remain unchanged.
- Source consumers importing POLIS Go packages must update imports to `/v4`.
- A future Git tag `v4.0.0` becomes compatible with the module path after this migration is committed.
- No release tag is created by this change.

## Acceptance

- `go.mod` declares `module github.com/MarcosAlves90/polis/v4`;
- repository-owned Go imports resolve through `/v4`;
- README and installation guide use the `/v4/cmd/polis@latest` command;
- tests protect module and installation identity;
- complete test suite, race detector, vet, module verification, build, doctor, and strict project-wide coverage remain passing.
