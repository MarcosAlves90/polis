# TDD evidence — Go v4 module identity

## Red

Before the migration, `go.mod` declared:

`module github.com/MarcosAlves90/polis`

and the active installation contract used:

`go install github.com/MarcosAlves90/polis/cmd/polis@latest`

That identity is incompatible with publishing the same module as Go major version `v4.0.0` under semantic import versioning.

The new module-identity contract test requires `github.com/MarcosAlves90/polis/v4` and therefore fails against the immutable baseline.

## Green

The module declaration and all repository-owned Go imports are migrated to `/v4`. Active public installation documentation is migrated to `github.com/MarcosAlves90/polis/v4/cmd/polis@latest`.

`internal/cicontract/module_identity_test.go` verifies the module declaration and public installation command. The existing installation contract is also updated to require the v4 command.

The migration changes package identity only; the CLI remains `polis` and production behavior is otherwise preserved.
