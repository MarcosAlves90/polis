# POLIS V4.0.0

POLIS is a deterministic software-delivery protocol and Go CLI for evidence-driven validation, packaging, and safe application of code changes.

Go module: `github.com/MarcosAlves90/polis/v4`.

![Banner do POLIS](./polis-banner.png)

POLIS V4 separates authority into three modules:

1. `guide/` — engineering workflow, SDD/TDD, scope, safety, and evidence obligations.
2. `spec/` — normative package bytes, schemas, statuses, Change Contract, policy, coverage, integrity, and evidence semantics.
3. `cmd/polis` + `internal/` — deterministic Go reference implementation.

## Installation

With Go 1.23+ and Git installed, install POLIS as a terminal command on Linux, macOS, or Windows:

```text
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
```

Then ensure the Go binary directory is on `PATH` and verify the installation with `polis doctor`.

See the [installation guide](docs/installation.md) for operating-system-specific `PATH` instructions, source installation, upgrades, and removal.

## Commands

```bash
polis doctor
polis init --repo /path/to/repo
polis capture-red --repo /path/to/repo --contract /outside/change.json --out /outside/regression.patch
polis build --repo /path/to/repo --project project-slug --change change-slug --contract /outside/change.json --regression-patch /outside/regression.patch --out /path/to/output
polis verify artifact.polis
polis apply --repo /path/to/repo artifact.polis
```

`--regression-patch` is required only for `defect` Change Contracts.

## Core contracts

- package format: v2, exactly seven regular members;
- Project Policy: schema v2;
- Change Contract: schema v1;
- direct argv execution without synthesized shell commands;
- defect deliveries require reproducible Red -> Green evidence;
- project-wide line coverage is computed by POLIS and must be strictly greater than 80% unless policy requires more;
- build/apply validate in isolation before real working-tree mutation;
- filesystem security boundaries use canonical physical paths through `internal/pathguard`, including symlink and alias resolution.

## Platform evidence

Platform support claims are evidence-scoped. Native CI exercises Linux, macOS, and Windows. Consumer validation reruns the relevant gates on the actual target platform before applying a delivery.

## Local SonarQube analysis

POLIS can be analyzed against a local SonarQube Server without CI. Set a local Sonar token and run the repository wrapper:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

The wrapper defaults `SONAR_HOST_URL` to `http://localhost:9000`, generates `.polis/coverage.out` with the same project-wide coverage command used by the POLIS policy, and then runs `sonar-scanner`. Override `SONAR_HOST_URL` when the Docker-published endpoint differs.

See [CHANGELOG.md](CHANGELOG.md) for release and post-release changes.
