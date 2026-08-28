# SDD-0002 — Canonical project policy execution

Status: Accepted for implementation (alpha.2)

## Objective

Replace the alpha.1 free-form gate-name list with one strict project-validation policy and one deterministic executor. The model may select software changes, but it MUST NOT invent shell syntax or runtime gate semantics during POLIS verification.

## Authority split

- `polis-policy.json` is the project-owned declaration of project validation commands.
- The POLIS Specification owns the gate registry, ordering, modes, status semantics, command schema, and evidence schema.
- The POLIS runtime owns command execution and status calculation.
- `integrity` and `target-tree` are runtime gates and MUST NOT appear in project policy.
- Delivery-specific `behavior`, `regression`, and `affected` evidence are outside this slice and remain reserved for a later delivery-plan contract.

## Canonical project gate order

1. `test.complete`
2. `coverage`
3. `lint`
4. `typecheck`
5. `build`
6. `smoke`
7. `compatibility`
8. `dependency`
9. `migration`
10. `security`
11. `platform`

Every project policy MUST contain exactly these eleven gates exactly once and in exactly this order.

## Gate modes

- `command`: runtime executes one direct process invocation.
- `not_applicable`: runtime executes nothing and emits `NOT_APPLICABLE` with a non-empty project-owned reason.

`test.complete` and `coverage` MUST use `command`. All other project gates MUST explicitly choose `command` or `not_applicable`.

## Command contract

A command contains exactly:

- `argv`: non-empty JSON array of non-empty strings. No shell string exists in the schema.
- `cwd`: normalized relative POSIX-style path under the target repository. `.` is allowed. Absolute paths, backslashes, empty segments, `.` segments inside a longer path, and `..` are invalid.
- `timeout_seconds`: integer from 1 through 3600.

The runtime MUST invoke `argv[0]` directly without a shell. It MUST NOT evaluate operators such as `&&`, pipes, redirections, variables, or command substitutions.

## Status calculation

- process exits 0 -> `PASS`
- process exits non-zero -> `FAIL`
- timeout -> `FAIL`
- process cannot start because executable/environment prerequisite is unavailable -> `BLOCKED`
- `not_applicable` -> `NOT_APPLICABLE`

Overall validation status uses precedence `FAIL > BLOCKED > PASS`; `NOT_APPLICABLE` is neutral. A policy with only PASS and NOT_APPLICABLE gates is PASS.

## Evidence

Runtime emits newline-delimited JSON. Each line MUST independently satisfy the Evidence Event v1 contract.

Event forms:

- `gate_started`: `event`, `gate`
- `command_finished`: `event`, `gate`, `status`, `argv`, `cwd`, `exit_code`, `duration_ms`, `stdout`, `stderr`
- `gate_finished`: `event`, `gate`, `status`, plus `reason` exactly when status is `BLOCKED` or `NOT_APPLICABLE`

For a process that never produces a normal exit code, `exit_code` is `-1`.

## Acceptance criteria

- Alpha.1 policies using `gates:["..."]` are rejected.
- Unknown, duplicate, missing, or out-of-order project gates are rejected.
- Unknown policy fields and unknown command fields are rejected.
- Invalid cwd, timeout, mode/field combinations, and empty argv values are rejected.
- Executor never invokes a shell.
- Executor emits schema-valid NDJSON for PASS, FAIL, BLOCKED, timeout, and NOT_APPLICABLE paths.
- Runtime-derived overall status follows the declared precedence.
