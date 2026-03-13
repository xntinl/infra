# 13. Docker Compose Workflow

<!--
difficulty: intermediate
concepts:
  - Docker Compose lifecycle management
  - variadic recipe arguments
  - backtick git SHA capture
  - confirm attribute for destructive ops
  - bootstrap recipe for onboarding
  - database backup and restore
  - grouped recipe organization
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - Docker and Docker Compose familiarity
  - basic database concepts
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| docker | >= 20.0 | `docker --version` |
| docker compose | >= 2.0 | `docker compose version` |

## Learning Objectives

- **Apply** variadic arguments (`*args`) to create flexible Docker Compose wrapper recipes that pass through arbitrary flags
- **Implement** a complete container lifecycle management system with build, run, teardown, and database operations
- **Design** a bootstrap recipe that automates new developer onboarding in a single command

## Why Docker Compose Justfiles

Docker Compose commands are verbose. `docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build` is a typical invocation, and nobody wants to type that twice. Teams create shell aliases, but those are invisible and machine-specific. A justfile makes these commands discoverable, documented, and version-controlled.

The lifecycle of a containerized development environment spans more than `up` and `down`. Developers need to view logs, shell into containers, rebuild images after dependency changes, back up and restore databases, and occasionally nuke everything to start fresh. Each operation has its own flags and footguns.

A bootstrap recipe is particularly valuable. When a new developer clones the repository, `just bootstrap` should take them from zero to a running development environment. This recipe chains together dependency checks, image builds, database migrations, and seed data -- eliminating the "it works on my machine" problem at the source.

## Step 1 -- Project Structure

Create the Docker Compose files and project structure.

### `docker-compose.yml`

```yaml
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "${APP_PORT:-3000}:3000"
    environment:
      - DATABASE_URL=postgres://app:apppass@db:5432/myapp
      - REDIS_URL=redis://redis:6379/0
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_USER: app
      POSTGRES_PASSWORD: apppass
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app -d myapp"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redisdata:/data

volumes:
  pgdata:
  redisdata:
```

### `.env`

```
APP_PORT=3000
COMPOSE_PROJECT_NAME=myapp
```

## Step 2 -- Core Justfile with Settings and Variables

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

# Git metadata
git_sha := `git rev-parse --short HEAD 2>/dev/null || echo "dev"`

# Docker Compose command (supports v1 and v2)
dc := "docker compose"

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands
default:
    @just --list --unsorted
```

## Step 3 -- Container Lifecycle Recipes

Add recipes for starting, stopping, and managing containers.

### `justfile` (append)

```just
# Start all services in the background
[group('compose')]
up *args:
    @printf '{{GREEN}}Starting services...{{NORMAL}}\n'
    {{dc}} up -d {{args}}

# Stop all services
[group('compose')]
down *args:
    @printf '{{YELLOW}}Stopping services...{{NORMAL}}\n'
    {{dc}} down {{args}}

# Restart all services (or specific ones)
[group('compose')]
restart *services:
    {{dc}} restart {{services}}

# Stop all services and remove volumes
[group('compose')]
[confirm("This will destroy all data. Continue? (yes/no)")]
nuke:
    @printf '{{RED}}Destroying all services and volumes...{{NORMAL}}\n'
    {{dc}} down -v --remove-orphans
    @printf '{{GREEN}}Clean slate.{{NORMAL}}\n'

# Show service status
[group('compose')]
ps:
    {{dc}} ps --format "table {{{{.Name}}}}\t{{{{.Status}}}}\t{{{{.Ports}}}}"

# Show resource usage
[group('compose')]
stats:
    docker stats --no-stream --format "table {{{{.Name}}}}\t{{{{.CPUPerc}}}}\t{{{{.MemUsage}}}}"
```

Note the double-brace escaping: `{{{{.Name}}}}` is required because just uses `{{` for its own variable interpolation. The outer `{{` and `}}` escape to produce literal `{{` and `}}` that Docker's Go template syntax expects.

**Intermediate Verification:**

```bash
just ps
```

You should see the status table (or a message that no services are running).

## Step 4 -- Build and Image Recipes

Add recipes for building and tagging images.

### `justfile` (append)

```just
# Build images (with git SHA tag)
[group('build')]
build *args:
    @printf '{{GREEN}}Building images ({{git_sha}})...{{NORMAL}}\n'
    {{dc}} build {{args}}

# Build with no cache
[group('build')]
build-fresh:
    @printf '{{YELLOW}}Building images from scratch...{{NORMAL}}\n'
    {{dc}} build --no-cache

# Pull latest base images
[group('build')]
pull:
    {{dc}} pull
```

## Step 5 -- Log and Shell Access

Add recipes for viewing logs and accessing container shells.

### `justfile` (append)

```just
# Follow logs (all services or specific ones)
[group('debug')]
logs *services:
    {{dc}} logs -f --tail=100 {{services}}

# Show last N lines of logs
[group('debug')]
logs-tail n="50" *services:
    {{dc}} logs --tail={{n}} {{services}}

# Open a shell in a running service
[group('debug')]
shell service="app" shell_cmd="sh":
    {{dc}} exec {{service}} {{shell_cmd}}

# Open a psql session
[group('debug')]
psql:
    {{dc}} exec db psql -U app -d myapp

# Open a Redis CLI session
[group('debug')]
redis-cli:
    {{dc}} exec redis redis-cli
```

**Intermediate Verification:**

```bash
just --show shell
```

You should see the recipe with its default arguments: `service="app"` and `shell_cmd="sh"`.

## Step 6 -- Database Operations

Add recipes for database backup, restore, and migrations.

### `justfile` (append)

```just
# Create a database backup
[group('database')]
db-backup name="backup":
    #!/usr/bin/env bash
    set -euo pipefail
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    BACKUP_FILE="backups/{{name}}_${TIMESTAMP}.sql"
    mkdir -p backups
    printf '{{GREEN}}Creating backup: %s{{NORMAL}}\n' "$BACKUP_FILE"
    {{dc}} exec -T db pg_dump -U app -d myapp > "$BACKUP_FILE"
    printf '{{GREEN}}Backup complete: %s (%s){{NORMAL}}\n' \
        "$BACKUP_FILE" "$(du -h "$BACKUP_FILE" | cut -f1)"

# Restore a database backup
[group('database')]
[confirm("This will overwrite the current database. Continue? (yes/no)")]
db-restore file:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f "{{file}}" ]; then
        printf '{{RED}}Backup file not found: {{file}}{{NORMAL}}\n'
        exit 1
    fi
    printf '{{YELLOW}}Restoring from {{file}}...{{NORMAL}}\n'
    {{dc}} exec -T db psql -U app -d myapp < "{{file}}"
    printf '{{GREEN}}Restore complete.{{NORMAL}}\n'

# Run database migrations
[group('database')]
db-migrate:
    @printf '{{GREEN}}Running migrations...{{NORMAL}}\n'
    {{dc}} exec app sh -c 'cd /app && ./migrate up'

# Seed the database with sample data
[group('database')]
db-seed:
    @printf '{{GREEN}}Seeding database...{{NORMAL}}\n'
    {{dc}} exec -T db psql -U app -d myapp < seeds/seed.sql

# Reset database (drop + recreate + migrate + seed)
[group('database')]
[confirm("This will destroy all database data. Continue? (yes/no)")]
db-reset:
    @printf '{{RED}}Resetting database...{{NORMAL}}\n'
    {{dc}} exec -T db psql -U app -c "DROP DATABASE IF EXISTS myapp"
    {{dc}} exec -T db psql -U app -c "CREATE DATABASE myapp OWNER app"
    @printf '{{GREEN}}Database recreated.{{NORMAL}}\n'
```

## Step 7 -- Bootstrap and Health Check

Add the bootstrap recipe that chains everything for new developer onboarding.

### `justfile` (append)

```just
# Bootstrap the entire development environment from scratch
[group('setup')]
bootstrap:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}{{GREEN}}Bootstrapping development environment...{{NORMAL}}\n\n'

    # Step 1: Check dependencies
    printf '{{BOLD}}[1/5] Checking dependencies...{{NORMAL}}\n'
    for cmd in docker git; do
        if ! command -v "$cmd" &>/dev/null; then
            printf '{{RED}}Error: %s not found. Please install it.{{NORMAL}}\n' "$cmd"
            exit 1
        fi
    done
    if ! docker compose version &>/dev/null; then
        printf '{{RED}}Error: docker compose v2 not found.{{NORMAL}}\n'
        exit 1
    fi
    printf '  {{GREEN}}All dependencies present.{{NORMAL}}\n\n'

    # Step 2: Create env file if missing
    printf '{{BOLD}}[2/5] Checking environment...{{NORMAL}}\n'
    if [ ! -f .env ]; then
        cp .env.template .env 2>/dev/null || echo "APP_PORT=3000" > .env
        printf '  Created .env from template\n'
    else
        printf '  .env already exists\n'
    fi
    echo ""

    # Step 3: Build images
    printf '{{BOLD}}[3/5] Building images...{{NORMAL}}\n'
    {{dc}} build
    echo ""

    # Step 4: Start services
    printf '{{BOLD}}[4/5] Starting services...{{NORMAL}}\n'
    {{dc}} up -d
    echo ""

    # Step 5: Wait for healthy database
    printf '{{BOLD}}[5/5] Waiting for database...{{NORMAL}}\n'
    RETRIES=30
    until {{dc}} exec -T db pg_isready -U app -d myapp &>/dev/null; do
        RETRIES=$((RETRIES - 1))
        if [ "$RETRIES" -le 0 ]; then
            printf '{{RED}}Database failed to start.{{NORMAL}}\n'
            exit 1
        fi
        printf '  Waiting...\n'
        sleep 2
    done
    printf '  {{GREEN}}Database is ready.{{NORMAL}}\n\n'

    printf '{{BOLD}}{{GREEN}}Bootstrap complete!{{NORMAL}}\n'
    printf 'App running at: http://localhost:%s\n' "${APP_PORT:-3000}"
    printf 'Run {{BOLD}}just logs{{NORMAL}} to see output.\n'

# Check health of all services
[group('setup')]
health:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Service health:{{NORMAL}}\n\n'
    SERVICES=$({{dc}} ps --format json 2>/dev/null | jq -r '.Name' 2>/dev/null || {{dc}} ps --services)
    for svc in $SERVICES; do
        STATUS=$({{dc}} ps --format json 2>/dev/null | jq -r "select(.Name==\"$svc\") | .Status" 2>/dev/null || echo "unknown")
        if [[ "$STATUS" == *"Up"* ]]; then
            printf '  {{GREEN}}UP{{NORMAL}}   %s (%s)\n' "$svc" "$STATUS"
        else
            printf '  {{RED}}DOWN{{NORMAL}} %s (%s)\n' "$svc" "$STATUS"
        fi
    done
```

**Intermediate Verification:**

```bash
just --list --unsorted
```

You should see recipes organized under compose, build, debug, database, and setup groups.

## Common Mistakes

### Mistake 1: Forgetting `-T` for Non-Interactive Exec

**Wrong:**

```just
db-backup:
    {{dc}} exec db pg_dump -U app -d myapp > backup.sql
```

**What happens:** In CI or when piping output, `docker compose exec` allocates a TTY by default, which corrupts the backup file with terminal escape codes.

**Fix:** Use `-T` to disable TTY allocation:

```just
db-backup:
    {{dc}} exec -T db pg_dump -U app -d myapp > backup.sql
```

### Mistake 2: Not Escaping Docker Go Template Braces

**Wrong:**

```just
ps:
    {{dc}} ps --format "table {{.Name}}\t{{.Status}}"
```

**What happens:** just interprets `{{.Name}}` as a variable reference and fails with "variable `.Name` not defined".

**Fix:** Double the braces to escape them:

```just
ps:
    {{dc}} ps --format "table {{{{.Name}}}}\t{{{{.Status}}}}"
```

## Verify What You Learned

```bash
# 1. List all recipes by group
just --list --unsorted
# Expected: recipes organized under compose, build, debug, database, setup

# 2. Check the variadic args on the up recipe
just --show up
# Expected: recipe signature shows *args

# 3. View the bootstrap recipe structure
just --show bootstrap
# Expected: shebang recipe with 5-step onboarding flow

# 4. Confirm destructive ops have [confirm]
just --show nuke
# Expected: [confirm] attribute present

# 5. Show database backup recipe
just --show db-backup
# Expected: recipe with name="backup" default and timestamped output
```

## What's Next

In the next exercise, you will learn how to integrate just with GitHub Actions, creating a CI/CD pipeline that uses the same justfile locally and in CI.

## Summary

- Variadic arguments (`*args`) let Docker Compose wrapper recipes pass through arbitrary flags
- `[confirm]` protects destructive operations like `nuke`, `db-restore`, and `db-reset`
- Database backup recipes use `-T` flag to disable TTY and prevent output corruption
- Docker Go template braces must be double-escaped (`{{{{.Name}}}}`) inside justfiles
- A bootstrap recipe automates new developer onboarding with dependency checks, image builds, and health waiting
- Shebang recipes enable multi-step logic with loops, conditionals, and retry patterns

## Reference

- [just manual -- variadic parameters](https://just.systems/man/en/recipe-parameters.html)
- [just manual -- confirm attribute](https://just.systems/man/en/confirm.html)
- [just manual -- shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [Docker Compose CLI reference](https://docs.docker.com/compose/reference/)

## Additional Resources

- [Docker Compose healthcheck patterns](https://docs.docker.com/compose/compose-file/05-services/#healthcheck)
- [pg_dump best practices](https://www.postgresql.org/docs/current/app-pgdump.html)
