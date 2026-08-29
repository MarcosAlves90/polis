# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Versioned `.polis/policy.json` so the repository carries its canonical validation policy, including complete tests, strict project-wide coverage above 80%, vet, build, and module-integrity gates.
- Local SonarQube configuration through `sonar-project.properties`, importing the same Go coverage profile used by POLIS from `.polis/coverage.out`.
- `scripts/sonar-local.sh` for local SonarQube Server analysis: it generates the canonical Go coverage profile before running `sonar-scanner`, defaults `SONAR_HOST_URL` to `http://localhost:9000`, and keeps authentication in `SONAR_TOKEN`.
- SDD/TDD records for the post-release maintainability, duplication, and SonarQube remediation work.

### Changed

- Reduced cognitive complexity in package build/apply/verify, red-capture, policy, evidence, change-contract, and coverage code paths by decomposing large routines into focused helpers.
- Replaced repeated literals reported by SonarQube with shared constants while preserving rendered error messages.
- Replaced an excessive-parameter isolated-validation function with typed validation input.
- Consolidated duplicated Git execution, temporary worktree/index handling, tree inspection, isolated validation, external-file reads, and strict JSON EOF validation into shared internal packages.
- Centralized wrapped Git/isolation error formatting to prevent repeated `go:S1192` findings after the duplication refactor.
- Ignored disposable local SonarQube/SonarScanner state in `.scannerwork/`, `.sonar/`, and `.sonarlint/`.

### Fixed

- Removed the SonarQube maintainability findings identified after the V4 release, including cognitive-complexity, duplicated-literal, and excessive-parameter findings.
- Reduced structural code duplication without using Sonar CPD exclusions.
- Corrected the residual duplicated policy error literal and the later duplicated wrapped-error format introduced during refactoring.

## [4.0.0] - 2026-08-29

### Added

- POLIS V4 deterministic software-delivery protocol and Go CLI.
- `doctor`, `init`, `capture-red`, `build`, `verify`, and `apply` command flows.
- Package format v2 with the canonical manifest, policy, change contract, regression patch, payload patch, evidence stream, and checksums inventory.
- Project Policy schema v2 and Change Contract schema v1.
- Deterministic producer and consumer validation with exact target-tree verification.
- Defect delivery support with reproducible Red-to-Green evidence.
- Strict project-wide line coverage gate requiring coverage greater than 80%.
- Transactional apply behavior that validates in isolation and preserves the repository `HEAD` and index while applying the target to the working tree.
- Canonical physical-path and symlink boundary validation through `internal/pathguard`.
- Cross-platform validation workflow for Linux, macOS, and Windows.
- Native macOS validation evidence and deterministic Windows Git fixture handling.
- Cross-platform terminal installation documentation and POLIS project banner.
- SDD/TDD documentation covering the V4 protocol, policy execution, package building, transactional apply, coverage, policy initialization, change contracts, red capture, path boundaries, release preparation, CI, portability, and installation.

### Changed

- Migrated the Go module and all repository-owned imports to the semantic major-version path `github.com/MarcosAlves90/polis/v4`.
- Updated public installation commands to `go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest`.

### Fixed

- Aligned the Go module identity with the repository before the V4 semantic import-path migration.
- Made Git-based CI fixtures deterministic across Windows line-ending behavior.

[Unreleased]: https://github.com/MarcosAlves90/polis/compare/v4.0.0...HEAD
[4.0.0]: https://github.com/MarcosAlves90/polis/releases/tag/v4.0.0
