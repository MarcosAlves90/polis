# TDD evidence — Continuous integration

## Red

Before `.github/workflows/ci.yml` existed, `go test ./internal/cicontract -run TestCIWorkflowContract` failed because the workflow file could not be read. This established the missing CI contract independently of production behavior.

## Green

After adding the workflow, the same contract test passes and verifies:

- push, pull-request, and manual triggers;
- Ubuntu, macOS, and Windows runner coverage;
- complete tests, build, and doctor execution;
- formatting, vet, module integrity, race, and coverage gates;
- strict line coverage `>80.0%`;
- full commit-SHA pinning for third-party Actions;
- read-only contents permission.

The complete repository suite and coverage gate remain separate mandatory delivery gates; this contract test does not substitute for them.
