# CLI command reference

This document is the canonical source embedded by `polis help <command>`. Edit the marked command-help blocks here; the CLI help parity tests ensure their output remains synchronized.

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
  polis doctor [--repo <path> [--policy <policy-v3.json>]] [--format text|json]
Purpose:
  check Git/runtime prerequisites and optionally statically inspect project readiness
When to use:
  Before starting a workflow; add --repo to identify known project blockers early.
Prerequisites:
  A runnable POLIS binary; Git on PATH; --repo must resolve to a Git worktree.
Required inputs:
  None by default. --repo opts in to the effective committed Project Policy;
  --policy selects an external schema-v3 policy and requires --repo.
Workflow:
  Check Git/runtime; with --repo, validate policy schema, command portability,
  dependency plan/order, configured command workdirs/executables (including
  executables delegated through env wrappers), and coverage report path
  containment without gates.
Reads/writes:
  Reads runtime and PATH; executes Git inspection/version commands. With --repo,
  reads policy and filesystem metadata. No project gate commands are executed.
Options/defaults:
  --format defaults to text. --repo and --policy opt in to static repository checks.
  JSON includes a repository.checks list, effective plan and gates_executed=false.
Outcomes:
  Success (0): Git responds and, if --repo, checked static prerequisites pass.
  BLOCKED (4): Git/repository prerequisite or executable/workdir unavailable.
  Validation failed (6): invalid Project Policy or execution plan.
  Failure (2): invalid arguments/format; correct syntax.
Examples:
  polis doctor
  polis doctor --format json
  polis doctor --repo .
  polis doctor --repo . --policy /outside/policy-v3.json --format json
Do not use:
  Doctor PASS as proof that gates, transitive dependencies, tests, builds or
  consumer validation work. Executable lookup uses the doctor's environment;
  env wrappers that change PATH or working directory may make executable lookup
  inconclusive and are reported as BLOCKED when lookup depends on that state.
  generated coverage reports are not required to exist before their gates run.
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
  project's actual configuration. Non-dry-run requires absent policy.json and
  policy-provenance.json in .polis/.
Required inputs:
  Custom strict profile: repeated --test-argv, --coverage-argv, adapter, and report.
  Custom standard requires test argv; minimal may omit it. Do not lower guarantees
  merely to avoid specifying commands; any reduction needs explicit justification.
Workflow:
  Preview and review policy, then plan; use the same effective bytes for start/build.
Reads/writes:
  Reads repository root/profile information; does not run generated project gates.
  --dry-run prints validated policy JSON only. Otherwise creates .polis/policy.json
  and .polis/policy-provenance.json without overwriting or committing; review/commit
  both only with authorization.
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
  The ID is asserted by the executor; POLIS does not verify the environment.
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
  JSON records environment_assurance=caller_asserted for nonempty --environment-id;
  this declaration does not establish hermetic execution.
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
  polis workspace validate --repo <path> --contract <locked-v4-or-v6.json> [--policy <policy-v3.json>] [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--out-report <new-external.json>] [--jobs <1..16>] [--format text|json]
  polis workspace status --repo <path> --contract <external-locked-v4-or-v6.json> --report <external-workspace-validation-v1.json> [--policy <policy-v3.json>] [--format text|json]
Purpose:
  validate a locked change without a package, or compare an unsigned checkpoint's source identity
When to use:
  A local coding agent already in the checkout uses validate after implementation, without a package.
  Remote delivery is a separate flow: build -> verify -> consumer preflight/apply.
Prerequisites:
  Validate requires a Git worktree, clean real index, nonempty in-scope delta, locked strict contract,
  matching policy, required development proof, and project gate dependencies. Status requires a
  version-1 external report, external locked contract, matching policy, and a comparable source snapshot.
Required inputs:
  Validate: --contract; exact Red proof for Red-to-Green work and same plan bytes if used.
  Status: external --contract and an external workspace-validation-v1 --report.
Workflow:
  start -> required proof -> implementation -> workspace validate. Workspace status can then compare
  recorded base/tree/contract/policy identities. Use build -> verify only when remote delivery is required.
Reads/writes:
  Validate replays proof and runs enabled gates in isolated worktrees, then checks the target tree;
  it preserves HEAD/index/source and creates no .polis package. Optional --out-report writes a new
  external checkpoint. Local coding agents do not need build, sign, preflight, or apply. Status reads
  a bounded external report and current identities only; it does not execute gate commands or
  intentionally change the repository. Git may invoke configured clean
  filters while creating its temporary-index snapshot; status is not a sandbox. Neither command
  authenticates reports.
Options/defaults:
  --repo defaults to .; --policy defaults to committed .polis/policy.json. Validate requires
  --contract; Red-to-Green requires --regression-patch. Status requires external --contract and --report.
  Reports must be regular non-symlink files outside the resolved worktree. --jobs defaults to 1
  and accepts 1 through 16; only gates explicitly marked parallel_safe may overlap.
  --format defaults to text.
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
  local workspace validation does not use preflight or apply. Use build -> verify -> preflight/apply
  only for remote package delivery and consumer handoff.
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
  polis status [--repo <path>] [--policy <policy-v3.json>] [--contract <locked-contract.json>] [--implementation-plan <plan.json>] [--regression-patch <red.patch>] [--report <workspace-validation-v1.json>] [--package <artifact.polis>] [--format text|json]
Purpose:
  project the current strict-development workflow state and the next valid action
When to use:
  Resume a repository-retained or external-artifact change after context loss.
Prerequisites:
  Git worktree; valid committed retention preference if present; every explicit
  external input must satisfy its existing bounded non-symlink trust boundary.
Required inputs:
  Repository retention can autodiscover retained artifacts. External workflows need
  --contract or --package. A workspace --report is compared against whichever validated
  locked contract status selected, including a retained or package-embedded contract.
Workflow:
  Supply the artifacts already produced by the task. Read state/proven/stale_or_unproven/
  missing, then follow next_action. A plan, source delta, or unsigned workspace checkpoint
  is not implementation completion. A verified package remains the delivery authority.
Reads/writes:
  Reads Git, retention, explicit/retained contracts/plans/proofs/packages, optional
  Policy and workspace report; writes a summary only. Does not run project gates,
  create artifacts, or mutate Git state intentionally.
Options/defaults:
  --repo defaults to .; repository mode autodiscovers artifacts. --policy, --contract,
  --implementation-plan, --regression-patch, --report, and --package select explicit
  artifacts; --format defaults to text. A verified --package can supply its contract.
Outcomes:
  Success (0): summary is consistent, not necessarily complete; unavailable or blocked
  state can also exit 0. Read state, proven, stale_or_unproven, missing, and next_action.
  Failure (6): ambiguous/inconsistent state or derivation error; select the retained
  contract or resolve invalid explicit evidence. Missing prerequisites require repair,
  not invented progress. Invalid syntax/format returns 2.
Examples:
  polis status --repo /path/to/repo --format json
  polis status --repo /path/to/repo --policy /outside/policy.json --contract /outside/locked.json --implementation-plan /outside/plan.json --regression-patch /outside/red.patch --report /outside/workspace.json --package /outside/change.polis --format json
Do not use:
  To authenticate a workspace report, infer implementation completion from source edits,
  or treat an exit code of 0 as completed delivery.
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
  polis build --repo <path> [--policy <policy-v3.json>] --project <slug> --change <slug> --contract <change.json> [--regression-patch <red.patch>] [--implementation-plan <plan.json>] [--defer-gate <id> ...] [--jobs <1..16>] [--format text|json] --out <directory>
Purpose:
  build a .polis delivery package
When to use:
  A remote producer with a source checkout and POLIS runtime creates a portable package for transfer.
  If a local coding agent already edits the target checkout, use workspace validate instead.
Prerequisites:
  Valid Git baseline or descendant HEAD, clean real index, nonempty target delta,
  immutable captured tests, locked schema-v4/v6 contract, matching schema-v3 policy
  bytes, and dependencies for all producer-executed proof/project commands.
Required inputs:
  --repo, --project/--change canonical slugs, --contract, and --out directory physically outside
  the resolved target worktree. In-repository paths and symlinks resolving inside are rejected.
  --regression-patch is required for Red-to-Green features/defects and forbidden for
  behavior_preserving Green-to-Green. Contracts/proofs/plans are external or managed retained inputs.
Workflow:
  Remote producer: start -> required proof -> implementation -> build -> polis verify -> inspect;
  transfer the disposable package to its consumer for separately authorized preflight/apply.
  Local agent workflow: start -> proof -> implementation -> workspace validate; no package.
Reads/writes:
  Reads source delta/Git/policy/contract/proof/plan; executes proof and producer gates
  in isolated repositories; writes a verified .polis package in --out (creates directory,
  never overwrites a package) outside the target worktree. The package is disposable and is
  never retained in the repository. Repository retention may publish contracts/plans/proofs/
  evidence, but never packages, under .polis/artifacts/. Commands may write caches or use
  network/services.
Options/defaults:
  --repo/--project/--change/--contract/--out are required, no defaults.
  --policy defaults to committed unchanged .polis/policy.json; explicit policy is external.
  --format defaults to text. --regression-patch defaults absent, subject to proof mode above.
  --implementation-plan defaults absent (format v5); supplied same plan bytes as capture-red
  produce format v6. --defer-gate repeats enabled gate IDs (default none); deferral cannot
  break producer dependencies and transfers required execution to consumer preflight/apply.
  --jobs defaults to 1 and accepts 1 through 16; only gates explicitly marked parallel_safe
  may overlap, and coverage remains exclusive.
Outcomes:
  Success (0): verify/inspect the exact output; deferred gates remain a consumer obligation.
  Failure (2): inspect diagnostics for scope/proof/baseline/gate/output errors. BLOCKED
  checks did not run; restore dependencies. Do not weaken policy or recapture altered tests.
Examples:
  polis build --repo /path/to/repo --policy /outside/policy-v3.json --project app --change fix --contract /outside/locked-v6.json --regression-patch /outside/regression.patch --out /outside/output
  For behavior_preserving, omit --regression-patch and use locked Green-to-Green proof.
Do not use:
  A draft/legacy contract for new delivery, gates-only success as proof, or deferral to hide failure.
  For a local agent validating the live checkout without a consumer handoff, use workspace validate.
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
  A package consumer checks a received delivery against the intended repository before application.
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
  local workspace validation does not use preflight.
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
  A package consumer applies a received delivery only when the user explicitly requests it.
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
  local workspace validation does not use apply.
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
