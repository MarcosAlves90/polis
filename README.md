# POLIS V6

> Deterministic, evidence-driven software delivery for AI-assisted engineering.

POLIS is a Go CLI and delivery protocol for validating, packaging, inspecting,
signing, and safely applying code changes.

![POLIS banner](./polis-banner.png)

## Why POLIS

- Makes requirements, tests, evidence, and change scope explicit.
- Applies configurable validation with strict, safe defaults.
- Produces verifiable `.polis` delivery packages and detached signatures.
- Preserves repository integrity through isolated validation and transactional apply.
- Includes a portable offline runtime for coding agents without the source tree.

## Installation

### Go installation (recommended)

Requirements: Go 1.23 or newer and Git.

```bash
go install github.com/MarcosAlves90/polis/v6/cmd/polis@latest
polis doctor
```

For a reproducible installation, pin the release instead of using `@latest`:

```bash
go install github.com/MarcosAlves90/polis/v6/cmd/polis@v6.3.1
```

See the [installation guide](docs/installation.md) for `PATH` setup on Linux,
macOS, and Windows, plus upgrade, removal, and source-checkout instructions.

### Offline runtime

Download the `polis-<tag>-offline-<GOOS>-<GOARCH>.zip` asset that matches your
platform from the [latest release](https://github.com/MarcosAlves90/polis/releases/latest).
Extract it and run `bin/polis` (or `bin/polis.exe` on Windows).

The offline runtime requires Git and the matching OS/CPU architecture. It does
not require Go or another language runtime for POLIS operations. Read the
[offline runtime guide](spec/POLIS-OFFLINE.md) before transferring it to an AI.

### Source checkout

For development or a checked-out revision:

```bash
git clone https://github.com/MarcosAlves90/polis.git
cd polis
go install ./cmd/polis
polis doctor
```

## Documentation

- [Usage and V6 workflows](docs/usage.md)
- [Installation guide](docs/installation.md)
- [Offline runtime guide](spec/POLIS-OFFLINE.md)
- [POLIS Specification V6](spec/POLIS-SPEC-v6.md)
- [GitHub release process](docs/releases.md)
- [Architecture and design decisions](docs/sdd/)
- [Changelog](CHANGELOG.md)

The usage guide is the entry point for command reference, policy profiles,
strict SDD/TDD, artifact trust, signatures, and exit categories.
