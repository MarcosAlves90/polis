# TDD evidence — Terminal installation documentation

## Red

`TestInstallationDocumentationContract` was introduced against the baseline before `docs/installation.md` existed. The targeted command failed while attempting to read the missing installation guide. This established that the requested installation documentation was absent and that the test could detect that absence.

## Green

After adding the installation guide and README entry point, the same targeted test passes and verifies:

- Linux, macOS, and Windows PowerShell instructions;
- `go install github.com/MarcosAlves90/polis/cmd/polis@latest`;
- `GOBIN` / `GOPATH/bin` discovery;
- shell-specific `PATH` instructions;
- `polis doctor` verification;
- installation from source;
- upgrade and removal guidance.

The project runtime is unchanged. Complete-suite, coverage, static-analysis, build, and CI evidence remain independent gates for the final delivery.
