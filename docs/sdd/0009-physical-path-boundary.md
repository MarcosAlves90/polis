# SDD-0009 — Physical path containment

Status: accepted for 4.0.0

## Problem

POLIS uses repository boundaries as security boundaries for external Change Contract files, regression patches, capture-red outputs, and coverage reports. Lexical path comparison is insufficient on filesystems where multiple path spellings resolve to the same physical location, including macOS `/var` and `/private/var`, and when symlinks are present.

## Contract

1. Security-sensitive containment MUST compare canonical physical paths, not only lexical absolute paths.
2. Existing symlinks MUST be resolved before containment comparison.
3. A candidate that does not yet exist MUST be canonicalized by resolving its nearest existing ancestor and appending the missing suffix. This is required for output paths that are checked before creation.
4. Broken or otherwise unresolvable path components MUST fail closed for containment-sensitive decisions.
5. Paths equal to the repository root count as contained.
6. `packagebuild`, `redcapture`, and coverage report validation MUST use one shared containment implementation.
7. The implementation MUST work without shell invocation and without platform-specific path rewriting rules.

## Acceptance

- aliased/symlinked spelling of a repository path is recognized as contained;
- a non-existent output below an aliased repository path is recognized as contained;
- an external sibling path remains outside;
- packagebuild and capture-red reject inputs/outputs physically inside the repository even when reached through an alias;
- coverage reports resolving outside remain rejected;
- all existing tests remain Green.
