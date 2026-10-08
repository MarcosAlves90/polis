# SDD-0046 — Command and gate progress lifecycle

## Scope

Interactive progress on stderr is advisory telemetry. It must not influence
POLIS result/status computation, content digests, packages, project gate
evidence, authorization, or consumer mutation. No dependency or graphical
interface is introduced.

For each recognized executing command, emit exactly one `started` and one
`completed` progress record. For each configured gate **actually launched**
by the policy scheduler, emit one paired `started` and `completed` record.
Do not imply omitted, blocked by dependencies, or reused gates executed;
the authoritative gate result still identifies these gates and their statuses.

A lifecycle JSON record has `type: "progress"`, `event` (`started` or
`completed`), `scope` (`command` or `gate`), stable `id`, and existing `action`
and `why` text. Completed events additionally contain `status` (`PASS`,
`BLOCKED`, or `FAIL` for commands; the actual gate status for gates) and
nonnegative integer `duration_ms` measured with a monotonic clock. Started
events do not make a premature claim about final status or duration.
`workspace.validate` and `workspace.status` use disambiguated command IDs.

Existing intermediate action/why-only events remain informational messages;
they are not lifecycle transitions. Consumers correlate lifecycle records by
`scope` + `id` within one invocation (one start/completion per operation).
When jobs overlap, completions occur in actual observed order; their order is
not asserted to match gate policy order.

For successful commands, final result JSON remains on stdout; for failure
objects written to stderr, the `completed` record precedes the unchanged
structured failure object, preserving the final non-progress result record.
Legacy text output on stdout and exit categories are unchanged; stderr text
progress lines retain the `POLIS PROGRESS: action=... why=...` prefix and add
lifecycle metadata. `help` emits no progress.

Gate status always reflects the *executed gate outcome at completion*; later
whole-run source drift can invalidate recorded gate evidence without changing
past observations. Neither `duration_ms` nor lifecycle events are included in
package digests, normalized evidence, or deterministic project results.

## Validation

- Started and completed command events pair on PASS and failed JSON paths.
- Failed JSON output retains its existing status/exit_code/error/diagnostic
  shape and is the last stderr record.
- A launched gate receives a completion even when its command returns FAIL
  or BLOCKED. Omitted and reused gates are not described as executed.
- Gate callbacks run on the scheduler goroutine, and elapsed time is measured
  at actual worker completion, including when concurrent gate results queue.
- Existing CLI tests, static analysis and deterministic artifact checks continue
  to pass on supported toolchains.
