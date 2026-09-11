# Using POLIS V6

This guide covers the operational V6 workflow. Normative package and schema
rules live in the [POLIS Specification V6](../spec/POLIS-SPEC-v6.md).

## Repository map

- `guide/` defines the engineering workflow, scope, safety, and evidence duties.
- `spec/` defines machine contracts for packages, schemas, evidence, and apply.
- `cmd/polis` and `internal/` provide the deterministic Go implementation.

## Command reference

```bash
polis doctor [--format text|json]
polis init --repo /path/to/repo [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--dry-run]
polis plan --repo /path/to/repo [--policy /outside/policy-v3.json] [--format text|json]
polis start --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/draft-v3.json --out /outside/locked-v4.json
polis capture-red --repo /path/to/repo --contract /outside/change.json --out /outside/regression.patch
polis build --repo /path/to/repo --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/change.json --regression-patch /outside/regression.patch --out /outside/output
polis verify [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis inspect [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis preflight --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis apply --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis sign --key private.pem --out artifact.polis.sig [--format text|json] artifact.polis
polis export --out /outside/polis-v6-offline.zip [--format text|json]
```

`--regression-patch` is required for Red-to-Green defects and strict features.
`preflight` validates without applying; `apply` validates again before mutation.

## Canonical V6 delivery flow

The canonical producer path keeps Project Policy, Change Contract, regression
patch, package, signature, and caller-owned records outside the target repo.

Run `polis start` on a clean baseline. It locks the policy, Specification,
Change Contract, test scope, and proof requirements for later producer steps.

Use `polis capture-red` for Red-to-Green work. Use the locked contract with
`polis build`; the resulting package can be inspected, verified, and applied by
the consumer workflow.

Canonical external-policy execution is zero-residue. It preserves `HEAD`, the
real index, refs, Git configuration, and persistent Git objects.

## Project Policy and validation

`strict` is the default and preserves the strongest current behavior. Existing
V6 policies may omit the field for compatibility.

`standard` keeps `test.complete` required and disables generated coverage by
default. `minimal` disables generated project-quality gates by default.

Use repeated `--disable-gate <id>` only for generated project gates. Every
disabled gate is recorded with its reason; safety, integrity, baseline,
development-proof, and transactional invariants remain mandatory.

`polis init --profile auto` recognizes a root Go module only. Use `custom` for
other ecosystems and provide direct argv for test and coverage commands. POLIS
does not synthesize shell commands.

Each gate may declare `depends_on`. POLIS adds essential dependencies, rejects
unknown IDs, cycles, and enabled gates depending on `not_applicable` gates, then
executes a deterministic topological order.

`polis plan` is read-only. It reports the policy digest, commands, enabled and
disabled gates, dependency edges, execution order, mandatory invariants, and
the guarantees provided or absent by the effective policy.

## V6 contract summary

- Package format v3 has exactly seven regular members under `polis/`.
- Project Policy uses schema v3 with command environments and gate configuration.
- New builds require locked Change Contract schema v4 from `polis start`.
- Evidence v2 records bounded-output counts and digests, not raw streams.
- Detached Ed25519 signatures authenticate exact package bytes.
- Coverage adapters are Go coverprofile, LCOV, and Cobertura.
- Exact Git baseline and target-tree checks remain mandatory.
- Consumer validation is isolated and `apply` is transactional.

V6 can read supported historical V5 policies and Change Contracts for migration.
It does not create new legacy producer contracts.

## Strict SDD/TDD

Strict drafts contain the Specification, requirement-to-acceptance traceability,
test scope, and proof command.

Strict features and defects require Red-to-Green proof. Strict
`behavior_preserving` changes require the same characterization command to pass
on baseline and target without manufacturing a Red state.

Captured strict Red paths are immutable. Later steps revalidate the repository,
Specification, policy hash, baseline lock, and captured tests.

## Artifact and trust boundaries

The verifier treats `.polis` bytes as untrusted input and enforces bounded
archive, contract, evidence, patch, and event sizes.

Change Contract scopes are checked against the Git base-to-target path set.
Rename sources and destinations are both checked.

`polis sign` creates a detached Ed25519 signature over the artifact SHA-256.
Consumers must supply both `--signature` and `--trusted-key`; the package never
chooses its own trust root.

## Offline runtime

`polis export` creates a deterministic, checksummed ZIP containing the native V6
CLI, specification, schemas, manifest, and usage guide. It never reads target
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
- `4` blocked environment
- `5` baseline mismatch
- `6` validation failure
- `7` apply failure

## Local quality checks

The local SonarQube workflow remains available:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

Use `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`
before publishing changes. The repository CI exercises Linux, macOS, and
Windows.

## Related documentation

- [Installation](installation.md)
- [GitHub releases](releases.md)
- [POLIS Specification V6](../spec/POLIS-SPEC-v6.md)
- [V6 design decisions](sdd/)
- [Validation record](../VALIDATION.md)
