# Your first POLIS change

Use this short guide to choose the right path. POLIS has two workflows: validate
changes in the checkout you are already editing, or produce a transferable package
for a different checkout. The [V6 specification](../spec/POLIS-SPEC-v6.md) is normative;
[command reference](usage.md#command-reference) is the concise operational reference.

## Choose the workflow

- **Local coding agent:** you or the agent already has the target checkout. Use a
  locked Change Contract, the required Red/Green proof, and
  [`polis workspace validate`](workflows/local-agent-workspace.md). Do not build a
  package for the same checkout.
- **Remote/web handoff:** a producer with the source checkout and POLIS runtime
  creates a disposable `.polis` package outside the repository. A consumer in a
  separate checkout verifies and applies it. Follow the
  [Remote delivery guide](workflows/web-agent-delivery.md).

A chatbot without source-checkout and POLIS-runtime access cannot claim it built or
verified a package. Never put a delivery `.polis` file in a repository.

## Local coding agent: first validated change

Requirements: a Git worktree, Go and Git available to the configured gates, and a
clean committed baseline. Keep the effective Project Policy, Change Contract, Red
patch, plan (if used), and validation report outside the worktree.

```bash
# Check prerequisites before creating any temporary workspace state.
polis doctor --repo .

# Preserve a caller-provided external directory; otherwise allocate a default.
# If TMPDIR is inside the repository, set POLIS_OUTSIDE to another external path first.
POLIS_OUTSIDE="${POLIS_OUTSIDE:-$(mktemp -d)}"
mkdir -p "$POLIS_OUTSIDE"

polis init --repo . --profile auto --dry-run > "$POLIS_OUTSIDE/policy.json"
polis plan --repo . --policy "$POLIS_OUTSIDE/policy.json"
```

Prepare a strict schema-v5 draft for the actual change, then lock it before editing:

```bash
polis start --repo . \
  --policy "$POLIS_OUTSIDE/policy.json" \
  --contract "$POLIS_OUTSIDE/draft-v5.json" \
  --out "$POLIS_OUTSIDE/locked-v6.json"
```

For a feature or defect, add only the scoped failing test first, then capture the
Red proof. For behavior-preserving work, use a Green-to-Green characterization
instead; do not manufacture a failing test.

```bash
polis check-red-scope --repo . \
  --contract "$POLIS_OUTSIDE/locked-v6.json" \
  --path path/to/new_test.go
# Add the test-only change, confirm its intended failure, then:
polis capture-red --repo . \
  --contract "$POLIS_OUTSIDE/locked-v6.json" \
  --out "$POLIS_OUTSIDE/regression.patch"
```

Implement the change without editing captured proof bytes. Validate the complete
locked target without creating a package:

```bash
polis workspace validate --repo . \
  --policy "$POLIS_OUTSIDE/policy.json" \
  --contract "$POLIS_OUTSIDE/locked-v6.json" \
  --regression-patch "$POLIS_OUTSIDE/regression.patch" \
  --out-report "$POLIS_OUTSIDE/workspace-validation.json" \
  --format json
```

For Green-to-Green work, omit `--regression-patch`. Read the result and confirm
`workspace_validated: true`; the report is an external checkpoint, not a signed
package or consumer authorization. See the complete
[Local workspace guide](workflows/local-agent-workspace.md) for failure handling,
retention, and status.

## Remote/web handoff

Use this only when a different checkout needs a portable delivery:

```bash
polis build --repo /path/to/producer \
  --policy /outside/policy.json \
  --project project-slug --change change-slug \
  --contract /outside/locked-v6.json \
  --regression-patch /outside/regression.patch \
  --out /outside/package-output
polis verify /outside/package-output/<generated-file>.polis
```

Transfer the verified package to its consumer, who separately reviews, preflights,
and applies it with authorization. The output path must resolve outside the target repository,
and the package is disposable. Follow the full
[Remote delivery guide](workflows/web-agent-delivery.md) for signatures, baselines,
and handoff checks.
