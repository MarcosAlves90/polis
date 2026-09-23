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
./bin/polis start --repo /path/to/repository --policy /outside/policy-v3.json --contract /outside/draft-v3.json --out /outside/locked-v4.json
./bin/polis capture-red --repo /path/to/repository --contract /outside/locked-v4.json --out /outside/regression.patch
./bin/polis build --repo /path/to/repository --policy /outside/policy-v3.json --project project-slug --change change-slug --contract /outside/locked-v4.json --regression-patch /outside/regression.patch [--defer-gate gate-id ...] --out /outside/output
./bin/polis verify /outside/output/artifact.polis
./bin/polis preflight --repo /path/to/repository [--baseline-mode strict|compatible|permissive] /outside/output/artifact.polis
./bin/polis apply --repo /path/to/repository [--baseline-mode strict|compatible|permissive] /outside/output/artifact.polis
```

Use `polis sign` separately when a detached Ed25519 signature is required.
The external policy, locked contract, regression patch, package, and signature
should remain outside the target worktree in the canonical zero-residue flow.

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

New builds use package format v5 and Evidence v3. Repeat `--defer-gate <id>` on
`plan` to preview or on `build` to skip an enabled producer gate. The packaged
consumer policy still requires every enabled gate during `preflight` and
`apply`. Formats v2-v4 retain their Evidence v2 semantics.

The bundle does not contain a target repository or project-specific policy.
It can therefore be copied between projects without carrying project data.
