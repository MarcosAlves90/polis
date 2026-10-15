# Local coding-agent workspace validation

Use this path when Codex, Claude Code, another desktop agent, or a developer is already operating inside the target Git checkout and will leave the source changes there for review. This path validates the live worktree; it does not build a `.polis` delivery package.

## Keep workflow inputs outside the repository

Set `REPO` to the checkout and `POLIS_OUTSIDE_DIR` to an external directory. The Project Policy, draft/locked Change Contract, optional Implementation Plan, captured Red patch, and workspace-validation report belong outside the target worktree. `.polis/policy.json`, when a project already uses it, is Project Policy configuration—not a delivery package. The report path is an external report, not repository state.

## Local workflow

Start from the project's clean committed baseline. Use the exact same external Policy and immutable locked contract throughout the change:

```bash
mkdir -p "$POLIS_OUTSIDE_DIR"
polis init --repo "$REPO" --profile auto --dry-run > "$POLIS_OUTSIDE_DIR/policy.json"
polis plan --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --format json
polis start --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --contract "$POLIS_OUTSIDE_DIR/draft-v5.json" --out "$POLIS_OUTSIDE_DIR/locked-v6.json"
```

For Red-to-Green work, preflight proposed test paths, capture the failing tests before production edits, then implement while keeping the captured tests unchanged. For behavior-preserving work, run the same Green characterization command on the locked baseline and target instead of manufacturing Red evidence.

```bash
polis check-red-scope --repo "$REPO" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --path path/to/regression_test.go
polis capture-red --repo "$REPO" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --out "$POLIS_OUTSIDE_DIR/regression.patch"
# Edit and test the in-repository source change.
polis workspace validate --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --regression-patch "$POLIS_OUTSIDE_DIR/regression.patch" --out-report "$POLIS_OUTSIDE_DIR/workspace-validation.json" --format json
```

Omit `--regression-patch` for a valid Green-to-Green contract; pass the exact same optional plan bytes to proof and validation if the change uses a plan. `workspace validate` runs the locked proof and enabled project gates against the current target, preserves the real HEAD/index/source, and writes only the requested external checkpoint. Its successful result says `workspace_validated=true` and `delivery_artifact_built/verified=false`.

To resume or compare the saved checkpoint without rerunning gates:

```bash
polis status --repo "$REPO" --policy "$POLIS_OUTSIDE_DIR/policy.json" --contract "$POLIS_OUTSIDE_DIR/locked-v6.json" --regression-patch "$POLIS_OUTSIDE_DIR/regression.patch" --report "$POLIS_OUTSIDE_DIR/workspace-validation.json" --format json
```

`status` reports workflow evidence and checkpoint identity; an unsigned report is historical data, not fresh validation or proof of implementation completion. Run `workspace validate` again for current gate evidence.

## Commands not used by this path

No `.polis` delivery package is required or created. `polis build` and `polis sign` are not part of this workflow; `polis preflight` and `polis apply` are not part of this workflow. Do not package the local worktree merely to establish that its locked change and gates pass. Keep task-owned outputs external and disposable.

For a separate consumer who needs a portable change, use [remote/web-agent delivery](web-agent-delivery.md) instead.
