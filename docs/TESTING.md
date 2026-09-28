# Testing Strategy

POLIS uses co-located Go unit and integration tests. The committed Project
Policy runs `go test ./...`, `go vet ./...`, `go build ./...`, and module
verification; project-wide line coverage must remain above 80%. The CI workflow
also runs the race detector and coverage checks. These existing gates remain
the acceptance bar for changes.

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
