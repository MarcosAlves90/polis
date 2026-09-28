# Testing Strategy

POLIS uses co-located Go unit and integration tests. The committed Project
Policy runs `go test ./...`, `go vet ./...`, `go build ./...`, and module
verification; project-wide line coverage must remain above 80%. The CI workflow
also runs the race detector and coverage checks. These existing gates remain
the acceptance bar for changes.

Large-baseline regression covers a complete locked TAR above the former 32 MiB
ceiling, producer gate execution, package verification, consumer admission,
and tamper rejection. A separate over-limit case checks that failure is
reported as a baseline constraint with producer gates not run. Package tests
keep archive and aggregate limits finite and preserve historical-format
verification.

Run `polis gates --repo .` to execute the committed configured gates locally
without building or verifying a delivery artifact. A passing gate report is
not package verification; delivery still uses `polis build` followed by
`polis verify`.

Red probe scope preflight tests cover exact and directory-prefix matches,
multiple rejected paths with their `test_scope.allowed_paths` rule, invalid
path and stale-baseline rejection, text/JSON output, and preservation of the
source repository. `capture-red` tests still validate actual patch paths and
verify that rejected probes produce no proof.

For repository artifact retention, unit tests cover strict manifest parsing,
HEAD/working-copy agreement, content-addressed atomic writes, path containment,
and Git status filtering. Integration tests use temporary committed Git
repositories to exercise producer behavior, retained inputs, compatibility
when the manifest is absent, and preservation of the real index and refs. CLI
tests verify text and JSON reporting across the producer commands.

Run the focused package tests during development, then run `go test ./...`,
`go test -race ./...`, project coverage, `go vet ./...`, `go build ./...`,
`go mod verify`, and the POLIS build/verify flow before delivery. A failing
test is fixed or removed; it is not skipped indefinitely.
