package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorLookPathWindowsEnvDelegateUsesGateCWDAndPATHEXT(t *testing.T) {
	repo := t.TempDir()
	gateDir := filepath.Join(repo, "gate")
	binDir := filepath.Join(gateDir, "bin")
	otherDir := filepath.Join(repo, "other")
	for _, dir := range []string{binDir, otherDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	program := "polis-doctor-windows-delegate"
	gateExecutable := filepath.Join(binDir, program+".EXE")
	otherExecutable := filepath.Join(otherDir, program+".EXE")
	for _, path := range []string{gateExecutable, otherExecutable} {
		if err := os.WriteFile(path, []byte("fixture, never executed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATHEXT", ".EXE;.CMD")
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "1")
	t.Setenv("PATH", "bin"+string(os.PathListSeparator)+otherDir)

	got, err := doctorLookPath(program, gateDir, true)
	if err != nil || !strings.EqualFold(got, gateExecutable) {
		t.Fatalf("env delegate lookup = %q, %v; want %q", got, err, gateExecutable)
	}
	if err := os.Remove(gateExecutable); err != nil {
		t.Fatal(err)
	}
	got, err = doctorLookPath(program, gateDir, true)
	if err != nil || !strings.EqualFold(got, otherExecutable) {
		t.Fatalf("env delegate fallback = %q, %v; want %q", got, err, otherExecutable)
	}
	if err := os.Remove(otherExecutable); err != nil {
		t.Fatal(err)
	}
	if got, err := doctorLookPath(program, gateDir, true); err == nil {
		t.Fatalf("missing env delegate unexpectedly resolved to %q", got)
	}
}

func TestDoctorLookPathWindowsImplicitCWDDoesNotShadowPATH(t *testing.T) {
	repo := t.TempDir()
	gateDir := filepath.Join(repo, "gate")
	otherDir := filepath.Join(repo, "other")
	for _, dir := range []string{gateDir, otherDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	program := "polis-doctor-windows-cwd"
	gateExecutable := filepath.Join(gateDir, program+".exe")
	otherExecutable := filepath.Join(otherDir, program+".exe")
	for _, path := range []string{gateExecutable, otherExecutable} {
		if err := os.WriteFile(path, []byte("fixture, never executed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATHEXT", ".EXE")
	t.Setenv("PATH", otherDir)
	// Test the default Windows implicit-current-directory rule even when the
	// test runner has opted out of it in its inherited environment.
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "1")
	if err := os.Unsetenv("NoDefaultCurrentDirectoryInExePath"); err != nil {
		t.Fatal(err)
	}
	if got, err := doctorLookPath(program, gateDir, true); !errors.Is(err, exec.ErrDot) {
		t.Fatalf("implicit CWD shadow returned %q, %v; want ErrDot", got, err)
	}
	// An explicit PATH entry for the same file permits that resolution.
	t.Setenv("PATH", "."+string(os.PathListSeparator)+otherDir)
	if got, err := doctorLookPath(program, gateDir, true); err != nil || !strings.EqualFold(got, gateExecutable) {
		t.Fatalf("explicit CWD path returned %q, %v; want %q", got, err, gateExecutable)
	}
	// Disabling implicit CWD search allows the PATH executable to win.
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "1")
	t.Setenv("PATH", otherDir)
	if got, err := doctorLookPath(program, gateDir, true); err != nil || !strings.EqualFold(got, otherExecutable) {
		t.Fatalf("disabled implicit CWD returned %q, %v; want %q", got, err, otherExecutable)
	}
}
