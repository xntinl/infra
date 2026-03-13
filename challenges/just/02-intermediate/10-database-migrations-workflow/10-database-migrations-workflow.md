# 18. Database Migrations Workflow

<!--
difficulty: intermediate
concepts:
  - database connection management
  - migration versioning
  - seed data recipes
  - backup and restore with pg_dump
  - status checking
  - shebang recipes for complex logic
  - confirm for destructive ops
  - environment-based connection strings
tools: [just]
estimated_time: 40 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - SQL and PostgreSQL basics
  - migration concepts (up/down)
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| psql | >= 14 | `psql --version` |
| pg_dump | >= 14 | `pg_dump --version` |

You do not need a running PostgreSQL instance to study the recipes, but you will need one to execute them.

## Learning Objectives

- **Apply** environment-based connection management to safely target different databases from a single justfile
- **Implement** a complete migration lifecycle with versioned SQL files, status tracking, rollback, and seed data
- **Design** backup and restore recipes with safety gates that prevent accidental data loss in production

## Why Database Migration Justfiles

Database migrations are the most dangerous operations in any application lifecycle. A mistyped command can drop tables, corrupt data, or take down a production service. Teams mitigate this risk with migration tools (Flyway, golang-migrate, diesel, sqlx), but the migration tool itself still needs to be invoked correctly -- with the right connection string, the right flags, and the right ordering.

A justfile wraps these invocations with guardrails. Environment variables control the connection string, so `just db-migrate` targets dev while `just db-migrate --set env=prod` (or switching dotenv files) targets production. `[confirm]` prompts prevent accidental destructive operations. Shebang recipes validate preconditions before executing anything.

The workflow in this exercise covers the full database lifecycle: creating migration files, running them up or down, checking status, seeding data, backing up, restoring, and resetting. Each recipe is designed for both local development and CI, with clear error messages and safety checks.

## Step 1 -- Project Structure

Create the directory structure for migrations.

```
project/
  justfile
  .env
  .env.prod
  migrations/
    001_create_users.up.sql
    001_create_users.down.sql
    002_create_posts.up.sql
    002_create_posts.down.sql
  seeds/
    seed.sql
  backups/
    .gitkeep
```

### `.env`

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=app
DB_PASSWORD=apppass
DB_NAME=myapp_dev
DATABASE_URL=postgres://app:apppass@localhost:5432/myapp_dev
```

### `.env.prod`

```
DB_HOST=prod-db.internal
DB_PORT=5432
DB_USER=app_prod
DB_PASSWORD=MUST_BE_SET
DB_NAME=myapp_prod
DATABASE_URL=postgres://app_prod:MUST_BE_SET@prod-db.internal:5432/myapp_prod
```

### `migrations/001_create_users.up.sql`

```sql
CREATE TABLE IF NOT EXISTS users (
    id          SERIAL PRIMARY KEY,
    email       VARCHAR(255) NOT NULL UNIQUE,
    name        VARCHAR(255) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
```

### `migrations/001_create_users.down.sql`

```sql
DROP INDEX IF EXISTS idx_users_email;
DROP TABLE IF EXISTS users;
```

### `migrations/002_create_posts.up.sql`

```sql
CREATE TABLE IF NOT EXISTS posts (
    id          SERIAL PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       VARCHAR(500) NOT NULL,
    body        TEXT NOT NULL,
    published   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_posts_user_id ON posts(user_id);
CREATE INDEX idx_posts_published ON posts(published) WHERE published = TRUE;
```

### `migrations/002_create_posts.down.sql`

```sql
DROP INDEX IF EXISTS idx_posts_published;
DROP INDEX IF EXISTS idx_posts_user_id;
DROP TABLE IF EXISTS posts;
```

### `seeds/seed.sql`

```sql
INSERT INTO users (email, name) VALUES
    ('alice@example.com', 'Alice'),
    ('bob@example.com', 'Bob')
ON CONFLICT (email) DO NOTHING;

INSERT INTO posts (user_id, title, body, published) VALUES
    (1, 'First Post', 'Hello, world!', TRUE),
    (1, 'Draft Post', 'Work in progress...', FALSE),
    (2, 'Bob''s Post', 'Another perspective.', TRUE)
ON CONFLICT DO NOTHING;
```

## Step 2 -- Core Justfile with Connection Management

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Database connection (from .env)
db_url  := env("DATABASE_URL")
db_name := env("DB_NAME", "myapp_dev")
db_host := env("DB_HOST", "localhost")
db_port := env("DB_PORT", "5432")
db_user := env("DB_USER", "app")

# Paths
migrations_dir := "migrations"
seeds_dir      := "seeds"
backups_dir    := "backups"

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Database:{{NORMAL}} {{db_name}}@{{db_host}}:{{db_port}}\n'
```

**Intermediate Verification:**

```bash
just
```

You should see the recipe list and the active database connection info.

## Step 3 -- Connection Validation

Add recipes that verify the database connection before running operations.

### `justfile` (append)

```just
# Check database connectivity
[group('connection')]
ping:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{GREEN}}Checking connection to {{db_name}}@{{db_host}}:{{db_port}}...{{NORMAL}}\n'
    if pg_isready -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
        printf '{{GREEN}}{{BOLD}}Connected.{{NORMAL}}\n'
    else
        printf '{{RED}}{{BOLD}}Connection failed.{{NORMAL}}\n'
        printf '{{RED}}Check DB_HOST, DB_PORT, DB_USER, DB_NAME in .env{{NORMAL}}\n'
        exit 1
    fi

# Show connection details (redacts password)
[group('connection')]
info:
    @printf '{{BOLD}}Host:{{NORMAL}}     {{db_host}}\n'
    @printf '{{BOLD}}Port:{{NORMAL}}     {{db_port}}\n'
    @printf '{{BOLD}}User:{{NORMAL}}     {{db_user}}\n'
    @printf '{{BOLD}}Database:{{NORMAL}} {{db_name}}\n'
    @printf '{{BOLD}}URL:{{NORMAL}}      postgres://{{db_user}}:****@{{db_host}}:{{db_port}}/{{db_name}}\n'

# Open interactive psql session
[group('connection')]
psql *args:
    psql "$DATABASE_URL" {{args}}
```

## Step 4 -- Migration Recipes

Add recipes for running, rolling back, and creating migrations.

### `justfile` (append)

```just
# Run all pending migrations (up)
[group('migrate')]
migrate: ping
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{GREEN}}Running migrations...{{NORMAL}}\n'

    # Find and sort migration files
    UP_FILES=$(find {{migrations_dir}} -name '*.up.sql' | sort)
    if [ -z "$UP_FILES" ]; then
        printf '{{YELLOW}}No migration files found.{{NORMAL}}\n'
        exit 0
    fi

    APPLIED=0
    for file in $UP_FILES; do
        name=$(basename "$file" .up.sql)
        printf '  Applying %s...' "$name"
        psql "$DATABASE_URL" -f "$file" -v ON_ERROR_STOP=1 > /dev/null 2>&1
        printf ' {{GREEN}}done{{NORMAL}}\n'
        APPLIED=$((APPLIED + 1))
    done

    printf '{{GREEN}}{{BOLD}}Applied %d migration(s).{{NORMAL}}\n' "$APPLIED"

# Rollback the last N migrations (default: 1)
[group('migrate')]
[confirm("Roll back migration(s)? (yes/no)")]
rollback n="1": ping
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{YELLOW}}Rolling back {{n}} migration(s)...{{NORMAL}}\n'

    # Find the last N down migration files (reverse sorted)
    DOWN_FILES=$(find {{migrations_dir}} -name '*.down.sql' | sort -r | head -n {{n}})
    if [ -z "$DOWN_FILES" ]; then
        printf '{{YELLOW}}No down migration files found.{{NORMAL}}\n'
        exit 0
    fi

    for file in $DOWN_FILES; do
        name=$(basename "$file" .down.sql)
        printf '  Rolling back %s...' "$name"
        psql "$DATABASE_URL" -f "$file" -v ON_ERROR_STOP=1 > /dev/null 2>&1
        printf ' {{GREEN}}done{{NORMAL}}\n'
    done

    printf '{{YELLOW}}Rollback complete.{{NORMAL}}\n'

# Create a new migration file pair
[group('migrate')]
create name:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{migrations_dir}}

    # Find the next sequence number
    LAST=$(find {{migrations_dir}} -name '*.up.sql' | sort | tail -1 | grep -oP '^\w+/\K\d+' || echo "000")
    NEXT=$(printf "%03d" $((10#$LAST + 1)))

    UP_FILE="{{migrations_dir}}/${NEXT}_{{name}}.up.sql"
    DOWN_FILE="{{migrations_dir}}/${NEXT}_{{name}}.down.sql"

    cat > "$UP_FILE" << 'SQLEOF'
-- Migration: {{name}} (UP)
-- Created: $(date -u +%Y-%m-%dT%H:%M:%SZ)

SQLEOF

    cat > "$DOWN_FILE" << 'SQLEOF'
-- Migration: {{name}} (DOWN)
-- Created: $(date -u +%Y-%m-%dT%H:%M:%SZ)

SQLEOF

    printf '{{GREEN}}Created:{{NORMAL}}\n'
    printf '  %s\n' "$UP_FILE"
    printf '  %s\n' "$DOWN_FILE"

# List all migration files
[group('migrate')]
list:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Migrations:{{NORMAL}}\n\n'
    for f in $(find {{migrations_dir}} -name '*.up.sql' | sort); do
        name=$(basename "$f" .up.sql)
        down="{{migrations_dir}}/${name}.down.sql"
        if [ -f "$down" ]; then
            printf '  {{GREEN}}UP/DOWN{{NORMAL}} %s\n' "$name"
        else
            printf '  {{YELLOW}}UP only{{NORMAL}} %s\n' "$name"
        fi
    done
```

**Intermediate Verification:**

```bash
just list
```

You should see the two migration pairs listed with UP/DOWN status.

## Step 5 -- Seed Data Recipes

Add recipes for seeding the database.

### `justfile` (append)

```just
# Load seed data
[group('data')]
seed: ping
    @printf '{{GREEN}}Loading seed data...{{NORMAL}}\n'
    psql "$DATABASE_URL" -f {{seeds_dir}}/seed.sql -v ON_ERROR_STOP=1
    @printf '{{GREEN}}Seed data loaded.{{NORMAL}}\n'

# Show row counts for all tables
[group('data')]
counts: ping
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Row counts:{{NORMAL}}\n\n'
    TABLES=$(psql "$DATABASE_URL" -t -A -c "SELECT tablename FROM pg_tables WHERE schemaname='public'" 2>/dev/null)
    for table in $TABLES; do
        count=$(psql "$DATABASE_URL" -t -A -c "SELECT COUNT(*) FROM $table" 2>/dev/null)
        printf '  %-30s %s rows\n' "$table" "$count"
    done
```

## Step 6 -- Backup and Restore Recipes

Add recipes for safe backup and restore operations.

### `justfile` (append)

```just
# Create a database backup
[group('backup')]
backup name="backup": ping
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{backups_dir}}
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    FILE="{{backups_dir}}/{{name}}_${TIMESTAMP}.sql.gz"

    printf '{{GREEN}}Backing up {{db_name}} to %s...{{NORMAL}}\n' "$FILE"
    pg_dump "$DATABASE_URL" --no-owner --no-acl | gzip > "$FILE"

    SIZE=$(du -h "$FILE" | cut -f1)
    printf '{{GREEN}}{{BOLD}}Backup complete: %s (%s){{NORMAL}}\n' "$FILE" "$SIZE"

# List available backups
[group('backup')]
backup-list:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{BOLD}}Available backups:{{NORMAL}}\n\n'
    if ls {{backups_dir}}/*.sql* 1>/dev/null 2>&1; then
        for f in $(ls -t {{backups_dir}}/*.sql* 2>/dev/null); do
            size=$(du -h "$f" | cut -f1)
            printf '  %s  (%s)\n' "$f" "$size"
        done
    else
        printf '  {{YELLOW}}No backups found.{{NORMAL}}\n'
    fi

# Restore database from backup
[group('backup')]
[confirm("This will OVERWRITE {{db_name}}. Continue? (yes/no)")]
restore file: ping
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f "{{file}}" ]; then
        printf '{{RED}}File not found: {{file}}{{NORMAL}}\n'
        exit 1
    fi

    printf '{{YELLOW}}Restoring {{db_name}} from {{file}}...{{NORMAL}}\n'

    # Detect gzipped vs plain SQL
    if [[ "{{file}}" == *.gz ]]; then
        gunzip -c "{{file}}" | psql "$DATABASE_URL" -v ON_ERROR_STOP=1 > /dev/null
    else
        psql "$DATABASE_URL" -f "{{file}}" -v ON_ERROR_STOP=1 > /dev/null
    fi

    printf '{{GREEN}}{{BOLD}}Restore complete.{{NORMAL}}\n'
```

## Step 7 -- Reset and Full Lifecycle

Add a reset recipe and a full lifecycle recipe.

### `justfile` (append)

```just
# Reset database: drop → create → migrate → seed
[group('danger')]
[confirm("DESTROY {{db_name}} and recreate from scratch? (yes/no)")]
reset: ping
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{RED}}{{BOLD}}Resetting {{db_name}}...{{NORMAL}}\n\n'

    # Drop and recreate
    printf '  Dropping database...\n'
    psql "postgres://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/postgres" \
        -c "DROP DATABASE IF EXISTS $DB_NAME" > /dev/null

    printf '  Creating database...\n'
    psql "postgres://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/postgres" \
        -c "CREATE DATABASE $DB_NAME OWNER $DB_USER" > /dev/null

    printf '{{GREEN}}  Database recreated.{{NORMAL}}\n\n'

    # Run migrations
    printf '  Running migrations...\n'
    just migrate

    # Seed data
    printf '  Loading seed data...\n'
    just seed

    printf '\n{{GREEN}}{{BOLD}}Reset complete. {{db_name}} is fresh.{{NORMAL}}\n'

# Pre-deploy check: validate migrations can run cleanly
[group('ci')]
validate: ping
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{GREEN}}Validating migrations...{{NORMAL}}\n'

    # Check all up files have matching down files
    ERRORS=0
    for f in $(find {{migrations_dir}} -name '*.up.sql' | sort); do
        name=$(basename "$f" .up.sql)
        down="{{migrations_dir}}/${name}.down.sql"
        if [ ! -f "$down" ]; then
            printf '  {{RED}}MISSING DOWN{{NORMAL}} %s\n' "$name"
            ERRORS=$((ERRORS + 1))
        fi
    done

    # Check SQL syntax (basic validation)
    for f in $(find {{migrations_dir}} -name '*.sql' | sort); do
        if ! grep -qiE '(CREATE|ALTER|DROP|INSERT|UPDATE|DELETE|SELECT)' "$f"; then
            printf '  {{YELLOW}}WARN: %s may be empty{{NORMAL}}\n' "$(basename $f)"
        fi
    done

    if [ "$ERRORS" -gt 0 ]; then
        printf '{{RED}}Validation failed: %d error(s){{NORMAL}}\n' "$ERRORS"
        exit 1
    fi
    printf '{{GREEN}}{{BOLD}}All migrations valid.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just --list --unsorted
```

You should see recipes organized under connection, migrate, data, backup, danger, and ci groups.

## Common Mistakes

### Mistake 1: Not Using `-v ON_ERROR_STOP=1` with psql

**Wrong:**

```just
migrate:
    psql "$DATABASE_URL" -f migrations/001_create_users.up.sql
```

**What happens:** If the SQL has an error, psql continues executing subsequent statements. Partial migrations leave the database in an inconsistent state.

**Fix:** Always include `-v ON_ERROR_STOP=1`:

```just
migrate:
    psql "$DATABASE_URL" -f migrations/001_create_users.up.sql -v ON_ERROR_STOP=1
```

### Mistake 2: Using the Same Connection for Drop/Create as for Queries

**Wrong:**

```just
reset:
    psql "$DATABASE_URL" -c "DROP DATABASE $DB_NAME"
```

**What happens:** You cannot drop a database you are connected to. The command fails with "cannot drop the currently open database".

**Fix:** Connect to the `postgres` maintenance database for DDL operations:

```just
reset:
    psql "postgres://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/postgres" \
        -c "DROP DATABASE IF EXISTS $DB_NAME"
```

## Verify What You Learned

```bash
# 1. Show connection info (with redacted password)
just info
# Expected: host, port, user, database, URL with ****

# 2. List migration files
just list
# Expected: 001_create_users (UP/DOWN), 002_create_posts (UP/DOWN)

# 3. Create a new migration
just create add_comments
# Expected: 003_add_comments.up.sql and .down.sql created

# 4. Validate migrations
just validate
# Expected: all migrations valid (assuming down files exist)

# 5. List backups
just backup-list
# Expected: list of backups or "No backups found."
```

## What's Next

In the next exercise, you will learn advanced dotenv patterns including multi-file strategies, required variable validation, and reading secrets from external sources.

## Summary

- Environment variables (`DATABASE_URL`, `DB_HOST`, etc.) control which database recipes target
- `[confirm]` prompts protect destructive operations: rollback, restore, reset
- `-v ON_ERROR_STOP=1` ensures psql stops on first error, preventing partial migrations
- Drop/create operations must connect to the `postgres` maintenance database, not the target database
- Compressed backups (`gzip`/`gunzip`) save storage; the restore recipe auto-detects format
- Validation recipes check for missing down-migration files before deployment

## Reference

- [just manual -- shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [just manual -- confirm attribute](https://just.systems/man/en/confirm.html)
- [just manual -- env function](https://just.systems/man/en/env.html)
- [just manual -- groups](https://just.systems/man/en/groups.html)

## Additional Resources

- [PostgreSQL pg_dump documentation](https://www.postgresql.org/docs/current/app-pgdump.html)
- [Database migration best practices](https://www.postgresql.org/docs/current/ddl.html)
- [golang-migrate tool](https://github.com/golang-migrate/migrate)
