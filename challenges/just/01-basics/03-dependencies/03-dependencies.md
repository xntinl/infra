# 3. Dependencies

<!--
difficulty: basic
concepts: [prior-dependencies, posterior-dependencies, argument-passing-to-deps, deduplication, private-recipes]
tools: [just]
estimated_time: 20m
bloom_level: understand
prerequisites: [02-variables-and-backticks]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between prior and posterior dependencies
- **Construct** dependency chains that pass arguments between recipes
- **Describe** how just deduplicates dependencies in a dependency graph

## Why Dependencies

Real workflows are rarely a single command. Building software typically involves cleaning old artifacts, compiling source code, running tests, and packaging the result. Each step depends on the previous one completing successfully. Without dependency declarations, you either duplicate commands across recipes or rely on developers to remember the correct invocation order.

Just's dependency system lets you express these relationships declaratively. Prior dependencies run before the recipe body, posterior dependencies run after. This is fundamentally different from `make`, where dependencies are file-based. In just, dependencies are recipe-based -- they express execution order, not file freshness.

A critical property of just's dependency resolution is deduplication: if the same recipe appears multiple times in a dependency graph, it runs only once. This prevents redundant work in complex graphs where multiple recipes share a common dependency like `clean` or `setup`.

## Step 1 -- Create a Chain with Prior Dependencies

Prior dependencies are listed after a colon following the recipe name. They run before the recipe body executes.

### `justfile`

```justfile
# Run the full pipeline: clean → build → test
test: build
    @echo "[test] Running tests..."
    @echo "[test] All tests passed."

# Build the project (depends on clean)
build: clean
    @echo "[build] Compiling source code..."
    @echo "[build] Build complete."

# Remove build artifacts
clean:
    @echo "[clean] Removing build artifacts..."
    @echo "[clean] Clean complete."
```

### Intermediate Verification

```bash
just test
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling source code...
[build] Build complete.
[test] Running tests...
[test] All tests passed.
```

Note the execution order: `clean` runs first, then `build`, then `test`. Just resolves the full dependency chain automatically.

## Step 2 -- Add Posterior Dependencies with `&&`

Posterior dependencies run after the recipe body completes. Use the `&&` syntax to declare them. This is useful for notification or cleanup steps that should only run after the main work succeeds.

### `justfile`

```justfile
# Run the full pipeline: clean → build → test
test: build
    @echo "[test] Running tests..."
    @echo "[test] All tests passed."

# Build the project (depends on clean)
build: clean
    @echo "[build] Compiling source code..."
    @echo "[build] Build complete."

# Remove build artifacts
clean:
    @echo "[clean] Removing build artifacts..."
    @echo "[clean] Clean complete."

# Release: build first, then tag and notify after
release: build && tag notify
    @echo "[release] Packaging release..."
    @echo "[release] Package complete."

# Tag the release in git
tag:
    @echo "[tag] Creating git tag..."
    @echo "[tag] Tag created."

# Send a notification
notify:
    @echo "[notify] Sending release notification..."
    @echo "[notify] Notification sent."
```

### Intermediate Verification

```bash
just release
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling source code...
[build] Build complete.
[release] Packaging release...
[release] Package complete.
[tag] Creating git tag...
[tag] Tag created.
[notify] Sending release notification...
[notify] Notification sent.
```

The order is: prior deps (`build`, which pulls in `clean`) -> recipe body (`release`) -> posterior deps (`tag`, `notify`).

## Step 3 -- Pass Arguments to Dependencies

You can pass arguments to dependencies using parenthesized syntax. This enables a single recipe to be reused with different configurations.

### `justfile`

```justfile
# Default: run debug build and test
default: (build "debug") test-unit
    @echo "[default] Done."

# Run the full pipeline in release mode
release: (build "release") test-unit && tag notify
    @echo "[release] Packaging release..."
    @echo "[release] Package complete."

# Build with a specified profile
build profile="debug": clean
    @echo "[build] Compiling in {{profile}} mode..."
    @echo "[build] Build complete ({{profile}})."

# Run unit tests
test-unit:
    @echo "[test] Running unit tests..."
    @echo "[test] All tests passed."

# Remove build artifacts
clean:
    @echo "[clean] Removing build artifacts..."
    @echo "[clean] Clean complete."

# Tag the release in git
tag:
    @echo "[tag] Creating git tag..."

# Send a notification
notify:
    @echo "[notify] Sending release notification..."
```

### Intermediate Verification

```bash
just default
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling in debug mode...
[build] Build complete (debug).
[test] Running unit tests...
[test] All tests passed.
[default] Done.
```

```bash
just release
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling in release mode...
[build] Build complete (release).
[test] Running unit tests...
[test] All tests passed.
[release] Packaging release...
[release] Package complete.
[tag] Creating git tag...
[notify] Sending release notification...
```

Note that `clean` runs only once even though it appears in the dependency graph through `build` which is called by both `default`/`release` and could theoretically be triggered twice.

## Common Mistakes

### Circular Dependencies

**Wrong:**

```justfile
a: b
    @echo "a"

b: a
    @echo "b"
```

**What happens:**

```
error: Recipe `a` has circular dependency `a -> b -> a`
```

Just detects circular dependencies at load time and refuses to run.

**Fix:**

Restructure the dependency graph to be acyclic. Extract the shared logic into a third recipe:

```justfile
a: shared
    @echo "a"

b: shared
    @echo "b"

shared:
    @echo "shared setup"
```

### Expecting Shell State to Persist Between Recipes

**Wrong assumption:** a variable set in one recipe is visible in a dependent recipe.

```justfile
setup:
    export MY_VAR="hello"

use-var: setup
    @echo "Var is: $MY_VAR"
```

**What happens:**

```
Var is:
```

Each recipe runs in a separate shell process. Environment variables, working directory changes, and shell state do not persist across recipes.

**Fix:**

Use just variables for shared data, or export at the justfile level:

```justfile
export MY_VAR := "hello"

setup:
    @echo "Setup complete"

use-var: setup
    @echo "Var is: $MY_VAR"
```

## Verify What You Learned

```bash
just default
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling in debug mode...
[build] Build complete (debug).
[test] Running unit tests...
[test] All tests passed.
[default] Done.
```

```bash
just release 2>&1 | head -3
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling in release mode...
```

```bash
just build "release"
```

Expected:

```
[clean] Removing build artifacts...
[clean] Clean complete.
[build] Compiling in release mode...
[build] Build complete (release).
```

```bash
just --dry-run release
```

Expected (shows what would run without executing):

```
#!:differedoutput
echo "[clean] Removing build artifacts..."
echo "[clean] Clean complete."
echo "[build] Compiling in release mode..."
echo "[build] Build complete (release)."
echo "[test] Running unit tests..."
echo "[test] All tests passed."
echo "[release] Packaging release..."
echo "[release] Package complete."
echo "[tag] Creating git tag..."
echo "[notify] Sending release notification..."
```

## What's Next

In [Exercise 4 -- Arguments and Parameters](../04-arguments-and-parameters/04-arguments-and-parameters.md), you will learn about required arguments, variadic parameters, environment-exported arguments, and input validation patterns.

## Summary

- **Prior dependencies** -- recipes listed after `:` run before the recipe body
- **Posterior dependencies** -- recipes after `&&` run after the recipe body succeeds
- **Argument passing** -- `(recipe "arg")` syntax passes arguments to dependencies
- **Deduplication** -- each recipe runs at most once per invocation, even if referenced multiple times
- **Process isolation** -- each recipe runs in a separate shell; state does not persist between recipes

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Dependencies](https://just.systems/man/en/chapter_23.html) -- complete dependency syntax and semantics
- [Just Manual -- Recipe Attributes](https://just.systems/man/en/chapter_31.html) -- attributes that modify recipe behavior
