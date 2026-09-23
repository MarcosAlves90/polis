# SDD-0039 — Artifact-Backed Commit Intent and Local Commit Mode

## Status

Accepted for implementation of GitHub issue #3. The source contract is the issue and its linked implementation brief. Production changes must follow the versioning, authorization, commit, rollback, and compatibility rules below.

## Objective

Carry an optional producer-authored commit message inside the Change Contract, then let a consumer explicitly choose apply-only, confirmed commit, or automatic local commit. The created commit must contain exactly the consumer target tree that passed POLIS validation.

## Change Contract versioning

Change Contract schemas v1–v4 retain their existing semantics and remain closed to the new `commit` property. Do not reinterpret historic version numbers.

Add a new strict draft schema v5 and locked schema v6:

- Draft v5 keeps `development_method: strict_sdd_tdd_v1`, the existing strict specification and traceability rules, and no `baseline_lock`.
- `polis start` accepts either the legacy strict draft v3 or new draft v5. It locks v3 to v4 and v5 to v6, respectively. The v5-to-v6 path preserves the decoded `commit.message` value exactly while adding the normal v6 locked baseline facts.
- Locked v6 keeps `development_method: strict_sdd_tdd_v2` and requires `baseline_lock`, as v4 does. `polis build` accepts locked v4 and v6; it rejects unlocked drafts and all unsupported producer schemas.
- Strict-development predicates include v3/v4/v5/v6. A locked-contract predicate includes only v4 and v6. All start, capture-red, devlock, build, verify, apply, schema, and offline-resource checks must use the correct predicate.
- `commit` is an optional object containing only `message`. It has no authorization field. When present, message must be non-empty, valid UTF-8, contain no NUL, contain at most 16,384 Unicode scalar values, and use at most 64 KiB of UTF-8 bytes. A whitespace-only message is still non-empty; no whitespace, casing, newline, subject, body, or footer is rewritten. Tests cover the exact decoded message bytes, multiline text, and presence or absence of a final newline.
- Keep v3/v4 schemas strict: a `commit` property in those contracts is rejected. Historical fixtures and readers remain valid without conversion.

## Package integrity and authentication

New artifacts remain package format v5 with Evidence v3 and the existing eight-member inventory. The exact locked contract bytes stay in `polis/polis-change.json`; `manifest.change_contract_sha256`, the package checksum inventory, and the optional detached artifact signature cover that existing member. No package-format bump or new archive member is required.

Verification decodes and validates v6 contracts and exposes the optional message only after normal package semantic and integrity checks. `inspect` reports the exact message when present and reports absence without synthesizing a suggestion. Package digests/checksums provide internal binding/integrity. They do not prove producer identity for an unsigned artifact; a trusted detached signature, when supplied and verified, authenticates the message transitively.

## Apply authorization and ordering

Add `--commit-mode none|prompt|auto` to `polis apply`; omission and the zero value mean `none`. `preflight` remains read-only and has no commit mode.

- `none` retains apply-only behavior. It can report the artifact message as a suggestion, but it does not create a commit or change HEAD/the real index.
- `prompt` and `auto` require valid commit metadata and resolvable consumer author/committer identity before real mutation. Missing metadata is a clear pre-mutation error.
- The package-apply layer receives an explicit `CommitMode` and, for prompt only, an injected confirmation callback. The CLI checks that input is interactive before prompting; a non-interactive prompt request fails closed without waiting.
- Prompt displays the exact commit message and validated target tree after package, baseline, isolated validation, and predictable commit preconditions have passed. Refusal is blocked authorization, mapped to the existing blocked result/exit category (exit 4). Refusal and non-TTY paths leave HEAD, index, and worktree unchanged.
- Immediately after confirmation, revalidate HEAD, index, worktree, and the already validated consumer assessment. Apply remains fail-closed if the consumer state changed.
- Auto is explicit consumer authorization and never prompts. No artifact field can select auto.

The required order for an authorized commit is:

```text
verify artifact and baseline
-> isolated consumer validation
-> validate commit metadata and Git identity
-> optional prompt confirmation
-> revalidate consumer HEAD/index/worktree
-> git apply --check and apply exact payload
-> verify real post-apply tree == validated consumer target tree
-> materialize that same tree using a temporary index and exact payload
-> construct one commit with that tree, validated consumer HEAD, and exact message
-> verify commit tree, sole parent, and message
-> compare-and-swap HEAD/ref from validated consumer HEAD to the new commit
-> align and verify real index; verify clean status and final commit invariants
-> PASS with commit result
```

The commit parent is the consumer HEAD admitted and revalidated immediately before real mutation, including compatible/permissive mode; it is never inferred from the artifact's producer base commit. The committed tree is the dynamic consumer target tree computed for that parent. The commit message is sent as stdin to Git plumbing, never interpolated into a shell command. Use `git commit-tree` without signing flags; use `git update-ref` with the expected old OID. Do not use `git commit`, real-index staging to derive content, or commit hooks. Do not perform push, remote-ref, branch, tag, PR, release, or other remote operations.

## Transaction and rollback boundary

The issue's rollback guarantee is defined over the named logical repository state: the original HEAD/ref value, original real-index contents/tree, and originally clean worktree. Before mutation, retain enough external temporary state to verify that snapshot. On any failure after `git apply`, reverse the exact patch and verify the original worktree; restore the index snapshot. If HEAD was moved, restore it only with a guarded compare-and-swap from the candidate commit to the original consumer HEAD. Never overwrite a concurrent ref change. Return an explicit rollback failure if the snapshot cannot be proven restored; never convert it to PASS.

Git's object database does not provide a transaction spanning object writes, refs, the index, and worktree. A failed attempt may therefore leave unreachable content-addressed tree/blob/commit objects in the local object database. This is outside the issue's explicit logical rollback surface (HEAD/ref, index, and worktree), does not create an additional ref or staged/worktree content, and can be reclaimed by ordinary Git garbage collection. POLIS must not delete object files manually. If a future requirement expands rollback to byte-for-byte object-database restoration, commit mode must fail closed until a safe strategy is proven.

Only return a commit SHA after verifying the final commit object, HEAD, real-index tree, target tree, message bytes, parent, and clean status. Successful commit mode is an intentional local product result: its new commit and ref are not disposable tool residue. The existing zero-residue rule remains for uncommitted apply and for temporary tool state.

## Results and compatibility

Apply result JSON adds `committed` (boolean), `commit_sha` (string or null), and `commit_message` (string or null). For `none`, `committed` is false and `commit_sha` is null; `commit_message` echoes artifact metadata or is null. Text output reports `Committed: no` and, when present, `Suggested commit message`; success reports `Committed: yes`, the commit SHA, and exact message. Inspect text/JSON exposes the optional intent. Existing baseline/deferred-gate result fields and exit categories remain intact.

Formats v2–v5 and Change Contract schemas v1–v4 remain readable and apply-compatible under their existing rules. A historical contract without commit metadata works in default/`none` mode. Requesting `prompt` or `auto` for an artifact without metadata fails before real mutation and never generates a message. Package format v5, Evidence v3, eight members, Project Policy v3, baseline modes, deferred-gate behavior, and optional-signature rules remain unchanged.

## Acceptance traceability and validation

Tests must directly prove:

- Draft v5/locked v6 schema validation, message boundaries, v3/v4 rejection of the new property, and v1–v4 historic compatibility.
- `start -> build -> verify -> inspect` with and without metadata; exact message preservation, manifest/checksum tamper rejection, detached-signature verification and tamper rejection; unchanged eight-member inventory and offline resources.
- Default/`none` apply-only behavior; `auto`; prompt acceptance, refusal, and non-TTY; missing metadata; output fields and exact message display.
- Exactly one commit with exact validated consumer parent, target tree, and message, clean final status, and no implicit hook or signing execution.
- State-change detection and failure injection at commit construction, ref CAS, index update, and final verification; rollback of HEAD/ref, index, and worktree or explicit rollback failure, never PASS.
- Compatible and permissive parent selection; unchanged remote refs and no push; deferred-gate fields preserved.
- Repository policy gates: `go test ./...`, coverage strictly greater than 80%, `go vet ./...`, `go build ./...`, and `go mod verify`; also formatting, diff checks, offline export inspection, and the configured `npm run verify` attempt.

No test or validation result is claimed until observed. The next SDD/TDD sequence numbers were rechecked against `origin/main` at `67f806b`: this is SDD-0039 and TDD-0034.
