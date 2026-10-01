# TDD-0037 — Incremental gate validation

## Red

A clean baseline at dc1b3b6 was locked using installed POLIS 6.9.0 and the
unchanged strict project policy. `polis capture-red` captured the new CLI
workflow test before production implementation. The baseline rejected the
new flags, causing the required `incremental gate workflow unavailable`
assertion (exit 1), not an unavailable tool or dependency.

## Green coverage

The captured CLI test exercises selected execution, valid reuse, new replay
identity and explicit rejection of stale source on replay. It stays unchanged.
Additional gaterun tests cover every identity category, missing evidence,
dependency-output changes, secret-value exclusion, environment/version changes,
contract/baseline binding, current-source checks, index and deletion changes,
selection closure/fallback, empty replay selection, manifest limits/tampering
and safe output boundaries.

Review regressions first reproduced stale reuse/replay after intent-to-add
became a staged empty file, affected selection omitting staged changes canceled
in the worktree, environment values escaping through startup diagnostic reasons,
and manifest reads blocking on FIFOs. Each now passes; secret tests cover command,
module and environment diagnostics, saved manifests, CLI text/JSON and gate
evidence. FIFO validation runs in a bounded child process.

Scheduler tests use actual helper processes and external start/release files
to observe overlap and the two-job cap, exclusive unsafe execution, dependency
completion, stable output association and unrelated results after failure.
They synchronize on observable starts rather than inferring concurrency from
elapsed time. Both new packages are exercised under the race detector.

Legacy expectations change intentionally: coverage must not execute
when `test.complete` fails or cannot start; replacing a tracked coverage report
with a directory is rejected as invalid source before any command runs.
Prerequisite diagnostics still identify the category and unrun checks but no
longer persist tokens taken from output, which can contain environment secrets.
No existing test is skipped and the strict coverage threshold is unchanged.

## Delivery validation

The required validation path is full project tests, race detection, vet,
build/module verification and strict POLIS build followed by verify/inspect.
The exact execution results and artifact paths are recorded in the task
handoff rather than inferred from this design document.
