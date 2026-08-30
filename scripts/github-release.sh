#!/usr/bin/env bash
set -eo pipefail

START_DIR=$(pwd -P)

usage() {
  cat <<'USAGE'
Usage:
  scripts/github-release.sh --tag TAG [options]

Options:
  --tag TAG             Release tag to verify/create (required).
  --gh COMMAND          GitHub CLI command or executable path.
                        Defaults to POLIS_GH, then gh from PATH.
  --remote NAME         Git remote used for tag inspection/push (default: origin).
  --title TEXT          Explicit GitHub Release title.
  --notes-file FILE     Read release notes from FILE instead of generated notes.
  --draft               Create a draft release.
  --prerelease          Mark the release as a prerelease.
  --latest POLICY       Latest policy: automatic, true, or false (default: automatic).
  --asset FILE          Upload a final prebuilt asset. Repeat for multiple assets.
  --publish             Cross the remote-mutation boundary and create the release.
  -h, --help            Show this help.

Without --publish the script performs preflight only and does not create/push tags
or create a GitHub Release.
USAGE
}

fail() {
  printf 'POLIS RELEASE: FAIL: %s\n' "$*" >&2
  exit 1
}

resolve_command() {
  local candidate=$1
  if [[ "$candidate" == */* ]]; then
    if [[ "$candidate" != /* ]]; then
      candidate="$START_DIR/$candidate"
    fi
    [[ -x "$candidate" ]] || fail "GitHub CLI is not executable: $candidate"
    printf '%s\n' "$candidate"
    return
  fi
  command -v "$candidate" 2>/dev/null || fail "GitHub CLI command not found: $candidate"
}

sha256_file() {
  local file=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
    return
  fi
  fail "SHA-256 tool not found (sha256sum, shasum, or openssl required when assets are supplied)"
}

file_size() {
  local file=$1
  wc -c < "$file" | tr -d '[:space:]'
}

remote_tag_commit() {
  local remote=$1
  local tag=$2
  local raw line sha ref direct=''
  raw=$(git ls-remote --tags "$remote" "refs/tags/$tag" "refs/tags/$tag^{}") || return 1
  while IFS=$'\t' read -r sha ref; do
    [[ -n "${sha:-}" && -n "${ref:-}" ]] || continue
    if [[ "$ref" == "refs/tags/$tag^{}" ]]; then
      printf '%s\n' "$sha"
      return 0
    fi
    if [[ "$ref" == "refs/tags/$tag" ]]; then
      direct=$sha
    fi
  done <<< "$raw"
  printf '%s\n' "$direct"
}

assert_asset() {
  local file=$1
  [[ -f "$file" ]] || fail "asset is not a regular file: $file"
  [[ -s "$file" ]] || fail "asset is empty: $file"
}

GH_CHOICE=${POLIS_GH:-gh}
REMOTE=origin
TAG=''
TITLE=''
NOTES_FILE=''
DRAFT=false
PRERELEASE=false
LATEST=automatic
PUBLISH=false
ASSETS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ $# -ge 2 ]] || fail "--tag requires a value"
      TAG=$2
      shift 2
      ;;
    --gh)
      [[ $# -ge 2 ]] || fail "--gh requires a value"
      GH_CHOICE=$2
      shift 2
      ;;
    --remote)
      [[ $# -ge 2 ]] || fail "--remote requires a value"
      REMOTE=$2
      shift 2
      ;;
    --title)
      [[ $# -ge 2 ]] || fail "--title requires a value"
      TITLE=$2
      shift 2
      ;;
    --notes-file)
      [[ $# -ge 2 ]] || fail "--notes-file requires a value"
      NOTES_FILE=$2
      shift 2
      ;;
    --draft)
      DRAFT=true
      shift
      ;;
    --prerelease)
      PRERELEASE=true
      shift
      ;;
    --latest)
      [[ $# -ge 2 ]] || fail "--latest requires automatic, true, or false"
      LATEST=$2
      shift 2
      ;;
    --asset)
      [[ $# -ge 2 ]] || fail "--asset requires a file"
      ASSETS+=("$2")
      shift 2
      ;;
    --publish)
      PUBLISH=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$TAG" ]] || fail "--tag is required"
case "$LATEST" in
  automatic|true|false) ;;
  *) fail "--latest must be automatic, true, or false" ;;
esac

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || fail "current directory is not inside a Git worktree"
cd "$ROOT"

[[ -z "$(git status --porcelain=v1 --untracked-files=all)" ]] || fail "source worktree must be clean"
SOURCE_COMMIT=$(git rev-parse HEAD) || fail "cannot resolve HEAD"
git remote get-url "$REMOTE" >/dev/null 2>&1 || fail "Git remote not found: $REMOTE"

GH=$(resolve_command "$GH_CHOICE")
"$GH" --version >/dev/null 2>&1 || fail "selected GitHub CLI cannot execute: $GH"
"$GH" auth status >/dev/null 2>&1 || fail "GitHub CLI authentication failed: $GH"
REPO=$("$GH" repo view --json nameWithOwner --jq '.nameWithOwner') || fail "cannot resolve GitHub repository from current checkout"
[[ -n "$REPO" ]] || fail "GitHub repository resolved to an empty value"

if [[ -n "$NOTES_FILE" ]]; then
  [[ -f "$NOTES_FILE" && -r "$NOTES_FILE" ]] || fail "release notes file is not readable: $NOTES_FILE"
fi

asset_names=''
for asset in "${ASSETS[@]}"; do
  assert_asset "$asset"
  name=$(basename "$asset")
  [[ "$name" != "SHA256SUMS" ]] || fail "asset basename SHA256SUMS is reserved by the release script"
  while IFS= read -r seen; do
    [[ -z "$seen" || "$seen" != "$name" ]] || fail "duplicate asset basename: $name"
  done <<< "$asset_names"
  asset_names+="$name"$'\n'
done

LOCAL_TAG=$(git rev-parse -q --verify "refs/tags/$TAG^{commit}" 2>/dev/null || true)
if [[ -n "$LOCAL_TAG" && "$LOCAL_TAG" != "$SOURCE_COMMIT" ]]; then
  fail "local tag $TAG targets $LOCAL_TAG, expected $SOURCE_COMMIT"
fi

REMOTE_TAG=$(remote_tag_commit "$REMOTE" "$TAG") || fail "cannot inspect tag $TAG on remote $REMOTE"
if [[ -n "$REMOTE_TAG" && "$REMOTE_TAG" != "$SOURCE_COMMIT" ]]; then
  fail "remote tag $TAG targets $REMOTE_TAG, expected $SOURCE_COMMIT"
fi

if ! RELEASE_TAGS=$("$GH" release list --repo "$REPO" --limit 10000 --json tagName --jq '.[].tagName'); then
  fail "cannot inspect existing GitHub Releases for $REPO"
fi
while IFS= read -r existing_tag; do
  [[ "$existing_tag" != "$TAG" ]] || fail "GitHub Release already exists for tag $TAG"
done <<< "$RELEASE_TAGS"

TMP_DIR=''
CHECKSUM_FILE=''
UPLOADS=("${ASSETS[@]}")
cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

if [[ ${#ASSETS[@]} -gt 0 ]]; then
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/polis-release.XXXXXX")
  CHECKSUM_FILE="$TMP_DIR/SHA256SUMS"
  : > "$CHECKSUM_FILE"
  for asset in "${ASSETS[@]}"; do
    printf '%s  %s\n' "$(sha256_file "$asset")" "$(basename "$asset")" >> "$CHECKSUM_FILE"
  done
  UPLOADS+=("$CHECKSUM_FILE")
fi

printf 'POLIS RELEASE PREFLIGHT: PASS\n'
printf 'Repository: %s\n' "$REPO"
printf 'Source commit: %s\n' "$SOURCE_COMMIT"
printf 'Tag: %s\n' "$TAG"
printf 'Git remote: %s\n' "$REMOTE"
printf 'GitHub CLI: %s\n' "$GH"
printf 'Assets: %d\n' "${#ASSETS[@]}"
if [[ -n "$CHECKSUM_FILE" ]]; then
  printf 'Checksums:\n'
  cat "$CHECKSUM_FILE"
fi

if [[ "$PUBLISH" != true ]]; then
  printf 'Publication: blocked until --publish\n'
  exit 0
fi

if [[ -z "$REMOTE_TAG" ]]; then
  if [[ -z "$LOCAL_TAG" ]]; then
    git tag -a "$TAG" -m "Release $TAG"
  fi
  git push "$REMOTE" "refs/tags/$TAG:refs/tags/$TAG"
fi

CREATE_ARGS=(release create "$TAG" --repo "$REPO" --verify-tag --fail-on-no-commits)
if [[ -n "$TITLE" ]]; then
  CREATE_ARGS+=(--title "$TITLE")
fi
if [[ -n "$NOTES_FILE" ]]; then
  CREATE_ARGS+=(--notes-file "$NOTES_FILE")
else
  CREATE_ARGS+=(--generate-notes)
fi
if [[ "$DRAFT" == true ]]; then
  CREATE_ARGS+=(--draft)
fi
if [[ "$PRERELEASE" == true ]]; then
  CREATE_ARGS+=(--prerelease)
fi
case "$LATEST" in
  true) CREATE_ARGS+=(--latest) ;;
  false) CREATE_ARGS+=(--latest=false) ;;
esac
CREATE_ARGS+=("${UPLOADS[@]}")

"$GH" "${CREATE_ARGS[@]}"

META=$("$GH" release view "$TAG" --repo "$REPO" --json tagName,isDraft,isPrerelease,isImmutable,url --jq '[.tagName, (.isDraft|tostring), (.isPrerelease|tostring), (.isImmutable|tostring), .url] | join("|")') || fail "cannot verify created release"
IFS='|' read -r ACTUAL_TAG ACTUAL_DRAFT ACTUAL_PRERELEASE IMMUTABLE RELEASE_URL <<< "$META"
[[ "$ACTUAL_TAG" == "$TAG" ]] || fail "created release tag mismatch: $ACTUAL_TAG"
[[ "$ACTUAL_DRAFT" == "$DRAFT" ]] || fail "created release draft state mismatch: $ACTUAL_DRAFT"
[[ "$ACTUAL_PRERELEASE" == "$PRERELEASE" ]] || fail "created release prerelease state mismatch: $ACTUAL_PRERELEASE"

if [[ ${#UPLOADS[@]} -gt 0 ]]; then
  REMOTE_ASSETS=$("$GH" release view "$TAG" --repo "$REPO" --json assets --jq '.assets[] | [.name, (.size|tostring), (.digest // "")] | join("|")') || fail "cannot inspect release assets"
  for upload in "${UPLOADS[@]}"; do
    expected_name=$(basename "$upload")
    expected_size=$(file_size "$upload")
    expected_hash=$(sha256_file "$upload")
    found=false
    while IFS='|' read -r remote_name remote_size remote_digest; do
      [[ "$remote_name" == "$expected_name" ]] || continue
      found=true
      [[ "$remote_size" == "$expected_size" ]] || fail "remote size mismatch for $expected_name"
      if [[ -n "$remote_digest" && "$remote_digest" == sha256:* ]]; then
        [[ "${remote_digest#sha256:}" == "$expected_hash" ]] || fail "remote digest mismatch for $expected_name"
      fi
      break
    done <<< "$REMOTE_ASSETS"
    [[ "$found" == true ]] || fail "release asset missing: $expected_name"
  done
fi

if [[ "$IMMUTABLE" == true ]]; then
  "$GH" release verify --help >/dev/null 2>&1 || fail "immutable release requires gh release verify support"
  "$GH" release verify "$TAG" --repo "$REPO"
  for upload in "${UPLOADS[@]}"; do
    "$GH" release verify-asset --help >/dev/null 2>&1 || fail "immutable release assets require gh release verify-asset support"
    "$GH" release verify-asset "$TAG" "$upload" --repo "$REPO"
  done
fi

printf 'POLIS RELEASE: PASS\n'
printf 'Release: %s\n' "$RELEASE_URL"
