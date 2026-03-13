# 12. Environment Management with Dotenv

<!--
difficulty: intermediate
concepts:
  - set dotenv-load
  - set dotenv-filename
  - env() function with defaults
  - environment switching
  - shebang recipes for validation
  - multi-environment configuration
  - private recipes
tools: [just]
estimated_time: 30 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - understanding of environment variables
  - dotenv file conventions
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.36 | `just --version` |

## Learning Objectives

- **Apply** `set dotenv-load`, `set dotenv-filename`, and `env()` to manage configuration across development, staging, and production environments
- **Implement** environment switching recipes that safely toggle between configurations with validation
- **Design** a validation pattern using shebang recipes that prevents deployments with missing or invalid variables

## Why Environment Management in Justfiles

Every application beyond "Hello World" needs different configuration for different environments: database URLs, API keys, feature flags, log levels. The dotenv pattern -- storing these values in `.env` files -- is widely adopted, but managing multiple environments (`.env.dev`, `.env.staging`, `.env.prod`) introduces complexity. Which file is active? Are all required variables present? Did someone accidentally commit production secrets?

just's dotenv integration addresses this at the task-runner level. The `set dotenv-load` setting automatically reads a `.env` file and exports its values to all recipes. Combined with `set dotenv-filename`, you can dynamically select which environment file to load. The `env()` function provides defaults for optional variables and fails loudly for required ones.

This exercise builds a complete environment management system: switching between environments, validating configuration, listing active values, and preventing deployment with missing variables. These patterns apply to any project regardless of language or framework.

## Step 1 -- Create Environment Files

Set up dotenv files for three environments.

### `.env.dev`

```
APP_ENV=development
APP_PORT=3000
APP_DEBUG=true
DATABASE_URL=postgres://dev:devpass@localhost:5432/myapp_dev
REDIS_URL=redis://localhost:6379/0
LOG_LEVEL=debug
API_BASE_URL=http://localhost:3000
SECRET_KEY=dev-secret-not-real
FEATURE_NEW_UI=true
```

### `.env.staging`

```
APP_ENV=staging
APP_PORT=8080
APP_DEBUG=false
DATABASE_URL=postgres://staging:stagingpass@staging-db.internal:5432/myapp_staging
REDIS_URL=redis://staging-redis.internal:6379/0
LOG_LEVEL=info
API_BASE_URL=https://staging.myapp.example.com
SECRET_KEY=staging-secret-change-me
FEATURE_NEW_UI=true
```

### `.env.prod`

```
APP_ENV=production
APP_PORT=8080
APP_DEBUG=false
DATABASE_URL=postgres://prod:CHANGEME@prod-db.internal:5432/myapp_prod
REDIS_URL=redis://prod-redis.internal:6379/0
LOG_LEVEL=warn
API_BASE_URL=https://myapp.example.com
SECRET_KEY=MUST_BE_SET_FROM_VAULT
FEATURE_NEW_UI=false
```

### `.env`

```
# Symlink target / active environment
# This file is auto-managed by `just env-switch`
APP_ENV=development
APP_PORT=3000
APP_DEBUG=true
DATABASE_URL=postgres://dev:devpass@localhost:5432/myapp_dev
REDIS_URL=redis://localhost:6379/0
LOG_LEVEL=debug
API_BASE_URL=http://localhost:3000
SECRET_KEY=dev-secret-not-real
FEATURE_NEW_UI=true
```

Add `.env*` to `.gitignore` (except a template):

### `.gitignore`

```
.env
.env.dev
.env.staging
.env.prod
```

### `.env.template`

```
APP_ENV=
APP_PORT=
APP_DEBUG=
DATABASE_URL=
REDIS_URL=
LOG_LEVEL=
API_BASE_URL=
SECRET_KEY=
FEATURE_NEW_UI=
```

## Step 2 -- Core Justfile with Dotenv Loading

Create the justfile that loads the active `.env` file.

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

# Required variables (fail if missing)
app_env := env("APP_ENV")

# Optional variables (provide defaults)
app_port  := env("APP_PORT", "3000")
log_level := env("LOG_LEVEL", "info")

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands and current environment
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Active environment:{{NORMAL}} {{app_env}}\n'
```

**Intermediate Verification:**

```bash
just
```

You should see the recipe list and "Active environment: development".

## Step 3 -- Environment Switching

Add recipes to switch between environments.

### `justfile` (append)

```just
# Switch to a different environment
[group('env')]
env-switch env:
    #!/usr/bin/env bash
    set -euo pipefail
    ENV_FILE=".env.{{env}}"
    if [ ! -f "$ENV_FILE" ]; then
        printf '{{RED}}Error: %s not found{{NORMAL}}\n' "$ENV_FILE"
        printf 'Available environments:\n'
        ls -1 .env.* 2>/dev/null | sed 's/\.env\./  /' | grep -v template || true
        exit 1
    fi
    cp "$ENV_FILE" .env
    printf '{{GREEN}}Switched to {{env}} environment{{NORMAL}}\n'
    printf '{{BOLD}}APP_ENV:{{NORMAL}} %s\n' "$(grep APP_ENV .env | cut -d= -f2)"
    printf '{{BOLD}}APP_PORT:{{NORMAL}} %s\n' "$(grep APP_PORT .env | cut -d= -f2)"

# List available environments
[group('env')]
env-list:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Available environments:{{NORMAL}}\n'
    for f in .env.*; do
        [ "$f" = ".env.template" ] && continue
        env_name="${f#.env.}"
        if grep -q "^APP_ENV=" "$f"; then
            app_env=$(grep "^APP_ENV=" "$f" | cut -d= -f2)
            printf '  {{GREEN}}%s{{NORMAL}} (APP_ENV=%s)\n' "$env_name" "$app_env"
        fi
    done
    printf '\n{{BOLD}}Active:{{NORMAL}} '
    grep "^APP_ENV=" .env | cut -d= -f2 || echo "none"
```

**Intermediate Verification:**

```bash
just env-list
just env-switch staging
just
```

After switching, the default recipe should show "Active environment: staging".

## Step 4 -- Environment Display and Comparison

Add recipes to inspect and compare environments.

### `justfile` (append)

```just
# Show all environment variables (redacts secrets)
[group('env')]
env-show:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Current environment: %s{{NORMAL}}\n\n' "$APP_ENV"
    while IFS='=' read -r key value; do
        [[ "$key" =~ ^#.*$ ]] && continue
        [[ -z "$key" ]] && continue
        # Redact sensitive values
        if [[ "$key" =~ (SECRET|PASSWORD|KEY|TOKEN) ]]; then
            printf '  {{YELLOW}}%-20s{{NORMAL}} = %s\n' "$key" "********"
        else
            printf '  %-20s = %s\n' "$key" "$value"
        fi
    done < .env

# Compare two environments
[group('env')]
env-diff env_a env_b:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f ".env.{{env_a}}" ] || [ ! -f ".env.{{env_b}}" ]; then
        printf '{{RED}}Error: one or both env files not found{{NORMAL}}\n'
        exit 1
    fi
    printf '{{BOLD}}Differences between {{env_a}} and {{env_b}}:{{NORMAL}}\n\n'
    diff --side-by-side --suppress-common-lines \
        <(sort .env.{{env_a}}) <(sort .env.{{env_b}}) || true
```

## Step 5 -- Environment Validation

Add validation recipes that check for required variables and valid values.

### `justfile` (append)

```just
# Validate current environment configuration
[group('env')]
env-check:
    #!/usr/bin/env bash
    set -euo pipefail
    ERRORS=0
    printf '{{BOLD}}Validating environment: %s{{NORMAL}}\n\n' "$APP_ENV"

    # Required variables
    REQUIRED_VARS=(APP_ENV APP_PORT DATABASE_URL REDIS_URL SECRET_KEY)
    for var in "${REQUIRED_VARS[@]}"; do
        val="${!var:-}"
        if [ -z "$val" ]; then
            printf '  {{RED}}FAIL{{NORMAL}} %s is not set\n' "$var"
            ERRORS=$((ERRORS + 1))
        else
            printf '  {{GREEN}}OK{{NORMAL}}   %s is set\n' "$var"
        fi
    done

    echo ""

    # Value validations
    if [[ "$APP_PORT" -lt 1 || "$APP_PORT" -gt 65535 ]] 2>/dev/null; then
        printf '  {{RED}}FAIL{{NORMAL}} APP_PORT=%s is not a valid port\n' "$APP_PORT"
        ERRORS=$((ERRORS + 1))
    else
        printf '  {{GREEN}}OK{{NORMAL}}   APP_PORT=%s is valid\n' "$APP_PORT"
    fi

    if [[ "$DATABASE_URL" != postgres://* ]]; then
        printf '  {{RED}}FAIL{{NORMAL}} DATABASE_URL does not start with postgres://\n'
        ERRORS=$((ERRORS + 1))
    else
        printf '  {{GREEN}}OK{{NORMAL}}   DATABASE_URL has valid scheme\n'
    fi

    # Warn about default secrets
    if [[ "$SECRET_KEY" == *"CHANGEME"* || "$SECRET_KEY" == *"not-real"* ]]; then
        printf '\n  {{YELLOW}}WARN{{NORMAL}} SECRET_KEY looks like a placeholder\n'
    fi

    echo ""
    if [ "$ERRORS" -gt 0 ]; then
        printf '{{RED}}{{BOLD}}Validation failed: %d error(s){{NORMAL}}\n' "$ERRORS"
        exit 1
    else
        printf '{{GREEN}}{{BOLD}}All checks passed.{{NORMAL}}\n'
    fi

# Initialize environment from template
[group('env')]
env-init env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f ".env.{{env}}" ]; then
        printf '{{YELLOW}}.env.{{env}} already exists. Skipping.{{NORMAL}}\n'
        exit 0
    fi
    if [ ! -f ".env.template" ]; then
        printf '{{RED}}.env.template not found{{NORMAL}}\n'
        exit 1
    fi
    cp .env.template .env.{{env}}
    printf '{{GREEN}}Created .env.{{env}} from template.{{NORMAL}}\n'
    printf 'Edit it: {{BOLD}}$EDITOR .env.{{env}}{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just env-switch dev
just env-check
```

You should see all checks passing for the dev environment.

## Step 6 -- Pre-Deploy Validation

Add a deploy recipe that validates the environment first.

### `justfile` (append)

```just
# Deploy with environment validation gate
[group('deploy')]
[confirm("Deploy to {{app_env}}? (yes/no)")]
deploy: env-check
    @printf '{{GREEN}}Deploying to %s on port %s...{{NORMAL}}\n' "{{app_env}}" "{{app_port}}"
    @echo "LOG_LEVEL={{log_level}}"
    @echo "(deploy command would go here)"

# Quick environment status for CI
[group('env')]
[private]
_env-export:
    @echo "APP_ENV=$APP_ENV"
    @echo "APP_PORT=$APP_PORT"
    @echo "LOG_LEVEL=$LOG_LEVEL"
```

The `[private]` attribute hides the `_env-export` helper from `just --list`, keeping the interface clean.

## Common Mistakes

### Mistake 1: Committing `.env` Files with Real Secrets

**Wrong:** Including `.env.prod` in version control with real database passwords.

**What happens:** Secrets are exposed in git history, even if the file is later deleted.

**Fix:** Always `.gitignore` all `.env*` files (except `.env.template`). Use a secrets manager for production values and populate `.env.prod` at deployment time.

### Mistake 2: Using `set dotenv-filename` with a Non-Existent File

**Wrong:**

```just
set dotenv-filename := ".env.production"
```

**What happens:** If `.env.production` does not exist, just exits with an error before running any recipe.

**Fix:** Use `set dotenv-load` with the default `.env` file, and manage switching via a recipe that copies the target file to `.env`. Alternatively, use `set dotenv-required := false` if the file is optional.

## Verify What You Learned

```bash
# 1. List available environments
just env-list
# Expected: dev, staging, prod with their APP_ENV values

# 2. Switch environment
just env-switch staging
# Expected: "Switched to staging environment"

# 3. Show environment with redacted secrets
just env-show
# Expected: SECRET_KEY shows ******** instead of actual value

# 4. Validate the environment
just env-check
# Expected: all checks pass with OK status

# 5. Compare two environments
just env-diff dev staging
# Expected: side-by-side diff showing value differences
```

## What's Next

In the next exercise, you will build a Docker Compose workflow justfile that manages container lifecycle, database operations, and developer onboarding.

## Summary

- `set dotenv-load` automatically reads `.env` and makes variables available to all recipes
- `env("VAR")` fails if the variable is missing; `env("VAR", "default")` provides a fallback
- Shebang recipes (`#!/usr/bin/env bash`) enable multi-line validation logic with loops and conditionals
- Environment switching copies the selected `.env.{env}` file to `.env`
- `[private]` hides helper recipes from `just --list`
- Sensitive values should be redacted in display recipes and excluded from version control

## Reference

- [just manual -- dotenv settings](https://just.systems/man/en/dotenv-settings.html)
- [just manual -- env function](https://just.systems/man/en/env.html)
- [just manual -- shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [just manual -- private attribute](https://just.systems/man/en/private.html)

## Additional Resources

- [12-factor app: config](https://12factor.net/config)
- [dotenv specification](https://dotenv.org/docs/security/env)
