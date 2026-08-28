# SDD-0012 — Cross-toolchain coverage margin

## Problem

A POLIS V3.1 consumer run of delivery V013 on macOS with Go 1.27 executed all targeted and affected tests successfully, but the authoritative project-wide line coverage gate produced `1723 / 2177 = 79.145613229215%`. The launcher correctly stopped before mutating the consumer worktree because coverage was not strictly greater than 80%.

The Linux/Go 1.23 producer result had only a narrow margin above the threshold. A release whose PASS depends on small coverprofile/toolchain differences is not sufficiently robust.

## Decision

Do not change the coverage parser, threshold, operator, production exclusions, or mandatory suite. Increase executable coverage of existing production branches with deterministic tests.

Add focused coverage for:

- package-build filesystem, Git, external-input, hashing, and exclusive-copy failure paths;
- package-apply baseline, Git-index, working-tree identity, and hashing failure paths;
- policy decoding, gate validation, threshold validation, and repository-relative path validation failure paths.

Tests MUST avoid OS-specific assumptions and MUST not alter production behavior merely to raise the percentage.

## Acceptance

- The new tests pass independently.
- Existing tests remain Green.
- Project-wide line coverage remains strictly `>80.0` with a meaningful margin on the producer toolchain.
- The consumer launcher retains the same strict `>80.0` gate and still refuses real application on coverage failure.
