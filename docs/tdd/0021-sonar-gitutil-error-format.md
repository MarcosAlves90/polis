# TDD-0021 — SonarQube gitutil error-format remediation

## Red evidence

The supplied SonarQube export contains exactly one open finding after the duplication refactor: `go:S1192` in `internal/gitutil/gitutil.go`, reporting `"%s: %w"` three times.

## Green implementation

The target introduces a private `wrappedErrorFormat` constant and reuses it for staging, worktree-creation, and optional-label error wrapping.

## Green evidence

Validation must demonstrate that the literal occurs once, existing error behavior is preserved, all Go tests and race checks pass, project coverage remains above the strict 80% gate, and POLIS producer/consumer validation succeeds.
