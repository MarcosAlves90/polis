# POLIS V6 Offline Kit

This bundle is a self-contained POLIS V6 runtime for an offline coding agent.
It contains the native POLIS executable, the V6 specification, and the JSON
schemas needed to create and inspect POLIS inputs and outputs.

## Requirements

- Git must be installed and reachable through `PATH`.
- The bundle must run on the operating system and CPU architecture recorded in
  `manifest.json`.
- No Go, Python, Node.js, Ruby, or internet access is required by the POLIS runtime. A project command configured in a policy may have its own network dependency.

After extraction, the executable is `bin/polis` on Unix-like systems and
`bin/polis.exe` on Windows.

`polis export` uses the current executable and runtime by default. Release
automation may pass `--executable <file>` and `--runtime <GOOS/GOARCH>` to a
host POLIS exporter so it can package a cross-compiled target binary without
executing that binary. The runtime declaration is recorded in `manifest.json`
and determines the executable member suffix.

## V6 workflow

Run the executable directly from this bundle:

```text
./bin/polis doctor
./bin/polis init --repo /path/to/repository --profile auto
./bin/polis plan --repo /path/to/repository
./bin/polis start --repo /path/to/repository --policy /outside/policy-v3.json --contract /outside/draft-v5.json --out /outside/locked-v6.json
./bin/polis implementation-plan --repo /path/to/repository [--policy /outside/policy-v3.json] --contract /outside/locked-v6.json --out /outside/implementation-plan.json
./bin/polis capture-red --repo /path/to/repository --contract /outside/locked-v6.json [--implementation-plan /outside/implementation-plan.json] --out /outside/regression.patch
./bin/polis build --repo /path/to/repository --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/locked-v6.json --regression-patch /outside/regression.patch [--implementation-plan /outside/implementation-plan.json] [--defer-gate gate-id ...] --out /outside/output
./bin/polis verify /outside/output/artifact.polis
./bin/polis preflight --repo /path/to/repository [--baseline-mode strict|compatible|permissive] /outside/output/artifact.polis
./bin/polis apply --repo /path/to/repository [--baseline-mode strict|compatible|permissive] [--commit-mode none|prompt|auto] /outside/output/artifact.polis
```

Use `polis sign` separately when a detached Ed25519 signature is required.
The external policy, locked contract, regression patch, package, and signature
should remain outside the target worktree in the canonical zero-residue flow.
The optional Implementation Plan is generated only from a locked contract and a
clean baseline. It remains external, and both producer commands validate it when
explicitly supplied. Unplanned builds continue to use format v5; planned builds
use format v6 and authenticate the exact plan as the ninth package member.

The legacy strict schema-v3 draft to locked schema-v4 flow remains supported.
To include commit intent, put an optional `commit.message` in the schema-v5
draft before `start`; `start` locks it as schema v6 and `build` packages it in
the existing `polis/polis-change.json` member. Package format v5 and its
eight-member inventory do not change. A trusted detached signature
authenticates the message when verified; package checksums alone do not prove
producer identity.

`apply` remains apply-only by default (`--commit-mode none`). `prompt` displays
the exact message and validated target tree before real mutation and requires
interactive confirmation. `auto` explicitly authorizes a local commit without
confirmation. Both committing modes require commit metadata, create exactly one
commit with the validated consumer HEAD as parent and the validated target tree,
and leave the repository clean on success. Refusal or a non-interactive prompt
is blocked without repository mutation. Commit mode does not run hooks, sign,
push, or change remote refs.

`strict` is the default validation level. `standard` and `minimal` reduce only
optional project-quality gates; policy, contract, path, baseline-compatibility,
integrity, security, development-proof, and transactional-apply invariants
remain mandatory. `polis plan` and execution evidence show which gates are
enabled or disabled.

Consumer baseline mode is a separate option. `strict` requires the exact artifact
base, `compatible` accepts a different descendant only after complete
compatibility validation, and `permissive` can attempt a non-descendant base only
when the locked artifact base remains available and all patch, proof, scope, and
policy checks still pass. Relaxed modes never suppress real conflicts.

Unplanned builds use package format v5; planned builds use format v6. Both use
Evidence v3. Repeat `--defer-gate <id>` on
`plan` to preview or on `build` to skip an enabled producer gate. The packaged
consumer policy still requires every enabled gate during `preflight` and
`apply`. Formats v2-v4 retain their Evidence v2 semantics.

The offline kit includes Change Contract schema v5/v6 resources for commit
metadata, Implementation Plan schema v1, and the format-v6 manifest schema.
Historical contract schemas v1-v4 remain supported under their existing rules;
package formats v2-v5 retain their exact inventory and behavior.

The bundle does not contain a target repository or project-specific policy.
It can therefore be copied between projects without carrying project data.
