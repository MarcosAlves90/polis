# Using POLIS V6

This guide covers the operational V6 workflow. Normative package and schema
rules live in the [POLIS Specification V6](../spec/POLIS-SPEC-v6.md).

## Repository map

- `guide/` defines the engineering workflow, scope, safety, and evidence duties.
- `spec/` defines machine contracts for packages, schemas, evidence, and apply.
- `cmd/polis` and `internal/` provide the deterministic Go implementation.

## Command reference

```bash
polis help [command]
polis doctor [--format text|json]
polis init --repo /path/to/repo [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--dry-run]
polis plan --repo /path/to/repo [--policy /outside/policy-v3.json] [--format text|json]
polis start --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/draft-v5.json --out /outside/locked-v6.json
polis implementation-plan --repo /path/to/repo [--policy /outside/policy-v3.json] --contract /outside/locked-v6.json --out /outside/implementation-plan.json
polis capture-red --repo /path/to/repo --contract /outside/locked-v6.json [--implementation-plan /outside/implementation-plan.json] --out /outside/regression.patch
polis build --repo /path/to/repo --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/locked-v6.json --regression-patch /outside/regression.patch [--implementation-plan /outside/implementation-plan.json] --out /outside/output
polis verify [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis inspect [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis preflight --repo /path/to/repo [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis apply --repo /path/to/repo [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--commit-mode none|prompt|auto] [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis sign --key private.pem --out artifact.polis.sig [--format text|json] artifact.polis
polis export --out /outside/polis-v6-offline.zip [--format text|json] [--executable <file>] [--runtime <GOOS/GOARCH>]
```

`polis help` lists all commands and can receive a command name to show its
syntax. `polis -h` and `polis --help` are equivalent top-level aliases.

`--executable` embeds a selected POLIS binary without executing it, and
`--runtime` declares the target `GOOS/GOARCH` recorded in the bundle manifest.
Both default to the current POLIS executable and runtime.

`--regression-patch` is required for Red-to-Green defects and strict features.
`preflight` validates without applying; `apply` validates again before mutation.
`--commit-mode` applies only to `apply` and defaults to `none`.

`polis implementation-plan` creates a deterministic optional plan from a locked
schema-v4 or schema-v6 Change Contract while the baseline repository is clean.
The contract, policy, plan, and output stay outside the target worktree. Pass the
same plan explicitly to `capture-red` and `build`; omitting it preserves the
unplanned format-v5 workflow. A planned build uses format v6, whose ninth member
contains the exact supplied plan bytes and is bound by the manifest digest,
checksums, package verification, and any detached signature.

Consumer baseline handling is independent from Project Policy validation level. The
baseline mode defaults to `strict`:

- `strict` requires exact equality with the artifact base commit;
- `compatible` accepts a different clean descendant `HEAD` only after ancestry,
  payload-context, isolated target, scope, development-proof, and policy checks
  pass;
- `permissive` also permits non-descendant history. Required locked development
  proof uses the local baseline when available, otherwise the authenticated
  format-v4/v5/v6 embedded baseline. It emits an explicit warning when ancestry is not
  proven.

New format-v5 and v6 artifacts carry `polis/polis-baseline.tar`, so permissive consumption
can replay locked development proof even when the consumer object database does
not contain the producer base commit. Valid embedded proof is not an override and
does not reduce guarantees.
The embedded baseline carries the locked commit and complete tree/blob closure, not refs or parent history. `polis build` replays the locked baseline proof from that exact embedded state, so a baseline command that requires unavailable history is rejected during build rather than producing a non-portable artifact.

For historical artifacts without embedded baseline proof,
`--allow-missing-baseline-proof` may be supplied only with `permissive`. It must be
repeated for `apply`, is never inherited from preflight, and visibly reports every
waived guarantee. It does not bypass package integrity, dirty-state rejection,
patch conflicts, scope, complete target/project validation, or transactional
post-apply verification.

## Canonical V6 delivery flow

The canonical producer path keeps Project Policy, Change Contract, regression
patch, package, signature, and caller-owned records outside the target repo.

Run `polis start` on a clean baseline. It locks the policy, Specification,
Change Contract, test scope, and proof requirements for later producer steps.

Use `polis capture-red` for Red-to-Green work. Use the locked contract with
`polis build`; the resulting package can be inspected, verified, and applied by
the consumer workflow.

To carry producer-authored commit intent, put the optional field below in a
strict Change Contract schema-v5 draft before `polis start`:

```json
{
  "commit": {
    "message": "feat(apply): support artifact-backed commit creation\n\nPreserve the validated target tree."
  }
}
```

`polis start` locks schema v5 as schema v6 while preserving the message. The
existing schema-v3 draft to schema-v4 locked flow remains supported for
contracts without commit metadata. New packages keep format v5 and the existing
eight-member inventory; the message remains in the checksummed
`polis/polis-change.json` member. A trusted detached signature authenticates it
when supplied and verified; checksums alone do not identify the producer.

The message is limited to 16,384 Unicode scalar values and 64 KiB of UTF-8,
must be non-empty, valid UTF-8, and contain no NUL. POLIS preserves whitespace,
multiline content, and final-newline presence without imposing a universal
commit-message convention.

Consumers still apply only by default. Use `--commit-mode prompt` to review the
exact message and target tree and confirm before real mutation, or
`--commit-mode auto` to explicitly authorize a local commit without prompting.
Both modes require artifact commit metadata. A refusal or unavailable TTY is
blocked and leaves HEAD, index, and worktree unchanged. A created commit uses
the validated consumer HEAD as its parent and exactly the validated consumer
target tree; successful commit mode leaves the repository clean. POLIS does not
run commit hooks, request signing, push, or change remote refs. Apply JSON
reports `committed`, `commit_sha`, and `commit_message`.

Canonical external-policy execution is zero-residue during build, verify,
inspect, preflight, and `apply --commit-mode none`; these commands preserve
`HEAD`, the real index, refs, Git configuration, and persistent Git objects.
An explicitly authorized prompt/auto success intentionally creates a local
commit and ref. A failed commit attempt restores `HEAD`, index, and worktree
when rollback can be verified; an unreachable Git object may remain.

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
the guarantees provided or absent by the effective policy. Repeat
`--defer-gate <id>` to preview transferring enabled gate validation to the
consumer. A producer-executed gate cannot depend directly or transitively on a
deferred gate.

## V6 contract summary

- Unplanned builds use package format v5 with eight regular members under `polis/`. Explicitly planned builds use format v6 with the exact plan as a ninth member; both include the authenticated `polis/polis-baseline.tar`. Valid historical v2-v5 formats remain readable.
- Project Policy uses schema v3 with command environments and gate configuration.
- New builds accept locked Change Contract schema v4 for compatibility and schema v6 for the new producer flow. Schema v5 is a draft accepted by `polis start`, which locks it as v6. Schemas v1-v4 retain their existing meaning.
- Formats v5 and v6 use Evidence v3 to record deferred gates as `DEFERRED`; formats v2-v4 retain Evidence v2 semantics. Both use bounded-output counts and digests, not raw streams.
- `polis inspect` reports plan presence, schema, strategy, step count, and requirement-to-plan-to-proof traceability after successful package verification; historical and unplanned artifacts report plan absence.
- `polis build --defer-gate <id>` may repeat the option. Deferred gates are skipped by the producer and run with the packaged policy during consumer `preflight` and `apply`.
- Detached Ed25519 signatures authenticate exact package bytes.
- Coverage adapters are Go coverprofile, LCOV, and Cobertura.
- Strict consumer mode keeps exact Git baseline identity; compatible retains ancestry proof; permissive can source locked development proof from local or embedded baseline state while retaining deterministic exact consumer target-tree validation. Missing proof can be waived only through the explicit permissive-only override.
- Consumer validation is isolated and `apply` is transactional. `--commit-mode none` preserves apply-only behavior; explicit prompt/auto modes create a local commit only after the post-apply target-tree check.

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
