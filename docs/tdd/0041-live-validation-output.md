# TDD-0041 — Live validation output and incremental results

The regression scope covers the observable boundary first: a slow helper gate
must emit stdout before its process exits, its stderr must arrive live, and the
same sequence must be readable from an external JSON Lines file while the CLI is
still running. The tests also require one run identity across all live records,
incremental `result` events, real stage IDs for `gates`, `workspace validate`,
and `build`, unchanged final stdout, strict external-file path rejection, text
control-sequence sanitization, and byte-compatible ordinary output when
streaming is disabled.

The Red state fails to compile because the streaming observer/options APIs do
not exist. After the RED proof is captured, production code is added in layers:
bounded commandexec tee, presentation-only policyexec/gaterun callbacks, and
CLI reporter/flags. All three immutable test files remain byte-identical to the
captured proof; package-scoped tests verify concurrency correlation, observer
drops/errors, cancellation, invalid UTF-8, and unchanged status/evidence
semantics.
