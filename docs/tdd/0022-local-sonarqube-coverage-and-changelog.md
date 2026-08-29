# TDD-0022 — Local SonarQube coverage and changelog

## Red evidence

Local SonarQube analysis reports zero coverage because the scanner has no configured Go coverage report path, even though POLIS already produces `.polis/coverage.out` for its own policy gate. The repository also has no `CHANGELOG.md`.

## Green implementation

The target adds a root SonarScanner configuration, a local scan wrapper that generates the canonical POLIS Go coverage profile before analysis, and a Keep a Changelog-compatible project history.

## Green evidence

Validation must demonstrate that the scanner configuration imports `.polis/coverage.out`, the local wrapper produces that exact report before invoking `sonar-scanner`, credentials remain environment-only, the CI workflow is untouched, the changelog contains only real release history, and all existing Go/POLIS validation gates remain green.
