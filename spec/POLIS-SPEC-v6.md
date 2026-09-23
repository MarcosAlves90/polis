# POLIS Specification V6 — Mandatory Strict Development Producer

## 1. Authority and compatibility model

POLIS V6 uses package format v5 for new producer builds while retaining reader compatibility for valid package formats v2, v3, and v4. Project Policy schema v3, Evidence v2 for historical formats v2-v4, Evidence v3 for format v5, exact-tree, signature, bounded-input, environment, and transactional-apply contracts remain in force unless this specification explicitly supersedes them.

For new V6 producer operations, this specification supersedes the V5 producer-admission rules and the committed-policy-only baseline-lock semantics defined by earlier POLIS specifications.

V6 distinguishes three compatibility directions:

- **canonical V6 producer:** uses an explicit external Project Policy schema v3 and a locked Change Contract schema v4;
- **legacy producer compatibility:** when no explicit policy is supplied, `start` and `build` may continue to use an exact committed `.polis/policy.json` for existing/self-hosting repositories;
- **reader compatibility:** `verify` and `inspect` continue to decode valid historical schemas supported by V5, and `preflight`/`apply` may consume historical valid artifacts for migration.

Compatibility MUST NOT weaken the canonical zero-residue producer path or authorize creation of new legacy Change Contract schemas.

## 2. Runtime identity

The V6 CLI version is `6.6.0`.

The Go module path is:

```text
github.com/MarcosAlves90/polis/v6
```

## 3. Required producer state machine

The canonical V6 delivery state sequence is:

```text
external Project Policy v3 + clean Git baseline
  -> accepted strict Change Contract schema-v3 draft
  -> polis start --policy <external-policy>
  -> locked Change Contract schema v4 / strict_sdd_tdd_v2
  -> development proof appropriate to change kind
  -> polis build --policy <same-effective-policy>
  -> polis verify
  -> optional signature
  -> consumer preflight
  -> consumer apply
```

The external Project Policy is a caller-owned input outside the target worktree. Its pathname is execution context only and MUST NOT be serialized into the Change Contract, package, evidence, payload, or target repository.

`polis build` MUST reject Change Contract schemas v1, v2, and v3 as producer input. The rejection occurs before target construction and before execution of Project Policy gates.

A valid producer contract MUST satisfy all schema-v4 semantics defined by POLIS Specification v4 except where this V6 specification supersedes the source of `baseline_lock.policy_sha256`.

## 4. Effective Project Policy and `polis start`

### 4.1 Canonical external policy

For the canonical zero-residue path, `polis start` receives `--policy <external-policy>`.

It MUST:

- operate on a clean committed Git baseline;
- require the supplied effective Project Policy to validate as schema v3;
- require the external policy path to be outside the target worktree;
- canonicalize the validated external policy before hashing it;
- accept an external strict schema-v3 draft using `strict_sdd_tdd_v1`;
- emit an external schema-v4 contract using `strict_sdd_tdd_v2`;
- bind Git object format, base commit, base tree, SHA-256 of the canonical effective Project Policy, and canonical Specification SHA-256;
- never serialize the external policy pathname;
- never create `.polis`, modify HEAD, modify the real index, modidwfy existing worktree files, or write tool-specific Git metadata;
- never overwrite its output.

`baseline_lock.policy_sha256` therefore identifies policy bytes, not a repository pathname.

### 4.2 Validation reinforcement

Project Policy schema v3 may contain the optional `validation_level` field. When absent, its effective value is `strict` for backward compatibility; generated strict policies may omit the field so existing V6 consumers continue to read them. Supported values are:

- `strict`: `test.complete` MUST use `command` mode and `coverage` MUST use `coverage` mode; this is the current V6 behavior;
- `standard`: `test.complete` MUST use `command` mode, while `coverage` may use explicit `not_applicable` mode;
- `minimal`: `test.complete` and `coverage` may each use explicit `not_applicable` mode.

All project gates remain present in the canonical registry and each gate's mode remains authoritative. `not_applicable` always requires a non-empty reason. A lower level is a minimum assurance profile: additional gates may remain enabled, but the actual enabled and disabled gate lists MUST be recorded in the validation evidence. The effective policy can be committed for project configuration or supplied outside the worktree for an individual execution.

`polis init --validation-level standard` generates the Go profile with coverage disabled by an explicit reason. `polis init --validation-level minimal` generates all project-quality gates as explicitly not applicable. Repeated `--disable-gate <id>` selectively disables generated project gates; it MUST reject unknown gates and attempts to disable required gates at an incompatible level.

Validation reinforcement levels apply only to project-quality gates. They MUST NOT disable policy/contract validation, path and environment safety, the selected consumer baseline-compatibility contract, deterministic target-tree checks, required development proof, package and evidence integrity, signature checks, isolated consumer validation, or transactional apply protections.

### 4.2.1 Read-only execution plan

`polis plan` MUST compile the effective Project Policy without executing a project command or modifying the repository. It MUST report the policy schema, source class (`committed` or `external`), policy SHA-256, current runtime, validation level, complete canonical gate inventory, effective dependency edges, deterministic execution order, mandatory invariants, and the guarantee status for every project gate. The plan output uses `plan_version: 1` and is available as text or JSON.

The policy executor MUST consume the same compiled gate plan used by `polis plan`; a gate shown as `not_applicable` in the plan MUST NOT be executed. The plan MUST NOT serialize the external policy pathname.

### 4.2.2 Policy dependency graph and lint

Project Policy schema v3 MAY declare `depends_on` on any project gate. POLIS also maintains built-in essential dependencies; the current built-in edge is `coverage` depends on `test.complete`. Declared dependencies are additive and MUST NOT remove a built-in dependency. Policies that omit `depends_on` remain valid with the built-in graph applied.

Before any project command executes, policy validation MUST lint the combined graph. It MUST reject unknown or missing gate IDs, self-dependencies, duplicate dependency IDs, cycles, and any enabled gate whose dependency is `not_applicable`. A disabled gate's dependencies remain structurally validated, but a disabled gate does not require its dependencies to be enabled. Invalid graphs MUST fail closed.

The linter MUST produce a deterministic topological execution order. The executor MUST use that order, while plan output MUST retain the canonical gate inventory and report the effective dependency edges and execution order. Disabling a dependency removes the guarantee represented by the dependent enabled gate; it MUST never happen silently.

### 4.3 Committed-policy compatibility

When `--policy` is omitted, V6 MAY retain the historical producer behavior that reads exact committed `.polis/policy.json` bytes. This exists for compatibility and self-hosting; it is not the canonical zero-residue workflow.

Committed-policy compatibility MUST NOT cause the explicit external-policy path to read, create, update, or require `.polis/policy.json`.

## 5. Change-kind development proof

### 5.1 Feature

A feature MUST use Red-to-Green proof. The test-only Red delta MUST be captured after `polis start` and before production implementation. Captured test paths remain immutable in the final target.

### 5.2 Defect

A defect MUST use Red-to-Green proof with the declared baseline exit code and output oracle. The Red proof MUST reproduce the defect on the locked baseline before the fix.

### 5.3 Behavior preserving

A `behavior_preserving` change MUST use Green-to-Green characterization. The same explicit characterization command MUST pass on the locked baseline and on the target. No artificial Red state and no regression patch are permitted.

## 6. `capture-red`

V6 `capture-red` accepts only a locked schema-v4 contract whose semantics require Red-to-Green.

An unlocked schema-v3 draft MUST be rejected with guidance to run `polis start` first.

For a schema-v4 contract, `capture-red` revalidates repository-dependent baseline facts available from the target repository: Git object format, base commit, base tree, and Specification identity. It does not require a repository policy file; the effective policy hash was already locked by `start` and is revalidated when policy bytes are available to producer/package verification boundaries.

Temporary Git indexes or object writes used to calculate or validate the Red patch MUST be isolated from the target repository's persistent Git object database.

All existing test-scope, source-mutation, oracle, path, bounded-input, and captured-test immutability rules remain in force.

## 7. Producer `build`

Canonical V6 `build` uses `--policy <external-policy>` and requires:

- effective Project Policy schema v3;
- the same canonical effective policy bytes whose SHA-256 is locked in `baseline_lock.policy_sha256`;
- locked Change Contract schema v4;
- `development_method: strict_sdd_tdd_v2`;
- valid repository-dependent `baseline_lock` facts against the producer repository;
- regression patch exactly when the change requires Red-to-Green;
- all existing behavior, affected, policy, scope, coverage, target-tree, evidence, and package-integrity checks.

For V6 producer `build`, the immutable baseline and the current producer `HEAD` are distinct after development begins. This section supersedes the V4 rule that treated every producer HEAD change as baseline drift:

- `baseline_lock.base_commit` remains the exact artifact base commit;
- the tree resolved from `baseline_lock.base_commit` MUST equal `baseline_lock.base_tree`;
- current producer `HEAD` MAY equal the locked base commit or be a descendant of it;
- if the locked base commit is not an ancestor of current producer `HEAD`, build MUST fail closed before target construction;
- the real index MUST equal current `HEAD`; staged changes remain invalid;
- target-tree and payload construction MUST start from `baseline_lock.base_commit` and capture the complete current worktree state, thereby including both descendant committed changes and permitted unstaged/untracked target changes;
- a clean worktree is valid when descendant commits already contain the target, but an empty locked-base-to-target payload remains invalid.

`capture-red` is not relaxed by this producer-build rule and continues to require the exact locked baseline before Red proof.

### 7.1 Package formats v4 and v5 and embedded baseline proof

New V6 builds MUST emit package format v5. Formats v4 and v5 contain exactly eight regular canonical members under `polis/`:

```text
polis/polis-baseline.tar
polis/polis-change.json
polis/polis-checksums.sha256
polis/polis-evidence.ndjson
polis/polis-manifest.json
polis/polis-payload.patch
polis/polis-policy.json
polis/polis-regression.patch
```

Formats v2 and v3 retain their exact historical seven-member inventory and MUST NOT contain `polis/polis-baseline.tar` or `baseline_sha256`. Format v4 retains its exact historical eight-member inventory and Evidence v2 semantics. New producer builds MUST NOT emit v2, v3, or v4.

`polis/polis-baseline.tar` is a deterministic uncompressed tar stream containing the locked `base_commit` object plus the complete recursively reachable tree/blob closure of `baseline_lock.base_tree`. Git object IDs MUST be recomputed from the raw object payload under `manifest.git_object_format`, the embedded commit MUST equal `manifest.base_commit`, and its root tree MUST equal `baseline_lock.base_tree`. Duplicate, unrelated, missing, malformed, non-canonical, Git-invalid, or identity-mismatched objects MUST invalidate the artifact. Verification MUST pass the reconstructed objects through native Git object validation in isolated temporary state before accepting the artifact. Parent commits and consumer/producer refs are not transported by this representation. Before a new v5 artifact is accepted by `build`, the locked development proof MUST be replayed against the materialized embedded baseline itself; therefore a configured baseline command that requires unavailable history or refs MUST make the producer build fail closed rather than producing an artifact that can only fail later at the consumer.

The v4 and v5 manifests MUST contain `baseline_sha256`, the SHA-256 of `polis/polis-baseline.tar`. The baseline member MUST also be covered by `polis/polis-checksums.sha256`. Its uncompressed member size is capped at 32 MiB; the existing 64 MiB archive and aggregate-uncompressed caps remain in force. The producer MUST determine that the projected canonical TAR exceeds no baseline-member bound before loading full object payloads into memory. A producer whose required baseline material exceeds the configured bounds MUST fail closed rather than omit material, allocate the entire oversized snapshot first, or silently increase limits.

A policy hash mismatch MUST fail before an artifact is accepted. The canonical policy bytes remain embedded in `polis/polis-policy.json`. Candidate packages MUST be verified through the canonical package verifier before publication.

When `--policy` is omitted, committed-policy compatibility MAY remain as defined in section 4.2.

Change Contract schemas v1-v3 are decode-compatible but invalid producer input.

Temporary target-tree construction and embedded-baseline construction MUST keep temporary index/object writes outside the target repository's persistent Git object database.

### 7.2 Consumer-deferred project gates and Evidence v3

`polis plan` and `polis build` accept repeatable `--defer-gate <id>` options. Planning remains read-only and uses the same compiled execution plan as build. IDs MUST be enabled project gates, MUST be unique, and MUST use the canonical project-gate order. Unknown, disabled, duplicate, and invalid deferrals MUST fail before project validation commands run. A producer-executed gate MUST NOT depend directly or transitively on a deferred prerequisite; a deferred gate may depend on a gate that the producer executes.

A producer MUST skip each deferred gate's command and any gate-specific side effect, including deleting or replacing coverage reports. Evidence MUST use Evidence v3, include a complete `validation_configured` inventory with an explicit `deferred_gates` array, and record each deferred gate as `DEFERRED` with the stable reason `deferred to consumer`. Deferred is distinct from both `PASS` and `NOT_APPLICABLE`. Formats v2-v4 retain their historical Evidence v2 decoding and validation semantics; format v5 selects Evidence v3.

Package verification MUST authenticate the package members and validate the complete policy/evidence trace, including the enabled deferred inventory and matching `DEFERRED` events. A consumer MUST compile the packaged policy with an empty deferral set and execute every enabled gate during both `preflight` and `apply`. A failing or blocked consumer gate MUST prevent real apply mutation. Text and JSON reports from plan, build, verify, inspect, preflight, and apply MUST show deferred gates and whether consumer validation is required; successful consumer reports MUST identify consumer validation as `PASS`.

## 8. Reader and consumer compatibility

`verify` and `inspect` remain capable of validating historical package/contract schemas already supported by V5 when those artifacts are otherwise valid. Formats v2/v3 keep their historical local-object-database baseline behavior; format v4 adds authenticated embedded baseline proof while retaining Evidence v2 semantics, and format v5 adds Evidence v3 deferred-gate semantics without redefining older package behavior.

For locked schema-v4 Change Contracts, package verification MUST prove that the packaged Project Policy SHA-256 equals `baseline_lock.policy_sha256`. For package formats v4 and v5 it MUST additionally validate the embedded baseline digest, canonical object inventory, Git object identities, complete locked tree/blob closure, and locked base commit/tree relationship before consumer admission. Consumer `preflight` and `apply` therefore MUST NOT require `.polis/policy.json` in the target repository.

At consumer boundaries, `preflight` and `apply` execute validation with the packaged effective policy, validate deterministic target-tree identity and scope, and perform fail-closed patch checks. The consumer baseline mode is explicit and defaults to `strict`:

- `strict` requires consumer `HEAD` to equal the artifact/locked base commit exactly and preserves the historical exact-baseline behavior. Embedded proof MUST NOT relax this equality requirement;
- `compatible` permits a different clean consumer `HEAD` only when the artifact base is locally available and provably an ancestor of that `HEAD`, the exact payload applies cleanly to the observed consumer tree, and complete isolated target validation passes. Embedded proof MUST NOT substitute for the consumer ancestry requirement;
- `permissive` may admit a clean consumer `HEAD` whose ancestry from the artifact base cannot be proven. Required development proof MUST use a locally resolvable locked baseline when available, otherwise a valid format-v4 or v5 embedded baseline, and only if neither source can establish the proof may the caller explicitly authorize `--allow-missing-baseline-proof`. A non-ancestral admission MUST remain visibly reported.

Baseline proof source resolution is therefore `local -> embedded -> explicitly overridden`. A successful embedded replay carries proof and MUST NOT be reported as an override or reduced-safety execution.

`--allow-missing-baseline-proof` is valid only with `--baseline-mode permissive`. It is never enabled by default, MUST be supplied independently to `preflight` and `apply`, and MUST waive only the development proof that cannot be established because the locked producer baseline is unavailable. POLIS MUST report `override_active`, every `bypassed_guarantee`, and warnings in machine-readable output and MUST prominently identify the active override in text output. The bypass list is derived from the actual change/proof path and may include locked baseline behavior replay, Red proof reconstruction, Red regression-path reconstruction, and strict Red blob-identity reconstruction.

The missing-baseline override MUST NOT bypass package structure/checksums, embedded-baseline corruption, Git object-format compatibility, consumer cleanliness, exact payload `git apply --check`, payload identity, Change Contract scope, complete target/project validation, deterministic consumer target-tree verification, or transactional post-apply verification. Malformed or inconsistent embedded baseline material is an invalid artifact and is never converted into an override-eligible package.

For non-exact consumer baselines, the package's `target_tree` remains an integrity claim about applying the payload to the artifact base. POLIS MUST compute a separate deterministic consumer target tree by applying the exact payload to the observed consumer `HEAD` in isolated Git state. Isolated target validation and the real post-apply check MUST require that computed consumer target tree exactly. Change scope is evaluated relative to the observed consumer `HEAD`, so pre-existing consumer commits are not misclassified as payload changes.

Successful consumer results MUST expose at least `baseline_mode`, `baseline_source` (`local|embedded|overridden`), `consumer_base_commit`, `baseline_ancestry` (`exact|proven_descendant|unproven`), `override_active`, `bypassed_guarantees`, warnings when present, and the dynamically validated target tree.

All consumer modes require matching Git object format and a clean real worktree/index. `compatible` MUST reject non-descendant history. Normal `permissive` execution MUST NOT skip artifact verification, development proof, scope validation, project validation, conflict detection, or transactional post-apply verification. Without valid local/embedded proof, permissive execution remains fail-closed unless the explicit missing-proof override is supplied. Patch conflict, failed precondition, scope failure, project-validation failure, or post-apply mismatch remains fail-closed even under that override.

Embedded baseline objects MUST be materialized only into temporary isolated Git state outside the consumer repository. Consumer isolation MUST NOT create consumer refs, write producer-only objects to the consumer persistent object database, modify consumer HEAD/index/configuration, or leave linked-worktree administration in the target repository. Temporary embedded-baseline state MUST be removed after success or failure.

`preflight` remains read-only with respect to the target. `apply` repeats validation and MUST NOT reuse a cached preflight PASS or cached override authorization. Immediately before real mutation, `apply` MUST require the real consumer `HEAD` and clean status to remain exactly the state that completed isolated validation; a newly changed but otherwise compatible `HEAD` requires a fresh validation pass.

## 8. Offline runtime bundle

`polis export --out <bundle.zip> [--executable <file>] [--runtime <GOOS/GOARCH>]` MUST emit one self-contained ZIP bundle for offline POLIS V6 use. The bundle is a runtime distribution and is distinct from a delivery `.polis` package. When `--executable` is omitted, the current executable is embedded; when `--runtime` is omitted, the current `GOOS/GOARCH` is recorded. An explicit target runtime MUST use the `GOOS/GOARCH` form and determines the executable member suffix without executing the embedded binary.

The bundle MUST contain:

- a `polis` executable for the declared target `GOOS` and `GOARCH` (or the exporting runtime when no target is declared);
- `POLIS-OFFLINE.md` with the offline operating instructions;
- `spec/POLIS-SPEC-v6.md`;
- the V6 policy, Change Contract v3/v4, package manifest, evidence-event, and signature schemas;
- `manifest.json` declaring the POLIS version, runtime, executable path, prerequisites, and `network_required: false`;
- `SHA256SUMS` covering every other bundle member.

The bundle MUST NOT contain the target repository, a project-specific policy, credentials, or external input paths. It MUST be deterministic, MUST NOT overwrite an existing output, and MUST validate its archive contents before returning success.

Using the bundle MUST NOT require Go or another programming-language runtime, and the runtime bundle itself MUST NOT perform network access. A project command configured in a policy MAY have its own network dependency. Git remains a required native prerequisite for POLIS repository operations, and a consumer MUST verify that the host matches the runtime recorded in the manifest. The bundle is not accepted by `polis verify`; it is used by extracting and invoking its embedded executable directly.

## 9. Zero-residue target invariant

Successful canonical external-policy execution MUST leave no tool-owned state in the target repository after command completion.

For `start`, `capture-red`, `build`, `preflight`, and `apply`, this includes, where applicable:

- no `.polis` directory or policy/configuration file created by the workflow;
- no `.git/polis` directory or persistent result/evidence file;
- no linked-worktree administration created under the target Git metadata;
- no temporary indexes, lock files, or tool-owned configuration entries;
- no tool-created persistent Git objects caused solely by temporary tree/index construction;
- no generated payload reference to the tool name merely because the payload was produced by the tool.

Temporary files, clones, object databases, and evidence may exist outside the target repository while a command is running. A successful command MUST remove its temporary external state before returning when that state is owned by the command. Cleanup failure that can leave tool-owned residue MUST fail closed.

`apply` evidence is ephemeral by default: it is written outside the target repository, validated before real mutation, and removed before successful return. The default successful result does not expose a persistent evidence path.

The zero-residue invariant does not rename or remove canonical members inside the external `.polis` delivery artifact itself; the artifact is not target-repository state.

## 11. Traceability

Strict schema-v4 Specification traceability remains authoritative:

```text
REQ -> AC -> regression proof
```

Every requirement MUST be covered by at least one acceptance criterion, every acceptance criterion MUST reference existing requirements, and the proof binding remains deterministic.

## 12. Unchanged contracts

V6 preserves the following contracts unless explicitly superseded above:

- exact reader compatibility for historical package formats v2/v3/v4; new builds use format v5 with the authenticated baseline member and Evidence v3;
- Project Policy schema v3 gate registry, its backward-compatible `validation_level` reinforcement setting, and additive `depends_on` dependency declarations;
- Change Contract schema v4 structure;
- Evidence v2 package member, digest, and bounded-output contract for formats v2-v4; format v5 Evidence v3 adds the explicit deferred-gate inventory and `DEFERRED` terminal status while retaining bounded output;
- coverage adapters and strict `>` threshold semantics;
- bounded stdout/stderr retention and full-stream digests;
- direct argv execution and declared environments;
- detached Ed25519 signature model;
- explicit consumer baseline-compatibility admission with `strict` default and deterministic exact target-tree validation for the admitted consumer base;
- transactional apply preserving HEAD and the real index;
- existing package/member resource limits, with the 32 MiB embedded-baseline member limit for v4/v5 while the 64 MiB archive and aggregate caps remain unchanged.

## 13. Major-version rationale

V6 remains a semantic major because producer operations that were valid in V5 become invalid: a caller cannot create a new artifact directly from Change Contract schema v2 or unlocked strict schema v3. The required `polis start` lock and development proof are part of the producer contract.

This V6 refinement introduces package format v5 and Evidence v3 for new builds without changing the Project Policy or Change Contract schema versions. Formats v2-v4 retain their original Evidence v2 semantics and remain part of the supported reader compatibility contract.
