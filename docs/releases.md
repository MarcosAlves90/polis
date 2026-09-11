# Publishing GitHub Releases

POLIS provides `scripts/github-release.sh` for deliberate, direct GitHub Release publication from a clean source checkout. It is a Bash operator tool and uses GitHub CLI (`gh`) for GitHub authentication and release operations.

The script is the `local_direct` publication authority. In the current repository revision, `.github/workflows/ci.yml` is validation-only and does not publish GitHub Releases. Do not run this script for a tag that is also published by a future GitHub Actions release workflow or another release system.

## Prerequisites

Install and authenticate:

- Git;
- Bash;
- GitHub CLI (`gh`);
- Go, to build the offline POLIS asset.

Check the default GitHub CLI:

```bash
gh --version
gh auth status
```

The repository must be clean before preflight or publication.

## GitHub CLI selection

By default the script resolves `gh` from `PATH`:

```bash
./scripts/github-release.sh --tag v6.3.1
```

Set `POLIS_GH` when a specific GitHub CLI should be the default for the shell/session:

```bash
POLIS_GH="$HOME/tools/gh" ./scripts/github-release.sh --tag v6.3.1
```

Use `--gh` for an explicit per-invocation override. `--gh` takes precedence over `POLIS_GH`:

```bash
./scripts/github-release.sh \
  --tag v6.3.1 \
  --gh /opt/github-cli/bin/gh
```

`--gh` may be either an executable path or a command name resolvable from `PATH`.

## Go executable selection

Every release includes a native POLIS V6 offline runtime bundle. By default the
script resolves `go` from `PATH`; set `POLIS_GO` for a shell/session default or
use `--go` for a per-invocation override. The selected Go command must be able
to build `./cmd/polis` and its host OS/architecture must match the generated
binary, because the release flow runs that binary to produce the bundle.

```bash
POLIS_GO=/opt/go/bin/go ./scripts/github-release.sh --tag v6.3.1
./scripts/github-release.sh --tag v6.3.1 --go /opt/go/bin/go
```

## Preflight first

`--tag` is required. Without `--publish`, the script performs Git/GitHub checks,
builds and self-validates the required offline asset, and hashes all assets in a
temporary directory. It does not create a tag, push a tag, or create a GitHub
Release:

```bash
./scripts/github-release.sh --tag v6.3.1
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
- Go can build the V6 CLI for the host runtime;
- `polis export` can create and self-validate the offline bundle;
- supplied assets and the generated offline asset have unique basenames.

Any ambiguity or conflict is a hard failure.

## Publish

After reviewing preflight, repeat the command with `--publish`:

```bash
./scripts/github-release.sh --tag v6.3.1 --publish
```

If the tag does not exist, the script creates an annotated tag at the already-resolved source commit and pushes only that tag. It then calls `gh release create` with `--verify-tag`. This prevents GitHub CLI from silently creating a missing tag from the repository default branch.

The script never force-pushes, moves a tag, uses `--clobber`, or repairs an existing release. A release already present for the tag causes a failure.

The uploaded asset set always includes the generated offline bundle and a
release-level `SHA256SUMS` file. Additional files supplied with `--asset` remain
supported.

## Release notes and release type

Generated GitHub release notes are the default:

```bash
./scripts/github-release.sh --tag v6.3.1 --publish
```

Use maintained notes instead:

```bash
./scripts/github-release.sh \
  --tag v6.3.1 \
  --notes-file ./RELEASE_NOTES.md \
  --publish
```

Optional release metadata:

```bash
./scripts/github-release.sh \
  --tag v6.3.1-rc.1 \
  --title "POLIS V6.3.1-rc.1" \
  --prerelease \
  --latest false \
  --publish
```

Supported Latest policies are `automatic` (default), `true`, and `false`. `--draft` creates a draft release.

## Offline asset and optional assets

The release script generates one native offline asset from the exact clean
`HEAD` used by the release:

```text
polis-v6.3.1-offline-<GOOS>-<GOARCH>.zip
```

The bundle contains the compiled V6 CLI, embedded specification and schemas,
manifest, and internal checksums. The release-level `SHA256SUMS` also covers
the offline bundle and every additional `--asset` file. The current Go target
must equal the Go host target; use a target-native release runner for another
platform.

Supply only additional final files that have already been built and validated
by the authoritative process for that release. `--asset` does not replace or
disable the required offline asset.

Pass `--asset` more than once when needed:

```bash
./scripts/github-release.sh \
  --tag v6.3.1 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --asset ./release-assets/polis-darwin-arm64.tar.gz
```

Before any remote mutation, the script:

1. verifies every asset is a non-empty regular file;
2. rejects duplicate basenames;
3. computes SHA-256 for the exact supplied bytes;
4. builds and exports the offline bundle from `HEAD`;
5. creates a temporary `SHA256SUMS` file for all upload assets;
6. uploads those exact assets plus the generated bundle and `SHA256SUMS`.

After publication it independently queries the release, verifies every asset's
name and size, and compares GitHub-provided `sha256:` digests when they are
available. If the release reports itself immutable, the script also requires
`gh release verify` and uses `gh release verify-asset` for every uploaded asset.

Add `--publish` only after reviewing the preflight output:

```bash
./scripts/github-release.sh \
  --tag v6.3.1 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --publish
```

## Other options

Use a remote other than `origin` only when that is intentionally the Git remote that owns the release tag:

```bash
./scripts/github-release.sh --tag v6.3.1 --remote upstream
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
- the required offline bundle is built from the exact source commit and all
  upload bytes are hashed before publication;
- user-supplied optional asset bytes are never rebuilt by the script;
- remote state is queried again after publication;
- immutable release attestations are verified when supported and applicable.

Running the script is an explicit publication operation. The repository does not automatically publish a release merely because this script exists.
