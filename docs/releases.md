# Publishing GitHub Releases

POLIS provides `scripts/github-release.sh` for deliberate, direct GitHub Release publication from a clean source checkout. It is a Bash operator tool and uses GitHub CLI (`gh`) for GitHub authentication and release operations.

The script is the `local_direct` publication authority. In the current repository revision, `.github/workflows/ci.yml` is validation-only and does not publish GitHub Releases. Do not run this script for a tag that is also published by a future GitHub Actions release workflow or another release system.

## Prerequisites

Install and authenticate:

- Git;
- Bash;
- GitHub CLI (`gh`);
- Go, to build the host exporter and cross-compile the Linux and Windows offline assets.

Check the default GitHub CLI:

```bash
gh --version
gh auth status
```

The repository must be clean before preflight or publication.

## GitHub CLI selection

By default the script resolves `gh` from `PATH`:

```bash
./scripts/github-release.sh --tag v6.6.0
```

Set `POLIS_GH` when a specific GitHub CLI should be the default for the shell/session:

```bash
POLIS_GH="$HOME/tools/gh" ./scripts/github-release.sh --tag v6.6.0
```

Use `--gh` for an explicit per-invocation override. `--gh` takes precedence over `POLIS_GH`:

```bash
./scripts/github-release.sh \
  --tag v6.6.0 \
  --gh /opt/github-cli/bin/gh
```

`--gh` may be either an executable path or a command name resolvable from `PATH`.

## Go executable selection

Every release includes a native host POLIS V6 offline runtime bundle plus Linux
amd64 and Windows amd64 bundles. By default the script resolves `go` from
`PATH`; set `POLIS_GO` for a shell/session default or use `--go` for a
per-invocation override. The selected Go command must be able to build the host
exporter and cross-compile `./cmd/polis` for the target runtimes. Only the host
exporter is executed; target binaries are embedded as bytes.

```bash
POLIS_GO=/opt/go/bin/go ./scripts/github-release.sh --tag v6.6.0
./scripts/github-release.sh --tag v6.6.0 --go /opt/go/bin/go
```

## Preflight first

`--tag` is required. Without `--publish`, the script performs Git/GitHub checks,
builds and self-validates the required offline assets, and hashes all assets in a
temporary directory. It does not create a tag, push a tag, or create a GitHub
Release:

```bash
./scripts/github-release.sh --tag v6.6.0
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
- Go can build the V6 host exporter and each required target runtime;
- the host `polis export` can create and self-validate each target bundle;
- supplied assets and all generated offline assets have unique basenames.

Any ambiguity or conflict is a hard failure.

## Publish

After reviewing preflight, repeat the command with `--publish`:

```bash
./scripts/github-release.sh --tag v6.6.0 --publish
```

If the tag does not exist, the script creates an annotated tag at the already-resolved source commit and pushes only that tag. It then calls `gh release create` with `--verify-tag`. This prevents GitHub CLI from silently creating a missing tag from the repository default branch.

The script never force-pushes, moves a tag, uses `--clobber`, or repairs an existing release. A release already present for the tag causes a failure.

The uploaded asset set always includes the generated host, Linux amd64, and
Windows amd64 offline bundles (deduplicated when the host matches a target) and
a release-level `SHA256SUMS` file. Additional files supplied with `--asset`
remain supported.

## Release notes and release type

Generated GitHub release notes are the default:

```bash
./scripts/github-release.sh --tag v6.6.0 --publish
```

Use maintained notes instead:

```bash
./scripts/github-release.sh \
  --tag v6.6.0 \
  --notes-file ./RELEASE_NOTES.md \
  --publish
```

Optional release metadata:

```bash
./scripts/github-release.sh \
  --tag v6.6.0-rc.1 \
  --title "POLIS V6.6.0-rc.1" \
  --prerelease \
  --latest false \
  --publish
```

Supported Latest policies are `automatic` (default), `true`, and `false`. `--draft` creates a draft release.

## Offline asset and optional assets

The release script generates offline assets from the exact clean `HEAD` used by
the release. It always includes the host runtime plus `linux/amd64` and
`windows/amd64`, deduplicating a target that is already the host runtime:

```text
polis-v6.6.0-offline-<GOOS>-<GOARCH>.zip
```

Each bundle contains the compiled V6 CLI for its declared target, embedded
specification and schemas, manifest, and internal checksums. The release-level
`SHA256SUMS` covers every offline bundle and every additional `--asset` file.
The host exporter is built for the Go host runtime, while target binaries are
cross-compiled with explicit `GOOS` and `GOARCH` values and are never executed
by the release script.
The exporter receives each target binary through `--executable` and records its
declared `--runtime GOOS/GOARCH` in the bundle manifest.

Supply only additional final files that have already been built and validated
by the authoritative process for that release. `--asset` does not replace or
disable the required offline bundles.

Pass `--asset` more than once when needed:

```bash
./scripts/github-release.sh \
  --tag v6.6.0 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --asset ./release-assets/polis-darwin-arm64.tar.gz
```

Before any remote mutation, the script:

1. verifies every asset is a non-empty regular file;
2. rejects duplicate basenames;
3. computes SHA-256 for the exact supplied bytes;
4. builds the host exporter, cross-compiles each target binary, and exports every offline bundle from `HEAD`;
5. creates a temporary `SHA256SUMS` file for all upload assets;
6. uploads those exact assets plus the generated bundles and `SHA256SUMS`.

After publication it independently queries the release, verifies every asset's
name and size, and compares GitHub-provided `sha256:` digests when they are
available. If the release reports itself immutable, the script also requires
`gh release verify` and uses `gh release verify-asset` for every uploaded asset.

Add `--publish` only after reviewing the preflight output:

```bash
./scripts/github-release.sh \
  --tag v6.6.0 \
  --asset ./release-assets/polis-linux-amd64.tar.gz \
  --publish
```

## Other options

Use a remote other than `origin` only when that is intentionally the Git remote that owns the release tag:

```bash
./scripts/github-release.sh --tag v6.6.0 --remote upstream
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
- the required host, Linux, and Windows offline bundles are built from the exact
  source commit and all upload bytes are hashed before publication;
- user-supplied optional asset bytes are never rebuilt by the script;
- remote state is queried again after publication;
- immutable release attestations are verified when supported and applicable.

Running the script is an explicit publication operation. The repository does not automatically publish a release merely because this script exists.
