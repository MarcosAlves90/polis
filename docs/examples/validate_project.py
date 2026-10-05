"""Example project runner; copy to <project>/scripts/validate_project.py."""

from importlib.util import find_spec
from pathlib import Path
import shutil
import subprocess
import sys
from typing import Sequence


def run_command(argv: Sequence[str], cwd: Path, executable_name: str) -> int:
    if not cwd.is_dir():
        print(f"missing validation directory: {cwd}", file=sys.stderr)
        return 1
    try:
        return subprocess.run(list(argv), cwd=cwd, check=False).returncode
    except FileNotFoundError:
        print(f"missing required executable: {executable_name}", file=sys.stderr)
        return 127


def run_python_tests(backend_dir: Path) -> int:
    if find_spec("pytest") is None:
        print(f"missing required Python module: pytest (interpreter: {sys.executable})", file=sys.stderr)
        return 1
    return run_command([sys.executable, "-m", "pytest"], backend_dir, sys.executable)


def run_flutter_tests(frontend_dir: Path) -> int:
    if shutil.which("flutter") is None:
        print("missing required executable: flutter", file=sys.stderr)
        return 127
    return run_command(["flutter", "test"], frontend_dir, "flutter")


def validate_project(project_root: Path) -> int:
    result = run_python_tests(project_root / "backend")
    if result != 0:
        return result
    return run_flutter_tests(project_root / "frontend")


def main() -> int:
    # This example is intended to be copied to <project>/scripts/.
    project_root = Path(__file__).resolve().parent.parent
    return validate_project(project_root)


if __name__ == "__main__":
    raise SystemExit(main())
