# 19. Secrets and Advanced Dotenv

<!--
difficulty: intermediate
concepts:
  - set dotenv-load
  - set dotenv-filename
  - set dotenv-path
  - multi-environment dotenv files
  - required env var validation
  - private recipes for secret handling
  - reading secrets from external sources
  - 1Password CLI integration
  - AWS SSM Parameter Store pattern
tools: [just]
estimated_time: 35 minutes
bloom_level: analyze
prerequisites:
  - just basics (exercises 1-8)
  - environment management (exercise 12)
  - dotenv concepts
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.36 | `just --version` |

Optional tools for secret source integration:
- 1Password CLI (`op`) for vault-based secrets
- AWS CLI (`aws`) for SSM Parameter Store

## Learning Objectives

- **Analyze** the differences between `set dotenv-load`, `set dotenv-filename`, and `set dotenv-path` to choose the correct dotenv strategy
- **Implement** required variable validation that prevents recipe execution when critical configuration is missing
- **Design** a secrets management pattern that reads sensitive values from external sources without storing them in files

## Why Advanced Dotenv and Secrets Management

Basic dotenv usage -- loading a `.env` file and reading variables -- covers development workflows. But production systems need more. Secrets must come from a vault (1Password, HashiCorp Vault, AWS SSM), not from files checked into git. Different environments need different dotenv files loaded automatically. Some variables are mandatory (the app must not start without a `DATABASE_URL`), while others are optional with sensible defaults.

just provides three dotenv-related settings that address different needs. `set dotenv-load` reads the default `.env` file. `set dotenv-filename` changes which filename to look for (e.g., `.env.staging`). `set dotenv-path` specifies an absolute or relative path to a specific file. Understanding when to use each -- and when to combine them with `env()` validation -- is the key to robust configuration management.

This exercise builds a progressive configuration system: from simple dotenv loading to multi-environment support to external secret injection. Each layer adds safety without sacrificing developer convenience.

## Step 1 -- Understanding the Three Dotenv Settings

### Setting Comparison

| Setting | Default | Purpose | File Location |
|---------|---------|---------|---------------|
| `set dotenv-load` | `false` | Enable/disable dotenv loading | Looks for `.env` in justfile directory |
| `set dotenv-filename` | `".env"` | Change the filename to look for | Searches up from working directory |
| `set dotenv-path` | _(none)_ | Specify exact file path | Absolute or relative to justfile |
| `set dotenv-required` | `true` | Error if dotenv file missing | Used with `dotenv-load` |

### Examples

```just
# Basic: load .env from justfile directory
set dotenv-load

# Load a specific filename (searches up directory tree)
set dotenv-load
set dotenv-filename := ".env.local"

# Load from an exact path
set dotenv-load
set dotenv-path := "/etc/myapp/.env"

# Load .env but don't error if missing
set dotenv-load
set dotenv-required := false
```

## Step 2 -- Multi-Environment Dotenv Setup

Create environment files and a justfile that switches between them.

### `.env.dev`

```
APP_ENV=development
APP_PORT=3000
DATABASE_URL=postgres://dev:devpass@localhost:5432/myapp_dev
REDIS_URL=redis://localhost:6379/0
API_KEY=dev-key-not-secret
LOG_LEVEL=debug
```

### `.env.staging`

```
APP_ENV=staging
APP_PORT=8080
DATABASE_URL=postgres://staging:CHANGE_ME@staging-db:5432/myapp_staging
REDIS_URL=redis://staging-redis:6379/0
API_KEY=MUST_COME_FROM_VAULT
LOG_LEVEL=info
```

### `.env.prod`

```
APP_ENV=production
APP_PORT=8080
DATABASE_URL=MUST_COME_FROM_VAULT
REDIS_URL=MUST_COME_FROM_VAULT
API_KEY=MUST_COME_FROM_VAULT
LOG_LEVEL=warn
```

### `.env`

```
# Default environment (development)
APP_ENV=development
APP_PORT=3000
DATABASE_URL=postgres://dev:devpass@localhost:5432/myapp_dev
REDIS_URL=redis://localhost:6379/0
API_KEY=dev-key-not-secret
LOG_LEVEL=debug
```

## Step 3 -- Justfile with Required Variable Validation

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Required variables (just exits immediately if missing)
app_env := env("APP_ENV")
db_url  := env("DATABASE_URL")

# Optional variables with defaults
app_port  := env("APP_PORT", "3000")
log_level := env("LOG_LEVEL", "info")
redis_url := env("REDIS_URL", "redis://localhost:6379/0")

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands and current environment
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Environment:{{NORMAL}} {{app_env}}\n'
    @printf '{{BOLD}}Port:{{NORMAL}} {{app_port}}\n'
    @printf '{{BOLD}}Log level:{{NORMAL}} {{log_level}}\n'
```

The `env("APP_ENV")` call (without a second argument) makes `APP_ENV` a required variable. If it is not set in the dotenv file or the environment, just exits with a clear error message before running any recipe.

**Intermediate Verification:**

```bash
just
```

You should see the recipe list with environment, port, and log level from the `.env` file.

## Step 4 -- Environment Switching with Validation

### `justfile` (append)

```just
# Switch active environment
[group('env')]
env-switch env:
    #!/usr/bin/env bash
    set -euo pipefail
    SOURCE=".env.{{env}}"
    if [ ! -f "$SOURCE" ]; then
        printf '{{RED}}Error: %s not found{{NORMAL}}\n' "$SOURCE"
        printf 'Available: '
        ls -1 .env.* 2>/dev/null | sed 's/\.env\.//' | tr '\n' ' '
        printf '\n'
        exit 1
    fi
    cp "$SOURCE" .env
    printf '{{GREEN}}Switched to {{env}}.{{NORMAL}}\n'

# Validate all required variables are set and non-placeholder
[group('env')]
env-validate:
    #!/usr/bin/env bash
    set -euo pipefail
    ERRORS=0
    printf '{{BOLD}}Validating environment: %s{{NORMAL}}\n\n' "$APP_ENV"

    # Define required variables
    REQUIRED=(APP_ENV APP_PORT DATABASE_URL REDIS_URL API_KEY)

    for var in "${REQUIRED[@]}"; do
        val="${!var:-}"
        if [ -z "$val" ]; then
            printf '  {{RED}}MISSING{{NORMAL}}     %s\n' "$var"
            ERRORS=$((ERRORS + 1))
        elif [[ "$val" == *"MUST_COME_FROM"* || "$val" == *"CHANGE_ME"* || "$val" == *"MUST_BE_SET"* ]]; then
            printf '  {{RED}}PLACEHOLDER{{NORMAL}} %s = %s\n' "$var" "$val"
            ERRORS=$((ERRORS + 1))
        else
            printf '  {{GREEN}}OK{{NORMAL}}          %s\n' "$var"
        fi
    done

    echo ""
    if [ "$ERRORS" -gt 0 ]; then
        printf '{{RED}}{{BOLD}}%d variable(s) need attention.{{NORMAL}}\n' "$ERRORS"
        exit 1
    fi
    printf '{{GREEN}}{{BOLD}}All variables valid.{{NORMAL}}\n'

# Show environment variables (redacts sensitive values)
[group('env')]
env-show:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Active environment: %s{{NORMAL}}\n\n' "$APP_ENV"
    SENSITIVE_PATTERN="(PASSWORD|SECRET|KEY|TOKEN|URL)"
    while IFS='=' read -r key value; do
        [[ "$key" =~ ^#.*$ || -z "$key" ]] && continue
        if [[ "$key" =~ $SENSITIVE_PATTERN ]]; then
            printf '  %-20s = {{YELLOW}}[redacted]{{NORMAL}}\n' "$key"
        else
            printf '  %-20s = %s\n' "$key" "$value"
        fi
    done < .env
```

**Intermediate Verification:**

```bash
just env-validate
```

For the dev environment, all variables should show OK. Switch to prod and validate:

```bash
just env-switch prod
just env-validate
```

The prod environment should fail with PLACEHOLDER errors for DATABASE_URL, REDIS_URL, and API_KEY.

## Step 5 -- Reading Secrets from External Sources

Add recipes that fetch secrets from external vaults and inject them into the environment.

### `justfile` (append)

```just
# --- Secret Sources ---

# Inject secrets from 1Password (requires `op` CLI)
[group('secrets')]
[private]
_secrets-from-1password vault="Development":
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v op &>/dev/null; then
        printf '{{RED}}Error: 1Password CLI (op) not installed{{NORMAL}}\n'
        exit 1
    fi
    printf '{{GREEN}}Fetching secrets from 1Password vault: {{vault}}...{{NORMAL}}\n'

    # Example: read specific secrets
    DB_URL=$(op read "op://{{vault}}/myapp-database/connection-string" 2>/dev/null || echo "")
    API_KEY=$(op read "op://{{vault}}/myapp-api/key" 2>/dev/null || echo "")

    if [ -n "$DB_URL" ]; then
        # Update .env in place (only the secret values)
        sed -i.bak "s|^DATABASE_URL=.*|DATABASE_URL=$DB_URL|" .env
        printf '  {{GREEN}}DATABASE_URL{{NORMAL}} updated from vault\n'
    fi
    if [ -n "$API_KEY" ]; then
        sed -i.bak "s|^API_KEY=.*|API_KEY=$API_KEY|" .env
        printf '  {{GREEN}}API_KEY{{NORMAL}} updated from vault\n'
    fi
    rm -f .env.bak

# Inject secrets from AWS SSM Parameter Store
[group('secrets')]
[private]
_secrets-from-ssm prefix="/myapp/prod":
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v aws &>/dev/null; then
        printf '{{RED}}Error: AWS CLI not installed{{NORMAL}}\n'
        exit 1
    fi
    printf '{{GREEN}}Fetching secrets from SSM: {{prefix}}...{{NORMAL}}\n'

    # Fetch all parameters under the prefix
    PARAMS=$(aws ssm get-parameters-by-path \
        --path "{{prefix}}" \
        --with-decryption \
        --query "Parameters[*].[Name,Value]" \
        --output text 2>/dev/null)

    while IFS=$'\t' read -r name value; do
        # Convert /myapp/prod/DATABASE_URL → DATABASE_URL
        key=$(basename "$name")
        sed -i.bak "s|^${key}=.*|${key}=${value}|" .env
        printf '  {{GREEN}}%s{{NORMAL}} updated from SSM\n' "$key"
    done <<< "$PARAMS"
    rm -f .env.bak

# Inject secrets based on environment
[group('secrets')]
secrets-inject source="auto":
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{source}}" in
        1password|op)
            just _secrets-from-1password
            ;;
        ssm|aws)
            just _secrets-from-ssm "/myapp/$APP_ENV"
            ;;
        auto)
            if command -v op &>/dev/null; then
                printf '{{GREEN}}Detected 1Password CLI{{NORMAL}}\n'
                just _secrets-from-1password
            elif command -v aws &>/dev/null; then
                printf '{{GREEN}}Detected AWS CLI{{NORMAL}}\n'
                just _secrets-from-ssm "/myapp/$APP_ENV"
            else
                printf '{{YELLOW}}No secret source detected. Using .env as-is.{{NORMAL}}\n'
            fi
            ;;
        *)
            printf '{{RED}}Unknown source: {{source}}{{NORMAL}}\n'
            printf 'Options: 1password, ssm, auto\n'
            exit 1
            ;;
    esac
    printf '{{GREEN}}Secrets injected. Run: just env-validate{{NORMAL}}\n'
```

Note that the secret-fetching recipes are `[private]` -- they do not appear in `just --list`. Only `secrets-inject` is public, providing a clean interface.

## Step 6 -- Safe Deploy Pattern

Add a deploy recipe that combines validation and secret injection.

### `justfile` (append)

```just
# Deploy with full validation chain
[group('deploy')]
[confirm("Deploy to {{app_env}}? (yes/no)")]
deploy: env-validate
    #!/usr/bin/env bash
    set -euo pipefail

    # Extra safety: prevent accidental production deploy
    if [ "$APP_ENV" = "production" ]; then
        printf '{{RED}}{{BOLD}}PRODUCTION DEPLOY{{NORMAL}}\n'
        printf 'Current branch: %s\n' "$(git branch --show-current)"
        read -p "Type the environment name to confirm: " CONFIRM
        if [ "$CONFIRM" != "production" ]; then
            printf '{{RED}}Aborted.{{NORMAL}}\n'
            exit 1
        fi
    fi

    printf '{{GREEN}}Deploying to %s...{{NORMAL}}\n' "$APP_ENV"
    echo "(deployment command would go here)"
    printf '{{GREEN}}{{BOLD}}Deployed.{{NORMAL}}\n'

# Full setup: switch env → inject secrets → validate → deploy
[group('deploy')]
deploy-full env="staging":
    just env-switch {{env}}
    just secrets-inject
    just env-validate
    just deploy
```

**Intermediate Verification:**

```bash
just --list --unsorted
```

Private recipes (`_secrets-from-1password`, `_secrets-from-ssm`) should not appear in the list.

## Step 7 -- Dotenv File Security Checklist

Add a recipe that audits dotenv file security.

### `justfile` (append)

```just
# Audit dotenv files for security issues
[group('env')]
env-audit:
    #!/usr/bin/env bash
    set -euo pipefail
    ISSUES=0
    printf '{{BOLD}}Dotenv Security Audit{{NORMAL}}\n\n'

    # Check .gitignore
    if ! grep -q '\.env' .gitignore 2>/dev/null; then
        printf '  {{RED}}FAIL{{NORMAL}} .gitignore does not exclude .env files\n'
        ISSUES=$((ISSUES + 1))
    else
        printf '  {{GREEN}}OK{{NORMAL}}   .gitignore excludes .env files\n'
    fi

    # Check for real secrets in .env files
    for f in .env .env.*; do
        [ ! -f "$f" ] && continue
        [[ "$f" == *.template ]] && continue

        # Check if file is tracked by git
        if git ls-files --error-unmatch "$f" &>/dev/null 2>&1; then
            printf '  {{RED}}FAIL{{NORMAL}} %s is tracked by git!\n' "$f"
            ISSUES=$((ISSUES + 1))
        else
            printf '  {{GREEN}}OK{{NORMAL}}   %s is not tracked by git\n' "$f"
        fi

        # Check file permissions (should not be world-readable)
        PERMS=$(stat -f '%A' "$f" 2>/dev/null || stat -c '%a' "$f" 2>/dev/null || echo "unknown")
        if [ "$PERMS" != "unknown" ] && [ "$PERMS" -gt 644 ] 2>/dev/null; then
            printf '  {{YELLOW}}WARN{{NORMAL}} %s has permissive mode: %s\n' "$f" "$PERMS"
        fi
    done

    echo ""
    if [ "$ISSUES" -gt 0 ]; then
        printf '{{RED}}{{BOLD}}%d issue(s) found.{{NORMAL}}\n' "$ISSUES"
        exit 1
    fi
    printf '{{GREEN}}{{BOLD}}No issues found.{{NORMAL}}\n'
```

## Common Mistakes

### Mistake 1: Using `set dotenv-filename` When You Need `set dotenv-path`

**Wrong:**

```just
set dotenv-load
set dotenv-filename := "config/.env.staging"
```

**What happens:** `dotenv-filename` only accepts a filename, not a path with directories. It searches for that filename starting from the working directory upward.

**Fix:** Use `dotenv-path` for paths that include directories:

```just
set dotenv-load
set dotenv-path := "config/.env.staging"
```

### Mistake 2: Storing Injected Secrets in .env Permanently

**Wrong:** The `secrets-inject` recipe writes real secrets into `.env`, which then persists on disk and may accidentally be committed.

**What happens:** Secrets are written to a file that could be read by other processes, copied in backups, or committed to git.

**Fix:** For production, prefer injecting secrets as environment variables at runtime rather than writing them to files. Use the file-based approach only for development, and always run `just env-audit` before committing:

```just
# For CI/production: set env vars directly
deploy:
    DATABASE_URL=$(op read "op://Production/db/url") \
    API_KEY=$(op read "op://Production/api/key") \
    ./deploy.sh
```

## Verify What You Learned

```bash
# 1. Validate the dev environment
just env-validate
# Expected: all variables show OK

# 2. Switch to prod and validate
just env-switch prod && just env-validate
# Expected: PLACEHOLDER errors for DATABASE_URL, REDIS_URL, API_KEY

# 3. Show redacted environment
just env-switch dev && just env-show
# Expected: sensitive values show [redacted]

# 4. Run security audit
just env-audit
# Expected: .gitignore check, git tracking check, permissions check

# 5. Check that private recipes are hidden
just --list
# Expected: _secrets-from-1password and _secrets-from-ssm are NOT shown
```

## What's Next

In the next exercise, you will build a testing and QA workflow justfile with different test suites, coverage reporting, and a CI-ready quality pipeline.

## Summary

- `set dotenv-load` enables `.env` loading; `set dotenv-filename` changes the filename; `set dotenv-path` specifies an exact path
- `env("VAR")` without a default makes a variable required -- just exits immediately if it is missing
- `[private]` hides secret-handling recipes from `just --list`, exposing only the public `secrets-inject` interface
- Placeholder detection (`MUST_COME_FROM_VAULT`, `CHANGE_ME`) catches unresolved secrets before deployment
- Security audits check `.gitignore`, git tracking, and file permissions
- Production secrets should be injected at runtime, not written to persistent `.env` files

## Reference

- [just manual -- dotenv settings](https://just.systems/man/en/dotenv-settings.html)
- [just manual -- env function](https://just.systems/man/en/env.html)
- [just manual -- private attribute](https://just.systems/man/en/private.html)
- [just manual -- conditional expressions](https://just.systems/man/en/conditional-expressions.html)

## Additional Resources

- [1Password CLI documentation](https://developer.1password.com/docs/cli/)
- [AWS SSM Parameter Store](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-parameter-store.html)
- [dotenv security best practices](https://dotenv.org/docs/security)
