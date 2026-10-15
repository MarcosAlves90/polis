# Using POLIS V6

This page is the navigation index for POLIS V6. Normative package and schema rules live in the [POLIS Specification V6](../spec/POLIS-SPEC-v6.md); command help is the canonical operational reference; tutorials show common paths.

## First change

New users should start with the [First change quickstart](quickstart.md), which distinguishes package-free local validation from remote package handoff.

## Choose the agent workflow

- **Remote delivery:** a producer with the source checkout and POLIS runtime creates a disposable `.polis` delivery package outside the repository, then transfers it to a consumer for verification and application. Read the [remote delivery guide](workflows/web-agent-delivery.md).
- **Local workspace:** a coding agent already in the target checkout uses locked development proof and `polis workspace validate`; this path does not build a delivery package. Read the [local workspace guide](workflows/local-agent-workspace.md).

## Command reference

The [Command reference](commands.md#agent-command-instructions) is the single canonical source for embedded `polis help`; installed help is available with `polis help [command]`.

## Reference pages

- [Policy and validation](policy-and-validation.md)
- [Development proof](development-proof.md)
- [Repository retention](retention.md)
- [Gate execution and progress](gates-and-progress.md)
- [Artifact trust](artifact-trust.md)

## Repository map

- `guide/` defines the engineering workflow, scope, safety, and evidence duties.
- `spec/` defines machine contracts for packages, schemas, evidence, and apply.
- `cmd/polis` and `internal/` provide the deterministic Go implementation.

## More documentation

- [Installation](installation.md)
- [Offline runtime specification](../spec/POLIS-OFFLINE.md)
- [V6 design decisions](sdd/)
- [Validation record](../VALIDATION.md)
- [GitHub releases](releases.md)
