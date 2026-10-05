package commandexec

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestCleanEnvironmentPassesOnlyAllowlistedValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	t.Setenv("POLIS_SECRET_TEST", "must-not-leak")
	t.Setenv("POLIS_ALLOWED_TEST", "visible")
	cmd := spec.CommandSpec{Argv: []string{"sh", "-c", `printf '%s|%s' "$POLIS_ALLOWED_TEST" "$POLIS_SECRET_TEST"`}, Cwd: ".", TimeoutSeconds: 5, Environment: &spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"PATH", "POLIS_ALLOWED_TEST"}}}
	obs := Run(t.TempDir(), cmd)
	if obs.Status != spec.StatusPass {
		t.Fatalf("run failed: %+v", obs)
	}
	if strings.TrimSpace(obs.Stdout) != "visible|" {
		t.Fatalf("unexpected child environment: %q", obs.Stdout)
	}
	if os.Getenv("POLIS_SECRET_TEST") != "must-not-leak" {
		t.Fatal("parent environment changed")
	}
}

func TestCleanEnvironmentKeepsExplicitValuesAndDropsArbitraryValues(t *testing.T) {
	t.Setenv("POLIS_ALLOWED_VALUE_TEST", "visible")
	t.Setenv("POLIS_UNLISTED_VALUE_TEST", "must-not-leak")

	got, err := environmentFor(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"POLIS_ALLOWED_VALUE_TEST"}})
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := environmentValue(got, "POLIS_ALLOWED_VALUE_TEST", false); !ok || value != "visible" {
		t.Fatalf("explicitly allowed value missing: %v", got)
	}
	if _, ok := environmentValue(got, "POLIS_UNLISTED_VALUE_TEST", false); ok {
		t.Fatalf("arbitrary parent value leaked: %v", got)
	}
}

func TestCleanEnvironmentPreservesWindowsBootstrapVariables(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process environment")
	}
	variables := []string{
		"SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "ProgramFiles",
		"ProgramFiles(x86)", "ProgramW6432", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "HOMEDRIVE", "HOMEPATH",
	}
	for _, name := range variables {
		t.Setenv(name, "parent-value:"+name)
	}

	got, err := environmentFor(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range variables {
		if value, ok := environmentValue(got, name, true); !ok || value != "parent-value:"+name {
			t.Errorf("Windows bootstrap variable %q = %q, present=%t; child=%v", name, value, ok, got)
		}
	}
}

func TestCleanEnvironmentDoesNotSynthesizeAbsentWindowsBootstrapVariable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process environment")
	}
	const name = "ProgramW6432"
	original, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, original)
		}
	})

	got, err := environmentFor(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := environmentValue(got, name, true); ok {
		t.Fatalf("absent parent variable %q was synthesized: %v", name, got)
	}
}

func TestCleanEnvironmentUsesWindowsCaseInsensitiveNamesOnce(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process environment")
	}
	t.Setenv("POLIS_CASE_INSENSITIVE_TEST", "visible")

	got, err := environmentFor(spec.EnvironmentSpec{
		Mode: spec.EnvironmentModeClean,
		Pass: []string{"polis_case_insensitive_test", "POLIS_CASE_INSENSITIVE_TEST"},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range got {
		name, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, "POLIS_CASE_INSENSITIVE_TEST") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("case-insensitive variable emitted %d times: %v", count, got)
	}
}

func TestEnvironmentForParentBuildsWindowsCleanEnvironment(t *testing.T) {
	variables := []string{
		"SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "ProgramFiles",
		"ProgramFiles(x86)", "ProgramW6432", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "HOMEDRIVE", "HOMEPATH",
	}
	parent := make([]string, 0, len(variables)+2)
	for _, name := range variables {
		parent = append(parent, strings.ToLower(name)+"=parent-value:"+name)
	}
	parent = append(parent, "custom_allowed=visible", "POLIS_SECRET_TEST=must-not-leak")

	got, err := environmentForParent(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"CUSTOM_ALLOWED"}}, parent, "windows")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range variables {
		if value, ok := environmentValue(got, name, true); !ok || value != "parent-value:"+name {
			t.Errorf("Windows bootstrap variable %q = %q, present=%t; child=%v", name, value, ok, got)
		}
	}
	if value, ok := environmentValue(got, "custom_allowed", true); !ok || value != "visible" {
		t.Errorf("explicitly allowed variable = %q, present=%t; child=%v", value, ok, got)
	}
	if _, ok := environmentValue(got, "POLIS_SECRET_TEST", true); ok {
		t.Fatalf("arbitrary parent variable leaked: %v", got)
	}
}

func TestEnvironmentForParentDoesNotSynthesizeWindowsVariables(t *testing.T) {
	got, err := environmentForParent(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}, []string{"SystemRoot=C:\\Windows"}, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SystemRoot=C:\\Windows" {
		t.Fatalf("clean Windows environment synthesized or lost values: %v", got)
	}
}

func TestEnvironmentForParentDeduplicatesWindowsAllowlistCaseInsensitively(t *testing.T) {
	got, err := environmentForParent(spec.EnvironmentSpec{
		Mode: spec.EnvironmentModeClean,
		Pass: []string{"polis_test_value", "POLIS_TEST_VALUE"},
	}, []string{"POLIS_TEST_VALUE=visible"}, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.EqualFold(got[0], "POLIS_TEST_VALUE=visible") {
		t.Fatalf("case-insensitive allowlist was not deduplicated: %v", got)
	}
}

func TestEnvironmentForParentKeepsPOSIXNamesCaseSensitive(t *testing.T) {
	got, err := environmentForParent(spec.EnvironmentSpec{
		Mode: spec.EnvironmentModeClean,
		Pass: []string{"path"},
	}, []string{"PATH=upper", "path=lower", "SystemRoot=not-a-posix-bootstrap"}, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "path=lower" {
		t.Fatalf("POSIX environment was not case-sensitive and allowlisted: %v", got)
	}
}

func TestEnvironmentForParentRejectsConflictingSelectedWindowsNamesOnly(t *testing.T) {
	_, err := environmentForParent(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean, Pass: []string{"PATH"}}, []string{
		"PATH=first", "Path=second", "SECRET=one", "secret=two",
	}, "windows")
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("selected case-colliding values were accepted: %v", err)
	}

	got, err := environmentForParent(spec.EnvironmentSpec{Mode: spec.EnvironmentModeClean}, []string{"SECRET=one", "secret=two"}, "windows")
	if err != nil || len(got) != 0 {
		t.Fatalf("unselected environment collision affected clean mode: values=%v err=%v", got, err)
	}
}

func environmentValue(entries []string, name string, caseInsensitive bool) (string, bool) {
	for _, entry := range entries {
		entryName, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		matches := entryName == name
		if caseInsensitive {
			matches = strings.EqualFold(entryName, name)
		}
		if matches {
			return value, true
		}
	}
	return "", false
}
