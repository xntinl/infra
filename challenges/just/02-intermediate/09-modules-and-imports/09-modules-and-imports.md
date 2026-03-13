# 17. Modules and Imports

<!--
difficulty: intermediate
concepts:
  - mod keyword
  - import keyword
  - namespaced recipes
  - module file resolution
  - doc and group in modules
  - mod vs import decision
  - multi-module project structure
tools: [just]
estimated_time: 40 minutes
bloom_level: analyze
prerequisites:
  - just basics (exercises 1-8)
  - understanding of recipe dependencies
  - grouped recipes
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.19 | `just --version` |

The `mod` keyword requires just 1.19 or later. The `import` keyword requires just 1.20 or later.

## Learning Objectives

- **Analyze** the difference between `mod` (namespaced subcommands) and `import` (flat merging) to choose the right approach for each use case
- **Implement** a multi-module project with three separate module files that organize recipes by domain
- **Design** a module structure that balances discoverability, reusability, and maintainability for growing projects

## Why Modules and Imports

As a project grows, a single justfile can reach hundreds of lines. Database recipes sit next to Docker recipes, which sit next to deployment recipes. Finding a specific recipe requires scrolling or grepping. The `[group]` attribute helps with `just --list`, but the file itself remains a monolith.

just's module system solves this with two complementary mechanisms. The `mod` keyword creates namespaced subcommands: `just db migrate` runs the `migrate` recipe from the `db` module. The `import` keyword merges another file's recipes into the current namespace: imported recipes appear as if they were defined in the main justfile.

The distinction matters. Modules (`mod`) create boundaries -- `just docker build` is clearly a Docker operation. Imports (`import`) share utility recipes across files without forcing callers to know which file defined them. A well-designed project uses both: modules for domain boundaries, imports for shared helpers.

## Step 1 -- Project Layout

Create a project with multiple module files.

```
project/
  justfile          # Root justfile with mod/import declarations
  docker.just       # Docker module (namespaced)
  db.just           # Database module (namespaced)
  shared.just       # Shared utilities (imported, flat)
  .env
```

### `.env`

```
APP_NAME=myapp
APP_ENV=development
DATABASE_URL=postgres://app:apppass@localhost:5432/myapp
```

## Step 2 -- Shared Utilities (import)

Create the shared utilities file that will be imported (merged) into the main namespace.

### `shared.just`

```just
# Color constants used across all modules
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Print a section header
[private]
[no-cd]
_header message:
    @printf '{{BOLD}}=== %s ==={{NORMAL}}\n' "{{message}}"

# Print a success message
[private]
[no-cd]
_success message:
    @printf '{{GREEN}}%s{{NORMAL}}\n' "{{message}}"

# Print a warning message
[private]
[no-cd]
_warning message:
    @printf '{{YELLOW}}%s{{NORMAL}}\n' "{{message}}"

# Check that a command exists
[private]
[no-cd]
_require cmd:
    #!/usr/bin/env bash
    if ! command -v {{cmd}} &>/dev/null; then
        printf '{{RED}}Error: {{cmd}} is not installed{{NORMAL}}\n'
        exit 1
    fi
```

Key points:
- `[private]` hides these helper recipes from `just --list`
- `[no-cd]` ensures the recipe runs in the caller's directory, not the file's directory
- These recipes will appear in the root namespace after `import`

**Intermediate Verification:**

Create the file and confirm it is valid:

```bash
just --justfile shared.just --list
```

You should see the private recipes (they appear with `--list` when that file is the root).

## Step 3 -- Docker Module (mod)

Create the Docker module with namespaced recipes.

### `docker.just`

```just
set shell := ["bash", "-euo", "pipefail", "-c"]

# Docker variables
git_sha   := `git rev-parse --short HEAD 2>/dev/null || echo "dev"`
image     := env("APP_NAME", "myapp")
registry  := "ghcr.io/myorg"

# Build Docker image
[group('lifecycle')]
[doc("Build the application Docker image with git SHA tag")]
build tag=git_sha:
    @printf '{{GREEN}}Building {{image}}:{{tag}}...{{NORMAL}}\n'
    docker build -t {{image}}:{{tag}} -t {{image}}:latest .

# Run Docker image
[group('lifecycle')]
[doc("Run the application container on port 3000")]
run tag=git_sha: build
    docker run --rm -p 3000:3000 {{image}}:{{tag}}

# Stop all project containers
[group('lifecycle')]
[doc("Stop and remove all project containers")]
stop:
    docker ps -q --filter "ancestor={{image}}" | xargs -r docker stop

# Push image to registry
[group('registry')]
[doc("Push image to container registry")]
[confirm("Push {{image}} to {{registry}}? (yes/no)")]
push tag=git_sha: build
    docker tag {{image}}:{{tag}} {{registry}}/{{image}}:{{tag}}
    docker push {{registry}}/{{image}}:{{tag}}
    @printf '{{GREEN}}Pushed {{registry}}/{{image}}:{{tag}}{{NORMAL}}\n'

# List project images
[group('info')]
[doc("List all local images for this project")]
images:
    docker images {{image}} --format "table {{{{.Repository}}}}\t{{{{.Tag}}}}\t{{{{.Size}}}}\t{{{{.CreatedAt}}}}"

# Remove all project images
[group('lifecycle')]
[doc("Remove all local images for this project")]
[confirm("Remove all {{image}} images? (yes/no)")]
prune:
    docker images {{image}} -q | xargs -r docker rmi -f
    @printf '{{GREEN}}Pruned all {{image}} images.{{NORMAL}}\n'
```

Note the `[doc()]` attribute on each recipe. When used inside a module, this text appears in `just --list --list-submodules`, making module recipes self-documenting.

## Step 4 -- Database Module (mod)

Create the database module.

### `db.just`

```just
set shell := ["bash", "-euo", "pipefail", "-c"]

# Database connection
db_url := env("DATABASE_URL", "postgres://app:apppass@localhost:5432/myapp")

# Run database migrations
[group('migrations')]
[doc("Run all pending migrations")]
migrate:
    @printf '{{GREEN}}Running migrations...{{NORMAL}}\n'
    # Example: using golang-migrate, diesel, or sqlx
    echo "migrate -database \"$DATABASE_URL\" -path migrations up"

# Rollback last migration
[group('migrations')]
[doc("Roll back the last migration")]
[confirm("Roll back the last migration? (yes/no)")]
rollback:
    @printf '{{YELLOW}}Rolling back last migration...{{NORMAL}}\n'
    echo "migrate -database \"$DATABASE_URL\" -path migrations down 1"

# Create a new migration file
[group('migrations')]
[doc("Create a new named migration file pair")]
create name:
    @printf '{{GREEN}}Creating migration: {{name}}{{NORMAL}}\n'
    mkdir -p migrations
    @TIMESTAMP=$(date +%Y%m%d%H%M%S) && \
        touch "migrations/${TIMESTAMP}_{{name}}.up.sql" && \
        touch "migrations/${TIMESTAMP}_{{name}}.down.sql" && \
        printf '{{GREEN}}Created migrations/%s_{{name}}.{up,down}.sql{{NORMAL}}\n' "$TIMESTAMP"

# Show migration status
[group('info')]
[doc("Show which migrations have been applied")]
status:
    @printf '{{BOLD}}Migration status:{{NORMAL}}\n'
    echo "migrate -database \"$DATABASE_URL\" -path migrations version"

# Open a psql session
[group('tools')]
[doc("Open interactive psql session")]
psql:
    psql "$DATABASE_URL"

# Dump database to file
[group('tools')]
[doc("Create a SQL dump of the database")]
dump name="backup":
    #!/usr/bin/env bash
    set -euo pipefail
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    FILE="backups/{{name}}_${TIMESTAMP}.sql"
    mkdir -p backups
    pg_dump "$DATABASE_URL" > "$FILE"
    printf '{{GREEN}}Dump saved: %s{{NORMAL}}\n' "$FILE"

# Restore database from file
[group('tools')]
[doc("Restore database from a SQL dump file")]
[confirm("This will overwrite the database. Continue? (yes/no)")]
restore file:
    @printf '{{YELLOW}}Restoring from {{file}}...{{NORMAL}}\n'
    psql "$DATABASE_URL" < "{{file}}"
    @printf '{{GREEN}}Restore complete.{{NORMAL}}\n'

# Reset database (drop, create, migrate)
[group('tools')]
[doc("Destroy and recreate the database from scratch")]
[confirm("This will DESTROY ALL DATA. Continue? (yes/no)")]
reset:
    @printf '{{RED}}Resetting database...{{NORMAL}}\n'
    dropdb --if-exists myapp
    createdb myapp
    @printf '{{GREEN}}Database recreated. Run: just db migrate{{NORMAL}}\n'
```

## Step 5 -- Root Justfile with mod and import

Now wire everything together in the root justfile.

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Import shared utilities (merged into root namespace)
import 'shared.just'

# Declare modules (namespaced subcommands)
mod docker
mod db

# Project metadata
app_name := env("APP_NAME", "myapp")
app_env  := env("APP_ENV", "development")

# Show all commands including submodules
default:
    @just --list --unsorted --list-submodules

# Full project status
[group('info')]
status:
    just _header "Project Status"
    @printf '{{BOLD}}App:{{NORMAL}} {{app_name}}\n'
    @printf '{{BOLD}}Env:{{NORMAL}} {{app_env}}\n'
    @echo ""
    just _header "Docker"
    -just docker images
    @echo ""
    just _header "Database"
    -just db status

# Bootstrap new developer environment
[group('setup')]
bootstrap:
    just _header "Bootstrapping {{app_name}}"
    just _require docker
    just _require psql
    just docker build
    just db migrate
    just _success "Bootstrap complete!"
```

**Intermediate Verification:**

```bash
just --list --unsorted --list-submodules
```

You should see:
- Root recipes: `default`, `status`, `bootstrap`
- `docker` module recipes: `docker build`, `docker run`, `docker stop`, etc.
- `db` module recipes: `db migrate`, `db rollback`, `db create`, etc.
- Imported private recipes (`_header`, `_success`) should be hidden

## Step 6 -- Module File Resolution Rules

Understanding how just finds module files is important for organizing larger projects.

When you write `mod docker`, just looks for the module file in this order:

1. `docker.just` (sibling file)
2. `docker/mod.just` (directory with mod.just)

The directory form is useful when a module needs its own sub-files:

```
project/
  justfile
  docker/
    mod.just          # Module entry point
    compose.just      # Additional file (imported by mod.just)
```

### `docker/mod.just` (alternative structure)

```just
import 'compose.just'

# ... docker recipes ...
```

### Key rules:

- Module recipes run with their working directory set to the module file's directory by default
- Use `[no-cd]` on recipes that should run in the caller's directory
- Variables from the parent justfile are NOT inherited by modules -- use `env()` or `set dotenv-load` in each module
- `[doc()]` attributes on module recipes appear in `just --list --list-submodules`

## Step 7 -- When to Use mod vs import

| Use Case | Use `mod` | Use `import` |
|----------|-----------|--------------|
| Domain boundary (docker, db, deploy) | Yes | No |
| Shared utility recipes | No | Yes |
| Team owns a subsystem | Yes | No |
| Color constants / helper functions | No | Yes |
| Subcommand-style invocation desired | Yes | No |
| Recipes should appear in root namespace | No | Yes |
| Module needs its own settings | Yes | No |

**Rule of thumb:** If you would prefix the recipe name with the domain (`docker-build`, `db-migrate`), use `mod` and let the namespace do the prefixing. If the recipe is a utility that multiple modules use, use `import`.

## Common Mistakes

### Mistake 1: Expecting Parent Variables to Be Available in Modules

**Wrong:** Defining `app_name` in the root justfile and referencing `{{app_name}}` in `docker.just`.

**What happens:** Modules have their own scope. The variable is undefined, and just reports an error.

**Fix:** Use `env()` in the module to read from environment variables (which are shared via `set dotenv-load`), or redefine the variable in the module:

```just
# In docker.just
app_name := env("APP_NAME", "myapp")
```

### Mistake 2: Forgetting `--list-submodules` When Listing

**Wrong:**

```bash
just --list
```

**What happens:** Module recipes are not shown. Only root-level recipes appear.

**Fix:**

```bash
just --list --list-submodules
```

Or configure the default recipe to include it:

```just
default:
    @just --list --unsorted --list-submodules
```

## Verify What You Learned

```bash
# 1. List all recipes including modules
just --list --unsorted --list-submodules
# Expected: root recipes + docker:: + db:: subcommands

# 2. Run a module recipe
just docker images
# Expected: Docker images table (or empty if none built)

# 3. Show a module recipe
just --show docker::build
# Expected: the build recipe from docker.just

# 4. Verify imported helpers are private
just --list
# Expected: _header, _success, _warning, _require are NOT shown

# 5. Run bootstrap (uses both import and mod)
just bootstrap
# Expected: header output, dependency checks, docker build, db migrate
```

## What's Next

In the next exercise, you will build a complete database migrations workflow justfile with versioned SQL files, connection management, and safety checks.

## Summary

- `mod name` creates a namespaced module; recipes are invoked as `just name recipe`
- `import 'file.just'` merges recipes into the current namespace as if defined inline
- Module files are resolved as `name.just` or `name/mod.just`
- Modules have their own variable scope; use `env()` or `set dotenv-load` for shared config
- `[doc()]` attributes provide descriptions for module recipes in `--list-submodules`
- `[private]` and `[no-cd]` are essential for shared utility recipes in imported files
- Use `mod` for domain boundaries, `import` for shared utilities

## Reference

- [just manual -- modules](https://just.systems/man/en/modules.html)
- [just manual -- imports](https://just.systems/man/en/imports.html)
- [just manual -- doc attribute](https://just.systems/man/en/doc.html)
- [just manual -- no-cd attribute](https://just.systems/man/en/no-cd.html)

## Additional Resources

- [just changelog -- mod keyword (v1.19)](https://github.com/casey/just/blob/master/CHANGELOG.md)
- [just changelog -- import keyword (v1.20)](https://github.com/casey/just/blob/master/CHANGELOG.md)
