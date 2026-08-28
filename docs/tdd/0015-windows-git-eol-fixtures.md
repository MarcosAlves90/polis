# TDD evidence — Windows Git EOL fixtures

## Red

GitHub Actions on `windows-latest` ran `go test ./...` for commit `dfc05b468359a4793b971b317e266431f04d6c9c` and failed with CRLF/LF mismatches after Git restore/apply operations:

- `TestRunApplyAppliesBuiltPackage`: `app.txt="changed\r\n"`;
- `TestApplyExactBaselinePreservesIndexAndWritesEvidenceOutsideWorktree`: `app.txt="changed\r\n"`;
- `TestApplyRejectsWrongHead`: `app mutated on wrong head: "base\r\n"`;
- `TestReversePatchRestoresAppliedChange`: `content="base\r\n"`.

The complete Windows package run exited nonzero. This is the observed regression evidence for the cross-platform test contract.

## Green design

The affected test packages now establish process-local Git configuration before `m.Run()`:

- `GIT_CONFIG_COUNT=1`;
- `GIT_CONFIG_KEY_0=core.autocrlf`;
- `GIT_CONFIG_VALUE_0=false`.

A package-level test executes `git config --get core.autocrlf` and requires exactly `false`, proving the fixture precondition rather than merely assuming it.

No production behavior is changed and the Windows CI job is not weakened or skipped. Final native Windows Green evidence is established only when GitHub Actions reruns the complete workflow successfully after this patch is committed.
