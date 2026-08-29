#!/usr/bin/env sh
set -eu

command -v go >/dev/null 2>&1 || {
  echo "go is required" >&2
  exit 1
}
command -v sonar-scanner >/dev/null 2>&1 || {
  echo "sonar-scanner is required" >&2
  exit 1
}
: "${SONAR_TOKEN:?SONAR_TOKEN must be set}"

export SONAR_HOST_URL="${SONAR_HOST_URL:-http://localhost:9000}"

mkdir -p .polis
go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out
sonar-scanner "$@"
