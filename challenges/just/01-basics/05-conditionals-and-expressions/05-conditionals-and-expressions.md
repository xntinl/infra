# 5. Conditionals and Expressions

<!--
difficulty: basic
concepts: [if-else-expressions, equality-operators, regex-match, path-exists, conditional-assignment]
tools: [just]
estimated_time: 20m
bloom_level: apply
prerequisites: [04-arguments-and-parameters]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `if/else` expressions to conditionally set variables and recipe behavior
- **Use** `==`, `!=`, and `=~` operators for string comparison and regex matching
- **Construct** cross-platform justfiles using `path_exists()` and `os()` conditionals

## Why Conditionals

Cross-platform projects face a fundamental tension: the justfile should be the single source of truth for project commands, but different operating systems need different commands. macOS uses `sed -i ''`, Linux uses `sed -i`, and Windows has neither. Without conditionals, you either maintain platform-specific justfiles or push complexity into shell scripts.

Just's conditional expressions resolve this at the justfile level. They are evaluated by just itself, not by the shell, which means they work identically across all platforms. The `if/else` syntax can appear both in variable assignments and inside recipe bodies, giving you fine-grained control over what gets executed.

The `path_exists()` function adds filesystem awareness. Many workflows need to behave differently based on whether a configuration file, lock file, or build directory exists. Checking this in just (rather than shell) keeps the logic visible in the justfile instead of buried in inline bash.

## Step 1 -- Conditional Variables Based on OS

Use `if/else` in variable assignment to set platform-specific values. The expression is evaluated at load time.

### `justfile`

```justfile
# Platform-specific settings
sed_inplace := if os() == "macos" {
    "sed -i ''"
} else {
    "sed -i"
}

open_cmd := if os() == "macos" {
    "open"
} else if os() == "linux" {
    "xdg-open"
} else {
    "echo 'Unsupported OS:'"
}

# Show platform-specific commands
show-platform:
    @echo "OS:           {{os()}}"
    @echo "sed in-place: {{sed_inplace}}"
    @echo "open command:  {{open_cmd}}"
```

### Intermediate Verification

```bash
just show-platform
```

Expected (on macOS):

```
OS:           macos
sed in-place: sed -i ''
open command:  open
```

## Step 2 -- Check File Existence with `path_exists()`

Use `path_exists()` to conditionally include configuration steps. Create a test file to see both branches.

### `justfile`

```justfile
# Platform-specific settings
sed_inplace := if os() == "macos" {
    "sed -i ''"
} else {
    "sed -i"
}

open_cmd := if os() == "macos" {
    "open"
} else if os() == "linux" {
    "xdg-open"
} else {
    "echo 'Unsupported OS:'"
}

config_status := if path_exists(".env") == "true" {
    "found"
} else {
    "not found"
}

# Show platform-specific commands
show-platform:
    @echo "OS:           {{os()}}"
    @echo "sed in-place: {{sed_inplace}}"
    @echo "open command:  {{open_cmd}}"

# Initialize the project, loading .env if present
init:
    @echo "Config file (.env): {{config_status}}"
    @if [ -f .env ]; then \
        echo "Loading configuration from .env..."; \
        echo "  $(head -1 .env)"; \
    else \
        echo "No .env file found. Using defaults."; \
    fi
    @echo "Initialization complete."
```

### Intermediate Verification

```bash
just init
```

Expected (without `.env`):

```
Config file (.env): not found
No .env file found. Using defaults.
Initialization complete.
```

Now create a `.env` file and run again:

```bash
echo 'DATABASE_URL=postgres://localhost/mydb' > .env
just init
```

Expected:

```
Config file (.env): found
Loading configuration from .env...
  DATABASE_URL=postgres://localhost/mydb
Initialization complete.
```

## Step 3 -- Regex Matching with `=~`

The `=~` operator matches a string against a regular expression. This is useful for validating input formats.

Add to your justfile:

```justfile
# Validate and display a semantic version
check-version version:
    @echo {{ if version =~ '[0-9]+\.[0-9]+\.[0-9]+' { "Valid semver: " + version } else { "ERROR: Invalid version format: " + version } }}
```

### Intermediate Verification

```bash
just check-version "1.2.3"
```

Expected:

```
Valid semver: 1.2.3
```

```bash
just check-version "not-a-version"
```

Expected:

```
ERROR: Invalid version format: not-a-version
```

## Step 4 -- Conditionals in Recipe Bodies

Use `if/else` expressions inline within recipe `echo` statements, or use shell conditionals for multi-line branching. The just `if` expression is for single-value selection; use shell `if` for complex control flow.

Add to your justfile:

```justfile
# Deploy based on environment
deploy env="dev":
    @echo "Target: {{ if env == "prod" { "PRODUCTION (requires approval)" } else { env } }}"
    @echo "Region: {{ if env == "prod" { "us-east-1" } else if env == "staging" { "us-west-2" } else { "local" } }}"
    #!/usr/bin/env bash
    set -euo pipefail

    if [[ "{{env}}" == "prod" ]]; then
        echo "WARNING: Production deployment requires manual approval."
        echo "Run: just deploy-prod-confirmed"
    elif [[ "{{env}}" == "staging" ]]; then
        echo "Deploying to staging cluster..."
        echo "Staging deploy complete."
    else
        echo "Deploying to local dev environment..."
        echo "Dev deploy complete."
    fi
```

### Intermediate Verification

```bash
just deploy
```

Expected:

```
Target: dev
Region: local
Deploying to local dev environment...
Dev deploy complete.
```

```bash
just deploy staging
```

Expected:

```
Target: staging
Region: us-west-2
Deploying to staging cluster...
Staging deploy complete.
```

```bash
just deploy prod
```

Expected:

```
Target: PRODUCTION (requires approval)
Region: us-east-1
WARNING: Production deployment requires manual approval.
Run: just deploy-prod-confirmed
```

## Common Mistakes

### Using Shell `if` Syntax in Just Expressions

**Wrong:**

```justfile
compiler := if [ "$(os)" = "macos" ]; then echo "clang"; else echo "gcc"; fi
```

**What happens:**

```
error: Expected ':=', but found 'if'
```

Just expressions use their own `if/else` syntax, not shell syntax.

**Fix:**

```justfile
compiler := if os() == "macos" { "clang" } else { "gcc" }
```

### Forgetting That `path_exists()` Returns a String

**Wrong:**

```justfile
has_config := if path_exists(".env") { "yes" } else { "no" }
```

**What happens:**

```
error: Expected '==', '!=', or '=~', but found '{'
```

`path_exists()` returns the string `"true"` or `"false"`, not a boolean. You must compare it.

**Fix:**

```justfile
has_config := if path_exists(".env") == "true" { "yes" } else { "no" }
```

### Missing the `else` Branch

**Wrong:**

```justfile
mode := if env("CI", "false") == "true" { "ci" }
```

**What happens:**

```
error: Expected 'else', but found end of line
```

Just `if` expressions always require an `else` branch -- there is no "if without else."

**Fix:**

```justfile
mode := if env("CI", "false") == "true" { "ci" } else { "local" }
```

## Verify What You Learned

```bash
just show-platform 2>&1 | head -1
```

Expected:

```
OS:           macos
```

```bash
just check-version "2.0.0"
```

Expected:

```
Valid semver: 2.0.0
```

```bash
just check-version "abc"
```

Expected:

```
ERROR: Invalid version format: abc
```

```bash
just deploy staging 2>&1 | head -1
```

Expected:

```
Target: staging
```

```bash
rm -f .env && just init 2>&1 | head -1
```

Expected:

```
Config file (.env): not found
```

## What's Next

In [Exercise 6 -- Built-in Functions](../06-built-in-functions/06-built-in-functions.md), you will explore just's extensive function library for string manipulation, path operations, and build metadata generation.

## Summary

- **`if/else` expressions** -- just-native conditionals evaluated at load time, usable in both variables and recipe bodies
- **`==` / `!=` operators** -- string equality comparison
- **`=~` operator** -- regex matching against a string pattern
- **`path_exists()`** -- returns `"true"` or `"false"` (string, not boolean) for filesystem checks
- **Conditional variables** -- set platform-specific or context-dependent values at the justfile level

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Conditional Expressions](https://just.systems/man/en/chapter_30.html) -- full conditional syntax reference
- [Just Manual -- Functions](https://just.systems/man/en/chapter_32.html) -- `path_exists()` and related functions
- [Rust Regex Syntax](https://docs.rs/regex/latest/regex/#syntax) -- regex syntax reference for the `=~` operator
