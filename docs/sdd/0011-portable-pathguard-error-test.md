# SDD-0011 — portable pathguard absolute-path error validation

Status: accepted for 4.0.0 delivery correction

## Problem

The V005 consumer validation on macOS failed in `TestCanonicalPropagatesAbsolutePathResolutionFailure`. The test attempted to force `filepath.Abs` to fail by changing into a temporary directory, deleting that directory, and then resolving a relative path. That setup produced the expected error on the Linux producer but macOS did not guarantee the same `Getwd` failure behavior.

The production containment behavior was not shown to be incorrect by this failure; the test oracle itself depended on operating-system filesystem behavior that is outside the contract.

## Contract

- Production `canonical` behavior MUST remain unchanged.
- The `filepath.Abs` error-propagation branch MUST be testable deterministically without mutating the process current working directory.
- The test MUST inject a controlled absolute-path resolver failure and assert that the exact error is propagated.
- No platform skip or weakened assertion is permitted for this error path.
