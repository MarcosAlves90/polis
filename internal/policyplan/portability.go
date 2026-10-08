package policyplan

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/MarcosAlves90/polis/v6/spec"
)

type commandPlatform string

const (
	posixCommandPlatform   commandPlatform = "posix"
	windowsCommandPlatform commandPlatform = "windows"
)

var shellExecutables = map[string]commandPlatform{
	"sh":             posixCommandPlatform,
	"bash":           posixCommandPlatform,
	"dash":           posixCommandPlatform,
	"ash":            posixCommandPlatform,
	"zsh":            posixCommandPlatform,
	"ksh":            posixCommandPlatform,
	"fish":           posixCommandPlatform,
	"sh.exe":         posixCommandPlatform,
	"bash.exe":       posixCommandPlatform,
	"dash.exe":       posixCommandPlatform,
	"ash.exe":        posixCommandPlatform,
	"zsh.exe":        posixCommandPlatform,
	"ksh.exe":        posixCommandPlatform,
	"fish.exe":       posixCommandPlatform,
	"cmd":            windowsCommandPlatform,
	"cmd.exe":        windowsCommandPlatform,
	"powershell":     windowsCommandPlatform,
	"powershell.exe": windowsCommandPlatform,
	"pwsh":           windowsCommandPlatform,
	"pwsh.exe":       windowsCommandPlatform,
}

func validateCommandPortability(policy spec.Policy) error {
	for _, gate := range policy.Gates {
		if gate.Command == nil || len(gate.Command.Argv) == 0 {
			continue
		}
		if reason := commandPortabilityIssue(gate.Command.Argv, runtime.GOOS); reason != "" {
			return fmt.Errorf("gate %q command is not portable: %s; use direct argv or a versioned cross-platform project runner", gate.ID, reason)
		}
	}
	return nil
}

func commandPortabilityIssue(argv []string, goos string) string {
	if issue := envWrapperIssue(argv); issue != "" {
		return issue
	}
	name, shellArgs := shellExecutable(argv)
	if name != "" {
		platform := shellExecutables[name]
		if usesShellCommandString(name, shellArgs) {
			return fmt.Sprintf("executable %q uses shell command-string execution", name)
		}
		if platform == posixCommandPlatform && goos == "windows" {
			return fmt.Sprintf("executable %q requires a POSIX shell on Windows", name)
		}
		if platform == windowsCommandPlatform && goos != "windows" {
			return fmt.Sprintf("executable %q requires a Windows shell on %s", name, goos)
		}
	}

	executable := executableBase(argv[0])
	switch {
	case strings.HasSuffix(executable, ".sh") && goos == "windows":
		return fmt.Sprintf("script %q requires a POSIX runtime on Windows", executable)
	case strings.HasSuffix(executable, ".bat"), strings.HasSuffix(executable, ".cmd"):
		return fmt.Sprintf("script %q requires the Windows command shell", executable)
	case strings.HasSuffix(executable, ".ps1"):
		return fmt.Sprintf("script %q requires an explicit PowerShell runner", executable)
	default:
		return ""
	}
}

func shellExecutable(argv []string) (string, []string) {
	for len(argv) > 0 && isEnvExecutable(argv[0]) {
		index, issue := envCommandIndex(argv)
		if issue != "" || index >= len(argv) {
			return "", nil
		}
		argv = argv[index:]
	}
	if len(argv) == 0 {
		return "", nil
	}
	name := executableBase(argv[0])
	if _, ok := shellExecutables[name]; ok {
		return name, argv[1:]
	}
	return "", nil
}

// PreflightExecutables returns the executable chain for direct argv and env
// wrappers. It uses the same wrapper parsing as command portability validation.
// pathMayChange and cwdMayChange indicate that delegated relative commands
// cannot necessarily be resolved using the doctor's PATH or working directory.
func PreflightExecutables(argv []string) (executables []string, pathMayChange, cwdMayChange bool) {
	for len(argv) > 0 && isEnvExecutable(argv[0]) {
		executables = append(executables, argv[0])
		index, issue := envCommandIndex(argv)
		if issue != "" || index >= len(argv) {
			return executables, pathMayChange, cwdMayChange
		}
		pathChanged, cwdChanged := envLookupChanges(argv[1:index])
		pathMayChange = pathMayChange || pathChanged
		cwdMayChange = cwdMayChange || cwdChanged
		argv = argv[index:]
	}
	if len(argv) > 0 {
		executables = append(executables, argv[0])
	}
	return executables, pathMayChange, cwdMayChange
}

func envLookupChanges(args []string) (pathChanged, cwdChanged bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-", arg == "--ignore-environment", arg == "--path", strings.HasPrefix(arg, "--path="):
			pathChanged = true
		case arg == "--chdir", strings.HasPrefix(arg, "--chdir="):
			cwdChanged = true
		case strings.Contains(arg, "=") && isEnvPATHName(strings.SplitN(arg, "=", 2)[0]):
			pathChanged = true
		case arg == "--unset":
			if i+1 < len(args) && isEnvPATHName(args[i+1]) {
				pathChanged = true
			}
			i++
		case strings.HasPrefix(arg, "--unset="):
			if isEnvPATHName(strings.TrimPrefix(arg, "--unset=")) {
				pathChanged = true
			}
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			for j := 1; j < len(arg); j++ {
				switch arg[j] {
				case 'i', 'P':
					pathChanged = true
					if arg[j] == 'P' {
						j = len(arg)
					}
				case 'C':
					cwdChanged = true
					j = len(arg)
				case 'u':
					if j+1 < len(arg) {
						if isEnvPATHName(arg[j+1:]) {
							pathChanged = true
						}
					} else if i+1 < len(args) && isEnvPATHName(args[i+1]) {
						pathChanged = true
					}
					j = len(arg)
				case 'a':
					j = len(arg)
				}
			}
		}
	}
	return pathChanged, cwdChanged
}

func isEnvPATHName(name string) bool {
	return name == "PATH" || (runtime.GOOS == "windows" && strings.EqualFold(name, "PATH"))
}

func envWrapperIssue(argv []string) string {
	for len(argv) > 0 && isEnvExecutable(argv[0]) {
		index, issue := envCommandIndex(argv)
		if issue != "" {
			return issue
		}
		if index >= len(argv) {
			return ""
		}
		argv = argv[index:]
	}
	return ""
}

func isEnvExecutable(value string) bool {
	return strings.TrimSuffix(executableBase(value), ".exe") == "env"
}

// envCommandIndex only accepts explicitly understood long options. GNU env
// accepts abbreviated long options, but their meaning depends on the env
// implementation and can change PATH/CWD without a safe static lookup.
func envCommandIndex(argv []string) (int, string) {
	assignmentsStarted := false
	for index := 1; index < len(argv); index++ {
		arg := argv[index]
		// env accepts options before NAME=VALUE operands only. Once an
		// assignment is seen, the first non-assignment starts the command.
		if assignmentsStarted && (!strings.Contains(arg, "=") || strings.HasPrefix(arg, "-")) {
			return index, ""
		}
		switch {
		case arg == "--":
			return index + 1, ""
		case arg == "-", arg == "--ignore-environment": // env's environment-clearing options
			continue
		case arg == "-S", arg == "--split-string", strings.HasPrefix(arg, "--split-string="):
			return len(argv), "env split-string execution cannot be classified safely"
		case arg == "--argv0", arg == "--unset", arg == "--chdir", arg == "--path":
			index++
		case strings.HasPrefix(arg, "--argv0="), strings.HasPrefix(arg, "--unset="), strings.HasPrefix(arg, "--chdir="), strings.HasPrefix(arg, "--path="):
			continue
		case strings.HasPrefix(arg, "--"):
			return len(argv), fmt.Sprintf("unsupported env long option %q cannot be classified safely", arg)
		case strings.HasPrefix(arg, "-") && arg != "-":
			for optionIndex := 1; optionIndex < len(arg); optionIndex++ {
				switch arg[optionIndex] {
				case 'S':
					return len(argv), "env split-string execution cannot be classified safely"
				case 'u', 'C', 'P', 'a':
					if optionIndex == len(arg)-1 {
						index++
					}
					optionIndex = len(arg)
				}
			}
		case strings.Contains(arg, "="):
			assignmentsStarted = true
			continue
		default:
			return index, ""
		}
	}
	return len(argv), ""
}

func usesShellCommandString(shell string, args []string) bool {
	shell = strings.TrimSuffix(shell, ".exe")
	switch shell {
	case "sh", "bash", "dash", "ash", "zsh", "ksh", "fish":
		return usesPOSIXShellCommandString(args)
	case "cmd", "cmd.exe":
		for _, arg := range args {
			lower := strings.ToLower(arg)
			switch {
			case lower == "/c", lower == "/k":
				return true
			case lower == "/d", lower == "/q", lower == "/a", lower == "/u", lower == "/s", strings.HasPrefix(lower, "/e:"), strings.HasPrefix(lower, "/f:"), strings.HasPrefix(lower, "/v:"):
				continue
			default:
				return false
			}
		}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return usesPowerShellCommandString(shell, args)
	}
	return false
}

func usesPowerShellCommandString(shell string, args []string) bool {
	shell = strings.TrimSuffix(shell, ".exe")
	for index := 0; index < len(args); index++ {
		arg := strings.ToLower(args[index])
		switch arg {
		case "-command", "-commandwithargs", "-cwa", "-c", "-encodedcommand", "-e", "-ec", "-enc":
			return true
		case "-file", "-f", "--":
			return false
		case "-executionpolicy", "-ex", "-ep", "-inputformat", "-inp", "-if", "-outputformat", "-of", "-o", "-windowstyle", "-w", "-configurationname", "-config", "-custompipename", "-workingdirectory", "-wd", "-settingsfile", "-settings":
			index++
		case "-i":
			if shell == "pwsh" {
				continue
			}
			index++
		case "-version":
			if shell == "powershell" {
				index++
			}
		case "-psconsolefile", "-encodedarguments":
			if shell == "powershell" {
				index++
			}
		case "-configurationfile":
			if shell == "pwsh" {
				index++
			}
		default:
			if !strings.HasPrefix(arg, "-") {
				return false
			}
		}
	}
	return false
}

func usesPOSIXShellCommandString(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			return false
		}
		if arg == "--command" || strings.HasPrefix(arg, "--command=") {
			return true
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return false
		}
		if strings.HasPrefix(arg, "--") {
			if arg == "--rcfile" || arg == "--init-file" {
				index++
			}
			continue
		}
		options := arg[1:]
		for optionIndex, option := range options {
			if option == 'c' {
				return true
			}
			if option == 'o' || option == 'O' {
				if optionIndex == len(options)-1 {
					index++
				}
				break
			}
		}
	}
	return false
}

func executableBase(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	return strings.ToLower(value)
}
