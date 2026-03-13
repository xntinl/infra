# 4. Arguments and Parameters

<!--
difficulty: basic
concepts: [required-args, default-args, variadic-plus, variadic-star, env-exported-args, validation]
tools: [just]
estimated_time: 25m
bloom_level: apply
prerequisites: [03-dependencies]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Differentiate** between required, default, `+` variadic, and `*` variadic parameters
- **Apply** the `$` prefix to export recipe arguments as environment variables
- **Construct** recipes with input validation using shebang scripts

## Why Arguments and Parameters

Static recipes only get you so far. Real workflows need parameterization: deploying to different environments, running tests against specific files, building with different optimization levels. Without arguments, you end up duplicating recipes (`deploy-dev`, `deploy-staging`, `deploy-prod`) or relying on environment variables for everything.

Just provides four parameter types that cover different use cases. Required arguments enforce that callers provide critical values. Default arguments offer convenience while allowing overrides. Variadic parameters accept multiple values, which maps naturally to commands that operate on file lists or pass-through flags.

The `$` prefix on parameters deserves special attention. It exports the argument as an environment variable for the recipe's shell commands, bridging the gap between just's parameter system and tools that read configuration from the environment. This eliminates the awkward pattern of `export VAR={{arg}} && command` that clutters recipe bodies.

## Step 1 -- Required and Default Arguments

A parameter without a default value is required. A parameter with `:= "value"` or `="value"` has a default and becomes optional.

### `justfile`

```justfile
# Deploy to a specific environment (required argument)
deploy env:
    @echo "Deploying to {{env}}..."
    @echo "Deploy to {{env}} complete."

# Build with an optional profile (defaults to "debug")
build profile="debug":
    @echo "Building in {{profile}} mode..."
    @echo "Build complete."
```

### Intermediate Verification

```bash
just deploy staging
```

Expected:

```
Deploying to staging...
Deploy to staging complete.
```

```bash
just deploy
```

Expected:

```
error: Recipe `deploy` got 0 arguments but takes 1
usage:
    just deploy env
```

```bash
just build
```

Expected:

```
Building in debug mode...
Build complete.
```

```bash
just build release
```

Expected:

```
Building in release mode...
Build complete.
```

## Step 2 -- Variadic `+` Parameters (One or More)

The `+` prefix means "one or more values." The recipe fails if no arguments are provided. Values are space-separated and concatenated into a single string.

### `justfile`

```justfile
# Deploy to a specific environment (required argument)
deploy env:
    @echo "Deploying to {{env}}..."
    @echo "Deploy to {{env}} complete."

# Build with an optional profile (defaults to "debug")
build profile="debug":
    @echo "Building in {{profile}} mode..."
    @echo "Build complete."

# Back up one or more files (at least one required)
backup +files:
    @echo "Backing up: {{files}}"
    @for f in {{files}}; do \
        echo "  Backed up: $f"; \
    done
    @echo "Backup complete."
```

### Intermediate Verification

```bash
just backup main.rs lib.rs config.toml
```

Expected:

```
Backing up: main.rs lib.rs config.toml
  Backed up: main.rs
  Backed up: lib.rs
  Backed up: config.toml
Backup complete.
```

```bash
just backup
```

Expected:

```
error: Recipe `backup` got 0 arguments but takes 1 or more
usage:
    just backup +files
```

## Step 3 -- Variadic `*` Parameters (Zero or More)

The `*` prefix means "zero or more values." Unlike `+`, calling the recipe with no arguments succeeds. This is ideal for optional pass-through flags.

### `justfile`

```justfile
# Deploy to a specific environment (required argument)
deploy env:
    @echo "Deploying to {{env}}..."
    @echo "Deploy to {{env}} complete."

# Build with an optional profile (defaults to "debug")
build profile="debug":
    @echo "Building in {{profile}} mode..."
    @echo "Build complete."

# Back up one or more files (at least one required)
backup +files:
    @echo "Backing up: {{files}}"
    @for f in {{files}}; do \
        echo "  Backed up: $f"; \
    done
    @echo "Backup complete."

# Run tests with optional extra flags
test *flags:
    @echo "Running tests with flags: [{{flags}}]"
    @echo "test command: cargo test {{flags}}"
```

### Intermediate Verification

```bash
just test
```

Expected:

```
Running tests with flags: []
test command: cargo test
```

```bash
just test --release -- test_name
```

Expected:

```
Running tests with flags: [--release -- test_name]
test command: cargo test --release -- test_name
```

## Step 4 -- Environment-Exported Arguments with `$`

The `$` prefix exports the argument as an environment variable for the recipe's shell. This lets child processes read the value directly from the environment.

### `justfile`

```justfile
# Deploy to a specific environment (required argument)
deploy env:
    @echo "Deploying to {{env}}..."
    @echo "Deploy to {{env}} complete."

# Build with an optional profile (defaults to "debug")
build profile="debug":
    @echo "Building in {{profile}} mode..."
    @echo "Build complete."

# Back up one or more files (at least one required)
backup +files:
    @echo "Backing up: {{files}}"
    @for f in {{files}}; do \
        echo "  Backed up: $f"; \
    done
    @echo "Backup complete."

# Run tests with optional extra flags
test *flags:
    @echo "Running tests with flags: [{{flags}}]"
    @echo "test command: cargo test {{flags}}"

# Start the server on a given port (exported as env var)
serve $PORT="8080":
    @echo "PORT env var is: $PORT"
    @echo "Starting server on port $PORT..."
```

### Intermediate Verification

```bash
just serve
```

Expected:

```
PORT env var is: 8080
Starting server on port 8080...
```

```bash
just serve 3000
```

Expected:

```
PORT env var is: 3000
Starting server on port 3000...
```

## Step 5 -- Input Validation with a Shebang Recipe

For complex validation, use a shebang (`#!`) recipe to write a multi-line script. This avoids the one-line-per-command limitation of normal recipes.

Add this to the bottom of your justfile:

```justfile
# Create a new component with validation
create-component name:
    #!/usr/bin/env bash
    set -euo pipefail

    # Validate name contains only lowercase letters and hyphens
    if [[ ! "{{name}}" =~ ^[a-z][a-z0-9-]*$ ]]; then
        echo "Error: Component name must start with a lowercase letter"
        echo "       and contain only lowercase letters, digits, and hyphens."
        echo "       Got: '{{name}}'"
        exit 1
    fi

    # Validate length
    if [[ ${#1} -lt 2 || ${#1} -gt 30 ]]; then
        echo "Error: Component name must be 2-30 characters. Got: ${#1}"
        exit 1
    fi

    echo "Creating component: {{name}}"
    echo "  Directory: src/components/{{name}}/"
    echo "  Component created successfully."
```

### Intermediate Verification

```bash
just create-component my-widget
```

Expected:

```
Creating component: my-widget
  Directory: src/components/my-widget/
  Component created successfully.
```

```bash
just create-component "Invalid Name"
```

Expected:

```
Error: Component name must start with a lowercase letter
       and contain only lowercase letters, digits, and hyphens.
       Got: 'Invalid Name'
```

## Common Mistakes

### Using `+` When You Want `*`

**Wrong:**

```justfile
lint +flags:
    @echo "Linting with: {{flags}}"
```

**What happens:**

```bash
just lint
```

```
error: Recipe `lint` got 0 arguments but takes 1 or more
```

If the flags are optional, this is the wrong variadic type.

**Fix:**

```justfile
lint *flags:
    @echo "Linting with: {{flags}}"
```

### Forgetting That Variadic Args Are a Single String

**Wrong assumption:** each variadic arg is a separate variable.

```justfile
copy +args:
    @echo "Source: {{args[0]}}"  # This does NOT work
```

**What happens:**

```
error: Expected '}}', '(', '+', or '/', but found '['
```

Variadic parameters produce a single space-separated string. There is no indexing syntax.

**Fix:**

Use shell positional parameters or `$@` to process individual arguments:

```justfile
set positional-arguments

copy +args:
    @echo "All args: $@"
    @echo "First: $1"
```

## Verify What You Learned

```bash
just deploy production
```

Expected:

```
Deploying to production...
Deploy to production complete.
```

```bash
just build release
```

Expected:

```
Building in release mode...
Build complete.
```

```bash
just backup a.txt b.txt
```

Expected:

```
Backing up: a.txt b.txt
  Backed up: a.txt
  Backed up: b.txt
Backup complete.
```

```bash
just test --verbose
```

Expected:

```
Running tests with flags: [--verbose]
test command: cargo test --verbose
```

```bash
just serve 9090
```

Expected:

```
PORT env var is: 9090
Starting server on port 9090...
```

## What's Next

In [Exercise 5 -- Conditionals and Expressions](../05-conditionals-and-expressions/05-conditionals-and-expressions.md), you will learn how to use `if/else` expressions, comparison operators, regex matching, and `path_exists()` to create justfiles that adapt to their context.

## Summary

- **Required arguments** -- parameters without defaults that must be provided by the caller
- **Default arguments** -- `name="value"` syntax makes the parameter optional
- **`+` variadic** -- one or more values required, concatenated into a single string
- **`*` variadic** -- zero or more values, succeeds even with no arguments
- **`$` env export** -- `$name` exports the argument as an environment variable for shell commands
- **Shebang recipes** -- `#!/usr/bin/env bash` enables multi-line scripts for complex validation

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Parameters](https://just.systems/man/en/chapter_28.html) -- complete parameter syntax reference
- [Just Manual -- Shebang Recipes](https://just.systems/man/en/chapter_22.html) -- writing multi-line shebang scripts
- [Just Manual -- Settings](https://just.systems/man/en/chapter_25.html) -- `set positional-arguments` and related settings
