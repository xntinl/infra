# 15. Python Project Justfile

<!--
difficulty: intermediate
concepts:
  - virtual environment management
  - uv vs pip detection
  - pytest with markers
  - coverage reports
  - ruff linting
  - mypy type checking
  - wheel building
  - publish recipe
  - dotenv loading
  - conditional expressions
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - Python project structure familiarity
  - pip and virtual environments
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| python | >= 3.11 | `python3 --version` |
| uv | >= 0.4 (optional) | `uv --version` |

## Learning Objectives

- **Apply** conditional expressions to detect `uv` vs `pip` and adjust recipes accordingly
- **Implement** a complete Python development workflow covering virtual environments, testing with markers, linting, type checking, and package building
- **Design** recipes that work across different Python tooling setups without requiring manual configuration

## Why Python Project Justfiles

Python's tooling ecosystem is famously fragmented. A project may use pip, pip-tools, poetry, pdm, or uv for dependency management. Testing might involve pytest, unittest, or nose. Linting could be flake8, pylint, ruff, or some combination. Every team reinvents the same Makefile or shell script to glue these tools together.

A justfile normalizes this complexity. By detecting which tools are available at evaluation time, the justfile adapts to the developer's setup. Someone using `uv` gets fast installs; someone with only `pip` still has a working workflow. The recipes present a consistent interface regardless of the underlying tools.

The `ruff` linter and formatter have rapidly become the standard for Python projects, replacing both `flake8` and `black` with a single, fast Rust-based tool. Combining `ruff` with `mypy` for type checking and `pytest` for testing covers the complete quality pipeline. A justfile ties all three together with sensible defaults and CI-ready recipes.

## Step 1 -- Project Structure

Create a minimal Python project with standard layout.

### `pyproject.toml`

```toml
[project]
name = "mypackage"
version = "0.1.0"
description = "Example Python project"
requires-python = ">=3.11"
dependencies = [
    "httpx>=0.27",
    "pydantic>=2.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0",
    "pytest-cov>=5.0",
    "pytest-asyncio>=0.23",
    "ruff>=0.5",
    "mypy>=1.10",
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.pytest.ini_options]
testpaths = ["tests"]
markers = [
    "unit: unit tests",
    "integration: integration tests (may require external services)",
    "slow: slow tests",
]

[tool.ruff]
target-version = "py311"
line-length = 88

[tool.ruff.lint]
select = ["E", "F", "I", "N", "UP", "B"]

[tool.mypy]
python_version = "3.11"
strict = true
```

### `src/mypackage/__init__.py`

```python
"""My package."""

__version__ = "0.1.0"
```

### `src/mypackage/core.py`

```python
"""Core business logic."""

def add(a: int, b: int) -> int:
    """Add two numbers."""
    return a + b

def greet(name: str) -> str:
    """Return a greeting."""
    return f"Hello, {name}!"
```

### `tests/test_core.py`

```python
"""Tests for core module."""

import pytest
from mypackage.core import add, greet

@pytest.mark.unit
def test_add() -> None:
    assert add(2, 3) == 5

@pytest.mark.unit
def test_greet() -> None:
    assert greet("World") == "Hello, World!"
```

### `.env`

```
PYTHONDONTWRITEBYTECODE=1
PYTHONPATH=src
```

## Step 2 -- Justfile Settings and Tool Detection

Create the justfile with automatic tool detection.

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Tool detection: prefer uv, fall back to pip
has_uv := `command -v uv >/dev/null 2>&1 && echo "true" || echo "false"`
pip_cmd := if has_uv == "true" { "uv pip" } else { "pip" }
run_cmd := if has_uv == "true" { "uv run" } else { "python3 -m" }
venv_cmd := if has_uv == "true" { "uv venv" } else { "python3 -m venv" }

# Project metadata
project_name := `python3 -c "import tomllib; print(tomllib.load(open('pyproject.toml','rb'))['project']['name'])" 2>/dev/null || echo "mypackage"`
project_version := `python3 -c "import tomllib; print(tomllib.load(open('pyproject.toml','rb'))['project']['version'])" 2>/dev/null || echo "0.0.0"`

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Package:{{NORMAL}} {{project_name}} {{project_version}}\n'
    @printf '{{BOLD}}Package manager:{{NORMAL}} {{pip_cmd}}\n'
```

**Intermediate Verification:**

```bash
just
```

You should see the recipe list, package name/version, and whether `uv pip` or `pip` was detected.

## Step 3 -- Virtual Environment and Dependency Recipes

Add recipes for setting up the virtual environment and installing dependencies.

### `justfile` (append)

```just
# Create virtual environment
[group('setup')]
venv:
    @printf '{{GREEN}}Creating virtual environment...{{NORMAL}}\n'
    {{venv_cmd}} .venv
    @printf '{{GREEN}}Activate with: source .venv/bin/activate{{NORMAL}}\n'

# Install project with dev dependencies
[group('setup')]
install:
    @printf '{{GREEN}}Installing dependencies ({{pip_cmd}})...{{NORMAL}}\n'
    {{pip_cmd}} install -e ".[dev]"
    @printf '{{GREEN}}Installed.{{NORMAL}}\n'

# Bootstrap: create venv + install
[group('setup')]
bootstrap: venv install
    @printf '{{GREEN}}{{BOLD}}Environment ready.{{NORMAL}}\n'

# Update dependencies
[group('setup')]
update:
    {{pip_cmd}} install --upgrade -e ".[dev]"

# Show installed packages
[group('setup')]
deps:
    {{pip_cmd}} list --format=columns
```

**Intermediate Verification:**

```bash
just bootstrap
```

You should see a virtual environment created and dependencies installed.

## Step 4 -- Testing Recipes

Add comprehensive testing recipes with markers and coverage.

### `justfile` (append)

```just
# Run all tests
[group('test')]
test *args:
    @printf '{{GREEN}}Running all tests...{{NORMAL}}\n'
    {{run_cmd}} pytest {{args}}

# Run unit tests only
[group('test')]
test-unit:
    @printf '{{GREEN}}Running unit tests...{{NORMAL}}\n'
    {{run_cmd}} pytest -m unit

# Run integration tests only
[group('test')]
test-integration:
    @printf '{{YELLOW}}Running integration tests...{{NORMAL}}\n'
    {{run_cmd}} pytest -m integration

# Run tests with coverage
[group('test')]
coverage:
    @printf '{{GREEN}}Running tests with coverage...{{NORMAL}}\n'
    {{run_cmd}} pytest --cov=src --cov-report=term-missing --cov-report=html:htmlcov
    @printf '{{GREEN}}HTML report: htmlcov/index.html{{NORMAL}}\n'

# Open coverage report in browser
[group('test')]
coverage-open: coverage
    open htmlcov/index.html 2>/dev/null || xdg-open htmlcov/index.html 2>/dev/null || true

# Run tests in watch mode (requires pytest-watch)
[group('test')]
test-watch:
    {{run_cmd}} pytest -f --color=yes
```

## Step 5 -- Linting and Type Checking Recipes

Add ruff and mypy recipes.

### `justfile` (append)

```just
# Run ruff linter
[group('lint')]
lint:
    @printf '{{GREEN}}Running ruff linter...{{NORMAL}}\n'
    {{run_cmd}} ruff check src/ tests/

# Run ruff linter with auto-fix
[group('lint')]
lint-fix:
    {{run_cmd}} ruff check --fix src/ tests/
    @printf '{{GREEN}}Lint issues fixed.{{NORMAL}}\n'

# Check formatting with ruff
[group('lint')]
fmt-check:
    @printf '{{GREEN}}Checking formatting...{{NORMAL}}\n'
    {{run_cmd}} ruff format --check src/ tests/

# Format code with ruff
[group('lint')]
fmt:
    {{run_cmd}} ruff format src/ tests/
    @printf '{{GREEN}}Formatted.{{NORMAL}}\n'

# Run mypy type checker
[group('lint')]
typecheck:
    @printf '{{GREEN}}Running mypy...{{NORMAL}}\n'
    {{run_cmd}} mypy src/

# All quality checks
[group('lint')]
quality: fmt-check lint typecheck
    @printf '{{GREEN}}{{BOLD}}All quality checks passed.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just quality
```

You should see ruff format check, ruff lint, and mypy all pass in sequence.

## Step 6 -- Build and Publish Recipes

Add recipes for building wheels and publishing.

### `justfile` (append)

```just
# Build package (wheel + sdist)
[group('build')]
build:
    @printf '{{GREEN}}Building {{project_name}} {{project_version}}...{{NORMAL}}\n'
    {{run_cmd}} python -m build
    @printf '{{GREEN}}Build artifacts:{{NORMAL}}\n'
    @ls -lh dist/

# Clean build artifacts
[group('build')]
clean:
    rm -rf dist/ build/ htmlcov/ .mypy_cache/ .pytest_cache/ .ruff_cache/
    find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
    find . -type f -name "*.pyc" -delete 2>/dev/null || true
    @printf '{{GREEN}}Cleaned.{{NORMAL}}\n'

# Publish to PyPI (requires twine or uv publish)
[group('build')]
[confirm("Publish {{project_name}} {{project_version}} to PyPI? (yes/no)")]
publish: build
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v uv &>/dev/null; then
        uv publish
    else
        python3 -m twine upload dist/*
    fi
    printf '{{GREEN}}Published {{project_name}} {{project_version}}.{{NORMAL}}\n'

# Publish to Test PyPI
[group('build')]
publish-test: build
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v uv &>/dev/null; then
        uv publish --index-url https://test.pypi.org/simple/
    else
        python3 -m twine upload --repository testpypi dist/*
    fi
    printf '{{GREEN}}Published to Test PyPI.{{NORMAL}}\n'
```

## Step 7 -- CI Aggregate Recipe

Add a CI pipeline recipe.

### `justfile` (append)

```just
# Full CI pipeline: quality → test → build
[group('ci')]
ci: quality test build
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# Quick check (fast feedback)
[group('ci')]
check: lint typecheck test-unit
    @printf '{{GREEN}}Quick check passed.{{NORMAL}}\n'

# Show project info
[group('help')]
info:
    @printf '{{BOLD}}Project:{{NORMAL}} {{project_name}} {{project_version}}\n'
    @printf '{{BOLD}}Python:{{NORMAL}} '
    @python3 --version
    @printf '{{BOLD}}Package manager:{{NORMAL}} {{pip_cmd}}\n'
    @printf '{{BOLD}}Has uv:{{NORMAL}} {{has_uv}}\n'
```

## Common Mistakes

### Mistake 1: Hardcoding `pip` Instead of Detecting the Tool

**Wrong:**

```just
install:
    pip install -e ".[dev]"
```

**What happens:** Developers using `uv` get a slower experience, and the recipe may install into the wrong environment if `pip` points to the system Python.

**Fix:** Detect the available tool at evaluation time:

```just
has_uv := `command -v uv >/dev/null 2>&1 && echo "true" || echo "false"`
pip_cmd := if has_uv == "true" { "uv pip" } else { "pip" }

install:
    {{pip_cmd}} install -e ".[dev]"
```

### Mistake 2: Missing `PYTHONPATH` for src Layout

**Wrong:** Running pytest without setting `PYTHONPATH` when using the `src/` layout.

**What happens:** Python cannot find your package. Tests fail with `ModuleNotFoundError`.

**Fix:** Set `PYTHONPATH=src` in your `.env` file and enable `set dotenv-load` in the justfile, or install the package in editable mode (`pip install -e .`).

## Verify What You Learned

```bash
# 1. Show project info with tool detection
just info
# Expected: project name, version, python version, detected package manager

# 2. Run all quality checks
just quality
# Expected: fmt-check, lint, typecheck pass in sequence

# 3. Run unit tests only
just test-unit
# Expected: only tests marked @pytest.mark.unit run

# 4. Run tests with coverage
just coverage
# Expected: coverage percentage and HTML report path

# 5. Build the package
just build
# Expected: dist/ directory with .whl and .tar.gz files
```

## What's Next

In the next exercise, you will build a complete Node.js/TypeScript project justfile with pnpm, vitest, eslint, and Docker integration.

## Summary

- Backtick expressions with `command -v` detect available tools (`uv` vs `pip`) at evaluation time
- Conditional expressions (`if has_uv == "true"`) adapt recipes to the developer's tooling setup
- `pyproject.toml` metadata can be extracted with backtick Python one-liners using `tomllib`
- pytest markers (`-m unit`, `-m integration`) enable targeted test execution via separate recipes
- `ruff` replaces both `flake8` and `black` with a single tool for linting and formatting
- `[confirm]` protects the publish recipe from accidental PyPI uploads

## Reference

- [just manual -- conditional expressions](https://just.systems/man/en/conditional-expressions.html)
- [just manual -- dotenv settings](https://just.systems/man/en/dotenv-settings.html)
- [just manual -- confirm attribute](https://just.systems/man/en/confirm.html)

## Additional Resources

- [ruff documentation](https://docs.astral.sh/ruff/)
- [uv documentation](https://docs.astral.sh/uv/)
- [pytest markers documentation](https://docs.pytest.org/en/stable/how-to/mark.html)
- [Python packaging user guide](https://packaging.python.org/en/latest/)
