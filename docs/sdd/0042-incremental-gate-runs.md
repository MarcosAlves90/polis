# SDD-0042 — Incremental, reproducible and bounded gate runs

## Scope and authority

Implements the gates-only workflow of issues #25–29. Project Policy validation,
locked development proof, producer build, package verification and transactional
consumer application remain authoritative. Incremental records cannot satisfy
artifact gate evidence and are never read by build/apply.

## Input identity and trust boundaries

Each gate identity hashes named categories: source, contract, baseline, policy,
command, environment, dependencies and POLIS/runtime version. Source identity
covers HEAD, staging entries, the staged delta, index visibility flags and
sorted tracked/nonignored worktree contents, executable bits and contained
regular-file symlink targets. Commands receive
only the existing declared environment; manifests store names/modes, never
values. External toolchain, ignored resources and environment changes require
a caller-supplied nonsecret environment version. Unversioned results execute
but are not reusable or replayable. This is not hermetic execution.

A source mutation detected before scheduling or after validation makes the run
noncurrent and invalidates passing outcomes. Concurrent source edits are not
supported; observation is not an operating-system lock. Escaping symlinks,
submodules, special files and bounded-source violations fail closed.

## Selection and reuse

Explicit selection is dependency-closed using the compiled policy graph.
Affected selection captures the union of baseline-to-index and index-to-worktree
Git deltas, including untracked and deleted paths. A worktree edit canceling a
staged change cannot hide it. Optional `input_paths` are caller assertions of
complete input mapping; unmapped gates always run and unmapped changed paths
select all enabled checks. Identities conservatively cover the entire source,
not only mapped files. Reuse requires a current, passing, complete matching
identity and an observation with successful exit status. Dependency identities
include prerequisite status and output digests, not nondeterministic durations.
Omitted gates provide no current passing guarantee.

## Records and replay

Schema-v1 JSON records have a 1 MiB limit, strict field decoding, an exact gate
inventory and a checksum-derived run ID. A random nonce makes replay a distinct
run. Records include declarations, selection/dependencies, jobs, exact input
digests and safe per-gate observations; output text is excluded. New external
records use exclusive creation with mode 0600 and never overwrite inputs.
Output-derived startup diagnostics persist only fixed categories because an
expanded executable/module name can contain a secret environment value.
Manifest reads reject special files before opening and recheck the descriptor;
Unix nonblocking opens prevent a replacement FIFO from stalling validation.
Checksums do not authenticate an untrusted author. Replay loads current policy
and contract independently and compares all declared categories before running;
it never executes commands solely supplied by a record. Replay always executes
the recorded selection afresh, preserves the recorded jobs bound and records
`replay_of`. Inspection describes history, not current gate validity.

## Scheduling

The shared executor is serial by default and accepts a bound of 1–16 gates.
A dependency must finish PASS before its dependent starts. Unrelated gates
remain runnable after a failure. Commands without `parallel_safe: true` are
exclusive; coverage is always exclusive because report cleanup/measurement
shares state. Explicit parallel safety is a trusted assertion, not inferred
from cwd or command names. Workers buffer bounded per-gate evidence; the
coordinator publishes results/evidence in deterministic topological plan order.
Delivery callers never supply selection or reuse options.

## Compatibility

Old policies omit both optional fields and stay serial/full-validation.
Older readers reject policies using the new fields. Dependency failures now
block dependents rather than running them anyway. Invalid source objects can
fail before command execution instead of producing a later cleanup failure.
No new module dependencies or persisted environment values are introduced.
