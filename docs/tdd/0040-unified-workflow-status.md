# TDD-0040 — Unified workflow status projection

The regression scope covers both status sources: automatic repository-retained
artifacts and explicit external workflow artifacts.

The external-flow test creates an external canonical Policy, locked Change
Contract, contract-bound Implementation Plan, and workspace-validation report.
Top-level `polis status` must reconstruct them under `retention_mode=external`,
show the matching checkpoint as unsigned/unproven, and report the verified
package as still missing. After a later source change is packaged, the same
status call must report `complete`, prove the package, and mark the older
workspace checkpoint stale. A package-only status call must reconstruct the
embedded contract and remain complete.

The Red workflow regression also passes the external captured patch explicitly
to top-level status and requires it to be projected as a validated Red proof.
Existing repository-retention ambiguity, tamper/symlink rejection, and focused
workspace-status tests remain part of the suite.
