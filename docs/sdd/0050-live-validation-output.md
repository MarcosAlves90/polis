# SDD-0050 — Live validation output and incremental results

## Problem

Issue #44 reported that POLIS already emitted command and gate lifecycle
progress, but a long-running gate still looked opaque: the running command's
stdout/stderr and the completed gate's observed result were only available after
the whole validation finished. Operators and coding agents need to see what is
actually happening while it happens, without turning a partial observation into
a final success claim or weakening immutable validation evidence.

## Locked scope

The implementation uses strict SDD/TDD with the immutable schema-6 contract
`locked-v6-issue44.json`, base commit
`66760b838854cd68c054b514e6a0a7bbf33f4d6c`, base tree
`659fa4456b253ed41cef67f7af243f4f0b649d46`, policy SHA-256
`2e744eb5a45b5237af4559084459e48ae6edd2011f0e6af73e08ff5e7aaa692c`, and locked
contract SHA-256 `9e8e909a7e2cb99c13964c4fc5eb4fa4a600f705e09f04704daef3824122979c`.
The captured Red patch SHA-256 is
`6a9cc00796d61d88b256c997332d77da68de2c2d90c0a792a9567b4d632e6fd3`. The Red
baseline failed with undefined streaming APIs before any production code was
added; only the three scoped test files were captured. Chosen files were edited
after that proof; the immutable tests were not changed.

## Design decisions

1. **Explicit opt-in.** `--stream-output` enables live events on `gates`,
   `workspace validate`, and `build`. No flag means the previous final result
   and metadata-only progress contract, byte-for-byte.
2. **Presentation-only boundary.** `internal/commandexec` tees already-captured
   bytes to a bounded non-blocking observer. Digests, byte counts, bounded
   previews, timeouts, exit classification, evidence order, and status logic are
   unchanged. The optional `StreamStats` field is excluded from JSON evidence.
3. **Fail-safe observer.** A 64-entry queue, 16 KiB chunks, and non-blocking
   offers prevent a chatty child or slow observer from blocking validation.
   Dropped chunks/bytes and observer errors are accounted but never fail a gate.
4. **Authentic attribution.** Each output chunk carries the owning gate, stream,
   per-stream chunk sequence, timing, encoding, and drop/truncation counters.
   Stage records use the real validation stages actually entered. Completed gate
   results report the observed status, configured command, reason, and duration.
   No synthetic test-case names are inferred from arbitrary output.
5. **Correlated JSON Lines.** `polis-live-v1` adds a per-command `run_id`, a
   monotonically increasing whole-stream `sequence`, and RFC3339 timestamps to
   every stderr JSON record when streaming is on. `data` is sanitized UTF-8 text
   escaped by the JSON encoder; terminal escape sequences are removed before
   presentation.
6. **Separate transports.** Final text/JSON stays on stdout. Live JSON Lines or
   sanitized `POLIS STAGE:` / `POLIS LIVE:` / `POLIS RESULT:` text stays on
   stderr. Optional `--events-file` mirrors the JSON Lines to a new regular
   non-symlink file outside the target worktree, created with `0600` and never
   overwritten. Path errors fail with usage code 2 before commands execute;
   later write errors affect only the optional log.
7. **No evidence leakage.** Live events, raw output, and events-file paths never
   enter final reports, checksummed evidence, replay manifests, coverage files,
   package schemas, Git state, or `.polis` retained artifacts.
8. **Privacy by choice.** Raw command output can contain secrets and is not
   redacted. Documentation requires a trusted consumer and cleanup guidance.

## Rejected alternatives

- Implicit streaming for all commands: secret exposure and extra stderr traffic
  would change established output for every user.
- Writing live output to stdout or final reports: this would break JSON
  parseability and pollute immutable evidence.
- Inventing per-test events from arbitrary runner text: unsupported and likely
  to report observations that never happened.
- Letting observer writes block or fail the child: a slow/broken sink must not
  change validation outcomes or deadlock a gate.
- Allowing an events file inside the worktree, as an existing path, or through a
  symlink: that would create repository-retained secret-bearing output.
- Relaxing strict policy validation: immutable gate semantics, thresholds, and
  enabled/disabled decisions remain unchanged.

## Validation

The focused regression contract runs
`go test -p=1 ./internal/commandexec ./internal/policyexec ./cmd/polis -run TestIssue44 -count=1`.
It proves live stdout before child exit, external events-file mirroring, JSON
parsing and common run identity, text control-sequence sanitization, strict
events-file path rejection, unchanged output when disabled, incremental gate
results, and stage visibility for `gates`, `workspace validate`, and `build`.
The complete package suite and the final strict POLIS validation are recorded in
the pull request and validation artifacts.
