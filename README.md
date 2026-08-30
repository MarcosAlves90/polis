# POLIS V5

POLIS is a deterministic software-delivery protocol and Go CLI for evidence-driven validation, packaging, inspection, and transactional application of code changes.

Go module: `github.com/MarcosAlves90/polis/v5`.

![Banner do POLIS](./polis-banner.png)

V5 keeps the V4 exact-baseline and exact-target-tree model and strengthens the consumer trust boundary. Authority remains separated into:

1. `guide/` — engineering workflow, scope, safety, and evidence obligations.
2. `spec/` — machine contracts for package bytes, schemas, evidence, status, integrity, and application semantics.
3. `cmd/polis` + `internal/` — deterministic Go reference implementation.

## Installation

With Go 1.23+ and Git installed:

```text
go install github.com/MarcosAlves90/polis/v5/cmd/polis@latest
```

Then run `polis doctor`. See the [installation guide](docs/installation.md) for OS-specific `PATH` instructions.

## Commands

```bash
polis doctor [--format text|json]
polis init --repo /path/to/repo
polis capture-red --repo /path/to/repo --contract /outside/change.json --out /outside/regression.patch
polis build --repo /path/to/repo --project project-slug --change change-slug --contract /outside/change.json --regression-patch /outside/regression.patch --out /path/to/output
polis verify [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis inspect [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis preflight --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis apply --repo /path/to/repo [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis sign --key private.pem --out artifact.polis.sig [--format text|json] artifact.polis
```

`--regression-patch` is required only for `defect` Change Contracts. `preflight` never applies the payload and a later `apply` always validates again.

## V5 contracts

- package format v3, still exactly seven regular members under `polis/`;
- Project Policy schema v3, with explicit command environments;
- Change Contract schema v2, with `scope.allowed_paths`;
- Evidence v2 stores bounded-output byte counts and SHA-256 digests rather than raw stdout/stderr;
- detached Ed25519 signatures authenticate exact `.polis` bytes when the consumer supplies a trusted public key;
- coverage adapters: `go-coverprofile-v1`, `lcov-v1`, and `cobertura-v1`;
- exact Git baseline and target tree remain mandatory;
- consumer validation remains isolated and `apply` preserves `HEAD` and the real index;
- project-wide line coverage remains strictly greater than 80% unless project policy requires more.

V5 can read Project Policy v2 and Change Contract v1 to support migration from V4. New `polis init` output uses the stronger V5 schemas.

## Artifact hardening

The verifier treats `.polis` bytes as untrusted input. V5 bounds archive size, total uncompressed content, individual contract/evidence/patch members, and NDJSON event lines. Runtime stdout/stderr retention is limited to 1 MiB per stream while digesting all received bytes.

Change Contract v2 scopes are checked from the Git base-to-target path set. `.` authorizes the full repository; directory entries ending in `/` authorize that prefix; other entries authorize an exact path. Rename source and destination are both checked.

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

## Local SonarQube analysis

The existing local SonarQube workflow remains available:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

See [POLIS Specification v2](spec/POLIS-SPEC-v2.md), [SDD-0023](docs/sdd/0023-polis-v5-portable-trust.md), and [CHANGELOG.md](CHANGELOG.md).
