# 25. Full-Stack Development with Hot Reload

<!--
difficulty: advanced
concepts:
  - multi-service orchestration
  - alias for recipe shortcuts
  - confirm for destructive DB operations
  - Docker Compose integration
  - bootstrap patterns
  - sqlx migrations
  - grouped recipes per service
  - CI pipeline recipe
tools: [just, docker, cargo-watch, pnpm, sqlx-cli]
estimated_time: 50 minutes
bloom_level: analyze
prerequisites:
  - just intermediate (dependencies, environment variables)
  - Docker Compose basics
  - familiarity with backend/frontend toolchains
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| docker | 24+ | `docker --version` |
| cargo-watch | 8.0+ | `cargo watch --version` |
| pnpm | 8.0+ | `pnpm --version` |
| sqlx-cli | 0.7+ | `sqlx --version` |

## Learning Objectives

- **Analyze** how recipe grouping and aliases simplify complex multi-service development workflows
- **Design** a bootstrap pattern that brings a developer from zero to running in a single command
- **Evaluate** the tradeoffs between background processes, Docker Compose, and manual service management

## Why Orchestrate Full-Stack Development with Just

A typical full-stack project requires starting a database, running migrations, launching a backend with hot reload, and starting a frontend dev server — all before writing a single line of code. New developers spend hours getting the environment running. A well-structured justfile encodes this tribal knowledge as executable documentation.

The challenge is managing lifecycle: services start in a specific order (database before backend), some run in the foreground (frontend dev server), and others in the background (database container). Resetting the database should require explicit confirmation. The CI pipeline needs to run the same tests locally and in the pipeline. Aliases provide muscle-memory shortcuts for frequent operations.

This exercise builds a justfile for a project with a Rust/axum backend, a TypeScript/React frontend, and a PostgreSQL database managed via Docker Compose. Each service gets its own group of recipes, and a bootstrap command handles first-time setup.

## Step 1 -- Project Configuration and Aliases

```just
# justfile

set dotenv-load
set export

# ─── Configuration ──────────────────────────────────────
project      := "fullstack-app"
db_name      := "app_dev"
db_user      := "postgres"
db_password  := "postgres"
db_port      := "5432"
db_url       := "postgres://" + db_user + ":" + db_password + "@localhost:" + db_port + "/" + db_name
backend_port := "8080"
frontend_port := "3000"

DATABASE_URL := db_url

# ─── Color Constants ───────────────────────────────────
GREEN  := '\033[0;32m'
YELLOW := '\033[1;33m'
BLUE   := '\033[0;34m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# ─── Aliases ───────────────────────────────────────────
alias b := backend-dev
alias f := frontend-dev
alias m := db-migrate
alias s := status
alias t := test-all
```

Aliases map short names to full recipes. `just b` is faster than `just backend-dev` when you run it fifty times a day. Keep aliases for the 3-5 most frequent commands.

## Step 2 -- Bootstrap: Zero to Running

```just
# ─── Bootstrap ─────────────────────────────────────────

# First-time setup: install deps, start DB, run migrations, seed data
bootstrap: _check-tools db-up db-wait db-migrate db-seed backend-deps frontend-deps
    @echo ""
    @echo "{{ GREEN }}{{ BOLD }}Bootstrap complete!{{ NORMAL }}"
    @echo ""
    @echo "  Start backend:  {{ BOLD }}just b{{ NORMAL }}"
    @echo "  Start frontend: {{ BOLD }}just f{{ NORMAL }}"
    @echo "  Run all tests:  {{ BOLD }}just t{{ NORMAL }}"
    @echo ""

# Verify required tools are installed
_check-tools:
    @echo "{{ BLUE }}Checking tools...{{ NORMAL }}"
    {{ require("docker") }}
    {{ require("cargo") }}
    {{ require("cargo-watch") }}
    {{ require("pnpm") }}
    {{ require("sqlx") }}
    @echo "{{ GREEN }}All tools present{{ NORMAL }}"
```

The bootstrap recipe chains every setup step in the correct order. A new developer clones the repo, runs `just bootstrap`, and has a working environment. The `_check-tools` recipe is private — it provides a clear error if anything is missing before wasting time on partial setup.

## Step 3 -- Database Recipes

```just
# ─── Database ──────────────────────────────────────────

# Start PostgreSQL via Docker Compose
db-up:
    @echo "{{ BLUE }}Starting PostgreSQL...{{ NORMAL }}"
    docker compose up -d postgres
    @echo "{{ GREEN }}PostgreSQL running on port {{ db_port }}{{ NORMAL }}"

# Wait for database to accept connections
db-wait:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BLUE }}Waiting for PostgreSQL...{{ NORMAL }}"
    for i in $(seq 1 30); do
        if docker compose exec -T postgres pg_isready -U {{ db_user }} >/dev/null 2>&1; then
            echo "{{ GREEN }}PostgreSQL is ready{{ NORMAL }}"
            exit 0
        fi
        sleep 1
    done
    echo "{{ RED }}PostgreSQL did not become ready in 30 seconds{{ NORMAL }}"
    exit 1

# Run pending database migrations
db-migrate:
    @echo "{{ BLUE }}Running migrations...{{ NORMAL }}"
    sqlx migrate run --source backend/migrations
    @echo "{{ GREEN }}Migrations applied{{ NORMAL }}"

# Create a new migration
db-migration name:
    sqlx migrate add -r {{ name }} --source backend/migrations
    @echo "{{ GREEN }}Created migration: {{ name }}{{ NORMAL }}"

# Seed the database with test data
db-seed:
    @echo "{{ BLUE }}Seeding database...{{ NORMAL }}"
    sqlx database reset --source backend/migrations -y
    @echo "{{ GREEN }}Database seeded{{ NORMAL }}"

# Reset database: drop, recreate, migrate, seed
[confirm("Reset the database? All data will be lost. (yes/no)")]
db-reset: db-down db-up db-wait db-migrate db-seed
    @echo "{{ GREEN }}Database reset complete{{ NORMAL }}"

# Stop PostgreSQL
db-down:
    docker compose down -v
    @echo "{{ YELLOW }}PostgreSQL stopped, volumes removed{{ NORMAL }}"

# Open psql shell
db-shell:
    docker compose exec postgres psql -U {{ db_user }} -d {{ db_name }}

# Show database connection info
db-info:
    @echo "Host:     localhost:{{ db_port }}"
    @echo "Database: {{ db_name }}"
    @echo "User:     {{ db_user }}"
    @echo "URL:      {{ db_url }}"
```

The `[confirm]` on `db-reset` prevents accidental data loss. The `db-wait` recipe uses a retry loop — necessary because `docker compose up -d` returns immediately but the database takes seconds to initialize.

## Step 4 -- Backend Recipes

```just
# ─── Backend ───────────────────────────────────────────

# Install backend dependencies
backend-deps:
    @echo "{{ BLUE }}Installing backend dependencies...{{ NORMAL }}"
    cd backend && cargo fetch
    @echo "{{ GREEN }}Backend dependencies installed{{ NORMAL }}"

# Start backend with hot reload
backend-dev:
    @echo "{{ BLUE }}Starting backend on port {{ backend_port }}...{{ NORMAL }}"
    cd backend && cargo watch -x 'run -- --port {{ backend_port }}'

# Build backend for release
backend-build:
    @echo "{{ BLUE }}Building backend (release)...{{ NORMAL }}"
    cd backend && cargo build --release
    @echo "{{ GREEN }}Backend built{{ NORMAL }}"

# Run backend tests
backend-test:
    @echo "{{ BLUE }}Testing backend...{{ NORMAL }}"
    cd backend && cargo test
    @echo "{{ GREEN }}Backend tests passed{{ NORMAL }}"

# Lint backend
backend-lint:
    @echo "{{ BLUE }}Linting backend...{{ NORMAL }}"
    cd backend && cargo fmt --check && cargo clippy -- -D warnings
    @echo "{{ GREEN }}Backend lint passed{{ NORMAL }}"

# Check sqlx query correctness against the live database
backend-check-queries: db-up db-wait
    cd backend && cargo sqlx prepare --check
    @echo "{{ GREEN }}All SQL queries valid{{ NORMAL }}"
```

`cargo watch -x run` recompiles and restarts the backend on every file change. The `-x` flag specifies the cargo subcommand to run.

## Step 5 -- Frontend Recipes

```just
# ─── Frontend ──────────────────────────────────────────

# Install frontend dependencies
frontend-deps:
    @echo "{{ BLUE }}Installing frontend dependencies...{{ NORMAL }}"
    cd frontend && pnpm install --frozen-lockfile
    @echo "{{ GREEN }}Frontend dependencies installed{{ NORMAL }}"

# Start frontend dev server with HMR
frontend-dev:
    @echo "{{ BLUE }}Starting frontend on port {{ frontend_port }}...{{ NORMAL }}"
    cd frontend && pnpm dev --port {{ frontend_port }}

# Build frontend for production
frontend-build:
    @echo "{{ BLUE }}Building frontend (production)...{{ NORMAL }}"
    cd frontend && pnpm build
    @echo "{{ GREEN }}Frontend built{{ NORMAL }}"

# Run frontend tests
frontend-test:
    @echo "{{ BLUE }}Testing frontend...{{ NORMAL }}"
    cd frontend && pnpm test
    @echo "{{ GREEN }}Frontend tests passed{{ NORMAL }}"

# Lint frontend
frontend-lint:
    @echo "{{ BLUE }}Linting frontend...{{ NORMAL }}"
    cd frontend && pnpm lint
    @echo "{{ GREEN }}Frontend lint passed{{ NORMAL }}"

# Type-check frontend
frontend-typecheck:
    cd frontend && pnpm tsc --noEmit
    @echo "{{ GREEN }}Type check passed{{ NORMAL }}"
```

## Step 6 -- Cross-Cutting Recipes

```just
# ─── All Services ──────────────────────────────────────

# Run all tests
test-all: backend-test frontend-test
    @echo "{{ GREEN }}{{ BOLD }}All tests passed{{ NORMAL }}"

# Lint everything
lint-all: backend-lint frontend-lint
    @echo "{{ GREEN }}{{ BOLD }}All lints passed{{ NORMAL }}"

# Build everything for production
build-all: backend-build frontend-build
    @echo "{{ GREEN }}{{ BOLD }}All services built{{ NORMAL }}"

# Show status of all services
status:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BOLD }}Service Status{{ NORMAL }}"
    echo "───────────────────────────────"
    # Check PostgreSQL
    if docker compose ps --status running 2>/dev/null | grep -q postgres; then
        echo "  PostgreSQL:  {{ GREEN }}running{{ NORMAL }} (port {{ db_port }})"
    else
        echo "  PostgreSQL:  {{ RED }}stopped{{ NORMAL }}"
    fi
    # Check backend
    if lsof -i :{{ backend_port }} >/dev/null 2>&1; then
        echo "  Backend:     {{ GREEN }}running{{ NORMAL }} (port {{ backend_port }})"
    else
        echo "  Backend:     {{ RED }}stopped{{ NORMAL }}"
    fi
    # Check frontend
    if lsof -i :{{ frontend_port }} >/dev/null 2>&1; then
        echo "  Frontend:    {{ GREEN }}running{{ NORMAL }} (port {{ frontend_port }})"
    else
        echo "  Frontend:    {{ RED }}stopped{{ NORMAL }}"
    fi

# Stop everything
stop:
    docker compose down
    @echo "{{ YELLOW }}All services stopped{{ NORMAL }}"

# ─── CI Pipeline ───────────────────────────────────────

# CI: lint, test, build (mirrors CI/CD pipeline)
ci: lint-all test-all build-all
    @echo "{{ GREEN }}{{ BOLD }}CI pipeline passed{{ NORMAL }}"
```

## Common Mistakes

**Wrong: Starting the backend before the database is ready**
```just
dev: db-up backend-dev
```
What happens: `docker compose up -d` returns instantly while PostgreSQL is still initializing. The backend tries to connect and crashes with "connection refused." The developer restarts manually and it works — a flaky experience.
Fix: Insert `db-wait` between `db-up` and any recipe that needs the database. The wait recipe polls until PostgreSQL responds to `pg_isready`.

**Wrong: Using `docker compose up` (foreground) for the database**
```just
db-up:
    docker compose up postgres
```
What happens: The recipe blocks forever showing PostgreSQL logs. The developer cannot run the next recipe without opening a new terminal. Dependencies after `db-up` never execute.
Fix: Always use `docker compose up -d` (detached) for infrastructure services. Use `docker compose logs -f postgres` as a separate recipe if you need to tail logs.

## Verify What You Learned

```bash
# Check all aliases are defined
just --list
# Expected: aliases (b, f, m, s, t) appear alongside full recipe names

# Verify bootstrap checks tools first
just _check-tools
# Expected: "All tools present" or clear error

# Verify database status
just status
# Expected: service status table showing running/stopped

# Verify db-reset requires confirmation
just db-reset
# Expected: "Reset the database? All data will be lost." prompt

# Run CI pipeline
just ci
# Expected: lint → test → build for both backend and frontend
```

## What's Next

The next exercise ([26. Polyglot Shebang Recipes](../06-polyglot-shebang-recipes/06-polyglot-shebang-recipes.md)) explores using multiple programming languages within a single justfile through shebang recipes and the `[script]` attribute.

## Summary

- Bootstrap recipes chain setup steps in dependency order for zero-to-running onboarding
- `alias` maps short names to frequently used recipes (keep to 3-5 aliases)
- `[confirm]` guards destructive database operations (reset, drop)
- Wait-for-ready patterns prevent race conditions between service startup and connections
- Recipe grouping (db-*, backend-*, frontend-*) scales to many services
- `status` recipes give a quick overview of running services
- CI pipeline recipes mirror the real CI/CD pipeline for local testing

## Reference

- [Just Aliases](https://just.systems/man/en/aliases.html)
- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just require() Function](https://just.systems/man/en/functions.html)
- [Just Private Recipes](https://just.systems/man/en/private-recipes.html)

## Additional Resources

- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [cargo-watch Documentation](https://github.com/watchexec/cargo-watch)
- [sqlx-cli Migrations](https://github.com/launchbadge/sqlx/tree/main/sqlx-cli)
