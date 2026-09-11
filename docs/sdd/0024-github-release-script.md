# SDD-0024 — Safe GitHub Release script

Status: implemented for POLIS V5 and extended for POLIS V6

## Problem

POLIS has no repository-owned command for creating GitHub Releases. Release publication therefore depends on ad-hoc `gh` commands, which can accidentally create a tag from the default branch, reuse a conflicting tag, publish from a dirty worktree, or use a different GitHub CLI binary than the operator intended.

## Required behavior

Add `scripts/github-release.sh` as the repository-owned `local_direct` GitHub Release publisher.

The script MUST:

- use `gh` from `PATH` by default;
- allow an explicit GitHub CLI executable with `--gh <path-or-command>`;
- allow `POLIS_GH` as an environment-level default overridden by `--gh`;
- build the required native POLIS V6 offline runtime with Go, using `go` by default;
- allow `POLIS_GO` as an environment-level Go default overridden by `--go <path-or-command>`;
- require an explicit release tag;
- resolve the source commit from `HEAD` and require a clean worktree before publication;
- resolve the target GitHub repository from the current checkout;
- authenticate through the selected `gh` executable;
- inspect local and remote tag state and reject any tag that resolves to another commit;
- create an annotated local tag only when the requested tag is absent;
- push only the requested tag and never force-push;
- invoke `gh release create` only with `--verify-tag`, so GitHub CLI cannot create the tag implicitly;
- fail when a GitHub Release already exists for the tag rather than overwriting or repairing it;
- default to generated GitHub release notes while allowing `--notes-file`;
- support draft, prerelease, explicit Latest policy, and additional optional prebuilt assets;
- validate the generated offline asset and optional assets as regular non-empty files before mutation;
- reject duplicate asset basenames;
- compute SHA-256 for every upload asset before upload and include a generated `SHA256SUMS` asset;
- upload the generated offline asset and exact supplied asset bytes without modifying supplied assets;
- query the created release independently after publication;
- compare remote asset names, sizes, and SHA-256 digests when GitHub exposes asset digests;
- verify GitHub release/asset attestations when the resulting release reports itself immutable and the selected `gh` supports those commands;
- require `--publish` before any tag creation, tag push, or release creation. Without `--publish`, it performs preflight only.

## V6 extension

Every V6 release includes one native `polis-<tag>-offline-<GOOS>-<GOARCH>.zip`
asset. The script builds `./cmd/polis` from the exact clean `HEAD`, runs the
resulting executable's `export` command, and adds the self-validating bundle to
the release. The release host must match the Go target because the exporter is
executed locally. A collision between the generated basename and a supplied
asset is a hard failure.

## Scope

The current `.github/workflows/ci.yml` is validation-only and does not publish releases. This change does not add a GitHub Actions release workflow, package-manager publisher, general binary build matrix, cross-compilation policy, or signing key. The V6 extension deliberately builds only the native offline runtime; additional assets remain operator-supplied and their bytes are not rebuilt.

## Safety invariants

- Exactly one publication authority is used: this script is `local_direct`.
- No `--clobber`, force-push, tag move, or historical release repair is permitted.
- A local or remote tag that targets another commit is a hard failure.
- A pre-existing GitHub Release for the requested tag is a hard failure.
- Remote mutation is impossible unless `--publish` is present.
- The GitHub CLI selected by the operator is the only `gh` executable used during the run.

## Documentation

Add `docs/releases.md` as the operator guide and a concise README entry point. Document the default `gh`, `POLIS_GH`, `--gh`, preflight/publish boundary, release notes, assets, and safety model.
