# TDD-0034 — Artifact-Backed Commit Intent and Local Commit Mode

## Scope

Implement SDD-0039 and GitHub issue #3: carry an optional exact commit message through Change Contract v5/v6, preserve historical v1-v4 behavior, and let a consumer explicitly request apply-only, confirmed local commit, or automatic local commit.

## Test pyramid

- **Unit/contract:** schema v5/v6 and aggregate schema inventory; message bounds and NUL/UTF-8 validation; historical v1-v4 decoding/rejection boundaries; version predicates; CLI option parsing and JSON/text serialization.
- **Integration:** real Git repositories and real `.polis` packages for start/build/verify/inspect; package digest/signature tamper; `packageapply` success/rollback and baseline modes; hooks/signing/remote-ref sentinels.
- **End-to-end:** start -> build -> verify -> inspect -> apply in none, prompt, and auto modes, asserting exact message, consumer parent, target tree, clean status, and unchanged remote refs.

Most cases belong in unit and integration suites. Use a small number of real-Git end-to-end cases for the complete artifact-to-commit path; do not replace Git invariants with mocks.

## Captured Red — schema-v5 draft is not accepted by `polis start`

The first vertical slice is the public `devstart.Start` behavior. The test is isolated in `internal/devstart/commit_intent_red_test.go`; that file is part of the captured Red contract and must remain byte-for-byte unchanged.

- `go test ./internal/devstart -run '^TestStartLocksCommitIntentV5Draft$' -count=1` — **RED**, observed rejection: `decode change contract: json: unknown field "commit"`.
- The assertion requires `polis start` to accept a strict schema-v5 draft and lock it as schema v6 while preserving a multiline message including trailing spaces and final newline.
- A clean baseline clone at `origin/main` commit `67f806b` was locked with the current v3->v4 `polis start`; `polis capture-red` validated the test-only Red patch.
- Captured patch SHA-256: `17efde7577744f4b3fecb85a9f415622053a353719e96608bb0e1a0a160218e0`.

## Observed Red/Green evidence

- `TestBuildTransportsLockedCommitIntent` first failed because package build accepted only locked schema v4; after adding the v6 producer/consumer gate it passed.
- `TestApplyAutoCommitsExactArtifactIntent` first failed because apply returned without creating a commit; it passed after exact-tree commit construction was added.
- The first commit-object implementation passed an unsupported `--cleanup` flag to `git commit-tree`; the observed command failure led to removing that flag, and the exact multiline-message test then passed.
- The post-ref index-lock failure test initially found rollback trying to rewrite an unchanged index while another process held `.git/index.lock`; rollback now skips that unnecessary write and the test passes.
- Focused coverage now includes exact message bounds/whitespace, historical v1-v4 rejection, v5-to-v6 start with and without commit metadata, artifact build/verify/inspect transport, and default apply-only behavior for a v6 artifact without intent.
- Git integration coverage includes explicit none, prompt accept/decline, auto, exact and no-final-newline messages, strict/compatible/permissive consumer parents, validated target trees, remote-ref preservation, hook/signing tripwires, wrong candidate trees, ref-CAS conflicts, index failures, final-verification failures, and logical rollback.
- CLI end-to-end tests exercise start -> build -> verify -> inspect -> auto commit and the same schema-v6 path without metadata through default apply. CLI tests also cover TTY refusal, exact prompt contents, all mode flags, and nullable JSON results.
- The captured `internal/devstart/commit_intent_red_test.go` remains byte-for-byte unchanged. Unowned schema identifiers were removed; the repository-wide search is part of final validation.

## Full validation gates

Run focused suites while progressing, then the current `.polis/policy.json` gates without weakening them:

- `go test ./...`
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out`, with normative line coverage strictly greater than 80%
- `go vet ./...`
- `go build ./...`
- `go mod verify`
- `gofmt -l .`, `git diff --check`, offline export inspection, and `npm run verify` attempt as required by the supplied engineering instructions

## Final validation results

Observed on Darwin arm64 with Go 1.27.1 and Git 2.55.0 after the final implementation diff:

- `go test ./...` — PASS, 495 tests across 22 packages.
- `go test -coverpkg=./... ./... -coverprofile=.polis/coverage.out` — PASS; normative `go-coverprofile-v1` line-union metric `5219 / 6503 = 80.255267%`, strictly greater than `80.0%`.
- `go vet ./...`, `go build ./...`, and `go mod verify` — PASS (`all modules verified`).
- `gofmt -l .` and `git diff --check` — PASS, no output.
- Offline export — PASS; the verified 14-member archive contains both Change Contract v5/v6 schemas and declares `network_required: false`; `unzip -t` reports no archive errors.
- Repository-wide search for the unowned schema host — PASS, no matches.
- `npm run verify` — unavailable because this Go repository has no `package.json`.
