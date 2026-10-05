package commandexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MarcosAlves90/polis/v6/spec"
)

const MaxRetainedOutputBytes = 1 << 20

type Observation struct {
	Status          spec.Status
	ExitCode        int
	DurationMS      int64
	Stdout          string
	Stderr          string
	StdoutBytes     int64
	StderrBytes     int64
	StdoutSHA256    string
	StderrSHA256    string
	StdoutTruncated bool
	StderrTruncated bool
	Prerequisite    string
}

var (
	missingCommand = regexp.MustCompile(`^(?:(?:/bin/)?(?:sh|bash|zsh): (?:(?:line )?\d+: )?)([^:\s]+): (?:command not found|not found)$`)
	missingModule  = regexp.MustCompile(`^(?:ModuleNotFoundError: No module named |\S*python\S*: No module named |(?:Error: )?Cannot find (?:module|package) )['"]?([^'"\s]+)`)
	missingEnv     = regexp.MustCompile(`^(?:(?:/bin/)?(?:sh|bash|zsh): (?:(?:line )?\d+: )?)?([^:\s]+): (?:parameter not set|unbound variable)$`)
)

type boundedDigestWriter struct {
	h         hash.Hash
	buf       []byte
	total     int64
	truncated bool
}

func newBoundedDigestWriter() *boundedDigestWriter {
	return &boundedDigestWriter{h: sha256.New(), buf: make([]byte, 0, 4096)}
}

func (w *boundedDigestWriter) Write(p []byte) (int, error) {
	_, _ = w.h.Write(p)
	w.total += int64(len(p))
	remaining := MaxRetainedOutputBytes - len(w.buf)
	if remaining > 0 {
		take := len(p)
		if take > remaining {
			take = remaining
		}
		w.buf = append(w.buf, p[:take]...)
	}
	if w.total > MaxRetainedOutputBytes {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedDigestWriter) string() string { return string(w.buf) }
func (w *boundedDigestWriter) digest() string { return hex.EncodeToString(w.h.Sum(nil)) }

func Run(repoRoot string, command spec.CommandSpec) Observation {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...)
	cmd.Dir = filepath.Join(repoRoot, filepath.FromSlash(command.Cwd))
	stdout, stderr := newBoundedDigestWriter(), newBoundedDigestWriter()
	if command.Environment != nil {
		childEnvironment, err := environmentFor(*command.Environment)
		if err != nil {
			return Observation{
				Status:       spec.StatusBlocked,
				ExitCode:     -1,
				StdoutSHA256: stdout.digest(),
				StderrSHA256: stderr.digest(),
				Prerequisite: "invalid clean environment: " + err.Error(),
			}
		}
		cmd.Env = childEnvironment
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	started := time.Now()
	err := cmd.Run()
	obs := Observation{
		Status: spec.StatusPass, ExitCode: 0, DurationMS: time.Since(started).Milliseconds(),
		Stdout: stdout.string(), Stderr: stderr.string(), StdoutBytes: stdout.total, StderrBytes: stderr.total,
		StdoutSHA256: stdout.digest(), StderrSHA256: stderr.digest(), StdoutTruncated: stdout.truncated, StderrTruncated: stderr.truncated,
	}
	if ctx.Err() == context.DeadlineExceeded {
		obs.Status = spec.StatusFail
		obs.ExitCode = -1
		return obs
	}
	if err == nil {
		return obs
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		obs.ExitCode = exitErr.ExitCode()
		if prerequisite := missingPrerequisite(obs); prerequisite != "" {
			obs.Status = spec.StatusBlocked
			obs.Prerequisite = prerequisite
		} else {
			obs.Status = spec.StatusFail
		}
		return obs
	}
	obs.Status = spec.StatusBlocked
	obs.ExitCode = -1
	if errors.Is(err, exec.ErrNotFound) {
		obs.Prerequisite = "missing executable " + command.Argv[0]
	} else if errors.Is(err, os.ErrNotExist) {
		obs.Prerequisite = "missing working directory or executable for " + command.Argv[0]
	} else {
		obs.Prerequisite = "command could not start: " + err.Error()
	}
	message := err.Error()
	if obs.Stderr != "" {
		message = "\n" + message
	}
	_, _ = stderr.Write([]byte(message))
	obs.Stderr = stderr.string()
	obs.StderrBytes = stderr.total
	obs.StderrSHA256 = stderr.digest()
	obs.StderrTruncated = stderr.truncated
	return obs
}

func missingPrerequisite(obs Observation) string {
	// A failed command can use any exit code, including 127, for an assertion.
	// Classify only recognizable startup diagnostics before any stdout is emitted.
	if obs.StdoutBytes != 0 || obs.StderrTruncated {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(obs.Stderr), "\n")
	if len(lines) == 0 {
		return ""
	}
	first := strings.TrimSpace(lines[0])
	if obs.ExitCode == 127 {
		if match := missingCommand.FindStringSubmatch(first); match != nil {
			return "missing executable " + match[1]
		}
	}
	if len(lines) == 1 {
		if match := missingModule.FindStringSubmatch(first); match != nil {
			return "missing dependency " + match[1]
		}
		if match := missingEnv.FindStringSubmatch(first); match != nil {
			return "missing environment condition " + match[1]
		}
	}
	return ""
}

func BlockedReason(obs Observation) *string {
	if obs.Status != spec.StatusBlocked {
		return nil
	}
	reason := "intended checks did not run"
	if obs.Prerequisite != "" {
		reason = fmt.Sprintf("%s; %s", obs.Prerequisite, reason)
	}
	return &reason
}

var windowsBootstrapEnvironment = []string{
	"SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "ProgramFiles",
	"ProgramFiles(x86)", "ProgramW6432", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "HOMEDRIVE", "HOMEPATH",
}

type environmentEntry struct {
	value string
}

func environmentFor(environment spec.EnvironmentSpec) ([]string, error) {
	return environmentForParent(environment, os.Environ(), runtime.GOOS)
}

func environmentForParent(environment spec.EnvironmentSpec, parent []string, goos string) ([]string, error) {
	if environment.Mode == spec.EnvironmentModeInherit {
		return append([]string(nil), parent...), nil
	}
	if environment.Mode != spec.EnvironmentModeClean {
		return nil, fmt.Errorf("unsupported environment mode %q", environment.Mode)
	}

	windows := goos == "windows"
	selectedNames := make(map[string]string, len(environment.Pass)+len(windowsBootstrapEnvironment))
	add := func(name string) {
		key := environmentKey(name, windows)
		if _, alreadySelected := selectedNames[key]; !alreadySelected {
			selectedNames[key] = name
		}
	}
	if windows {
		for _, name := range windowsBootstrapEnvironment {
			add(name)
		}
	}
	for _, name := range environment.Pass {
		add(name)
	}

	parentByName := make(map[string]environmentEntry, len(selectedNames))
	for _, value := range parent {
		name, variableValue, ok := strings.Cut(value, "=")
		if !ok || name == "" {
			continue
		}
		key := environmentKey(name, windows)
		if _, selected := selectedNames[key]; !selected {
			continue
		}
		if existing, found := parentByName[key]; found {
			if existing.value != variableValue {
				return nil, fmt.Errorf("conflicting values for environment variable %q", selectedNames[key])
			}
			continue
		}
		parentByName[key] = environmentEntry{value: variableValue}
	}

	keys := make([]string, 0, len(parentByName))
	for key := range parentByName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		entry := parentByName[key]
		result = append(result, selectedNames[key]+"="+entry.value)
	}
	return result, nil
}

func environmentKey(name string, windows bool) string {
	if windows {
		return strings.ToUpper(name)
	}
	return name
}
