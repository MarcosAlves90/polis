# POLIS V6

POLIS is a deterministic software-delivery protocol and Go CLI for evidence-driven validation, packaging, inspection, and transactional application of code changes.

Go module: `github.com/MarcosAlves90/polis/v6`.

![Banner do POLIS](./polis-banner.png)

V6 keeps the V5 portable trust boundary and makes locked SDD/TDD mandatory for every new producer build. Authority remains separated into:

1. `guide/` — engineering workflow, scope, safety, and evidence obligations.
2. `spec/` — machine contracts for package bytes, schemas, evidence, status, integrity, and application semantics.
3. `cmd/polis` + `internal/` — deterministic Go reference implementation.

## Installation

With Go 1.23+ and Git installed:

```text
go install github.com/MarcosAlves90/polis/v6/cmd/polis@latest
```

Then run `polis doctor`. See the [installation guide](docs/installation.md) for OS-specific `PATH` instructions.

## Commands

```bash
polis doctor [--format text|json]
polis init --repo /path/to/repo [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--dry-run]
polis start --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/draft-v3.json --out /outside/locked-v4.json
polis capture-red --repo /path/to/repo --contract /outside/change.json --out /outside/regression.patch
polis build --repo /path/to/repo --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/change.json --regression-patch /outside/regression.patch --out /path/to/output
polis verify [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis inspect [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis preflight --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis apply --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis sign --key private.pem --out artifact.polis.sig [--format text|json] artifact.polis
```

`--regression-patch` is required for Change Contracts whose proof mode is Red-to-Green (defects and strict features). `preflight` never applies the payload and a later `apply` always validates again.

### Zero-residue target workflow

The canonical V6 path is explicit rather than discoverable: policy, Change Contract, regression patch, package, signature, and optional caller-owned records live outside the target repository. Successful execution does not create `.polis`, `.git/polis`, linked-worktree administration, persistent temporary Git objects, or default apply-evidence files in the target. Temporary validation state is isolated outside the target and cleaned before successful return. The external `.polis` artifact still uses its normative member names; that artifact is not repository residue.

### Policy initialization

`polis init` keeps `--profile auto` fail-closed. V6 auto-detection recognizes only a root-level Go module. For other repositories, use the explicit `custom` profile and provide direct argv for both required executable gates:

```bash
polis init --repo . --profile custom \
  --test-argv npm \
  --test-argv test \
  --coverage-argv npm \
  --coverage-argv run \
  --coverage-argv coverage \
  --coverage-adapter lcov-v1 \
  --coverage-report coverage/lcov.info
```

Each repeated argv flag contributes exactly one argument; POLIS does not synthesize shell commands. `--coverage-threshold` is optional and defaults to `80.0` with the existing strict `>` operator. Supported adapters remain `go-coverprofile-v1`, `lcov-v1`, and `cobertura-v1`. All gates other than `test.complete` and `coverage` are generated as `not_applicable` with reasons because `custom` does not infer commands from ecosystem metadata.

Project Policy also supports explicit validation reinforcement. `strict` is the default and preserves the current behavior; its field may remain absent for compatibility with existing V6 policies. `standard` keeps `test.complete` required and the generated profile disables coverage by default; `minimal` disables all generated project-quality gates by default. An external policy may keep additional gates enabled at either lower level. Repeat `--disable-gate <id>` to disable a generated project gate with a recorded reason. Disabled gates are never silently skipped: the policy and execution evidence list every enabled and disabled gate. These levels do not disable structural, integrity, security, baseline, exact-tree, development-proof, or transactional-apply invariants. A policy committed in the project configures normal runs; a validated external policy passed with `--policy` configures that execution.

Add `--dry-run` to emit the validated policy JSON to stdout without creating or modifying `.polis/policy.json`, the Git index, `HEAD`, or other worktree files. For the canonical zero-residue workflow, redirect that output to a path outside the target repository and pass it explicitly to `polis start --policy` and `polis build --policy`. Non-dry-run initialization remains available for repositories that intentionally use committed-policy compatibility and never overwrites an existing policy.

## V6 contracts

- package format v3, still exactly seven regular members under `polis/`;
- Project Policy schema v3, with explicit command environments and configurable validation reinforcement;
- new V6 builds require locked Change Contract schema v4 created by `polis start`; schemas v1-v3 remain read-compatible for migration;
- Evidence v2 stores bounded-output byte counts and SHA-256 digests rather than raw stdout/stderr, plus the effective validation level and complete project-gate inventory;
- detached Ed25519 signatures authenticate exact `.polis` bytes when the consumer supplies a trusted public key;
- coverage adapters: `go-coverprofile-v1`, `lcov-v1`, and `cobertura-v1`;
- exact Git baseline and target tree remain mandatory;
- consumer validation remains isolated, `apply` preserves `HEAD` and the real index, and canonical external-policy execution leaves no tool-owned worktree or Git-metadata residue;
- project-wide line coverage remains strictly greater than 80% unless project policy requires more.

V6 can read historical Project Policy/Change Contract schemas supported by V5 for migration. New `polis init` output uses Project Policy v3. New `polis build` operations require locked Change Contract schema v4; schema v2 and unlocked schema v3 are no longer valid producer inputs.

## Strict SDD/TDD workflow

POLIS V6 makes machine enforcement of development order mandatory for new builds. A strict schema-v3 draft contains the Specification, requirement-to-acceptance traceability, test scope, and proof command. For the canonical zero-residue path, keep Project Policy schema v3 outside the target repository and run `polis start --policy /outside/policy-v3.json` on a clean committed baseline before implementation. The resulting schema-v4 `baseline_lock` binds the canonical policy bytes by SHA-256, not a repository pathname. Omitting `--policy` retains committed `.polis/policy.json` compatibility for existing/self-hosting repositories.

Strict feature and defect work requires Red-to-Green proof. Strict `behavior_preserving` work uses Green-to-Green characterization: the same explicit command must pass on both baseline and target, without manufacturing a Red state. Captured strict Red paths are immutable between capture and target, preventing tests from being weakened after the failing proof.

Schema-v4 `capture-red` revalidates repository-dependent Git/Specification baseline facts, while `build --policy` additionally proves the effective policy hash. `verify` proves the packaged policy matches `baseline_lock.policy_sha256`. `preflight` and `apply` therefore use the packaged policy and do not require `.polis/policy.json` in the consumer. They still revalidate repository-dependent baseline facts, and `inspect` exposes deterministic `REQ -> AC -> regression` links.

## Artifact hardening

The verifier treats `.polis` bytes as untrusted input. V6 retains the V5 bounds for archive size, total uncompressed content, individual contract/evidence/patch members, and NDJSON event lines. Runtime stdout/stderr retention is limited to 1 MiB per stream while digesting all received bytes.

Change Contract v2+ scopes are checked from the Git base-to-target path set. `.` authorizes the full repository; directory entries ending in `/` authorize that prefix; other entries authorize an exact path. Rename source and destination are both checked.

## Signature trust model

`polis sign` produces a detached signature. The signature signs SHA-256 of the exact artifact bytes with Ed25519. The `.polis` package never chooses its own trusted key; `verify`, `preflight`, and `apply` only authenticate when the consumer supplies both `--signature` and `--trusted-key`.

## Exit categories

Automation-oriented commands use these stable categories:

- `0` PASS
- `2` usage error
- `3` invalid artifact/signature
- `4` blocked environment
- `5` baseline mismatch
- `6` validation failure
- `7` apply failure

## GitHub Releases

Repository-owned GitHub Release publication is available through `scripts/github-release.sh`. It resolves the standard `gh` from `PATH` by default and accepts `POLIS_GH` or `--gh` when an explicit GitHub CLI executable is required.

Run preflight first:

```bash
./scripts/github-release.sh --tag v6.0.0
```

Remote mutation requires an explicit `--publish`. See [the GitHub Release guide](docs/releases.md) for tag safety, release notes, optional assets, SHA-256 verification, and immutable-release attestation checks.

## Local SonarQube analysis

The existing local SonarQube workflow remains available:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

See [POLIS Specification V6](spec/POLIS-SPEC-v6.md), [SDD-0030](docs/sdd/0030-polis-v6-mandatory-strict-development.md), and [CHANGELOG.md](CHANGELOG.md).
