# Using POLIS V6

This guide covers the operational V6 workflow. Normative package and schema
rules live in the [POLIS Specification V6](../spec/POLIS-SPEC-v6.md).

## Repository map

- `guide/` defines the engineering workflow, scope, safety, and evidence duties.
- `spec/` defines machine contracts for packages, schemas, evidence, and apply.
- `cmd/polis` and `internal/` provide the deterministic Go implementation.

## Command reference

```bash
polis help [command]
polis doctor [--format text|json]
polis init --repo /path/to/repo [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--dry-run]
polis plan --repo /path/to/repo [--policy /outside/policy-v3.json] [--format text|json]
polis gates [--repo /path/to/repo] [--policy /outside/policy-v3.json] [--contract /outside/locked-v6.json] [--gate <id> ... | --affected] [--jobs <1..16>] [--environment-id <nonsecret-version>] [--reuse /outside/run.json | --replay /outside/run.json | --inspect-run /outside/run.json] [--out-run /outside/new-run.json] [--format text|json]
polis workspace validate --repo /path/to/repo --contract /outside/locked-v6.json [--policy /outside/policy-v3.json] [--regression-patch /outside/regression.patch] [--implementation-plan /outside/plan.json] [--out-report /outside/workspace-validation.json] [--format text|json]
polis start --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/draft-v5.json --out /outside/locked-v6.json
polis status [--repo /path/to/repo] [--contract /outside/retained-locked-contract.json] [--format text|json]
polis implementation-plan --repo /path/to/repo [--policy /outside/policy-v3.json] --contract /outside/locked-v6.json --out /outside/implementation-plan.json
polis check-red-scope --repo /path/to/repo --contract /outside/locked-v6.json --path tests/new_test.go [--path tests/another_test.go ...] [--format text|json]
polis capture-red --repo /path/to/repo --contract /outside/locked-v6.json [--implementation-plan /outside/implementation-plan.json] --out /outside/regression.patch [--format text|json]
polis build --repo /path/to/repo --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/locked-v6.json --regression-patch /outside/regression.patch [--implementation-plan /outside/implementation-plan.json] --out /outside/output
polis verify [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis inspect [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis preflight --repo /path/to/repo [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis apply --repo /path/to/repo [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--commit-mode none|prompt|auto] [--format text|json] [--signature artifact.polis.sig --trusted-key public.pem] artifact.polis
polis sign --key private.pem --out artifact.polis.sig [--format text|json] artifact.polis
polis export --out /outside/polis-v6-offline.zip [--format text|json] [--executable <file>] [--runtime <GOOS/GOARCH>]
```

`polis help` lists all commands. `polis help <command>`, `polis <command> -h`,
and `polis <command> --help` show the same successful, read-only operational
instructions; use these help entry points without other arguments.
`polis -h` and `polis --help` are equivalent top-level aliases.
The [agent command instructions](#agent-command-instructions) below are the
canonical source embedded in the CLI, including its command index and syntax.
No repository checkout or network access is needed to read installed help.

### Command progress

Every executing POLIS command reports intermediate progress before material
work starts. Each progress event names the current `action` and explains
`why` that action is required. Help is documentation-only and does not emit
execution progress.

With text output, progress is written to stderr as `POLIS PROGRESS` lines so
the command result on stdout remains unchanged. With `--format json`, progress
is written to stderr as newline-delimited JSON objects with
`"type":"progress"`, `"action"`, and `"why"`. Successful JSON results remain
one JSON object on stdout. If a JSON command fails, its existing structured
failure object is the final JSON record on stderr after the progress records.
Consumers that capture JSON failures must therefore read stderr as JSON Lines
and use the final non-progress record as the command result.

Gate-running commands also emit a progress event immediately before each
configured gate starts. Progress never includes captured command output,
environment values, secrets, or terminal-only rendering state.

### Incremental gate runs and replay

`polis gates` now includes a versioned run manifest in JSON output. Use
`--out-run /outside/new-run.json` to save an inspectable record; the file must
not already exist. It records the source digest and HEAD, staging state,
effective policy digest, optional locked contract and baseline, commands and
repository-relative working directories, declared environment mode/names,
dependency-result identities, POLIS/runtime version, selection, jobs bound,
per-gate outcomes, exit status, duration and output digests. It never stores
environment values or command output text. The content-derived run ID is
stable for that record; a fresh nonce distinguishes every new execution.

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
normalized exact paths or directory prefixes (no globs), for example
`"input_paths": ["internal", "go.mod"]`. Mapping is a caller assertion of
complete gate inputs, not inferred dependency analysis. Unmapped gates are
always included; any unmapped changed path falls back to all configured
checks. Policies without mapping continue running all configured checks.

Only a prior passing result with a complete matching identity can be reused.
Reports distinguish `executed`, `reused`, `omitted`, and dependency-blocked
gates; stale results name changed categories in text and JSON. Source
identity conservatively covers all tracked and nonignored untracked files,
including file contents, executable bits, contained symlink targets and
staging entries, the staged delta and index visibility flags; any source change
invalidates reuse, even if unrelated to a gate's mapping. Submodules, escaping
symlinks, nonregular source files,
more than 100,000 files or more than 256 MiB of source fail closed.

Ignored files, tools, external services/resources, Git configuration and
environment values are not automatically fingerprinted. `--environment-id`
is an explicit nonsecret caller-provided version assertion for these inputs
and must change whenever any can affect a result. Without it, gates execute
normally but results are not reused and replay is refused. Never put secrets
in the identifier or literal command arguments. This is provenance under
declared inputs, not hermetic execution or authentication of an untrusted
manifest. Records are caller-owned and checksummed, not signed delivery proof.
Startup failure reasons use fixed prerequisite categories rather than tokens
from command output, since expanded command/module names can contain secrets.

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

## Canonical V6 delivery flow

By default, the canonical producer path keeps Project Policy, Change Contract,
regression patch, package, signature, and caller-owned records outside the
target repo.

Run `polis start` on a clean baseline. It locks the policy, Specification,
Change Contract, test scope, and proof requirements for later producer steps.

Before preparing a Red probe, run `polis check-red-scope` with each proposed
repository-relative file path and the locked Red-to-Green contract. The files
do not need to exist yet. The command reports every path as accepted or
rejected, lists the effective `test_scope.allowed_paths` and
`scope.allowed_paths`, and names the rejecting rule for each failure. Text and
JSON contain the same decisions. Exit code 0 means all proposed paths fit;
exit code 6 means a scope or contract validation failed. This check runs no
regression command, writes no patch, and does not modify the source repository.
It checks the proposed list only; `capture-red` still validates every path in
the actual captured patch before executing the Red command.

Use `polis capture-red` for Red-to-Green work. Use the locked contract with
`polis build`; the resulting package can be inspected, verified, and applied by
the consumer workflow.

Run `polis status --repo <path>` when resuming a repository-retained workflow.
It summarizes the selected locked contract, baseline, retained evidence, gates,
and next action. If more than one contract is retained, select one with
`--contract`. With external artifact retention, status reports that no persisted
workflow state is available. Before a verified package exists, status directs
the developer to continue implementation; source changes and a generated plan
alone do not prove that implementation is complete. A linked retained plan or
Red proof that fails validation is shown as incomplete, while artifacts bound
to another contract are excluded from the selected change summary. In JSON,
`next_action.action` names a workflow step and `next_action.command` is present
only when that step has a POLIS command, such as `polis start` or
`polis capture-red`.

## Repository artifact retention

A repository can commit `.polis/artifact-retention.json` to choose where POLIS
keeps generated artifacts. The optional strict manifest follows
[`artifact-retention-v1.schema.json`](../spec/schemas/artifact-retention-v1.schema.json):

```json
{
  "schema_version": 1,
  "mode": "repository"
}
```

`mode` is `repository` or `external`. If the manifest is absent, POLIS uses
`external` for compatibility. POLIS requires a present manifest to be committed
and byte-for-byte unchanged from `HEAD`; invalid or uncommitted preferences
fail closed. This setting is independent of Project Policy and works with
either committed or external `--policy` input.

In `repository` mode, `start`, `implementation-plan`, `capture-red`, and `build`
keep writing their explicit `--out` working results as before and also publish
exact, content-addressed copies under `.polis/artifacts/`. Contracts, plans,
regression proofs, bounded validation evidence, and `.polis` packages use
separate class directories. Later producer steps can read contracts, plans,
and proofs from their matching managed class. CLI output lists retained paths.

Retained files remain ordinary visible Git files. POLIS never stages, commits,
or deletes them. Only `.polis/artifacts/` is omitted from producer cleanliness
and application-change delta checks; the retention manifest and all other
paths remain subject to the normal checks. Committed artifacts stay in the
authenticated baseline and count toward the existing baseline size limit. If
the repository later changes to `external`, existing retained files are left
untouched.

To carry producer-authored commit intent, put the optional field below in a
strict Change Contract schema-v5 draft before `polis start`:

```json
{
  "commit": {
    "message": "feat(apply): support artifact-backed commit creation\n\nPreserve the validated target tree."
  }
}
```

`polis start` locks schema v5 as schema v6 while preserving the message. The
existing schema-v3 draft to schema-v4 locked flow remains supported for
contracts without commit metadata. New packages keep format v5 and the existing
eight-member inventory; the message remains in the checksummed
`polis/polis-change.json` member. A trusted detached signature authenticates it
when supplied and verified; checksums alone do not identify the producer.

The message is limited to 16,384 Unicode scalar values and 64 KiB of UTF-8,
must be non-empty, valid UTF-8, and contain no NUL. POLIS preserves whitespace,
multiline content, and final-newline presence without imposing a universal
commit-message convention.

Consumers still apply only by default. Use `--commit-mode prompt` to review the
exact message and target tree and confirm before real mutation, or
`--commit-mode auto` to explicitly authorize a local commit without prompting.
Both modes require artifact commit metadata. A refusal or unavailable TTY is
blocked and leaves HEAD, index, and worktree unchanged. A created commit uses
the validated consumer HEAD as its parent and exactly the validated consumer
target tree; successful commit mode leaves the repository clean. POLIS does not
run commit hooks, request signing, push, or change remote refs. Apply JSON
reports `committed`, `commit_sha`, and `commit_message`.

Canonical external-policy execution is zero-residue during build, verify,
inspect, preflight, and `apply --commit-mode none`; these commands preserve
`HEAD`, the real index, refs, Git configuration, and persistent Git objects.
An explicitly authorized prompt/auto success intentionally creates a local
commit and ref. A failed commit attempt restores `HEAD`, index, and worktree
when rollback can be verified; an unreachable Git object may remain.

## Project Policy and validation

`strict` is the default and preserves the strongest current behavior. Existing
V6 policies may omit the field for compatibility.

`standard` keeps `test.complete` required and disables generated coverage by
default. `minimal` disables generated project-quality gates by default.

Use repeated `--disable-gate <id>` only for generated project gates. Every
disabled gate is recorded with its reason; safety, integrity, baseline,
development-proof, and transactional invariants remain mandatory.

`polis init --profile auto` recognizes a root Go module only. Use `custom` for
other ecosystems and provide direct argv for test and coverage commands. POLIS
does not synthesize shell commands.

### Portable project commands

Policy commands are executed as direct `argv`, not through a shell. `polis plan`
reports a validation error for recognized shell command-string forms such as
`bash -lc`, `sh -c`, `powershell -Command`, and `cmd.exe /c`; producer and
consumer validation apply the same check before gates run. The check is lexical
and does not treat arguments to a direct executable as shell syntax.

Use `python -m pytest` for a single Python test command. For multiple tools or
working directories, put a runner under version control in the project and
declare one direct command such as
`python scripts/validate_project.py`. The
example at [`docs/examples/validate_project.py`](examples/validate_project.py)
can be copied to `<project>/scripts/validate_project.py` and adjusted to the
project's directory layout. It invokes pytest through
`sys.executable -m pytest`, runs Flutter directly with an explicit `cwd`,
preserves child exit codes, and reports missing required tools/modules as
failures. It never uses `shell=True`.

For `environment.mode=clean`, POLIS starts with an empty deterministic base,
adds only present Windows bootstrap variables (`SystemRoot`, `WINDIR`,
`COMSPEC`, `PATHEXT`, `TEMP`, `TMP`, `ProgramFiles`, `ProgramFiles(x86)`,
`ProgramW6432`, `USERPROFILE`, `LOCALAPPDATA`, `APPDATA`, `HOMEDRIVE`, and
`HOMEPATH`), then adds values named by the policy's explicit `pass` list.
Windows variable names are matched case-insensitively and emitted once. Missing
values are not invented; arbitrary variables and credentials remain excluded.
`inherit` continues to pass the process environment unchanged. Environment
values are never included in evidence or artifacts.

Do not let POLIS rewrite a policy already bound to a Change Contract or
artifact. If a project's command changes, create a new effective policy and
lock a new baseline with `polis start` before building a replacement artifact.

Each gate may declare `depends_on`. POLIS adds essential dependencies, rejects
unknown IDs, cycles, and enabled gates depending on `not_applicable` gates, then
executes a deterministic topological order.

`polis plan` is read-only. It reports the policy digest, commands, enabled and
disabled gates, dependency edges, execution order, mandatory invariants, and
the guarantees provided or absent by the effective policy. Repeat
`--defer-gate <id>` to preview transferring enabled gate validation to the
consumer. A producer-executed gate cannot depend directly or transitively on a
deferred gate.

Use `polis gates [--repo <path>] [--policy <policy-v3.json>] [--format text|json]`
to execute the configured enabled project gates locally. The command reports
each gate result and whether it was executed, along with the effective enabled
and disabled gate lists. Its output explicitly sets
`delivery_artifact_built` and `delivery_artifact_verified` to `false`: passing
Project Policy gates does not build or verify a delivery artifact. Use
`polis build` and `polis verify` for the delivery workflow.

When a configured command cannot start its intended checks, POLIS reports its
gate as `BLOCKED`, names a missing executable, dependency, or environment
condition when the startup diagnostic is clear, and says the intended checks
did not run. Text and JSON gate reports keep genuine assertion failures as
`FAIL` and mark those checks as executed. The same distinction applies to Red
regression proof: a missing prerequisite cannot satisfy its expected nonzero
exit code or output oracle.

## V6 contract summary

- Unplanned builds use package format v5 with eight regular members under `polis/`. Explicitly planned builds use format v6 with the exact plan as a ninth member; both include the authenticated `polis/polis-baseline.tar`. Valid historical v2-v5 formats remain readable.
- Project Policy uses schema v3 with command environments and gate configuration.
- New builds accept locked Change Contract schema v4 for compatibility and schema v6 for the new producer flow. Schema v5 is a draft accepted by `polis start`, which locks it as v6. Schemas v1-v4 retain their existing meaning.
- Formats v5 and v6 use Evidence v3 to record deferred gates as `DEFERRED`; formats v2-v4 retain Evidence v2 semantics. Both use bounded-output counts and digests, not raw streams.
- `polis inspect` reports plan presence, schema, strategy, step count, and requirement-to-plan-to-proof traceability after successful package verification. JSON separates test and implementation step IDs; `proof_plan_steps` lists each step declaring the linked contract proof with its ID and kind. Historical and unplanned artifacts report plan absence.
- `polis build --defer-gate <id>` may repeat the option. Deferred gates are skipped by the producer and run with the packaged policy during consumer `preflight` and `apply`.
- Detached Ed25519 signatures authenticate exact package bytes.
- Coverage adapters are Go coverprofile, LCOV, and Cobertura.
- Strict consumer mode keeps exact Git baseline identity; compatible retains ancestry proof; permissive can source locked development proof from local or embedded baseline state while retaining deterministic exact consumer target-tree validation. Missing proof can be waived only through the explicit permissive-only override.
- Consumer validation is isolated and `apply` is transactional. `--commit-mode none` preserves apply-only behavior; explicit prompt/auto modes create a local commit only after the post-apply target-tree check.

V6 can read supported historical V5 policies and Change Contracts for migration.
It does not create new legacy producer contracts.

## Strict SDD/TDD

Strict drafts contain the Specification, requirement-to-acceptance traceability,
test scope, and proof command.

Strict features and defects require Red-to-Green proof. Strict
`behavior_preserving` changes require the same characterization command to pass
on baseline and target without manufacturing a Red state.

Captured strict Red paths are immutable. Later steps revalidate the repository,
Specification, policy hash, baseline lock, and captured tests.

## Artifact and trust boundaries

The verifier treats `.polis` bytes as untrusted input and enforces bounded
archive, contract, evidence, patch, and event sizes.

Change Contract scopes are checked against the Git base-to-target path set.
Rename sources and destinations are both checked.

`polis sign` creates a detached Ed25519 signature over the artifact SHA-256.
Consumers must supply both `--signature` and `--trusted-key`; the package never
chooses its own trust root.

## Offline runtime

`polis export` creates a deterministic, checksummed ZIP containing the V6 CLI,
specification, schemas, manifest, and usage guide. It never reads target
repository data or project policy and never overwrites an existing output.

The bundle is not a delivery `.polis` package and must not be passed to
`polis verify`. It requires Git and a matching OS/CPU runtime; configured project
commands may have their own dependencies.

See the [offline runtime guide](../spec/POLIS-OFFLINE.md) and the
[release process](releases.md) for distribution details.

## Exit categories

- `0` PASS
- `2` usage error
- `3` invalid artifact or signature
- `4` blocked environment or commit authorization
- `5` baseline mismatch
- `6` validation failure
- `7` apply failure

## Agent command instructions

These marked blocks are consumed verbatim by command help, normalizing CRLF to LF
for cross-platform checkouts. Edit them here rather
than maintaining a second help registry. Tests enforce command coverage, required
sections, help aliases, and agreement with the embedded guide. There is no separate
JSON help format; `--format json` on supported commands formats execution results,
not help. Flags precede positional artifacts, and repeated argv flags supply one
argument per occurrence, never a shell command string. Paths in examples are
placeholders: choose existing inputs and absent external output files as required.
Do not reduce validation, waive proof, apply, or commit without the relevant
authorization. Git checks and isolated execution are not a sandbox for untrusted
project commands; review the code and policy before executing them.

### help

<!-- command-help: help -->
```text
Usage:
  polis help [command]
Purpose:
  show general or command-specific help
When to use:
  Discover commands or read operational guidance before selecting a workflow step.
Prerequisites:
  A runnable POLIS binary; no Git repository, policy, credentials, or network needed.
Required inputs:
  None for the command index; one known command name for detailed instructions.
Workflow:
  Read help, then doctor, then the relevant producer or consumer command.
Reads/writes:
  Reads embedded documentation only; writes stdout; does not execute a command.
Options/defaults:
  No command name shows the index. Top-level -h/--help also show the index.
  polis <command> -h/--help shows that command's guidance without other arguments.
Outcomes:
  Success (0): select a documented command. No environment-blocked outcome.
  Failure (2): unknown command or extra arguments; correct the request and retry.
Examples:
  polis help
  polis help build
  polis apply --help
Do not use:
  Help as evidence that prerequisites, project gates, or artifact validation passed.
```
<!-- /command-help -->

### doctor

<!-- command-help: doctor -->
```text
Usage:
  polis doctor [--format text|json]
Purpose:
  check Git and runtime prerequisites
When to use:
  Before starting a workflow or diagnosing a missing Git executable.
Prerequisites:
  A runnable POLIS binary; Git must be on PATH and able to report its version.
Required inputs:
  None; no repository or policy is read.
Workflow:
  After help, check the runtime, then inspect the repository and effective policy.
Reads/writes:
  Reads runtime information and PATH; executes git --version; writes a report only.
Options/defaults:
  --format defaults to text; json reports the same version and runtime observations.
Outcomes:
  Success (0): Git responds; inspect project dependencies separately before start.
  BLOCKED (4): Git missing or cannot run; repair PATH/environment and retry.
  Failure (2): invalid arguments/format; correct syntax. This is not a gate run.
Examples:
  polis doctor
  polis doctor --format json
Do not use:
  Doctor PASS as proof that project dependencies, tests, or consumer validation work.
```
<!-- /command-help -->

### init

<!-- command-help: init -->
```text
Usage:
  polis init [--repo <path>] [--profile auto|go|custom] [--validation-level strict|standard|minimal] [--disable-gate <id> ...] [--test-argv <arg> ... --coverage-argv <arg> ... --coverage-adapter <adapter> --coverage-report <path> [--coverage-threshold <percent>]] [--dry-run]
Purpose:
  create or preview a Project Policy
When to use:
  Bootstrap or preview policy before locking a change; prefer --dry-run for review.
Prerequisites:
  Git worktree; auto/go requires a root go.mod. Custom commands must come from the
  project's actual configuration. Non-dry-run requires an absent .polis/policy.json.
Required inputs:
  Custom strict profile: repeated --test-argv, --coverage-argv, adapter, and report.
  Custom standard requires test argv; minimal may omit it. Do not lower guarantees
  merely to avoid specifying commands; any reduction needs explicit justification.
Workflow:
  Preview and review policy, then plan; use the same effective bytes for start/build.
Reads/writes:
  Reads repository root/profile information; does not run generated project gates.
  --dry-run prints validated JSON only. Otherwise creates .polis/policy.json and its
  directory without overwriting or committing; review/commit only with authorization.
Options/defaults:
  --repo defaults to .; --profile auto detects Go only; go forces Go, custom uses argv.
  --validation-level defaults to strict; standard omits generated coverage by default,
  minimal omits generated quality gates. Mandatory safety/proof checks still apply.
  --disable-gate repeats project gate IDs; strict cannot disable test.complete or
  coverage; standard cannot disable test.complete. Other invalid IDs are rejected.
  --test-argv/--coverage-argv repeat individual arguments, default absent (custom only).
  --coverage-adapter/--coverage-report default absent (custom only); supply both with
  coverage argv. Adapters: go-coverprofile-v1, lcov-v1, cobertura-v1.
  --coverage-threshold defaults to 80 percent, uses > comparison (custom only).
  --dry-run defaults false. There is no --format; preview is always policy JSON.
Outcomes:
  Success (0): review policy and run plan; no gate or delivery proof was produced.
  Blocked prerequisite or failure (2): inspect the error, fix profile/inputs/environment
  or choose an absent output; preserve existing policy, do not weaken validation.
Examples:
  polis init --repo /path/to/repo --profile auto --dry-run
  For non-Go projects, supply reviewed direct argv with --profile custom; no shell.
Do not use:
  To overwrite policy, infer non-Go commands, silently reduce gates, or claim tests ran.
```
<!-- /command-help -->

### plan

<!-- command-help: plan -->
```text
Usage:
  polis plan [--repo <path>] [--policy <policy-v3.json>] [--defer-gate <id> ...] [--format text|json]
Purpose:
  compile and report the effective Project Policy
When to use:
  Review enabled/disabled gates, dependencies, execution order, and absent guarantees.
Prerequisites:
  Git worktree and valid effective policy; external policy must be outside the worktree.
Required inputs:
  --policy external schema-v3 JSON, or committed unchanged .polis/policy.json.
Workflow:
  After policy review and before start/build; unlike implementation-plan, this reports
  Project Policy execution, not a contract-bound implementation sequence.
Reads/writes:
  Reads repository/policy; writes a report only; does not execute project commands,
  create a plan file, lock a contract, or build/verify an artifact.
Options/defaults:
  --repo defaults to .; --policy defaults to committed .polis/policy.json.
  --format defaults to text; json exposes the same effective plan.
  --defer-gate repeats enabled gate IDs (default none); producer-executed gates cannot
  depend on deferred gates. This previews deferral; repeat selections explicitly in build.
Outcomes:
  Success (0): review missing guarantees, then start or build at the valid workflow step.
  Blocked prerequisite or failure (6): repair policy/dependencies/environment and retry;
  do not treat this as a passing gate run. Invalid syntax/format returns 2.
Examples:
  polis plan --repo /path/to/repo --policy /outside/policy-v3.json --format json
Do not use:
  Instead of gates, start, or implementation-plan; no checks or development proof ran.
```
<!-- /command-help -->

### gates

<!-- command-help: gates -->
```text
Usage:
  polis gates [--repo <path>] [--policy <policy-v3.json>] [--contract <locked.json>] [--gate <id> ... | --affected] [--jobs <1..16>] [--environment-id <nonsecret-version>] [--reuse <run.json> | --replay <run.json> | --inspect-run <run.json>] [--out-run <external-new.json>] [--format text|json]
Purpose:
  run configured project gates selectively with provenance without building a delivery artifact
When to use:
  Validate all gates, a dependency-closed subset, or gates affected by Git changes;
  inspect or replay a prior run when reproducibility and iteration speed matter.
Prerequisites:
  Git worktree and a valid effective policy. Reuse/replay also requires matching
  recorded inputs and an explicit nonsecret --environment-id for external inputs.
Required inputs:
  Optional external schema-v3 --policy and locked --contract; repeat --gate for
  selection, or use --affected. Inspect/replay/reuse take a prior run manifest.
Workflow:
  Add required prerequisites to selected gates; only reuse a prior PASS with a
  complete matching identity. Replay reexecutes the recorded selection and bound.
Reads/writes:
  Reads policy/source/manifest and executes commands in the real worktree. Commands
  are not sandboxed. Optional --out-run writes a new external manifest without
  environment values or command output; a delivery artifact is not built or verified.
Options/defaults:
  --repo defaults to .; --policy defaults to committed .polis/policy.json; --format
  defaults to text; --jobs defaults to 1 (range 1..16). --gate and --affected are
  mutually exclusive. Parallel overlap requires every concurrent gate to declare
  parallel_safe. --inspect-run only accepts --format; --replay forbids selection,
  reuse and explicit jobs. --out-run must be a new external file.
Outcomes:
  Success (0): selected/configured gates pass; this is local validation, not delivery
  proof. BLOCKED (4): intended checks did not run or prerequisites failed. Failure (6):
  inspect per-gate outcomes and stale-input categories. Invalid syntax returns 2.
Examples:
  polis gates --repo /repo --gate coverage --gate lint --jobs 2 --environment-id toolchain-v1 --out-run /outside/run.json
  polis gates --repo /repo --affected --reuse /outside/run.json --environment-id toolchain-v1
  polis gates --inspect-run /outside/run.json --format json
Do not use:
  As artifact verification, strict development proof, or a read-only check of untrusted
  code. Never place secret values in environment identifiers, manifests, or arguments.
```
<!-- /command-help -->

### workspace

<!-- command-help: workspace -->
```text
Usage:
  polis workspace validate --repo <path> --contract <locked-v4-or-v6.json> [--policy <policy-v3.json>] [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--out-report <new-external.json>] [--format text|json]
  polis workspace status --repo <path> --contract <external-locked-v4-or-v6.json> --report <external-workspace-validation-v1.json> [--policy <policy-v3.json>] [--format text|json]
Purpose:
  validate a locked change without a package, or compare an unsigned checkpoint's source identity
When to use:
  Use validate after implementation; use status to resume by comparing a saved report with the current checkout.
Prerequisites:
  Validate requires a Git worktree, clean real index, nonempty in-scope delta, locked strict contract,
  matching policy, required development proof, and project gate dependencies. Status requires a
  version-1 external report, external locked contract, matching policy, and a comparable source snapshot.
Required inputs:
  Validate: --contract; exact Red proof for Red-to-Green work and same plan bytes if used.
  Status: external --contract and an external workspace-validation-v1 --report.
Workflow:
  start -> required proof -> implementation -> workspace validate. Workspace status can then compare
  recorded base/tree/contract/policy identities. Use build -> verify when portable delivery is required.
Reads/writes:
  Validate replays proof and runs enabled gates in isolated worktrees, then checks the target tree;
  it preserves HEAD/index/source and creates no .polis package. Optional --out-report writes a new
  external checkpoint. Status reads a bounded external report and current identities only; it does
  not execute gate commands or intentionally change the repository. Git may invoke configured clean
  filters while creating its temporary-index snapshot; status is not a sandbox. Neither command
  authenticates reports.
Options/defaults:
  --repo defaults to .; --policy defaults to committed .polis/policy.json. Validate requires
  --contract; Red-to-Green requires --regression-patch. Status requires external --contract and --report.
  Reports must be regular non-symlink files outside the resolved worktree. --format defaults to text.
Outcomes:
  Validate success (0) means the exact target passed locked proof and enabled non-deferred gates;
  JSON says workspace_validated=true and delivery_artifact_built/verified=false. Status success (0)
  means comparison completed; JSON reports source_snapshot_matches, source_snapshot_differs, or
  checkpoint_unavailable. Status always says report_authenticated=false, current_validation_established=false,
  proof_input_digests_bound=false, and delivery_artifact_verified=false. Run validate for fresh gates.
  Malformed inputs or validation failure return 6; invalid syntax returns 2.
Examples:
  polis workspace validate --repo /repo --contract /outside/locked-v6.json --regression-patch /outside/red.patch --out-report /outside/workspace.json --format json
  polis workspace status --repo /repo --contract /outside/locked-v6.json --report /outside/workspace.json --format json
Do not use:
  Status as fresh validation, signature/producer authentication, or package verification. Version-1
  reports do not bind Red proof or plan digests; a matching source snapshot is not validation evidence.
  Use build -> verify -> preflight/apply for portable delivery and consumer handoff.
```
<!-- /command-help -->

### start

<!-- command-help: start -->
```text
Usage:
  polis start --repo <path> [--policy <policy-v3.json>] --contract <draft-v3-or-v5.json> --out <locked-v4-or-v6.json>
Purpose:
  lock a strict Change Contract baseline
When to use:
  Before test/prod edits, after requirements, scope, proof oracles, and policy are reviewed.
Prerequisites:
  Requires a clean committed Git worktree/index (managed retained artifacts excluded), valid policy,
  strict draft, and absent external output. Preserve user edits rather than reset/stash.
Required inputs:
  --repo, external --contract schema-v3 or schema-v5 strict draft, and external --out.
  Draft contains Specification, REQ-to-AC links, change/test scopes and proof commands.
Workflow:
  Locks v3 to v4 or v5 to v6; then optionally implementation-plan. Features/defects
  require check-red-scope and capture-red before implementation; behavior_preserving
  uses the same Green characterization command on baseline and target, without Red.
Reads/writes:
  Reads Git identity, policy, draft and committed retention preference; writes locked
  JSON binding baseline/policy/Specification. Repository retention also publishes a
  .polis/artifacts/contracts/ copy. Does not edit application code, stage, or commit.
Options/defaults:
  --repo/--contract/--out are required, no defaults. --policy defaults to committed
  unchanged .polis/policy.json; explicit schema-v3 policy must be outside the worktree.
  There is no --format; reuse the exact effective policy bytes for later build.
Outcomes:
  Success (0): keep the locked contract immutable and establish required development proof.
  Blocked prerequisite or failure (2): repair dirty state/invalid draft/policy/output
  with authorization; stop implementation until a valid baseline is actually locked.
Examples:
  polis start --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/draft-v5.json --out /outside/locked-v6.json
Do not use:
  After production edits to manufacture a baseline, or to mutate/forge a locked contract.
```
<!-- /command-help -->

### implementation-plan

<!-- command-help: implementation-plan -->
```text
Usage:
  polis implementation-plan --repo <path> [--policy <policy-v3.json>] --contract <locked-v4-or-v6.json> --out <external-plan.json> [--format text|json]
Purpose:
  generate an optional contract-bound implementation plan
When to use:
  After start and before implementation, when an explicit execution sequence is useful.
Prerequisites:
  Clean locked baseline, locked schema-v4/v6 contract, matching effective schema-v3
  policy bytes, valid retention preference, and absent external output file.
Required inputs:
  --repo, --contract (external or managed retained contract), and external --out.
Workflow:
  The optional plan is subordinate to the contract. Pass the same plan bytes to both
  capture-red and build when using it; omission keeps the unplanned workflow.
Reads/writes:
  Reads Git/policy/contract; writes deterministic plan JSON, no proof commands run.
  Repository retention also publishes .polis/artifacts/plans/; no staging/commit.
Options/defaults:
  --repo/--contract/--out are required, no defaults; --policy defaults to committed
  .polis/policy.json. --format defaults to text; json includes the generated plan.
Outcomes:
  Success (0): preserve plan bytes and proceed to required proof/implementation steps.
  Blocked prerequisite or failure (6): fix policy/baseline/contract/output and retry;
  do not change contract authority via a plan. Invalid syntax/format returns 2.
Examples:
  polis implementation-plan --repo /path/to/repo --policy /outside/policy-v3.json --contract /outside/locked-v6.json --out /outside/plan.json
Do not use:
  Instead of start, to add requirements/scope, or as evidence implementation is complete.
```
<!-- /command-help -->

### status

<!-- command-help: status -->
```text
Usage:
  polis status [--repo <path>] [--contract <retained-locked-contract.json>] [--format text|json]
Purpose:
  summarize persisted strict-development state and the next valid action
When to use:
  Resume a repository-retained change and inspect the next valid action/evidence gaps.
Prerequisites:
  Git worktree and valid committed retention preference if present. Persisted state
  requires repository retention; external mode deliberately reports unavailable.
Required inputs:
  Select --contract from managed retained contracts if multiple candidates exist.
Workflow:
  Follow next_action when present; validate retained evidence rather than infer progress.
  A plan or source delta is not implementation completion. A complete package may still
  require consumer validation for deferred gates; no consumer application is inferred.
Reads/writes:
  Reads Git, retention, retained contracts/plans/proofs/packages/evidence; writes a
  summary only. Does not run proof/project commands or alter workflow artifacts.
Options/defaults:
  --repo defaults to .; --contract defaults to the sole retained candidate (no arbitrary
  choice if ambiguous). --format defaults to text; json includes state and next_action.
Outcomes:
  Success (0): summary is consistent, not necessarily complete; unavailable or blocked
  state can also exit 0. Read state/problems and next_action before taking any action.
  Failure (6): ambiguous/inconsistent state or derivation error; select the retained
  contract or resolve invalid evidence. Missing prerequisites require repair, not
  invented progress. Invalid syntax/format returns 2.
Examples:
  polis status --repo /path/to/repo --format json
Do not use:
  To discover arbitrary external outputs or treat an exit code of 0 as completed delivery.
```
<!-- /command-help -->

### check-red-scope

<!-- command-help: check-red-scope -->
```text
Usage:
  polis check-red-scope [--repo <path>] --contract <locked-v4-or-v6.json> --path <file> [--path <file> ...] [--format text|json]
Purpose:
  check proposed Red probe paths against the locked test scope
When to use:
  Before creating Red tests for a locked feature/defect; files need not exist yet.
Prerequisites:
  Git worktree, locked Red-to-Green contract, and valid repository/retention boundaries.
Required inputs:
  --contract external or retained locked schema-v4/v6 contract and one or more --path
  repository-relative file names; each must fit both test_scope and change scope.
Workflow:
  start -> check-red-scope -> test-only delta -> capture-red -> implementation -> build.
Reads/writes:
  Reads contract/repository; writes per-path accepted/rejected decisions and rules only.
  Runs no regression command, writes no patch, and does not edit the worktree.
Options/defaults:
  --repo defaults to .; --contract has no default; --path repeats and has no default.
  --format defaults to text; json contains the same scope decisions.
Outcomes:
  Success (0): all proposed paths fit; create test-only edits, then capture-red.
  Failure (6): scope/contract error; pick allowed paths or re-lock a legitimately revised
  contract on a clean baseline. Missing prerequisites require repair. Syntax returns 2.
Examples:
  polis check-red-scope --repo /path/to/repo --contract /outside/locked-v6.json --path tests/new_test.go --format json
Do not use:
  As Red evidence or permission for unlisted edits; capture-red validates the actual patch.
```
<!-- /command-help -->

### capture-red

<!-- command-help: capture-red -->
```text
Usage:
  polis capture-red --repo <path> --contract <change.json> [--implementation-plan <plan.json>] [--format text|json] --out <regression.patch>
Purpose:
  capture the required Red proof
When to use:
  After a scoped test-only change genuinely fails on the locked baseline, before production edits.
Prerequisites:
  Locked Red-to-Green schema-v4/v6 contract, baseline still valid, clean real index,
  test-only non-ignored worktree delta inside test/change scopes, and absent external output.
  The proof command must start and match the declared nonzero exit/output oracle.
Required inputs:
  --repo, --contract external or managed retained locked contract, and external --out.
Workflow:
  Check proposed scope first; capture genuine Red, preserve captured tests immutable,
  implement production changes, then supply this --regression-patch to build.
Reads/writes:
  Reads Git/contract/test delta/optional plan; replays the test patch on an isolated
  baseline and runs the declared regression command. Writes validated patch output;
  repository retention also publishes .polis/artifacts/proofs/. Source/index/HEAD
  stay unchanged; invoked commands may write caches/reports or access external services.
Options/defaults:
  --repo/--contract/--out are required, no defaults; --format defaults to text.
  --implementation-plan defaults absent; if used, supply the same contract-bound
  plan bytes to build (external or managed retained plan).
Outcomes:
  Success (0): captured Red matches the oracle; keep tests unchanged and implement.
  Failure (2): scope, baseline, output, or Red mismatch; fix the cause and retry.
  Diagnostic BLOCKED means intended checks did not run, not genuine Red; restore the
  dependency/environment, never change the oracle to accept an environmental failure.
Examples:
  polis capture-red --repo /path/to/repo --contract /outside/locked-v6.json --out /outside/regression.patch
Do not use:
  For behavior_preserving Green-to-Green work, after production edits, or to manufacture Red.
```
<!-- /command-help -->

### build

<!-- command-help: build -->
```text
Usage:
  polis build --repo <path> [--policy <policy-v3.json>] --project <slug> --change <slug> --contract <change.json> [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--defer-gate <id> ...] [--format text|json] --out <directory>
Purpose:
  build a .polis delivery package
When to use:
  Package implemented in-scope changes after a locked baseline and required development proof.
Prerequisites:
  Valid Git baseline or descendant HEAD, clean real index, nonempty target delta,
  immutable captured tests, locked schema-v4/v6 contract, matching schema-v3 policy
  bytes, and dependencies for all producer-executed proof/project commands.
Required inputs:
  --repo, --project/--change canonical slugs, --contract, and external --out directory.
  --regression-patch is required for Red-to-Green features/defects and forbidden for
  behavior_preserving Green-to-Green. Contracts/proofs/plans are external or managed retained inputs.
Workflow:
  start -> required proof -> implementation -> build -> polis verify -> inspect;
  consumer preflight/apply is separate. Build replays proof and validates the target.
Reads/writes:
  Reads source delta/Git/policy/contract/proof/plan; executes proof and producer gates
  in isolated repositories; writes a verified .polis package in --out (creates directory,
  never overwrites a package). External-policy flow preserves source Git state.
  Repository retention also publishes contracts/plans/proofs/evidence/packages under
  .polis/artifacts/ without staging/committing. Commands may write caches or use network/services.
Options/defaults:
  --repo/--project/--change/--contract/--out are required, no defaults.
  --policy defaults to committed unchanged .polis/policy.json; explicit policy is external.
  --format defaults to text. --regression-patch defaults absent, subject to proof mode above.
  --implementation-plan defaults absent (format v5); supplied same plan bytes as capture-red
  produce format v6. --defer-gate repeats enabled gate IDs (default none); deferral cannot
  break producer dependencies and transfers required execution to consumer preflight/apply.
Outcomes:
  Success (0): verify/inspect the exact output; deferred gates remain a consumer obligation.
  Failure (2): inspect diagnostics for scope/proof/baseline/gate/output errors. BLOCKED
  checks did not run; restore dependencies. Do not weaken policy or recapture altered tests.
Examples:
  polis build --repo /path/to/repo --policy /outside/policy-v3.json --project app --change fix --contract /outside/locked-v6.json --regression-patch /outside/regression.patch --out /outside/output
  For behavior_preserving, omit --regression-patch and use locked Green-to-Green proof.
Do not use:
  A draft/legacy contract for new delivery, gates-only success as proof, or deferral to hide failure.
```
<!-- /command-help -->

### verify

<!-- command-help: verify -->
```text
Usage:
  polis verify [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>
Purpose:
  validate a .polis artifact
When to use:
  Check exact package bytes after build or before trusting a received delivery artifact.
Prerequisites:
  Readable bounded .polis package; signature verification additionally needs a detached
  signature and independently trusted Ed25519 public-key PEM, never a package-selected key.
Required inputs:
  One artifact positional path; put flags before that path.
Workflow:
  build -> verify -> inspect -> consumer preflight; package PASS does not authorize apply.
Reads/writes:
  Reads package and optional signature/key; checks integrity/contracts/evidence; writes
  a report only; does not execute project commands or mutate a repository/package.
Options/defaults:
  --format defaults to text; json reports validation metadata.
  --signature and --trusted-key default absent and must be supplied together. Without
  them, checksums validate integrity but do not authenticate the producer.
Outcomes:
  Success (0): package validates; inspect traceability and run preflight for the consumer.
  Failure (3): unreadable/invalid artifact or signature; stop consumption and obtain a
  valid artifact/trust root. Missing inputs are not PASS. Invalid syntax/format/pair returns 2.
Examples:
  polis verify /outside/artifact.polis
  polis verify --signature /outside/artifact.polis.sig --trusted-key /trusted/public.pem /outside/artifact.polis
Do not use:
  To re-run gates, prove current consumer compatibility, authenticate unsigned packages,
  or validate a polis export offline ZIP.
```
<!-- /command-help -->

### inspect

<!-- command-help: inspect -->
```text
Usage:
  polis inspect [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>
Purpose:
  inspect validated artifact metadata
When to use:
  Review scope, baselines, gates, contract/plan/proof traceability and optional commit intent.
Prerequisites:
  Readable valid .polis package; optional detached signature and independently trusted
  Ed25519 public-key PEM, never a package-selected trust root.
Required inputs:
  One artifact positional path; place flags first.
Workflow:
  Inspect verifies before exposing metadata; review intended changes before consumer preflight.
Reads/writes:
  Reads package and optional signature/key; writes validated metadata only;
  does not execute project commands, apply patches, or commit suggested messages.
Options/defaults:
  --format defaults to text; json includes requirement/proof/plan traceability.
  --signature/--trusted-key default absent, required together; unsigned integrity
  checks do not establish producer identity. Historical/unplanned packages report plan absence.
Outcomes:
  Success (0): review scope, absent/deferred guarantees, and commit intent, then preflight.
  Failure (3): invalid/unreadable package or signature; stop and obtain valid inputs.
  Missing prerequisites are not validated metadata; invalid syntax/format/pair returns 2.
Examples:
  polis inspect --format json /outside/artifact.polis
Do not use:
  Raw metadata as authorization, a plan as implementation proof, or inspection as consumer validation.
```
<!-- /command-help -->

### preflight

<!-- command-help: preflight -->
```text
Usage:
  polis preflight [--repo <path>] [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>
Purpose:
  validate an artifact without applying it
When to use:
  Check a delivery against the intended consumer before any authorized application.
Prerequisites:
  Clean consumer worktree/index, readable valid package, Git, and dependencies for
  packaged proof/project commands; review untrusted code/policy before execution.
Required inputs:
  One artifact positional path; identify the consumer with --repo and put flags first.
Workflow:
  After verify/inspect, validate isolated consumer target, including deferred gates.
  PASS describes current state only; apply needs authorization and repeats validation.
Reads/writes:
  Reads consumer Git/package/optional signature/key; runs proof/project commands in
  isolated repositories. Preserves consumer worktree/index/HEAD; no patch is applied.
  Commands may write external caches/reports or access network/services; isolation is not a sandbox.
Options/defaults:
  --repo defaults to .; --format defaults to text. --baseline-mode defaults to strict
  (exact base commit); compatible permits clean descendants after ancestry/context/target
  checks; permissive permits non-descendants with a warning and local or embedded proof.
  --allow-missing-baseline-proof defaults false; permissive-only explicit waiver for
  unavailable historical proof, reports bypassed guarantees. Never infer authorization.
  --signature/--trusted-key default absent; supply both for independent producer trust.
Outcomes:
  Success (0): review report and obtain authorization for apply; do not assume state stays valid.
  Failure (3): invalid artifact/signature; (5): baseline mismatch; (6): consumer validation
  or prerequisite error; (2): syntax/format/options. Diagnose/repair and retry. Do not
  silently relax baseline/proof rules; a blocked command is not executed validation.
Examples:
  polis preflight --repo /path/to/consumer --format json /outside/artifact.polis
Do not use:
  As authorization for apply, a durable approval of later state, or a sandbox for untrusted commands.
```
<!-- /command-help -->

### apply

<!-- command-help: apply -->
```text
Usage:
  polis apply [--repo <path>] [--baseline-mode strict|compatible|permissive] [--allow-missing-baseline-proof] [--commit-mode none|prompt|auto] [--format text|json] [--signature <file> --trusted-key <pem>] <artifact.polis>
Purpose:
  validate and apply an artifact transactionally
When to use:
  Only when the user explicitly requests application to the identified consumer.
Prerequisites:
  Explicit authorization, clean consumer worktree/index, valid package, Git, and all
  packaged proof/project dependencies. Commit modes additionally require artifact commit
  metadata and usable Git identity; prompt requires a TTY and affirmative confirmation.
Required inputs:
  One artifact positional path and intended --repo consumer; flags must precede artifact.
Workflow:
  verify/inspect -> preflight -> authorized apply. Revalidates current consumer state;
  prior preflight neither authorizes mutation nor transfers overrides to this invocation.
Reads/writes:
  Reads package/Git/optional signature/key; runs isolated proof/project validation, then
  mutates consumer worktree and checks exact target tree transactionally. Default none
  preserves HEAD/index/refs/config/objects in canonical external-policy flow. Explicit
  prompt/auto creates a local commit/ref with validated parent/tree; no hooks/signing/push.
  Temporary validation evidence is removed, not persisted in the consumer. Invoked
  project commands may write external caches/reports or use network/services.
Options/defaults:
  --repo defaults to .; --format defaults to text. --baseline-mode defaults to strict
  (exact base); compatible permits validated descendants; permissive permits non-descendants
  with warnings and local/embedded proof. --allow-missing-baseline-proof defaults false,
  requires permissive and explicit waiver of unavailable historical proof; repeat if authorized.
  --commit-mode defaults to none (apply-only); prompt asks approval of exact artifact
  message/tree; auto explicitly authorizes a local commit without prompting. Both need
  commit metadata; do not choose auto merely because the agent cannot answer a prompt.
  --signature/--trusted-key default absent, required together for independent producer trust.
Outcomes:
  Success (0): inspect target and committed/commit_sha fields; report only actual mutation.
  BLOCKED (4): commit authorization/TTY unavailable or refused; leave state unchanged.
  Failure (3): artifact/signature; (5): baseline; (6): validation; (7): apply/commit/rollback;
  (2): syntax. Stop, inspect error and Git state, and verify rollback before retrying.
  Failed commit rollback can leave unreachable objects; do not promise unconditional cleanup.
Examples:
  Only after explicit authorization:
  polis apply --repo /path/to/consumer /outside/artifact.polis
  With separately authorized interactive commit: add --commit-mode prompt before artifact.
Do not use:
  For producer packaging, on dirty state, without authorization, or with silent proof/baseline overrides.
```
<!-- /command-help -->

### sign

<!-- command-help: sign -->
```text
Usage:
  polis sign --key <private.pem> --out <artifact.polis.sig> [--format text|json] <artifact.polis>
Purpose:
  create a detached artifact signature
When to use:
  Authenticate exact reviewed package bytes after build/verify when signing is authorized.
Prerequisites:
  Readable artifact and authorized Ed25519 PKCS#8 private key PEM; absent signature
  output in an existing directory. Protect the private key; never print or commit it.
Required inputs:
  --key, --out, and one artifact positional path; flags precede the artifact.
Workflow:
  Verify first, sign exact bytes, then verify with signature and independently trusted
  public key. Distribute artifact/signature, never the private key.
Reads/writes:
  Reads artifact/private key; writes detached signature without overwriting. Does not
  modify package/repository; does not validate package contents or run project commands.
Options/defaults:
  --key/--out required, no defaults; --format defaults to text (json reports digests/paths).
  No key generation or automatic trust-root selection; consumer supplies its trusted public key.
Outcomes:
  Success (0): verify the signature against the exact artifact with the trusted public key.
  Blocked prerequisite or failure (6): fix readable inputs, key type, output directory,
  or absent output; do not expose key bytes. Invalid syntax/format returns 2.
Examples:
  polis sign --key /secure/private.pem --out /outside/artifact.polis.sig /outside/artifact.polis
Do not use:
  To bless unvalidated bytes, replace package verification, or make a package choose its own trust root.
```
<!-- /command-help -->

### export

<!-- command-help: export -->
```text
Usage:
  polis export --out <polis-offline.zip> [--format text|json] [--executable <file>] [--runtime <GOOS/GOARCH>]
Purpose:
  create a self-contained offline runtime bundle
When to use:
  Distribute the POLIS runtime/specification/schemas/usage guide for offline operation.
Prerequisites:
  Readable bounded regular executable file and absent output; recipients need Git,
  matching OS/CPU, and dependencies for their own configured project commands.
Required inputs:
  --out ZIP path; no repository, Project Policy, or Change Contract input.
Workflow:
  Distribute the runtime bundle separately from delivery .polis artifacts; recipients
  extract, run the bundled binary's help/doctor, then use their own delivery workflow.
Reads/writes:
  Reads selected/current executable and embedded resources; creates output directories
  and deterministic checksummed ZIP without overwriting. Does not execute the selected
  binary or read target source/policy; no network access or repository mutation.
Options/defaults:
  --out required, no default; --format defaults to text.
  --executable defaults to current executable; choose an actual release binary, not
  a go run/test temporary binary for distribution. --runtime defaults to current
  GOOS/GOARCH; explicit value labels the manifest, it does not cross-compile or validate
  the selected binary's architecture. Caller must supply a matching executable.
Outcomes:
  Success (0): check bundle manifest/checksums/runtime before distributing or executing.
  Blocked prerequisite or failure (6): fix binary/runtime/output and retry with absent
  output; no offline kit is proven from a failed export. Invalid syntax/format returns 2.
Examples:
  polis export --out /outside/polis-v6-offline.zip
Do not use:
  To package repository changes, cross-compile a binary, or pass the offline ZIP to polis verify.
```
<!-- /command-help -->

## Local quality checks

The local SonarQube workflow remains available:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

Use `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`
before publishing changes. The repository CI exercises Linux, macOS, and
Windows.

## Related documentation

- [Installation](installation.md)
- [GitHub releases](releases.md)
- [POLIS Specification V6](../spec/POLIS-SPEC-v6.md)
- [V6 design decisions](sdd/)
- [Validation record](../VALIDATION.md)
