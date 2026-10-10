package examples

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExampleRunnerUsesTheActiveVirtualenvInterpreter(t *testing.T) {
	python := findPython(t)
	root := t.TempDir()
	venv := filepath.Join(root, "venv")
	createVenv := exec.Command(python, "-m", "venv", "--without-pip", venv)
	if output, err := createVenv.CombinedOutput(); err != nil {
		t.Fatalf("create venv: %v\n%s", err, output)
	}
	venvPython := virtualenvPython(venv)
	backend := filepath.Join(root, "backend")
	modules := filepath.Join(root, "modules")
	if err := os.MkdirAll(backend, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	const fakePytest = "import os, sys\nopen(os.environ['POLIS_VENV_EXECUTABLE'], 'w').write(sys.executable)\nsys.exit(13)\n"
	if err := os.WriteFile(filepath.Join(modules, "pytest.py"), []byte(fakePytest), 0o600); err != nil {
		t.Fatal(err)
	}
	interpreterRecord := filepath.Join(root, "interpreter.txt")

	runner := exampleRunnerPath(t)
	code := "import importlib.util, pathlib, sys; p=pathlib.Path(sys.argv[1]); s=importlib.util.spec_from_file_location('validate_project', p); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); raise SystemExit(m.run_python_tests(pathlib.Path(sys.argv[2])))"
	cmd := exec.Command(venvPython, "-c", code, runner, backend)
	cmd.Env = withEnvironment(os.Environ(), "PYTHONPATH", modules)
	cmd.Env = withEnvironment(cmd.Env, "POLIS_VENV_EXECUTABLE", interpreterRecord)
	cmd.Env = withEnvironment(cmd.Env, "PYTHONDONTWRITEBYTECODE", "1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errorsAs(err, &exitErr) || exitErr.ExitCode() != 13 {
		t.Fatalf("runner did not preserve pytest exit code 13: err=%v output=%s", err, output)
	}
	usedInterpreter, err := os.ReadFile(interpreterRecord)
	if err != nil {
		t.Fatalf("read selected interpreter: %v; output=%s", err, output)
	}
	if !samePath(string(usedInterpreter), venvPython) {
		t.Fatalf("runner used %q, want active virtualenv interpreter %q", usedInterpreter, venvPython)
	}
}

func TestExampleRunnerFailsExplicitlyWhenFlutterIsMissing(t *testing.T) {
	python := findPython(t)
	root := t.TempDir()
	frontend := filepath.Join(root, "frontend")
	if err := os.MkdirAll(frontend, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyPath := filepath.Join(root, "empty-path")
	if err := os.MkdirAll(emptyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := exampleRunnerPath(t)
	code := "import importlib.util, pathlib, sys; p=pathlib.Path(sys.argv[1]); s=importlib.util.spec_from_file_location('validate_project', p); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); raise SystemExit(m.run_flutter_tests(pathlib.Path(sys.argv[2])))"
	cmd := exec.Command(python, "-c", code, runner, frontend)
	cmd.Env = withEnvironment(os.Environ(), "PATH", emptyPath)
	cmd.Env = withEnvironment(cmd.Env, "PYTHONDONTWRITEBYTECODE", "1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errorsAs(err, &exitErr) || exitErr.ExitCode() != 127 || !strings.Contains(string(output), "missing required executable: flutter") {
		t.Fatalf("missing Flutter did not fail explicitly: err=%v output=%s", err, output)
	}
}

func findPython(t *testing.T) string {
	t.Helper()
	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "python3"}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("Python interpreter is unavailable; Python runner example integration tests skipped")
	return ""
}

func virtualenvPython(venv string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venv, "Scripts", "python.exe")
	}
	return filepath.Join(venv, "bin", "python")
}

func exampleRunnerPath(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve example test source path")
	}
	return filepath.Join(filepath.Dir(source), "validate_project.py")
}

func withEnvironment(environment []string, name, value string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, name) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, fmt.Sprintf("%s=%s", name, value))
}

func samePath(got, want string) bool {
	got, want = filepath.Clean(got), filepath.Clean(want)
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(got, want)
	}
	return got == want
}

func errorsAs(err error, target **exec.ExitError) bool {
	return err != nil && errors.As(err, target)
}
