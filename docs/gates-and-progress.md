# Gate execution and progress

## Command progress

Recognized executing commands emit one `started` and one `completed` event on
stderr. Each launched configured gate also emits this lifecycle pair, even on
execution failures. Progress is advisory: it does not change gate outcomes,
output contracts, or immutable evidence. Help remains documentation-only.

With `--format json`, progress is newline-delimited JSON on stderr:

```json
{"type":"progress","event":"started","scope":"gate","id":"test.complete","action":"run configured gate test.complete","why":"the effective Project Policy selected this gate and all required prerequisites are satisfied"}
{"type":"progress","event":"completed","scope":"gate","id":"test.complete","status":"PASS","duration_ms":18420,"action":"run configured gate test.complete","why":"the effective Project Policy selected this gate and all required prerequisites are satisfied"}
```

The duration in this example is illustrative. Actual `duration_ms` values
are nonnegative integers measured at runtime using monotonic elapsed time.
Command events use `scope:"command"` and the CLI command or subcommand as `id`;
`completed.status` is `PASS`, `FAIL`, or `BLOCKED` based on the command's exit
category. Gate events report their actual execution status. If gates execute
concurrently, completion order may differ from start order. Gates that were
skipped, omitted or reused do not produce execution lifecycle events; inspect
final gate outcomes for their statuses.

Intermediate progress updates continue to include `type`, `action`, and `why`
without a lifecycle `event`; they are informational checkpoints rather than
operation boundaries. Existing text progress lines retain their `POLIS
PROGRESS: action=... why=...` prefix, with added lifecycle fields on start
and completion. Standard results stay on stdout. On JSON failure, the
structured failure remains the final stderr JSON record, *after* command
completion; consumers should use the last non-progress record as the result.
Progress does not include raw command output, environment values, or secrets.

## Incremental gate runs and replay

`polis gates` now includes a versioned run manifest in JSON output. Use
`--out-run /outside/new-run.json` to save an inspectable record; the file must
not already exist. It records the source digest and HEAD, staging state,
effective policy digest, optional locked contract and baseline, commands and
repository-relative working directories, declared environment mode/names,
dependency-result identities, POLIS/runtime version, selection, jobs bound,
per-gate outcomes, exit status, duration and output digests. It never stores
environment values or command output text. The content-derived run ID is
stable for that record; a fresh nonce distinguishes every new execution.

When `--environment-id` is provided, `run.inputs` in the JSON report and
`inputs` in a saved or inspected manifest include, for example:

```json
{"environment_id":"toolchain-v1","environment_assurance":"caller_asserted"}
```

The executor (user or agent) declares this identifier. POLIS does **not**
verify that the real toolchain, external resources, or runtime environment
match it, and the declaration does **not** establish hermetic execution.
Text reports for runs, reuse, replay and inspection state the same limitation.
Older manifests without `environment_assurance` still load and retain their
original checksums; their environment identifiers have the same caller-asserted
meaning. An empty `--environment-id` records no environment assertion.

```bash
polis gates --repo /repo --gate coverage --gate lint --jobs 2 --environment-id toolchain-v1 --out-run /outside/first.json
polis gates --repo /repo --gate coverage --gate lint --environment-id toolchain-v1 --reuse /outside/first.json --out-run /outside/second.json
polis gates --inspect-run /outside/first.json --format json
polis gates --repo /repo --environment-id toolchain-v1 --replay /outside/first.json --out-run /outside/replay.json
polis gates --repo /repo --affected --reuse /outside/first.json --environment-id toolchain-v1
```

Repeat `--gate` for explicit selection; all required prerequisites (including
`test.complete` for coverage) are added. `--affected` captures staged,
unstaged, deleted and nonignored untracked changes against HEAD, or against
the baseline of an explicitly supplied external `--contract <locked.json>`.
Policy schema-v3 command and coverage gates can declare `input_paths` as
normalized file paths or directory prefixes (no globs). These paths select
gates for `--affected`; a missing mapping conservatively selects all checks.
Selection mapping alone does **not** authorize scoped reuse.

By default, `--reuse` requires the existing global source identity, including
HEAD, tracked and nonignored untracked file contents, modes, symlink targets,
index and staging metadata. Any unrelated source change invalidates reuse.
For command gates with demonstrably exhaustive inputs, the policy author may
**explicitly assert** `"input_paths_complete": true` alongside non-empty
`input_paths`. For example:

```json
{"id":"lint","mode":"command","input_paths":["backend"],"input_paths_complete":true,"command":{"argv":["go","test","./backend/..."],"cwd":".","timeout_seconds":600,"environment":{"mode":"clean","pass":["PATH","HOME"]}}}
```

The opt-in scope hashes declared files (tracked, untracked and ignored),
content, executable modes, contained symlinks, directory existence and topology
(including empty directories), directory permissions, staging and index metadata;
HEAD changes alone and unrelated files no longer invalidate that gate.
Dependencies, policy, command, contract, baseline, runtime and declared
environment identities remain enforced. Reused gates must have complete prior
passing observations and unchanged dependency identities; old global identities
cannot be silently treated as scoped. Scope snapshots are rechecked after the
run, and unsafe paths, submodules or excessive input sets fail closed.
Coverage gates retain global identity because their report lifecycle is not a
closed file-input claim. Input-path claims do **not** prove completeness:
if a command reads undeclared paths or external state, scoped reuse may be stale.
Use global fallback for opaque commands. `--environment-id` versions external
resources, Git configuration, toolchains and environment; it must change when
any can affect results. Without it, no reuse is permitted. Matching identifiers
are evidence of matching **declarations**, not proof that conditions matched.
Gate-run manifests are checksummed, not authenticated delivery proof. The
policy `input_paths_complete` field is additive
but older strict policy decoders will reject policies that use it.

Replay requires the same available source/configuration and an explicit
matching environment identifier; supply the same external policy and locked
contract when originally used. Changed/missing inputs fail before execution.
Replay uses the recorded dependency-closed selection and jobs bound, executes
every selected gate anew and records `replay_of` with a new run ID. Do not pass
`--jobs`, `--gate`, `--affected` or `--reuse` to replay. Inspection describes a
historical record; it does not mark its results current in another worktree.

`--jobs` defaults to 1 and accepts 1 through 16. Gates overlap only when every
concurrently running gate explicitly declares `"parallel_safe": true` in
the policy. This asserts that shared inputs/outputs and external resources
do not contend; otherwise the gate is exclusive. Coverage remains exclusive
because it owns report cleanup/measurement. Prerequisites must finish PASS
before a dependent starts; failure blocks dependents, not unrelated gates.
Results and evidence are emitted in stable topological plan order.

Source identity is checked before scheduling and at completion. A detected
source mutation marks evidence stale rather than binding old outcomes to the
new source. Do not edit the source concurrently; checks are not a filesystem
lock and cannot guarantee detection of a transient change restored between
observations. Gate commands and their safety declarations remain trusted.
Incremental runs are validation-only: omitted enabled gates provide no
current guarantee, and build/apply never consume these records or reuse their
results. All delivery invariants and complete project validation still apply.
Existing v3 policies remain valid; older POLIS readers reject policies that
use these newly introduced optional fields.

`--executable` embeds a selected POLIS binary without executing it, and
`--runtime` declares the target `GOOS/GOARCH` recorded in the bundle manifest.
Both default to the current POLIS executable and runtime.

`--regression-patch` is required for Red-to-Green defects and strict features.
`preflight` validates without applying; `apply` validates again before mutation.
`--commit-mode` applies only to `apply` and defaults to `none`.

`polis implementation-plan` creates a deterministic optional plan from a locked
schema-v4 or schema-v6 Change Contract while the baseline repository is clean.
By default, the contract, policy, plan, and output stay outside the target
worktree. In repository-retention mode, a contract may also be read from
`.polis/artifacts/contracts/`; the generated plan is still written to the
explicit external `--out` path. Pass the same plan explicitly to `capture-red`
and `build`; omitting it preserves the unplanned format-v5 workflow. A planned
build uses format v6, whose ninth member contains the exact supplied plan bytes
and is bound by the manifest digest, checksums, package verification, and any
detached signature.

Consumer baseline handling is independent from Project Policy validation level. The
baseline mode defaults to `strict`:

- `strict` requires exact equality with the artifact base commit;
- `compatible` accepts a different clean descendant `HEAD` only after ancestry,
  payload-context, isolated target, scope, development-proof, and policy checks
  pass;
- `permissive` also permits non-descendant history. Required locked development
  proof uses the local baseline when available, otherwise the authenticated
  format-v4/v5/v6 embedded baseline. It emits an explicit warning when ancestry is not
  proven.

New format-v5 and v6 artifacts carry `polis/polis-baseline.tar`, so permissive consumption
can replay locked development proof even when the consumer object database does
not contain the producer base commit. Valid embedded proof is not an override and
does not reduce guarantees.
The embedded baseline carries the locked commit and complete tree/blob closure, not refs or parent history. `polis build` replays the locked baseline proof from that exact embedded state, so a baseline command that requires unavailable history is rejected during build rather than producing a non-portable artifact.

A complete embedded baseline can now occupy up to 128 MiB uncompressed; the
ZIP archive and aggregate uncompressed package remain bounded at 256 MiB each.
POLIS checks the projected canonical TAR before loading its objects, then
verifies every Git object ID, commit/tree identity, and package digest. If
construction or materialization cannot complete, `polis build` reports a
locked-baseline constraint and the producer gates that did not run. A newer
producer may create a package over the former 32 MiB limit that an older reader
rejects; valid historical packages remain readable.

For historical artifacts without embedded baseline proof,
`--allow-missing-baseline-proof` may be supplied only with `permissive`. It must be
repeated for `apply`, is never inherited from preflight, and visibly reports every
waived guarantee. It does not bypass package integrity, dirty-state rejection,
patch conflicts, scope, complete target/project validation, or transactional
post-apply verification.
