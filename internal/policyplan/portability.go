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
	if usesEnvSplitString(argv) {
		return "env split-string execution cannot be classified safely"
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
		index, splitString := envCommandIndex(argv)
		if splitString || index >= len(argv) {
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

func usesEnvSplitString(argv []string) bool {
	for len(argv) > 0 && isEnvExecutable(argv[0]) {
		index, splitString := envCommandIndex(argv)
		if splitString {
			return true
		}
		if index >= len(argv) {
			return false
		}
		argv = argv[index:]
	}
	return false
}

func isEnvExecutable(value string) bool {
	return strings.TrimSuffix(executableBase(value), ".exe") == "env"
}

func envCommandIndex(argv []string) (int, bool) {
	for index := 1; index < len(argv); index++ {
		arg := argv[index]
		switch {
		case arg == "--":
			return index + 1, false
		case arg == "-S", arg == "--split-string", strings.HasPrefix(arg, "--split-string="):
			return len(argv), true
		case arg == "--argv0", arg == "--unset", arg == "--chdir":
			index++
		case strings.HasPrefix(arg, "--argv0="), strings.HasPrefix(arg, "--unset="), strings.HasPrefix(arg, "--chdir="):
			continue
		case strings.HasPrefix(arg, "-") && arg != "-":
			for optionIndex := 1; optionIndex < len(arg); optionIndex++ {
				switch arg[optionIndex] {
				case 'S':
					return len(argv), true
				case 'u', 'C', 'a':
					if optionIndex == len(arg)-1 {
						index++
					}
					optionIndex = len(arg)
				}
			}
		case strings.Contains(arg, "="):
			continue
		default:
			return index, false
		}
	}
	return len(argv), false
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
