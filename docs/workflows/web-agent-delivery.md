# Remote/web-agent delivery with a `.polis` package

Use this path when a producer must hand a validated change to a separate consumer repository. The producer may be a web agent with repository/terminal tools, a CI job, or a developer acting for a remote chatbot. The defining property is the **artifact handoff**, not where the conversation UI runs.

## Producer prerequisites

A real producer must have the source checkout whose baseline is being changed, Git, and a runnable POLIS runtime (V6 CLI). A chat-only assistant without that checkout/runtime can help draft code, tests, contracts, or commands, but must not claim it built or verified a `.polis` package. The owner or an authorized automation with the actual checkout must run the producer workflow.

Set `REPO` to the producer checkout and `POLIS_OUTSIDE_DIR` to an existing directory physically outside the target repository. Keep the Project Policy, draft/locked contract, captured regression patch, signature, and output package outside `REPO`. `polis build` rejects an output path that resolves inside the target worktree; repository retention never copies a new package into `.polis/artifacts/packages/`.

## Produce and transfer

Starting from a clean committed baseline, follow the locked strict workflow. Use the exact same external policy bytes for `start` and `build`:

```bash
polis init --repo "$REPO" --profile auto --dry-run > "$POLIS_OUTSIDE_DIR/policy.json"
polis plan --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --format json
polis start --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --contract "$POLIS_OUTSIDE_DIR/draft-v5.json" --out "$POLIS_OUTSIDE_DIR/locked-v6.json"
polis check-red-scope --repo "$REPO" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --path path/to/regression_test.go
polis capture-red --repo "$REPO" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --out "$POLIS_OUTSIDE_DIR/regression.patch"
# Implement the scoped change and preserve the captured tests.
polis build --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --project project-slug --change change-slug --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --regression-patch "$POLIS_OUTSIDE_DIR/regression.patch" --out "$POLIS_OUTSIDE_DIR/packages"
```

For Green-to-Green behavior-preserving work, use the locked characterization command and omit `--regression-patch`; follow the contract's proof rules. `build` reports the exact generated `*.polis` path. Verify those exact bytes before transfer:

```bash
polis verify /absolute/path/outside/repo/to/the/reported-package.polis
polis inspect /absolute/path/outside/repo/to/the/reported-package.polis
```

Signing is optional unless the trust policy requires it. A detached signature authenticates a producer only when the consumer verifies it against an independently trusted public key; package checksums alone establish integrity, not publisher identity. Transfer the package and, when used, its signature and trust instructions over the agreed channel. Do not copy the package into the producer or consumer repository.

## Consumer handoff

The consumer chooses the destination repository and reviews the received package and trust information. `preflight` is read-only; it does not authorize a later mutation. After explicit authorization, `apply` repeats validation and applies transactionally:

```bash
polis verify /outside/received/change.polis
polis preflight --repo /path/to/consumer /outside/received/change.polis
polis apply --repo /path/to/consumer /outside/received/change.polis
```

The package is a transferable, disposable input. After the consumer no longer needs it for audit or replay, the owner may remove the external package/signature; POLIS does not delete it automatically. Do not confuse a `*.polis` delivery package with the offline runtime bundle produced by `polis export` as a `.zip`, or with `.polis/policy.json`, which is project configuration.

## When not to use this path

If the AI coding agent already edits the target checkout and no portable handoff is required, use [local agent workspace validation](local-agent-workspace.md). It validates the live change without building or requiring a delivery package.
