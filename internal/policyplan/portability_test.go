package policyplan

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestCompileRejectsShellInterpreters(t *testing.T) {
	cases := []struct {
		name string
		argv []string
	}{
		{name: "bash", argv: []string{"bash", "-lc", "cd backend && pytest"}},
		{name: "sh", argv: []string{"sh", "-c", "pytest"}},
		{name: "PowerShell", argv: []string{"powershell.exe", "-Command", "pytest"}},
		{name: "cmd", argv: []string{"cmd.exe", "/c", "pytest"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := planPolicy(spec.ValidationLevelStrict)
			policy.Gates[0].Command.Argv = tc.argv
			if _, err := Compile(policy); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.name)) {
				t.Fatalf("shell command %q was not rejected with a gate-specific portability diagnostic: %v", tc.argv, err)
			}
		})
	}
}

func TestPreflightExecutables(t *testing.T) {
	cases := []struct {
		name        string
		argv        []string
		want        []string
		pathChanged bool
		cwdChanged  bool
	}{
		{"direct", []string{"go", "test", "./..."}, []string{"go"}, false, false},
		{"plain env", []string{"env", "-u", "OTHER", "go"}, []string{"env", "go"}, false, false},
		{"assignment ends option parsing", []string{"env", "FOO=x", "-u", "FOO", "/bin/true"}, []string{"env", "-u"}, false, false},
		{"assignment ends double dash parsing", []string{"env", "FOO=x", "--", "/bin/true"}, []string{"env", "--"}, false, false},
		{"assignment ends split-string parsing", []string{"env", "FOO=x", "-S", "sh -c echo"}, []string{"env", "-S"}, false, false},
		{"multiple assignments before command", []string{"env", "FOO=x", "BAR=y", "go"}, []string{"env", "go"}, false, false},
		{"lowercase path assignment", []string{"env", "path=/nonexistent", "go"}, []string{"env", "go"}, runtime.GOOS == "windows", false},
		{"lowercase path unset", []string{"env", "-u", "path", "go"}, []string{"env", "go"}, runtime.GOOS == "windows", false},
		{"lowercase path attached unset", []string{"env", "-upath", "go"}, []string{"env", "go"}, runtime.GOOS == "windows", false},
		{"lowercase path long unset", []string{"env", "--unset=path", "go"}, []string{"env", "go"}, runtime.GOOS == "windows", false},
		{"lone dash clears environment", []string{"env", "-", "/bin/true"}, []string{"env", "/bin/true"}, true, false},
		{"lone dash with PATH lookup", []string{"env", "-", "go"}, []string{"env", "go"}, true, false},
		{"nested lone dash", []string{"env", "-", "env", "/bin/true"}, []string{"env", "env", "/bin/true"}, true, false},
		{"double dash leaves lone dash executable", []string{"env", "--", "-"}, []string{"env", "-"}, false, false},
		{"nested env", []string{"env", "A=1", "env", "B=2", "go"}, []string{"env", "env", "go"}, false, false},
		{"unset PATH", []string{"env", "-uPATH", "go"}, []string{"env", "go"}, true, false},
		{"grouped options", []string{"env", "-iuPATH", "go"}, []string{"env", "go"}, true, false},
		{"PATH assignment", []string{"env", "PATH=/somewhere", "go"}, []string{"env", "go"}, true, false},
		{"alternate PATH", []string{"env", "-P", "/custom", "go"}, []string{"env", "go"}, true, false},
		{"attached alternate PATH", []string{"env", "-P/custom", "go"}, []string{"env", "go"}, true, false},
		{"long alternate PATH", []string{"env", "--path", "/custom", "go"}, []string{"env", "go"}, true, false},
		{"changed cwd", []string{"env", "-C", "elsewhere", "./runner"}, []string{"env", "./runner"}, false, true},
		{"attached cwd", []string{"env", "-Celsewhere", "./runner"}, []string{"env", "./runner"}, false, true},
		{"nested changes", []string{"env", "-i", "env", "--chdir=/custom", "./runner"}, []string{"env", "env", "./runner"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, pathChanged, cwdChanged := PreflightExecutables(tc.argv)
			if !slices.Equal(got, tc.want) || pathChanged != tc.pathChanged || cwdChanged != tc.cwdChanged {
				t.Fatalf("PreflightExecutables(%q) = %q, PATH changed=%t, cwd changed=%t; want %q, %t, %t",
					tc.argv, got, pathChanged, cwdChanged, tc.want, tc.pathChanged, tc.cwdChanged)
			}
		})
	}
}

func TestCompileAcceptsDirectPortableArgv(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	policy.Gates[0].Command.Argv = []string{"python", "-m", "pytest"}
	if _, err := Compile(policy); err != nil {
		t.Fatalf("direct argv was rejected: %v", err)
	}
}

func TestCompileAllowsDirectScriptOnNativePlatform(t *testing.T) {
	policy := planPolicy(spec.ValidationLevelStrict)
	if runtime.GOOS == "windows" {
		policy.Gates[0].Command.Argv = []string{"powershell.exe", "-File", "scripts/check.ps1"}
	} else {
		policy.Gates[0].Command.Argv = []string{"./scripts/check.sh"}
	}
	if _, err := Compile(policy); err != nil {
		t.Fatalf("direct native script invocation was rejected: %v", err)
	}
}

func TestCommandPortabilityIssueClassifiesPlatformsAndShellStrings(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		goos string
		want string
	}{
		{name: "Windows shell on POSIX", argv: []string{"cmd.exe", "/q"}, goos: "darwin", want: "Windows shell"},
		{name: "POSIX shell on Windows", argv: []string{"bash", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "sh.exe POSIX shell on Windows", argv: []string{"sh.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "bash.exe POSIX shell on Windows", argv: []string{"bash.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "dash.exe POSIX shell on Windows", argv: []string{"dash.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "ash.exe POSIX shell on Windows", argv: []string{"ash.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "zsh.exe POSIX shell on Windows", argv: []string{"zsh.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "ksh.exe POSIX shell on Windows", argv: []string{"ksh.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "fish.exe POSIX shell on Windows", argv: []string{"fish.exe", "scripts/check.sh"}, goos: "windows", want: "POSIX shell"},
		{name: "bash.exe command string", argv: []string{"bash.exe", "-c", "pytest"}, goos: "windows", want: "shell command-string"},
		{name: "PowerShell on POSIX", argv: []string{"powershell.exe", "-File", "scripts/check.ps1"}, goos: "linux", want: "Windows shell"},
		{name: "Bash command string", argv: []string{"bash", "-lc", "pytest"}, goos: "darwin", want: "shell command-string"},
		{name: "env-wrapped shell command", argv: []string{"env", "-u", "TEST_VALUE", "bash", "-c", "pytest"}, goos: "darwin", want: "shell command-string"},
		{name: "env lone dash shell command", argv: []string{"env", "-", "bash", "-c", "pytest"}, goos: "darwin", want: "shell command-string"},
		{name: "env -S separated split string", argv: []string{"env", "-S", "bash -c pytest"}, goos: "darwin", want: "env split-string"},
		{name: "env assignment before -S command", argv: []string{"env", "FOO=bar", "-S", "bash -c pytest"}, goos: "darwin"},
		{name: "env.exe split-string wrapper", argv: []string{"env.exe", "-S", "bash -c pytest"}, goos: "windows", want: "env split-string"},
		{name: "nested env.exe split-string wrapper", argv: []string{"env.exe", "env.exe", "-S", "bash -c pytest"}, goos: "windows", want: "env split-string"},
		{name: "env --split-string separated split string", argv: []string{"env", "--split-string", "bash -c pytest"}, goos: "darwin", want: "env split-string"},
		{name: "env -S attached split string", argv: []string{"env", "-Sbash", "-c", "pytest"}, goos: "darwin", want: "env split-string"},
		{name: "env bundled -S split string", argv: []string{"env", "-iSbash", "-c", "pytest"}, goos: "darwin", want: "env split-string"},
		{name: "env --split-string attached split string", argv: []string{"env", "--split-string=bash", "-c", "pytest"}, goos: "darwin", want: "env split-string"},
		{name: "env --argv0 argument before shell", argv: []string{"env", "--argv0", "alias", "bash", "-c", "pytest"}, goos: "darwin", want: "shell command-string"},
		{name: "abbreviated env chdir attached", argv: []string{"env", "--ch=/tmp", "./runner"}, goos: "linux", want: "unsupported env long option"},
		{name: "abbreviated env chdir separated", argv: []string{"env", "--ch", "/tmp", "./runner"}, goos: "linux", want: "unsupported env long option"},
		{name: "abbreviated env unset PATH", argv: []string{"env", "--un=PATH", "go"}, goos: "linux", want: "unsupported env long option"},
		{name: "abbreviated env split string", argv: []string{"env", "--split", "bash -c pytest"}, goos: "linux", want: "unsupported env long option"},
		{name: "nested abbreviated env chdir", argv: []string{"env", "env", "--ch=/tmp", "./runner"}, goos: "linux", want: "unsupported env long option"},
		{name: "unknown env long option", argv: []string{"env", "--mystery", "go"}, goos: "linux", want: "unsupported env long option"},
		{name: "env null output with a command", argv: []string{"env", "-0", "/bin/true"}, goos: "linux", want: "unsupported env short option"},
		{name: "env unknown short option", argv: []string{"env", "-Z", "go"}, goos: "linux", want: "unsupported env short option"},
		{name: "env unknown grouped short option", argv: []string{"env", "-i0", "go"}, goos: "linux", want: "unsupported env short option"},
		{name: "nested env unknown short option", argv: []string{"env", "env", "-0", "go"}, goos: "linux", want: "unsupported env short option"},
		{name: "env recognized short options", argv: []string{"env", "-i", "-uPATH", "-C", "/tmp", "-P/custom", "-a", "alias", "go"}, goos: "linux"},
		{name: "env long option after assignment is executable", argv: []string{"env", "FOO=bar", "--ch=/tmp"}, goos: "linux"},
		{name: "env short option after assignment is executable", argv: []string{"env", "FOO=bar", "-0"}, goos: "linux"},
		{name: "env short option after separator is executable", argv: []string{"env", "--", "-0"}, goos: "linux"},
		{name: "env long option after separator is executable", argv: []string{"env", "--", "--ch=/tmp"}, goos: "linux"},
		{name: "absolute env.exe argv0 wrapper before shell", argv: []string{`C:\Program Files\Git\usr\bin\env.exe`, "--argv0", "alias", "bash", "-c", "pytest"}, goos: "windows", want: "shell command-string"},
		{name: "nested env.exe shell wrapper", argv: []string{"env.exe", "env.exe", "bash.exe", "-c", "pytest"}, goos: "windows", want: "shell command-string"},
		{name: "env -a argument before shell", argv: []string{"env", "-a", "alias", "bash", "-c", "pytest"}, goos: "darwin", want: "shell command-string"},
		{name: "native bash script argument resembles option", argv: []string{"bash", "script.sh", "-c"}, goos: "darwin"},
		{name: "cmd keep-open command string", argv: []string{"cmd.exe", "/k", "pytest"}, goos: "windows", want: "shell command-string"},
		{name: "PowerShell encoded command", argv: []string{"pwsh", "-EncodedCommand", "payload"}, goos: "windows", want: "shell command-string"},
		{name: "PowerShell command after execution policy value", argv: []string{"powershell.exe", "-ExecutionPolicy", "Bypass", "-Command", "Write-Output test"}, goos: "windows", want: "shell command-string"},
		{name: "env.exe PowerShell command after execution policy value", argv: []string{"env.exe", "powershell.exe", "-ExecutionPolicy", "Bypass", "-Command", "Write-Output test"}, goos: "windows", want: "shell command-string"},
		{name: "pwsh command after input format value", argv: []string{"pwsh", "-InputFormat", "XML", "-Command", "Write-Output test"}, goos: "windows", want: "shell command-string"},
		{name: "PowerShell command after encoded arguments value", argv: []string{"powershell.exe", "-EncodedArguments", "payload", "-Command", "Write-Output test"}, goos: "windows", want: "shell command-string"},
		{name: "PowerShell script argument resembles command option", argv: []string{"powershell.exe", "-File", "scripts/check.ps1", "-Command"}, goos: "windows"},
		{name: "direct Python command", argv: []string{"python", "-m", "pytest"}, goos: "windows"},
		{name: "direct POSIX script", argv: []string{"./scripts/check.sh"}, goos: "linux"},
		{name: "batch script requires shell", argv: []string{"check.bat"}, goos: "windows", want: "Windows command shell"},
		{name: "PowerShell script requires interpreter", argv: []string{"check.ps1"}, goos: "windows", want: "PowerShell runner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := commandPortabilityIssue(tc.argv, tc.goos)
			if tc.want == "" && got != "" {
				t.Fatalf("unexpected portability finding: %s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("finding=%q, want substring %q", got, tc.want)
			}
		})
	}
}
