# SDD-0022 — Local SonarQube coverage and changelog

Status: accepted for a development-tooling and documentation delivery on POLIS V4.

## Problem

The local SonarQube Server analysis can inspect POLIS source but reports zero coverage when `sonar-scanner` runs without a Go coverage profile configured for import. POLIS already generates the authoritative project-wide Go profile at `.polis/coverage.out`, so a second coverage format or CI integration is unnecessary.

The repository also lacks a conventional changelog that summarizes the published V4 release and post-release maintenance work.

## Design

1. Add `sonar-project.properties` at the repository root and configure `sonar.go.coverage.reportPaths=.polis/coverage.out`.
2. Keep SonarQube execution local; do not modify GitHub Actions or add CI credentials.
3. Add `scripts/sonar-local.sh` to generate the same `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` report required by the POLIS policy, then invoke `sonar-scanner`.
4. Keep the token outside the repository in `SONAR_TOKEN`; default `SONAR_HOST_URL` to the local Docker endpoint `http://localhost:9000` while allowing an environment override.
5. Add `CHANGELOG.md` using Keep a Changelog sections and Semantic Versioning. Record only the published `v4.0.0` release and an `Unreleased` section for commits made after that tag.
6. Preserve the existing GitHub Actions workflow unchanged.

## Acceptance criteria

- local Sonar configuration points to `.polis/coverage.out`;
- local analysis generates the report before running `sonar-scanner`;
- no Sonar token or other credential is committed;
- GitHub Actions is unchanged;
- the changelog records V4.0.0 and post-release work without inventing releases;
- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go mod verify`, build, and the strict existing coverage gate pass;
- POLIS build, verify, and clean consumer apply pass against the exact baseline.
