# POLIS Specification V6 — Mandatory Strict Development Producer

## 1. Authority and compatibility model

POLIS V6 retains package-format-v3, Project-Policy-v3, Evidence-v2, exact-tree, signature, bounded-input, environment, and transactional-apply contracts unless this specification explicitly supersedes them.

For new V6 producer operations, this specification supersedes the V5 producer-admission rules and the committed-policy-only baseline-lock semantics defined by earlier POLIS specifications.

V6 distinguishes three compatibility directions:

- **canonical V6 producer:** uses an explicit external Project Policy schema v3 and a locked Change Contract schema v4;
- **legacy producer compatibility:** when no explicit policy is supplied, `start` and `build` may continue to use an exact committed `.polis/policy.json` for existing/self-hosting repositories;
- **reader compatibility:** `verify` and `inspect` continue to decode valid historical schemas supported by V5, and `preflight`/`apply` may consume historical valid artifacts for migration.

Compatibility MUST NOT weaken the canonical zero-residue producer path or authorize creation of new legacy Change Contract schemas.

## 2. Runtime identity

The V6 CLI version is `6.0.0`.

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

Validation reinforcement levels apply only to project-quality gates. They MUST NOT disable policy/contract validation, path and environment safety, baseline or exact-tree checks, required development proof, package and evidence integrity, signature checks, isolated consumer validation, or transactional apply protections.

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

A policy hash mismatch MUST fail before an artifact is accepted. The canonical policy bytes are embedded in the existing `polis/polis-policy.json` package member; package format and member names do not change.

When `--policy` is omitted, committed-policy compatibility MAY remain as defined in section 4.2.

Schemas v1-v3 are decode-compatible but invalid producer input.

Temporary target-tree construction MUST keep temporary index object writes outside the target repository's persistent Git object database.

## 8. Reader and consumer compatibility

`verify` and `inspect` remain capable of validating historical package/contract schemas already supported by V5 when those artifacts are otherwise valid.

For schema-v4 artifacts, package verification MUST prove that the packaged Project Policy SHA-256 equals `baseline_lock.policy_sha256`. Consumer `preflight` and `apply` therefore MUST NOT require `.polis/policy.json` in the target repository.

At consumer boundaries, `preflight` and `apply` revalidate repository-dependent baseline facts, execute validation with the packaged effective policy, validate the exact target tree and scope, and perform the existing fail-closed patch checks. Consumer `HEAD` MUST still equal the artifact/locked base commit exactly; descendant producer-HEAD admission does not apply to consumers.

Consumer isolation MUST NOT create persistent linked-worktree administration or tool-created Git objects in the target repository. Isolated validation may use temporary external/shared clones or equivalent isolation whose cleanup is outside the target repository.

`preflight` remains read-only with respect to the target. `apply` repeats validation and MUST NOT reuse a cached preflight PASS.

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

## 10. Traceability

Strict schema-v4 Specification traceability remains authoritative:

```text
REQ -> AC -> regression proof
```

Every requirement MUST be covered by at least one acceptance criterion, every acceptance criterion MUST reference existing requirements, and the proof binding remains deterministic.

## 11. Unchanged contracts

V6 does not change:

- package format v3 and its seven canonical members;
- Project Policy schema v3 gate registry and its backward-compatible `validation_level` reinforcement setting;
- Change Contract schema v4 structure;
- Evidence v2 package member, digest, and bounded-output contract; V6 executions additionally emit `validation_configured` with the effective level and complete project-gate inventory;
- coverage adapters and strict `>` threshold semantics;
- bounded stdout/stderr retention and full-stream digests;
- direct argv execution and declared environments;
- detached Ed25519 signature model;
- exact baseline/target-tree validation;
- transactional apply preserving HEAD and the real index;
- package/member resource limits.

## 12. Major-version rationale

V6 remains a semantic major because producer operations that were valid in V5 become invalid: a caller cannot create a new artifact directly from Change Contract schema v2 or unlocked strict schema v3. The required `polis start` lock and development proof are part of the producer contract.

This zero-residue refinement changes where effective policy and temporary validation state live; it does not introduce a new package format or schema version.
