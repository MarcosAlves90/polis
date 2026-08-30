# Publishing GitHub Releases

POLIS provides `scripts/github-release.sh` for deliberate, direct GitHub Release publication from a clean source checkout. It is a Bash operator tool and uses GitHub CLI (`gh`) for GitHub authentication and release operations.

The script is the `local_direct` publication authority. In the current repository revision, `.github/workflows/ci.yml` is validation-only and does not publish GitHub Releases. Do not run this script for a tag that is also published by a future GitHub Actions release workflow or another release system.

## Prerequisites

Install and authenticate:

- Git;
- Bash;
- GitHub CLI (`gh`).

Check the default GitHub CLI:

```bash
gh --version
gh auth status
```

The repository must be clean before preflight or publication.

## GitHub CLI selection

By default the script resolves `gh` from `PATH`:

```bash
./scripts/github-release.sh --tag v5.0.0
```

Set `POLIS_GH` when a specific GitHub CLI should be the default for the shell/session:

```bash
POLIS_GH="$HOME/tools/gh" ./scripts/github-release.sh --tag v5.0.0
```

Use `--gh` for an explicit per-invocation override. `--gh` takes precedence over `POLIS_GH`:

```bash
./scripts/github-release.sh \
  --tag v5.0.0 \
  --gh /opt/github-cli/bin/gh
```

`--gh` may be either an executable path or a command name resolvable from `PATH`.

## Preflight first

`--tag` is required. Without `--publish`, the script performs read-only Git/GitHub checks plus local hashing of optional assets, but does not create a tag, push a tag, or create a GitHub Release:

```bash
./scripts/github-release.sh --tag v5.0.0
```

A successful preflight ends with:

```text
POLIS RELEASE PREFLIGHT: PASS
Publication: blocked until --publish
```

Preflight verifies, among other things:

- the source is a Git worktree;
- the worktree is clean;
- `HEAD` resolves to an explicit commit;
- the configured Git remote exists;
- the selected GitHub CLI executes and is authenticated;
- the GitHub repository can be resolved from the checkout;
- any existing local or remote tag with the requested name targets exactly `HEAD`;
- no GitHub Release already exists for that tag;
- optional assets are regular, non-empty files with unique basenames.

Any ambiguity or conflict is a hard failure.

## Publish

After reviewing preflight, repeat the command with `--publish`:

```bash
./scripts/github-release.sh --tag v5.0.0 --publish
```

If the tag does not exist, the script creates an annotated tag at the already-resolved source commit and pushes only that tag. It then calls `gh release create` with `--verify-tag`. This prevents GitHub CLI from silently creating a missing tag from the repository default branch.

The script never force-pushes, moves a tag, uses `--clobber`, or repairs an existing release. A release already present for the tag causes a failure.

## Release notes and release type

Generated GitHub release notes are the default:

```bash
./scripts/github-release.sh --tag v5.0.0 --publish
```

Use maintained notes instead:

```bash
./scripts/github-release.sh \
  --tag v5.0.0 \
  --notes-file ./RELEASE_NOTES.md \
  --publish
```

Optional release metadata:

```bash
./scripts/github-release.sh \
  --tag v5.0.0-rc.1 \
  --title "POLIS V5.0.0-rc.1" \
  --prerelease \
  --latest false \
  --publish
```

Supported Latest policies are `automatic` (default), `true`, and `false`. `--draft` creates a draft release.

## Optional assets

POLIS does not currently define a repository-wide binary release matrix, so the release script does not invent or rebuild release assets. Supply only final files that have already been built and validated by the authoritative process for that release.

Pass `--asset` more than once when needed:

```bash
./scripts/github-release.sh \
  --tag v5.0.0 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --asset ./release-assets/polis-darwin-arm64.tar.gz
```

Before any remote mutation, the script:

1. verifies every asset is a non-empty regular file;
2. rejects duplicate basenames;
3. computes SHA-256 for the exact supplied bytes;
4. creates a temporary `SHA256SUMS` file;
5. uploads those exact assets plus `SHA256SUMS` without rebuilding them.

After publication it independently queries the release, verifies asset names and sizes, and compares GitHub-provided `sha256:` digests when they are available. If the release reports itself immutable, the script also requires `gh release verify` and uses `gh release verify-asset` for supplied assets.

Add `--publish` only after reviewing the preflight output:

```bash
./scripts/github-release.sh \
  --tag v5.0.0 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --publish
```

## Other options

Use a remote other than `origin` only when that is intentionally the Git remote that owns the release tag:

```bash
./scripts/github-release.sh --tag v5.0.0 --remote upstream
```

Show the complete CLI contract with:

```bash
./scripts/github-release.sh --help
```

## Safety model

The release flow intentionally follows these invariants:

- one publication authority for a release;
- exact source commit resolved before mutation;
- clean worktree;
- no implicit tag creation by `gh release create`;
- no tag rewrite or force-push;
- no overwrite/repair of historical releases;
- optional asset bytes are hashed before upload and never rebuilt by the script;
- remote state is queried again after publication;
- immutable release attestations are verified when supported and applicable.

Running the script is an explicit publication operation. The repository does not automatically publish a release merely because this script exists.
