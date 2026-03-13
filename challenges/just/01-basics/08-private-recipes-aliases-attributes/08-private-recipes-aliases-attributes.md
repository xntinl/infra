# 8. Private Recipes, Aliases, and Attributes

<!--
difficulty: basic
concepts: [private-attribute, underscore-prefix, alias, confirm-attribute, no-cd, no-exit-message, no-quiet, doc-attribute]
tools: [just]
estimated_time: 25m
bloom_level: apply
prerequisites: [07-essential-settings]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Organize** justfiles by separating public-facing recipes from private implementation details
- **Create** aliases that provide shorthand names for frequently used recipes
- **Apply** attributes like `[confirm]`, `[no-cd]`, and `[doc]` to control recipe behavior and documentation

## Why Private Recipes, Aliases, and Attributes

As justfiles grow, they accumulate helper recipes that are implementation details -- setup steps, internal build phases, notification hooks. Exposing these in `just --list` clutters the interface and confuses users who only need the top-level commands. Private recipes solve this by hiding helpers from the listing while keeping them callable for composition.

Aliases serve the opposite purpose: they make frequently used recipes more accessible. A developer who runs `just t` a hundred times a day should not have to type `just test`. Aliases provide ergonomic shortcuts without duplicating recipe definitions.

Attributes are recipe-level annotations that modify behavior. The `[confirm]` attribute adds a safety gate to destructive operations -- deleting databases, wiping caches, force-pushing branches. The `[doc]` attribute lets you override the comment-based description that appears in `just --list`, which is useful when the recipe name alone is not descriptive enough. These features transform a flat list of recipes into a well-organized command interface.

## Step 1 -- Private Recipes with `[private]` and `_` Prefix

There are two ways to make a recipe private: the `[private]` attribute and the `_` name prefix. Both hide the recipe from `just --list` but leave it callable directly.

### `justfile`

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

# Build and test the project
build: _setup _compile
    @echo "[build] Build complete."

# Run the test suite
test: build _run-tests
    @echo "[test] All tests passed."

# --- Private recipes (hidden from --list) ---

[private]
setup-db:
    @echo "[setup-db] Initializing database schema..."
    @echo "[setup-db] Database ready."

# Underscore prefix also makes it private
_setup:
    @echo "[_setup] Checking prerequisites..."
    @echo "[_setup] All prerequisites met."

_compile:
    @echo "[_compile] Compiling source code..."
    @echo "[_compile] Compilation finished."

_run-tests:
    @echo "[_run-tests] Running unit tests..."
    @echo "[_run-tests] Running integration tests..."
```

### Intermediate Verification

```bash
just --list
```

Expected:

```
Available recipes:
    build # Build and test the project
    test  # Run the test suite
```

Note that `_setup`, `_compile`, `_run-tests`, and `setup-db` do not appear. But they are still callable:

```bash
just _setup
```

Expected:

```
[_setup] Checking prerequisites...
[_setup] All prerequisites met.
```

```bash
just test
```

Expected:

```
[_setup] Checking prerequisites...
[_setup] All prerequisites met.
[_compile] Compiling source code...
[_compile] Compilation finished.
[build] Build complete.
[_run-tests] Running unit tests...
[_run-tests] Running integration tests...
[test] All tests passed.
```

## Step 2 -- Aliases for Frequently Used Recipes

Aliases create alternative names for existing recipes. They appear in `just --list` with a reference to the original recipe.

### `justfile`

Add aliases below the recipe definitions:

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

# Build and test the project
build: _setup _compile
    @echo "[build] Build complete."

# Run the test suite
test: build _run-tests
    @echo "[test] All tests passed."

# Format source code
fmt:
    @echo "[fmt] Formatting code..."
    @echo "[fmt] All files formatted."

# Run the linter
lint: fmt
    @echo "[lint] Running linter..."
    @echo "[lint] No issues found."

# Deploy to an environment
deploy env="dev":
    @echo "[deploy] Deploying to {{env}}..."
    @echo "[deploy] Deployment complete."

# --- Aliases ---

alias b := build
alias t := test
alias f := fmt
alias l := lint
alias d := deploy

# --- Private recipes ---

_setup:
    @echo "[_setup] Checking prerequisites..."
    @echo "[_setup] All prerequisites met."

_compile:
    @echo "[_compile] Compiling source code..."
    @echo "[_compile] Compilation finished."

_run-tests:
    @echo "[_run-tests] Running unit tests..."
    @echo "[_run-tests] Running integration tests..."
```

### Intermediate Verification

```bash
just --list
```

Expected:

```
Available recipes:
    build          # Build and test the project
    test           # Run the test suite
    fmt            # Format source code
    lint           # Run the linter
    deploy env="dev" # Deploy to an environment
    b              # alias for `build`
    t              # alias for `test`
    f              # alias for `fmt`
    l              # alias for `lint`
    d              # alias for `deploy`
```

```bash
just t
```

Expected (same as `just test`):

```
[_setup] Checking prerequisites...
[_setup] All prerequisites met.
[_compile] Compiling source code...
[_compile] Compilation finished.
[build] Build complete.
[_run-tests] Running unit tests...
[_run-tests] Running integration tests...
[test] All tests passed.
```

```bash
just d staging
```

Expected:

```
[deploy] Deploying to staging...
[deploy] Deployment complete.
```

## Step 3 -- The `[confirm]` Attribute for Destructive Operations

`[confirm]` prompts the user for confirmation before running a recipe. This prevents accidental execution of destructive operations.

Add to your justfile:

```justfile
# Delete all build artifacts and caches
[confirm("This will delete ALL build artifacts. Continue? (y/N)")]
clean:
    @echo "[clean] Removing build directory..."
    @echo "[clean] Removing cache..."
    @echo "[clean] All artifacts removed."

# Reset the database (DESTRUCTIVE)
[confirm("WARNING: This will DROP all tables. Are you sure? (y/N)")]
reset-db: _setup
    @echo "[reset-db] Dropping all tables..."
    @echo "[reset-db] Recreating schema..."
    @echo "[reset-db] Database reset complete."
```

### Intermediate Verification

```bash
echo "y" | just clean
```

Expected:

```
This will delete ALL build artifacts. Continue? (y/N)
[clean] Removing build directory...
[clean] Removing cache...
[clean] All artifacts removed.
```

If you type `n` or just press Enter (empty input), the recipe aborts:

```bash
echo "n" | just clean 2>&1; echo "Exit code: $?"
```

Expected:

```
This will delete ALL build artifacts. Continue? (y/N)
error: Recipe `clean` was not confirmed
Exit code: 1
```

You can bypass confirmation in CI with the `--yes` flag:

```bash
just --yes clean
```

## Step 4 -- The `[no-cd]` Attribute

By default, just changes the working directory to the justfile's location before running each recipe. The `[no-cd]` attribute disables this, running the recipe in the directory where `just` was invoked.

Add to your justfile:

```justfile
# Show current directory (default: changes to justfile dir)
show-dir:
    @echo "Default behavior:"
    @echo "  pwd: $(pwd)"

# Show current directory (no-cd: stays in invocation dir)
[no-cd]
show-invocation-dir:
    @echo "With [no-cd]:"
    @echo "  pwd: $(pwd)"
```

### Intermediate Verification

```bash
cd /tmp && just --justfile /path/to/your/justfile show-dir
```

Expected:

```
Default behavior:
  pwd: /path/to/your/exercise
```

```bash
cd /tmp && just --justfile /path/to/your/justfile show-invocation-dir
```

Expected:

```
With [no-cd]:
  pwd: /tmp
```

## Step 5 -- The `[doc]` Attribute for Custom Descriptions

By default, `just --list` uses the comment above a recipe as its description. The `[doc]` attribute overrides this, which is useful when you want a detailed comment in the justfile but a concise description in the listing.

Add to your justfile:

```justfile
# This is a complex recipe that performs multiple initialization steps
# including environment validation, dependency installation, and
# configuration file generation for local development.
[doc("Initialize the development environment")]
init: _setup
    @echo "[init] Installing dependencies..."
    @echo "[init] Generating configuration..."
    @echo "[init] Development environment ready."

# Runs all quality checks: format, lint, test, and coverage.
# Designed to be run before pushing to remote.
[doc("Run all pre-push quality checks")]
check: fmt lint test
    @echo "[check] All quality checks passed."
```

### Intermediate Verification

```bash
just --list 2>&1 | grep -E "(init|check)"
```

Expected:

```
    init           # Initialize the development environment
    check          # Run all pre-push quality checks
```

The `[doc]` text appears instead of the multi-line comment.

## Common Mistakes

### Assuming `[private]` Prevents Direct Execution

**Wrong assumption:** private recipes cannot be run directly.

```justfile
[private]
secret-setup:
    @echo "Setting up secrets..."
```

**What happens:**

```bash
just secret-setup
```

```
Setting up secrets...
```

The recipe runs successfully. `[private]` only hides from `just --list`. It does not restrict access.

**Fix:**

This is by design. If you need to prevent direct execution, add a guard:

```justfile
[private]
_secret-setup caller="":
    #!/usr/bin/env bash
    if [[ -z "{{caller}}" ]]; then
        echo "Error: _secret-setup should not be called directly"
        exit 1
    fi
    echo "Setting up secrets..."
```

### Alias Pointing to a Non-Existent Recipe

**Wrong:**

```justfile
alias b := bild
```

**What happens:**

```
error: Alias `b` has an unknown target `bild`
```

Just validates alias targets at load time.

**Fix:**

```justfile
alias b := build
```

### Putting `[confirm]` on a Recipe with Dependencies

**What happens:**

The confirmation prompt appears before the recipe body, but after the dependencies have already run. If you need to confirm before any work starts, put the confirmation on the first recipe in the chain:

```justfile
# Wrong: deps run before confirm
[confirm]
deploy: build test
    @echo "Deploying..."

# Better: confirm first, then chain
[confirm("Deploy to production? (y/N)")]
deploy:
    @echo "Deploying..."

deploy-full: deploy
    @echo "Post-deploy steps..."
```

## Verify What You Learned

```bash
just --list 2>&1 | grep -v "alias"
```

Expected (private recipes not shown):

```
Available recipes:
    build          # Build and test the project
    test           # Run the test suite
    fmt            # Format source code
    lint           # Run the linter
    deploy env="dev" # Deploy to an environment
    clean          # ...
    reset-db       # ...
    show-dir       # ...
    show-invocation-dir # ...
    init           # Initialize the development environment
    check          # Run all pre-push quality checks
```

```bash
just t 2>&1 | tail -1
```

Expected:

```
[test] All tests passed.
```

```bash
just --yes clean
```

Expected:

```
[clean] Removing build directory...
[clean] Removing cache...
[clean] All artifacts removed.
```

```bash
just _compile
```

Expected (private but still callable):

```
[_compile] Compiling source code...
[_compile] Compilation finished.
```

## What's Next

Congratulations -- you have completed the basics series. You now have a solid foundation in just's core features. From here, consider exploring:

- **Modules and imports** -- `mod` and `import` for splitting large justfiles across multiple files
- **Shebang recipes with other languages** -- use Python, Node, or Ruby for complex logic
- **`[group]` attribute** -- organize recipes into logical groups in `just --list`
- **Conditional imports** -- `import?` for optional platform-specific recipe files

## Summary

- **`[private]` / `_` prefix** -- hides recipes from `just --list` but does not prevent direct invocation
- **`alias`** -- creates an alternative name for an existing recipe, with full argument support
- **`[confirm]`** -- prompts for user confirmation before executing; bypass with `--yes` in CI
- **`[no-cd]`** -- runs the recipe in the invocation directory instead of the justfile directory
- **`[doc("...")]`** -- overrides the `--list` description with a custom string

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Attributes](https://just.systems/man/en/chapter_31.html) -- complete attribute reference
- [Just Manual -- Aliases](https://just.systems/man/en/chapter_27.html) -- alias syntax and behavior
- [Just Manual -- Organizing Justfiles](https://just.systems/man/en/chapter_29.html) -- modules, imports, and file organization
