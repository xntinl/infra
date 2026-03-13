# 7. Essential Settings

<!--
difficulty: basic
concepts: [set-shell, set-dotenv-load, set-positional-arguments, set-export, set-tempdir, set-quiet]
tools: [just]
estimated_time: 25m
bloom_level: understand
prerequisites: [06-built-in-functions]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `set shell` changes recipe execution behavior and enables strict error handling
- **Use** `set dotenv-load` and `set export` to manage environment variables declaratively
- **Apply** `set positional-arguments` to write recipes that use `$1`, `$2` shell positional parameters

## Why Settings

Just's default behavior is intentionally minimal: recipes run with `sh -cu`, variables are not exported, and `.env` files are ignored. These defaults are safe but often insufficient for real projects. Settings let you opt into behaviors that match your project's requirements without modifying every recipe.

The `set shell` setting deserves particular attention. By default, each recipe line runs in `sh`, which lacks bash-specific features like arrays, `[[ ]]` tests, and `set -o pipefail`. Switching to bash with strict flags (`-euo pipefail`) catches errors that `sh` silently ignores -- a failed command in a pipeline, an unset variable, or a command that errors partway through a recipe.

Settings like `set dotenv-load` and `set export` reduce boilerplate. Without them, every recipe that needs environment variables must either source files manually or use `export` statements. With them, the justfile handles environment configuration in one place, and recipes stay focused on their actual purpose.

## Step 1 -- Set Shell with Strict Error Handling

Configure just to use bash with strict flags. This is the single most impactful setting for catching bugs.

### `justfile`

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

# Demonstrate strict error handling
strict-demo:
    echo "Step 1: starting"
    echo "Step 2: this runs"
    false
    echo "Step 3: this will NOT run (set -e catches the failure)"

# Show that pipefail catches errors in pipelines
pipe-demo:
    echo "Testing pipefail..."
    false | echo "Pipeline with failure"
    echo "This will NOT run because pipefail is set"
```

### Intermediate Verification

```bash
just strict-demo 2>&1; echo "Exit code: $?"
```

Expected:

```
Step 1: starting
Step 2: this runs
Exit code: 1
```

Step 3 never runs because `false` returns exit code 1 and `set -e` terminates the recipe.

```bash
just pipe-demo 2>&1; echo "Exit code: $?"
```

Expected:

```
Testing pipefail...
Pipeline with failure
Exit code: 1
```

Without `pipefail`, the pipeline would succeed because `echo` (the last command) succeeds. With `pipefail`, the `false` failure propagates.

## Step 2 -- Load Environment from `.env` File

Enable `set dotenv-load` to automatically load variables from a `.env` file. Create the `.env` file first.

```bash
cat > .env << 'DOTENV'
DATABASE_URL=postgres://localhost:5432/myapp
REDIS_URL=redis://localhost:6379
APP_SECRET=development-secret-key
LOG_LEVEL=debug
DOTENV
```

### `justfile`

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load

# Show loaded environment variables
show-env:
    @echo "DATABASE_URL: $DATABASE_URL"
    @echo "REDIS_URL:    $REDIS_URL"
    @echo "LOG_LEVEL:    $LOG_LEVEL"

# Run a health check using env vars
health-check:
    @echo "Checking database: $DATABASE_URL"
    @echo "Checking cache: $REDIS_URL"
    @echo "Health check passed."
```

### Intermediate Verification

```bash
just show-env
```

Expected:

```
DATABASE_URL: postgres://localhost:5432/myapp
REDIS_URL:    redis://localhost:6379
LOG_LEVEL:    debug
```

## Step 3 -- Export All Just Variables as Environment Variables

`set export` makes every just variable available as an environment variable in recipe shells. This bridges just's variable system with tools that read configuration from the environment.

### `justfile`

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load
set export

project   := "my-service"
version   := "1.0.0"
log_level := "info"

# Show that just variables are available as env vars
export-demo:
    @echo "From just interpolation: {{project}} v{{version}}"
    @echo "From shell env var:      $project v$version"
    @echo "From env - log_level:    $log_level"

# Pass just variables to a child process
child-process:
    @env | grep -E "^(project|version|log_level)=" | sort
```

### Intermediate Verification

```bash
just export-demo
```

Expected:

```
From just interpolation: my-service v1.0.0
From shell env var:      my-service v1.0.0
From env - log_level:    info
```

```bash
just child-process
```

Expected:

```
log_level=info
project=my-service
version=1.0.0
```

## Step 4 -- Positional Arguments with `$1`, `$2`

`set positional-arguments` maps recipe arguments to shell positional parameters (`$1`, `$2`, etc.). This is useful when passing arguments to tools that expect them.

### `justfile`

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load
set export
set positional-arguments

project   := "my-service"
version   := "1.0.0"
log_level := "info"

# Show that just variables are available as env vars
export-demo:
    @echo "From just interpolation: {{project}} v{{version}}"
    @echo "From shell env var:      $project v$version"
    @echo "From env - log_level:    $log_level"

# Pass just variables to a child process
child-process:
    @env | grep -E "^(project|version|log_level)=" | sort

# Grep files using positional arguments
search pattern +files:
    @echo "Searching for '$1' in: $2"
    @grep -rn "$1" $2 || echo "No matches found."

# Run a specific test by name
test-one name:
    @echo "Running test: $1"
    @echo "Command: cargo test $1 -- --nocapture"
```

### Intermediate Verification

Create a test file to search:

```bash
echo -e "hello world\nfoo bar\nhello just" > sample.txt
```

```bash
just search "hello" sample.txt
```

Expected:

```
Searching for 'hello' in: sample.txt
1:hello world
3:hello just
```

```bash
just test-one integration_auth
```

Expected:

```
Running test: integration_auth
Command: cargo test integration_auth -- --nocapture
```

## Common Mistakes

### Using `set dotenv-load` Without a `.env` File

**What happens:**

Nothing -- no error is raised. Just silently continues without loading any variables. This is by design, since `.env` files are typically git-ignored and may not exist in all environments.

However, if your recipe references an undefined environment variable with `set shell := ["bash", "-euo", "pipefail", "-c"]`, bash's `-u` flag will catch it:

```
bash: line 1: DATABASE_URL: unbound variable
```

**Fix:**

Either provide defaults in the justfile or check for the file:

```justfile
db_url := env("DATABASE_URL", "postgres://localhost/default")
```

### Forgetting That `set export` Affects ALL Variables

**Wrong assumption:** only some variables are exported.

```justfile
set export

password := "super-secret"
project  := "my-app"

# This leaks the password to all child processes
build:
    some-build-tool
```

**What happens:**

Every just variable, including `password`, becomes an environment variable visible to all child processes. Tools that log their environment will expose it.

**Fix:**

Be deliberate about what you export. If `set export` is too broad, use per-variable export or the `$` parameter prefix instead:

```justfile
# Only export what you need
export PROJECT := "my-app"

# Keep secrets in just scope only
password := "super-secret"

build:
    @echo "Building $PROJECT"
    @echo "Using password via just: {{password}}"
```

### Confusing `set positional-arguments` with Just Interpolation

With `set positional-arguments` enabled, you can access recipe args via both `$1` and `{{name}}`. They are equivalent, but mixing them in the same recipe creates confusion:

```justfile
set positional-arguments

deploy env:
    @echo "Deploying to {{env}}"   # just interpolation
    @echo "Deploying to $1"         # positional argument
```

Both lines produce the same output. Pick one style and use it consistently within each recipe.

## Verify What You Learned

```bash
just export-demo
```

Expected:

```
From just interpolation: my-service v1.0.0
From shell env var:      my-service v1.0.0
From env - log_level:    info
```

```bash
just child-process
```

Expected:

```
log_level=info
project=my-service
version=1.0.0
```

```bash
echo "test line" > verify.txt && just search "test" verify.txt
```

Expected:

```
Searching for 'test' in: verify.txt
1:test line
```

```bash
just test-one my_test
```

Expected:

```
Running test: my_test
Command: cargo test my_test -- --nocapture
```

## What's Next

In [Exercise 8 -- Private Recipes, Aliases, and Attributes](../08-private-recipes-aliases-attributes/08-private-recipes-aliases-attributes.md), you will learn how to organize justfiles with private helpers, create aliases for convenience, and use attributes like `[confirm]` and `[doc]`.

## Summary

- **`set shell`** -- configures the shell and flags for all recipes; `["bash", "-euo", "pipefail", "-c"]` is the recommended strict default
- **`set dotenv-load`** -- automatically loads variables from `.env` into the recipe environment
- **`set export`** -- exports all just variables as environment variables for recipe shells
- **`set positional-arguments`** -- maps recipe arguments to `$1`, `$2`, etc., for shell positional access
- **Strict mode** -- `set -euo pipefail` catches failed commands, unset variables, and pipeline failures

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Settings](https://just.systems/man/en/chapter_25.html) -- complete settings reference
- [Bash Strict Mode](http://redsymbol.net/articles/unofficial-bash-strict-mode/) -- explanation of `set -euo pipefail`
- [Just Manual -- dotenv Integration](https://just.systems/man/en/chapter_25.html#dotenv-settings) -- dotenv loading behavior details
