# TDD-0038 — Agent Workspace Validation

## Red

Against the clean baseline `c675cf4`, the captured test exercised
`polis workspace validate` with a locked Green-to-Green fixture. The baseline
returned exit 2 with `unknown command "workspace"`; the enclosing Go test failed
with the declared oracle `workspace validate command should pass`. POLIS
`capture-red` passed for the locked v6 contract and produced the external Red
patch whose SHA-256 is `b5f724defec75e3b8cb64141a6de2e7af7ac35dd62acf934b704d66d98a1b79f`.
The captured test also rejects a report path inside the repository when `--repo`
points at a nested directory. Final test bytes match the captured patch.

## Green

The new command routes through the shared package producer validation path and
returns a workspace-only report. The focused CLI and help tests, external report
writer tests, and offline report-schema test pass on the implementation.
The command output distinguishes a workspace PASS from a built/verified delivery
artifact, and the external report remains outside the target repository.

## Focused target checks (observed)

- `go test ./cmd/polis -run '^(TestRunWorkspaceValidate|TestAgentHelpEveryCommand|TestAgentHelpCanonicalUsage|TestAgentHelpSafetyAndOptions)$' -count=1` — PASS.
- `go test ./docs -run '^TestCommandHelpEntriesAreCanonicalAndIndependent$' -count=1` — PASS.
- `go test ./internal/fileutil -count=1` — PASS.
- `go test ./spec -run '^TestWorkspaceValidationReportSchemaIsEmbeddedAndClosed$' -count=1` — PASS.

The workspace validation report is a local checkpoint, not a portable package.
No package build/verification result is implied by these focused tests.
