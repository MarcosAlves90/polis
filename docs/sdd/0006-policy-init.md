# SDD-0006 — Canonical policy bootstrap

Status: Accepted for implementation (alpha.6)

## Objective

Make POLIS adoption deterministic for supported project families by generating the canonical `.polis/policy.json` rather than requiring a user or model to hand-author eleven gates.

## Command

`polis init [--repo <path>] [--profile auto|go]`

Defaults:

- `--repo .`
- `--profile auto`

Alpha.6 supports exactly the Go profile. `auto` selects Go only when a root-level `go.mod` exists. Unsupported or ambiguous projects are BLOCKED; the runtime does not invent a profile.

## Safety

- target MUST resolve to a Git worktree root;
- `.polis/policy.json` MUST NOT already exist;
- runtime MUST use exclusive creation and MUST NOT overwrite an existing policy;
- runtime MUST NOT stage, commit, reset, clean, or otherwise mutate Git history/index;
- generated policy MUST validate through the same `spec.DecodePolicy` path used by build/verify/apply.

## Canonical Go profile

1. `test.complete`: `go test ./...`
2. `coverage`: `go test ./... -coverprofile=.polis/coverage.out`, adapter `go-coverprofile-v1`, metric threshold `> 80.0`
3. `lint`: `go vet ./...`
4. `typecheck`: `NOT_APPLICABLE`, fixed reason: Go test/build perform compile checks and the canonical profile defines no independent typecheck command.
5. `build`: `go build ./...`
6. `smoke`: `NOT_APPLICABLE`, fixed reason.
7. `compatibility`: `NOT_APPLICABLE`, fixed reason.
8. `dependency`: `go mod verify`
9. `migration`: `NOT_APPLICABLE`, fixed reason.
10. `security`: `NOT_APPLICABLE`, fixed reason because alpha.6 does not assume external vulnerability tools are installed.
11. `platform`: `NOT_APPLICABLE`, fixed reason because local init does not prove cross-platform runtime behavior.

All commands use `cwd: "."` and fixed timeouts defined by the profile implementation.

## Output

Success reports:

- profile
- absolute policy path
- explicit next action: review and commit `.polis/policy.json` before `polis build`

## Acceptance criteria

- Go repository auto-detects and writes one schema-valid canonical policy.
- repeated init fails without changing bytes.
- non-Git directory fails.
- unsupported repository fails without creating `.polis/policy.json`.
- explicit unknown profile fails.
- init leaves HEAD, index, and pre-existing worktree state unchanged except the new `.polis/policy.json` path.
