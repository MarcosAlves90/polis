# SDD-0016 — Terminal installation documentation

Status: accepted for POLIS V4.0.0 documentation.

## Objective

Document a supported, reproducible way to install POLIS as the `polis` terminal command on every operating-system family exercised by the repository CI matrix: Linux, macOS, and Windows.

## Required behavior

- The README MUST expose a concise installation entry point.
- The canonical command MUST be `go install github.com/MarcosAlves90/polis/cmd/polis@latest`.
- A dedicated installation guide MUST cover Linux, macOS, and Windows PowerShell separately where `PATH` mechanics differ.
- The guide MUST explain that Go installs commands to `GOBIN`, falling back to `GOPATH/bin` when `GOBIN` is empty.
- The guide MUST show current-session and persistent `PATH` setup for the documented shells.
- Every OS section MUST end with executable verification through `polis doctor`.
- The guide MUST include installation from a source checkout, upgrade, removal, and command-not-found troubleshooting.
- Documentation MUST NOT claim package-manager integrations, signed installers, or release assets that the repository does not currently provide.

## Acceptance

`internal/cicontract.TestInstallationDocumentationContract` protects the README entry point, the dedicated guide, all three OS sections, installation commands, `PATH` mechanics, source installation, upgrade/removal sections, and `polis doctor` verification.

The change is documentation/test-only and MUST NOT modify POLIS runtime behavior.
