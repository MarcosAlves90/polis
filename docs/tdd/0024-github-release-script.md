# TDD evidence — Safe GitHub Release script

## Red

Before the production script or release documentation existed, `internal/cicontract/github_release_test.go` was added and executed with:

```text
go test ./internal/cicontract -run 'TestGitHubRelease' -count=1
```

The test failed because:

- `README.md` had no GitHub Release entry point;
- `docs/releases.md` did not exist;
- `scripts/github-release.sh` did not exist, so both default-`gh` preflight and explicit-`gh` publication scenarios failed to execute.

This established that the requested release capability was absent and that the tests could detect it.

## Green

`scripts/github-release.sh` now implements the bounded release contract and `docs/releases.md` documents it. The contract tests exercise:

- default `gh` resolution from `PATH`;
- `--gh` precedence over `POLIS_GH`;
- preflight without remote mutation;
- explicit `--publish` tag creation/push against a local bare Git remote;
- `gh release create` using `--verify-tag`;
- documentation entry points.

The test uses a fake GitHub CLI and local Git remote. It never contacts or mutates GitHub.

## Refactor

The script keeps publication policy in cohesive shell helpers for executable resolution, SHA-256, tag inspection, and asset validation. It does not add a release workflow, binary build matrix, or generic release framework because those are not current repository requirements.
