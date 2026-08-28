# SDD-0010 — POLIS V4.0.0 release boundary

Status: accepted for 4.0.0

POLIS V4.0.0 closes the remaining known semantic blocker from alpha.7: filesystem containment now uses a shared physical-path boundary instead of lexical path spellings. The package format remains v2, Project Policy remains v2, and Change Contract remains v1. No incompatible package-format change is introduced.

The version identifier exposed by the CLI and Guide is 4.0.0. Platform claims remain evidence-scoped: Linux runtime behavior is validated by the producer; other platforms must pass the same repository suite and POLIS consumer validation before being claimed as natively validated.
