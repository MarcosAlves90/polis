# TDD-0039: Workspace checkpoint status

## Baseline and Red

The new test `TestRunWorkspaceStatus` was added to the test scope and run against the locked baseline (`cb0ec4a872838dbf2d3c1580ac889809fb75134c`). The expected missing-operation failure was observed: `unknown workspace operation "status"`; the test failed with exit 1 and the declared oracle `workspace status command should report matching source snapshot`.

POLIS `capture-red` passed for the locked v6 contract and produced the external regression patch with SHA-256 `7bc9c4b2a09a8711a8ecc5b9e8ed3c692faba27735aa579831582e6814fc2921`. The captured test file SHA-256 was `09c642c54409d3765efe892cbdf0698b8078b85f60baf2f9c03e7bb13329730c`.

## Green behavior

The integration test covers matching identity, changed target tree, contract digest mismatch, staged-index unavailability, and in-worktree/symlink/duplicate-key/unknown-field/oversized report rejection. Focused package tests additionally exercise temporary-index snapshot preservation and strict report decoding. Independent review found two P2 input-boundary gaps: a parent-directory swap could invalidate the report containment check, and external contract/policy reads used path-based size checks followed by unbounded `ReadFile`. External inputs now use an opened file descriptor, repeated worktree-boundary checks, file-identity comparisons, and a limit+1 read. The default committed policy is read through a bounded descriptor; the exact HEAD OID is pinned for both blob-size and content reads, and HEAD is rechecked afterward. `TestWorkspaceStatusReportReadIsBounded` verifies the byte cap, and `TestWorkspaceCommittedPolicyPinsHeadDuringBlobRead` verifies fail-closed behavior if HEAD moves between reads.

## Validation

After the bounded-reader fix, `TestWorkspaceStatusReportReadIsBounded` and the focused workspace/help tests passed. The affected-package suite and final strict `polis workspace validate` are run against the complete final tree using the locked policy, contract, and captured Red patch. Their machine-readable result is kept outside the repository: embedding it here would change the very target tree the report records. Read the external command output before claiming validation complete.

The status command is an identity comparison only; it does not rerun project gates or authenticate an unsigned report.