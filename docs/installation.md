# Installing POLIS as a terminal command

POLIS is a Go command. The supported repository CI matrix exercises Linux, macOS, and Windows, and this guide documents installation for those operating-system families.

## Prerequisites

Install:

- Go 1.23 or newer;
- Git.

Verify both tools before installing POLIS:

```text
go version
git --version
```

## Recommended installation

The same command installs the current POLIS command on Linux, macOS, and Windows:

```text
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
```

`go install ...@latest` installs the executable into `GOBIN`. When `GOBIN` is empty, Go uses the `bin` directory under `GOPATH`.

After installation, `polis` must be reachable through `PATH`.

## Linux

Install POLIS:

```bash
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
```

Resolve the directory containing the executable:

```bash
POLIS_BIN="$(go env GOBIN)"
if [ -z "$POLIS_BIN" ]; then
  POLIS_BIN="$(go env GOPATH)/bin"
fi
printf '%s\n' "$POLIS_BIN"
```

If that directory is not already on `PATH`, enable it for the current shell:

```bash
export PATH="$PATH:$POLIS_BIN"
```

For Bash, persist the resolved directory for future terminals by adding it once to `~/.bashrc`:

```bash
printf '\nexport PATH="$PATH:%s"\n' "$POLIS_BIN" >> "$HOME/.bashrc"
source "$HOME/.bashrc"
```

Verify:

```bash
polis doctor
```

## macOS

Install POLIS:

```zsh
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
```

Resolve the directory containing the executable:

```zsh
POLIS_BIN="$(go env GOBIN)"
if [ -z "$POLIS_BIN" ]; then
  POLIS_BIN="$(go env GOPATH)/bin"
fi
printf '%s\n' "$POLIS_BIN"
```

If that directory is not already on `PATH`, enable it for the current shell:

```zsh
export PATH="$PATH:$POLIS_BIN"
```

The default macOS shell is normally Zsh. Persist the resolved directory for future terminals by adding it once to `~/.zshrc`:

```zsh
printf '\nexport PATH="$PATH:%s"\n' "$POLIS_BIN" >> "$HOME/.zshrc"
source "$HOME/.zshrc"
```

Verify:

```zsh
polis doctor
```

## Windows (PowerShell)

Install POLIS:

```powershell
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
```

Resolve the directory containing `polis.exe`:

```powershell
$goBin = (go env GOBIN).Trim()
if (-not $goBin) {
    $goBin = Join-Path ((go env GOPATH).Trim()) 'bin'
}
$goBin
```

Enable it in the current PowerShell session:

```powershell
$env:Path = "$env:Path;$goBin"
```

Persist it in the user `PATH` without intentionally adding a duplicate entry:

```powershell
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split ';' | Where-Object { $_ })
if ($entries -notcontains $goBin) {
    [Environment]::SetEnvironmentVariable(
        'Path',
        (($entries + $goBin) -join ';'),
        'User'
    )
}
```

New terminal windows will read the persistent user `PATH`. Verify the current session immediately with:

```powershell
polis doctor
```

## Install from a source checkout

Use this when developing POLIS or when you intentionally want the checked-out source instead of the version selected by `@latest`:

```text
git clone https://github.com/MarcosAlves90/polis.git
cd polis
go install ./cmd/polis
polis doctor
```

The same `GOBIN`/`GOPATH` and `PATH` rules above apply.

## Upgrade

Re-run the versioned installation command:

```text
go install github.com/MarcosAlves90/polis/v4/cmd/polis@latest
polis doctor
```

Go replaces the installed executable in the selected binary directory.

## Remove POLIS

First resolve the binary directory using the operating-system instructions above.

On Linux or macOS:

```bash
rm "$POLIS_BIN/polis"
```

On Windows PowerShell:

```powershell
Remove-Item (Join-Path $goBin 'polis.exe')
```

Removing POLIS does not remove Go, Git, source repositories, or POLIS artifacts created elsewhere.

## Troubleshooting

If `go install` succeeds but the shell reports that `polis` is not found, the executable directory is not on `PATH`. Resolve `GOBIN`, fall back to `GOPATH/bin`, update `PATH` for the current shell, and then persist that same directory using the operating-system section above.

If `polis doctor` reports `BLOCKED`, resolve the missing runtime prerequisite it identifies before using POLIS for verified delivery.
