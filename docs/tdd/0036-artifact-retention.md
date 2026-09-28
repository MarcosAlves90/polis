# TDD 0036 — Repository-Declared POLIS Artifact Retention

## Scope

Implement SDD-0041 without changing Project Policy, Change Contract, package,
evidence, signature, or baseline schemas. Keep the existing external output
path as the working result and publish retained copies only from the committed
repository preference.

## Test pyramid

- **Unit:** manifest decoding and committed-state loading; managed-path status
  filtering; safe, idempotent, content-addressed writes; retained-input class,
  path, digest, and size checks.
- **Integration:** producer functions operating on temporary committed Git
  repositories, including repository mode, external mode, a modified or
  uncommitted manifest, existing artifacts, symlinks, and index/HEAD
  preservation. Consumer preflight and apply also ignore staged managed
  artifacts in source-delta checks while preserving their index and bytes.
- **CLI:** `start`, `implementation-plan`, `capture-red`, and `build` expose
  retained paths in their existing output formats; a retained contract, plan,
  and proof can be passed to later producer steps.

## Red/Green sequence

1. Add `TestIssue16StartRetainsContract` to the existing `devstart` tests. Its
   fixture commits a repository-mode manifest. The focused test must fail on
   the locked baseline because no retained contract appears.
2. Capture that failing test with `polis capture-red` against the locked
   Change Contract.
3. Implement the shared retention loader, publisher, safe input reader, and
   managed-path filtering; integrate the four producers and CLI reporting.
4. Run the focused test to Green, then add the unit and cross-command
   integration cases above.
5. Run package tests, the project suite, race checks, coverage, vet, build,
   module verification, and the complete POLIS producer/consumer checks.

The configured repository gate remains the existing `test.complete` command
and strict project coverage threshold above 80%. No new CI command or coverage
exception is introduced.

## Evidence

- **Red:** `TestIssue16StartRetainsContract` failed against the locked baseline
  with the expected missing-retained-contract assertion. `polis capture-red`
  produced the proof patch with SHA-256
  `d588a33b96a9465c1d9ff745f42341199d05d39fedd48339bae5495028cb045f`.
- **Green:** focused artifact-retention and consumer-apply checks passed. The
  full race suite passed: `go test -race ./...` reported 724 tests across 25
  packages.
- **POLIS:** `polis start` passed and locked the approved scope at base commit
  `fa8b1b80915ded1e3a708f2b8710cc4fa4079730` (lock SHA-256
  `71eea355b33faceb24acf037ff3d0e60d2740d279e5030f286492363b4a49d74`).
  `polis build` passed at strict validation level with no deferred gates;
  `test.complete`, `coverage`, `lint`, `build`, and `dependency` all passed.
  The measured line coverage was 6,927 / 8,640 = 80.1736%, above the strict
  `> 80%` threshold. `polis verify` returned `PASS` and reported no consumer
  validation requirement.
- **Other checks:** `gofmt`, `git diff --check`, `go vet ./...`, `go build
  ./...`, and `go mod verify` passed. The repository has no `package.json`, so
  the generated global `npm run verify` command is unavailable here; the
  project’s configured POLIS gates were used for final validation.
