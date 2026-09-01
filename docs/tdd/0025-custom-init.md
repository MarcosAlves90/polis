# TDD-0025 — Explicit Custom Policy Initialization

## Scope

Implementation evidence for SDD-0025 under POLIS V5. The defect is that `polis init` cannot bootstrap a Project Policy for a Git repository without a root-level `go.mod`.

## Red — canonical defect regression

Before production changes, the regression test `TestResolveProfileCustomSupportsNonGoRepo` was added and executed with:

```text
go test ./internal/policyinit -run '^TestResolveProfileCustomSupportsNonGoRepo$' -count=1
```

Observed exit code: `1`.

Observed behavior-specific failure:

```text
--- FAIL: TestResolveProfileCustomSupportsNonGoRepo
CUSTOM_INIT_RED: resolveProfile() error = unknown init profile "custom"; supported profiles: auto, go
FAIL
```

The Red state was then captured before the production fix with canonical POLIS V5:

```text
go run ./cmd/polis capture-red --repo <worktree> --contract /mnt/data/polis-change-custom-init-v2.json --out /mnt/data/polis-custom-init-red.patch
```

Observed result: `POLIS CAPTURE-RED: PASS`.

Captured regression patch SHA-256:

```text
b92fa862742349c8eb93ebd2959da36d347f2885aba5dce5ab6258020e72e845
```

`HEAD`, index, and worktree state were re-read after capture; capture did not stage or commit changes.

## Additional Red coverage before production implementation

The custom-policy and CLI tests were added before production implementation and the affected packages were executed:

```text
go test ./internal/policyinit ./cmd/polis -count=1
```

Observed exit code: `1`.

The failures were caused by the absent `ProfileCustom`/custom options API and absent CLI flags such as `--dry-run` and `--test-argv`, confirming the new contract was not already implemented.

## Green — regression

After the smallest production implementation for the accepted SDD behavior:

```text
go test ./internal/policyinit -run '^TestResolveProfileCustomSupportsNonGoRepo$' -count=1
```

Observed exit code: `0`.

Observed result:

```text
ok github.com/MarcosAlves90/polis/v5/internal/policyinit
```

## Green — affected packages

```text
go test ./internal/policyinit ./cmd/polis -count=1
```

Observed exit code: `0`.

Observed results:

```text
ok github.com/MarcosAlves90/polis/v5/internal/policyinit
ok github.com/MarcosAlves90/polis/v5/cmd/polis
```

## Behaviors covered

- explicit `custom` profile in a non-Go Git repository;
- schema-v3 policy with exactly eleven canonical gates;
- literal argv preservation, including spaces and `&&` as single arguments;
- default `80.0` and explicit valid coverage thresholds;
- invalid/missing argv, adapters, reports, and thresholds fail before `.polis` creation;
- custom-only inputs are rejected outside `--profile custom`;
- unsupported `auto` detection recommends `--profile custom` without adding heuristic ecosystem detection;
- dry-run returns self-validating policy JSON without filesystem, index, `HEAD`, or worktree mutation;
- dry-run does not overwrite an existing policy;
- existing Go-profile tests remain part of affected and complete validation.

## Final validation

Repository-wide policy gates and the non-Go integration fixture are recorded by the final POLIS build evidence. This document records only observed commands; no PASS is claimed for checks not executed.
