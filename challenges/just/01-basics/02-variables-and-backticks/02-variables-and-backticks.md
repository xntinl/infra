---
difficulty: basic
concepts: [assignment-operator, backtick-evaluation, env-function, read-function, string-interpolation]
tools: [just]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-your-first-justfile]
---

# 2. Variables and Backticks

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between `:=` assignment and shell `=` assignment
- **Describe** when backtick expressions are evaluated (load time vs. run time)
- **Use** `env()`, `read()`, and backtick variables to build dynamic justfiles

## Why Variables and Backticks

Hardcoding values in recipes creates maintenance burdens. When a version string, build target, or file path appears in multiple recipes, changing it requires editing every occurrence. Just variables solve this by defining a value once and interpolating it wherever needed.

Backtick expressions take this further by capturing shell command output at justfile load time. This is essential for embedding dynamic values like git commit hashes, timestamps, or tool versions into build commands. However, load-time evaluation is a double-edged sword: if the command fails or produces unexpected output, every recipe in the justfile is affected.

The `env()` function bridges justfiles with the environment, enabling CI/CD pipelines to inject configuration without modifying the justfile. Combined with `read()` for file-based configuration, these features let you build justfiles that adapt to their execution context.

## Step 1 -- Read a Version from a File

Create a `VERSION` file and a justfile that reads it. The `read()` function loads file contents at evaluation time and strips trailing whitespace.

Create the `VERSION` file:

```bash
echo "1.0.0" > VERSION
```

### `justfile`

```justfile
version := read("VERSION")

# Show the current version
show-version:
    @echo "Version: {{version}}"
```

### Intermediate Verification

```bash
just show-version
```

Expected:

```
Version: 1.0.0
```

## Step 2 -- Add Backtick Variables for Git Metadata

Backtick expressions execute shell commands and capture their stdout. Add variables for the git commit hash and the current timestamp.

Initialize a git repo so the backtick commands succeed:

```bash
git init -q /tmp/just-ex02 && cd /tmp/just-ex02
echo "1.0.0" > VERSION
```

### `justfile`

```justfile
version   := read("VERSION")
git_hash  := `git rev-parse --short HEAD 2>/dev/null || echo "no-git"`
build_time := `date -u +"%Y-%m-%dT%H:%M:%SZ"`

# Show the current version
show-version:
    @echo "Version: {{version}}"

# Show full build metadata
build-info:
    @echo "Version:    {{version}}"
    @echo "Git hash:   {{git_hash}}"
    @echo "Build time: {{build_time}}"
```

Note the `2>/dev/null || echo "no-git"` fallback. Without it, running outside a git repository would cause the backtick to fail and the entire justfile to error.

### Intermediate Verification

```bash
just build-info
```

Expected (values will vary):

```
Version:    1.0.0
Git hash:   a1b2c3d
Build time: 2026-03-09T12:00:00Z
```

## Step 3 -- Use `env()` with Default Values

The `env()` function reads environment variables. The two-argument form provides a default when the variable is not set, which prevents hard failures in local development.

### `justfile`

```justfile
version    := read("VERSION")
git_hash   := `git rev-parse --short HEAD 2>/dev/null || echo "no-git"`
build_time := `date -u +"%Y-%m-%dT%H:%M:%SZ"`

ci         := env("CI", "false")
build_env  := env("BUILD_ENV", "development")
log_level  := env("LOG_LEVEL", "info")

# Show the current version
show-version:
    @echo "Version: {{version}}"

# Show full build metadata
build-info:
    @echo "Version:    {{version}}"
    @echo "Git hash:   {{git_hash}}"
    @echo "Build time: {{build_time}}"

# Show environment configuration
env-info:
    @echo "CI:        {{ci}}"
    @echo "Build env: {{build_env}}"
    @echo "Log level: {{log_level}}"
```

### Intermediate Verification

```bash
just env-info
```

Expected:

```
CI:        false
Build env: development
Log level: info
```

```bash
CI=true BUILD_ENV=production just env-info
```

Expected:

```
CI:        true
Build env: production
Log level: info
```

## Step 4 -- Inspect Variables with `just --evaluate`

The `--evaluate` flag prints all variable values without running any recipes. This is invaluable for debugging.

### Intermediate Verification

```bash
just --evaluate
```

Expected (values will vary):

```
build_env  := "development"
build_time := "2026-03-09T12:00:00Z"
ci         := "false"
git_hash   := "a1b2c3d"
log_level  := "info"
version    := "1.0.0"
```

You can also evaluate a single variable:

```bash
just --evaluate version
```

Expected:

```
1.0.0
```

## Common Mistakes

### Confusing `:=` (Just) with `=` (Shell)

**Wrong:**

```justfile
version = read("VERSION")
```

**What happens:**

```
error: Expected ':=' or ':', but found '='
```

Just uses `:=` for variable assignment. The `=` operator does not exist in justfile syntax.

**Fix:**

```justfile
version := read("VERSION")
```

### Backtick Evaluation Happens at Load Time

**Wrong assumption:** backtick values are computed when a recipe runs.

```justfile
current_time := `date -u +"%H:%M:%S"`

# These will show the SAME time
time-a:
    @echo "Time A: {{current_time}}"

time-b:
    @echo "Time B: {{current_time}}"
```

**What happens:**

Both recipes print the identical timestamp because `current_time` was evaluated once when the justfile was loaded, not when each recipe executes.

**Fix:** If you need per-recipe evaluation, use a shell command inside the recipe body:

```justfile
time-a:
    @echo "Time A: $(date -u +"%H:%M:%S")"

time-b:
    @echo "Time B: $(date -u +"%H:%M:%S")"
```

### Using `env()` Without a Default in Local Development

**Wrong:**

```justfile
api_key := env("API_KEY")
```

**What happens:**

```
error: Call to function `env` failed: environment variable `API_KEY` not present
```

The single-argument `env()` fails if the variable is not set.

**Fix:**

```justfile
api_key := env("API_KEY", "")
```

## Verify What You Learned

```bash
just show-version
```

Expected:

```
Version: 1.0.0
```

```bash
just build-info 2>&1 | head -1
```

Expected:

```
Version:    1.0.0
```

```bash
LOG_LEVEL=debug just env-info 2>&1 | grep "Log level"
```

Expected:

```
Log level: debug
```

```bash
just --evaluate version
```

Expected:

```
1.0.0
```

## What's Next

In [Exercise 3 -- Dependencies](../03-dependencies/03-dependencies.md), you will learn how to chain recipes together using prior and posterior dependencies, pass arguments to dependencies, and create private helper recipes.

## Summary

- **`:=` assignment** -- defines a variable at justfile scope, evaluated once at load time
- **Backtick expressions** -- capture shell command stdout at load time, useful for git hashes and timestamps
- **`read()` function** -- loads file contents into a variable, strips trailing whitespace
- **`env()` function** -- reads environment variables with optional defaults for safe fallback
- **`just --evaluate`** -- prints all variable values without executing any recipes

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Variables](https://just.systems/man/en/chapter_24.html) -- complete variable syntax and semantics
- [Just Manual -- Strings](https://just.systems/man/en/chapter_26.html) -- string quoting and interpolation rules
- [Just Manual -- Functions](https://just.systems/man/en/chapter_32.html) -- full list of built-in functions including `env()` and `read()`
