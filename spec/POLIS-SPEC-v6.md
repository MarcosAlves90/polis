# POLIS Specification V6 — Mandatory Strict Development Producer

## 1. Authority and compatibility model

POLIS V6 retains the existing package-format-v3, Project-Policy-v3, Evidence-v2, exact-tree, signature, bounded-input, environment, and transactional-apply contracts unless this specification explicitly supersedes them.

For new V6 producer operations, this specification supersedes the V5 producer-admission rules defined by POLIS Specification v2/v3/v4.

V6 distinguishes two compatibility directions:

- **producer compatibility:** intentionally breaking; new `polis build` operations accept only locked Change Contract schema v4;
- **reader compatibility:** preserved; `verify` and `inspect` continue to decode valid historical schemas supported by V5, and `preflight`/`apply` may consume historical valid artifacts for migration.

Reader compatibility MUST NOT be used to authorize creation of a new legacy artifact.

## 2. Runtime identity

The V6 CLI version is `6.0.0`.

The Go module path is:

```text
github.com/MarcosAlves90/polis/v6
```

## 3. Required producer state machine

Every new V6 delivery follows this state sequence:

```text
committed Project Policy v3 + clean Git baseline
  -> accepted strict Change Contract schema-v3 draft
  -> polis start
  -> locked Change Contract schema v4 / strict_sdd_tdd_v2
  -> development proof appropriate to change kind
  -> polis build
  -> polis verify
  -> optional signature
  -> consumer preflight
  -> consumer apply
```

`polis build` MUST reject Change Contract schemas v1, v2, and v3 as producer input. The rejection occurs before target construction and before execution of Project Policy gates.

A valid producer contract MUST satisfy all schema-v4 semantics defined by POLIS Specification v4, including a valid `baseline_lock`.

## 4. `polis start`

`polis start` is the canonical transition from strict schema v3 to locked schema v4.

It MUST:

- operate on a clean committed Git baseline;
- require the committed Project Policy to be schema v3;
- accept an external strict schema-v3 draft using `strict_sdd_tdd_v1`;
- emit an external schema-v4 contract using `strict_sdd_tdd_v2`;
- bind Git object format, base commit, base tree, committed Project Policy SHA-256, and canonical Specification SHA-256;
- never mutate HEAD, index, existing worktree files, or committed Project Policy;
- never overwrite its output.

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

All existing test-scope, source-mutation, oracle, path, bounded-input, and captured-test immutability rules remain in force.

## 7. Producer `build`

V6 `build` requires:

- Project Policy schema v3;
- locked Change Contract schema v4;
- `development_method: strict_sdd_tdd_v2`;
- valid `baseline_lock` against the producer repository;
- regression patch exactly when the change requires Red-to-Green;
- all existing behavior, affected, policy, scope, coverage, target-tree, evidence, and package-integrity checks.

Schemas v1-v3 are decode-compatible but invalid producer input.

## 8. Reader and consumer compatibility

`verify` and `inspect` remain capable of validating historical package/contract schemas already supported by V5 when those artifacts are otherwise valid.

`preflight` and `apply` may consume valid historical artifacts to preserve migration compatibility. This compatibility does not weaken the V6 producer rule.

For schema-v4 artifacts, consumer boundaries continue to revalidate repository-dependent baseline-lock facts before mutation.

## 9. Traceability

Strict schema-v4 Specification traceability remains authoritative:

```text
REQ -> AC -> regression proof
```

Every requirement MUST be covered by at least one acceptance criterion, every acceptance criterion MUST reference existing requirements, and the proof binding remains deterministic.

## 10. Unchanged contracts

V6 does not change:

- package format v3 and its seven canonical members;
- Project Policy schema v3 gate registry;
- Evidence v2 format;
- coverage adapters and strict `>` threshold semantics;
- bounded stdout/stderr retention and full-stream digests;
- direct argv execution and declared environments;
- detached Ed25519 signature model;
- exact baseline/target-tree validation;
- transactional apply preserving HEAD and the real index;
- package/member resource limits.

## 11. Major-version rationale

V6 is a semantic major because producer operations that were valid in V5 become invalid: a caller can no longer create a new artifact directly from Change Contract schema v2 or unlocked strict schema v3. The required `polis start` lock and development proof are now part of the producer contract.
