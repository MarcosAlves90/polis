# Policy inputs and validation

## Project Policy and validation

`strict` is the default and preserves the strongest current behavior. Existing
V6 policies may omit the field for compatibility.

`standard` keeps `test.complete` required and disables generated coverage by
default. `minimal` disables generated project-quality gates by default.

Use repeated `--disable-gate <id>` only for generated project gates. Every
disabled gate is recorded with its reason; safety, integrity, baseline,
development-proof, and transactional invariants remain mandatory.

`polis init --profile auto` recognizes a root Go module only. Use `custom` for
other ecosystems and provide direct argv for test and coverage commands. POLIS
does not synthesize shell commands.

### Portable project commands

Policy commands are executed as direct `argv`, not through a shell. `polis plan`
reports a validation error for recognized shell command-string forms such as
`bash -lc`, `sh -c`, `powershell -Command`, and `cmd.exe /c`; producer and
consumer validation apply the same check before gates run. The check is lexical
and does not treat arguments to a direct executable as shell syntax.

Use `python -m pytest` for a single Python test command. For multiple tools or
working directories, put a runner under version control in the project and
declare one direct command such as
`python scripts/validate_project.py`. The
example at [`docs/examples/validate_project.py`](examples/validate_project.py)
can be copied to `<project>/scripts/validate_project.py` and adjusted to the
project's directory layout. It invokes pytest through
`sys.executable -m pytest`, runs Flutter directly with an explicit `cwd`,
preserves child exit codes, and reports missing required tools/modules as
failures. It never uses `shell=True`.

For `environment.mode=clean`, POLIS starts with an empty deterministic base,
adds only present Windows bootstrap variables (`SystemRoot`, `WINDIR`,
`COMSPEC`, `PATHEXT`, `TEMP`, `TMP`, `ProgramFiles`, `ProgramFiles(x86)`,
`ProgramW6432`, `USERPROFILE`, `LOCALAPPDATA`, `APPDATA`, `HOMEDRIVE`, and
`HOMEPATH`), then adds values named by the policy's explicit `pass` list.
Windows variable names are matched case-insensitively and emitted once. Missing
values are not invented; arbitrary variables and credentials remain excluded.
`inherit` continues to pass the process environment unchanged. Environment
values are never included in evidence or artifacts.

Do not let POLIS rewrite a policy already bound to a Change Contract or
artifact. If a project's command changes, create a new effective policy and
lock a new baseline with `polis start` before building a replacement artifact.

Each gate may declare `depends_on`. POLIS adds essential dependencies, rejects
unknown IDs, cycles, and enabled gates depending on `not_applicable` gates, then
executes a deterministic topological order.

`polis plan` is read-only. It reports the policy digest, commands, enabled and
disabled gates, dependency edges, execution order, mandatory invariants, and
the guarantees provided or absent by the effective policy. Repeat
`--defer-gate <id>` to preview transferring enabled gate validation to the
consumer. A producer-executed gate cannot depend directly or transitively on a
deferred gate.

Plan JSON version 4 adds `{source, reason}` provenance for the effective
validation level, gate configuration/state, producer action, consumer
requirement, dependency edges, and guarantees; the same decisions appear in
text output. For example,
`coverage -> test.complete` has `source: mandatory_dependency`, and a producer
action changed by `--defer-gate coverage` has `source: executor_deferral` while
the gate's enabled state still comes from the policy. The machine-readable
decision reason is separate from the project-authored `reason` of a
`not_applicable` gate. `polis init` writes `.polis/policy-provenance.json`
alongside the policy; review and commit both. The manifest preserves
`profile_default` and explicit init overrides at generation time with a
digest per gate. Only still-matching gates from a committed, unchanged manifest
can be attributed to the profile. Changed gates, legacy policies without a
committed manifest, and external policies use `policy_explicit` for observable
declarations. A disabled gate's `provenance.configuration` records the
configuration source while its state and responsibilities remain
`not_applicable`. This provenance is descriptive project metadata, not a
signature proving authorship.

The manifest separately records validation-level intent. A requested
`--validation-level strict` and an omitted flag both produce compatible policy
JSON with no `validation_level` field, but their recorded plan origins differ:
`policy_explicit` and `profile_default` respectively. Legacy policies and
manifests without this record keep the policy-observable `policy_default`
classification when the level is omitted. The same committed-policy provenance
is resolved for the producer `polis build`; optional manifests are size-limited
and are ignored if untrusted or invalid.

Use `polis gates [--repo <path>] [--policy <policy-v3.json>] [--format text|json]`
to execute the configured enabled project gates locally. The command reports
each gate result and whether it was executed, along with the effective enabled
and disabled gate lists. Its output explicitly sets
`delivery_artifact_built` and `delivery_artifact_verified` to `false`: passing
Project Policy gates does not build or verify a delivery artifact. Use
`polis build` and `polis verify` for the delivery workflow.

When a configured command cannot start its intended checks, POLIS reports its
gate as `BLOCKED`, names a missing executable, dependency, or environment
condition when the startup diagnostic is clear, and says the intended checks
did not run. Text and JSON gate reports keep genuine assertion failures as
`FAIL` and mark those checks as executed. The same distinction applies to Red
regression proof: a missing prerequisite cannot satisfy its expected nonzero
exit code or output oracle.

## Local quality checks

The local SonarQube workflow remains available:

```bash
export SONAR_TOKEN='your-token'
./scripts/sonar-local.sh
```

Use `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go mod verify`
before publishing changes. The repository CI exercises Linux, macOS, and
Windows.
