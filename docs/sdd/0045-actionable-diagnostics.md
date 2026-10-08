# SDD-0045 — Stable, actionable CLI diagnostics

## Scope

Machine-readable diagnostics for known operational failures. Existing exit
codes, status fields, human-readable messages, package formats and validation
criteria remain authoritative. Diagnostic metadata is advisory: it does not
perform recovery, weaken any gate or authorize an action.

## Additive JSON fields

`diagnostic.Report` adds optional `code`, `category`, `observed_cause`,
`affected_operations` and `remediation`. All values are stable ASCII identifiers
except affected operation names, which refer to the existing CLI operation or
configured gate IDs. The existing `stage`, `condition`, `expected`, `actual`,
`paths`, `contributors`, `gate_statuses`, `command`, `commands` and `not_run`
retain their meanings. `not_run` records steps that did not execute, without
implying completion. New fields do not change legacy text output.

`writeFailure` ensures every operational JSON `FAIL` emitted through it has a
`diagnostic`. An existing structured classification takes precedence. Otherwise
only the known CLI exit category is used to produce a conservative fallback;
no text or process-output pattern matching is performed on unknown errors.

| Code | Category | Observed cause | Recommended action |
|---|---|---|---|
| `POLIS_OPERATION_REJECTED` | usage | invalid_operation_input | not determined |
| `POLIS_INVALID_ARTIFACT` | artifact | artifact_validation_failed | not determined |
| `POLIS_OPERATION_BLOCKED` | blocked | operation_blocked | not determined |
| `POLIS_BASELINE_MISMATCH` | baseline | baseline_validation_failed | not determined |
| `POLIS_VALIDATION_FAILED` | validation | operation_validation_failed | not determined |
| `POLIS_APPLY_FAILED` | apply | apply_not_completed | not determined |
| `POLIS_OPERATION_FAILED` | operation | operation_failed | not determined |
| `POLIS_GATE_VALIDATION_FAILED` | gate | gate_non_pass | not determined |
| `POLIS_GATE_PREREQUISITE_MISSING` | prerequisite | missing_executable / missing_dependency / missing_environment_condition | install_required_executable / restore_required_dependency / satisfy_required_environment_condition |
| `POLIS_RED_PROBE_SCOPE_VIOLATION` | scope | out_of_scope_red_probe_paths / proposed_path_rejected | restrict_red_probe_to_test_scope / review_change_and_test_scope |
| `POLIS_BASELINE_SIZE_LIMIT_EXCEEDED` | baseline | projected_baseline_exceeds_limit | reduce_baseline_size |
| `POLIS_BASELINE_CONSTRAINT` | baseline | baseline_snapshot_unavailable | not determined |
| `POLIS_WORKFLOW_STATE_INCONSISTENT` | workflow | workflow_evidence_inconsistent | inspect_workflow_evidence |
| `POLIS_GIT_PREREQUISITE_MISSING` | prerequisite | missing_executable | install_git |

Specific gate prerequisite classification requires one observed blocked root command
with a POLIS-known prerequisite category. Other blocked gates may be dependent
operations; a distinct failed gate or an unrecognized prerequisite makes the
classification generic (`POLIS_GATE_VALIDATION_FAILED`).
Diagnostic classification never copies untrusted output text, environment
values or arbitrary error strings into semantic identifiers. The original
command context is retained in existing bounded fields.

## Acceptance and protected behavior

- Known and fallback failures are parseable without regular expressions over
  textual `error` messages.
- `affected_operations` and existing `not_run` distinguish failed and skipped
  work where the runtime can determine these facts.
- Valid success outputs and legacy text reports remain unchanged. Old JSON
  fields retain their data and meanings; consumers accepting additive JSON
  fields remain compatible. Strict closed-world consumers need a schema update.
- No new dependencies or operational side effects are introduced.
- The regressions exercise serialization, semantic classification, ambiguous
  mixed gate failures, immutable Red proof, and unchanged legacy output.
