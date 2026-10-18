#!/usr/bin/env sh
set -eu

command -v go >/dev/null 2>&1 || {
  echo "go is required" >&2
  exit 1
}

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

exec go test -count=1 -timeout=15m \
  ./docs \
  ./docs/examples \
  ./spec \
  ./internal/artifactretention \
  ./internal/baselineproof \
  ./internal/changeexec \
  ./internal/changestatus \
  ./internal/commandexec \
  ./internal/devlock \
  ./internal/devstart \
  ./internal/diagnostic \
  ./internal/fileutil \
  ./internal/offlinekit \
  ./internal/packageverify \
  ./internal/pathguard \
  ./internal/policyexec \
  ./internal/policyinit \
  ./internal/policyload \
  ./internal/policyorigin \
  ./internal/policyplan \
  ./internal/signature
