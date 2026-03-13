# 1. Your First Justfile

<!--
difficulty: basic
concepts: [basic-recipe-syntax, silent-prefix, os-arch-functions, arguments-with-defaults, just-list]
tools: [just]
estimated_time: 15m
bloom_level: remember
prerequisites: [none]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the basic structure of a justfile and its recipe syntax
- **Recall** how to use `@` to suppress command echoing
- **Explain** how `just --list` discovers and displays available recipes

## Why Justfiles

Every project accumulates a set of commands that developers run repeatedly: building, testing, linting, deploying. These commands often live in a `Makefile`, shell scripts, or tribal knowledge passed around in Slack messages. `just` is a command runner that solves this problem with a simple, explicit syntax that avoids the quirks of `make` (tab sensitivity, implicit rules, phony targets).

A justfile acts as a single source of truth for project commands. Unlike Makefiles, justfiles are not build systems -- they do not track file modification times or build dependency graphs. This constraint is a feature: it keeps the tool focused on running commands predictably and transparently.

The `just` runner also provides built-in functions like `os()` and `arch()` that enable cross-platform justfiles without requiring shell-specific conditionals. This matters in teams where developers use macOS, Linux, and WSL side by side.

## Step 1 -- Create a Justfile with a Default Recipe

Create an empty directory for this exercise and add a file named `justfile` (no extension). The `default` recipe is special: it runs when you invoke `just` with no arguments.

### `justfile`

```justfile
# List all available recipes
default:
    @just --list --unsorted
```

The `@` prefix before `just --list --unsorted` suppresses echoing the command itself. Without it, just prints the command before its output.

### Intermediate Verification

```bash
just
```

Expected:

```
Available recipes:
    default # List all available recipes
```

## Step 2 -- Add a Hello Recipe with Arguments

Add a `hello` recipe that accepts a `name` argument with a default value. Arguments are declared after the recipe name, and defaults use the `:=` syntax.

### `justfile`

```justfile
# List all available recipes
default:
    @just --list --unsorted

# Greet someone by name
hello name="World":
    @echo "Hello, {{name}}!"
```

The `{{name}}` syntax interpolates the just variable into the shell command. This is not the same as shell variable expansion (`$name`).

### Intermediate Verification

```bash
just hello
```

Expected:

```
Hello, World!
```

```bash
just hello "Just Developer"
```

Expected:

```
Hello, Just Developer!
```

## Step 3 -- Add a System Info Recipe

Add an `info` recipe that uses built-in functions to display system information. These functions are evaluated by just itself, not by the shell.

### `justfile`

```justfile
# List all available recipes
default:
    @just --list --unsorted

# Greet someone by name
hello name="World":
    @echo "Hello, {{name}}!"

# Display system and project information
info:
    @echo "OS:            {{os()}}"
    @echo "Architecture:  {{arch()}}"
    @echo "CPU cores:     {{num_cpus()}}"
    @echo "Justfile dir:  {{justfile_directory()}}"
    @echo "Justfile:      {{justfile()}}"
    @echo "Invocation dir: {{invocation_directory()}}"
```

### Intermediate Verification

```bash
just info
```

Expected (values will vary by machine):

```
OS:            macos
Architecture:  aarch64
CPU cores:     10
Justfile dir:  /path/to/your/exercise
Justfile:      /path/to/your/exercise/justfile
Invocation dir: /path/to/your/exercise
```

## Common Mistakes

### Forgetting the `@` Silent Prefix

**Wrong:**

```justfile
hello name="World":
    echo "Hello, {{name}}!"
```

**What happens:**

```
echo "Hello, World!"
Hello, World!
```

The command itself is printed on the first line, followed by the output. This is just's default behavior for transparency.

**Fix:**

```justfile
hello name="World":
    @echo "Hello, {{name}}!"
```

### Using Shell Variables Instead of Just Interpolation

**Wrong:**

```justfile
hello name="World":
    @echo "Hello, $name!"
```

**What happens:**

```
Hello, !
```

The shell does not see `name` as an environment variable. Just arguments are interpolated with `{{}}`, not `$`.

**Fix:**

```justfile
hello name="World":
    @echo "Hello, {{name}}!"
```

## Verify What You Learned

```bash
just --list
```

Expected:

```
Available recipes:
    default # List all available recipes
    hello name="World" # Greet someone by name
    info               # Display system and project information
```

```bash
just hello "Rust"
```

Expected:

```
Hello, Rust!
```

```bash
just info 2>&1 | head -2
```

Expected (first two lines):

```
OS:            macos
Architecture:  aarch64
```

```bash
just --summary
```

Expected:

```
default hello info
```

## What's Next

In [Exercise 2 -- Variables and Backticks](../02-variables-and-backticks/02-variables-and-backticks.md), you will learn how to define reusable variables, capture shell output with backtick expressions, and read values from files and environment variables.

## Summary

- **Recipes** -- named blocks of shell commands, the fundamental unit of a justfile
- **`@` prefix** -- suppresses command echoing so only the output is displayed
- **Default recipe** -- the first recipe runs when `just` is invoked with no arguments
- **Arguments with defaults** -- `name="value"` syntax allows optional recipe parameters
- **Built-in functions** -- `os()`, `arch()`, `num_cpus()` provide platform info without shell commands

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Quick Start](https://just.systems/man/en/quick-start.html) -- getting started with your first justfile
- [Just Manual -- Recipes](https://just.systems/man/en/chapter_21.html) -- complete recipe syntax reference
- [Just Manual -- Functions](https://just.systems/man/en/chapter_32.html) -- all built-in functions
