# 16. Node.js/TypeScript Project Justfile

<!--
difficulty: intermediate
concepts:
  - pnpm integration
  - TypeScript type checking
  - vitest testing
  - eslint and prettier
  - dev server management
  - production build
  - Docker multi-stage build
  - grouped recipes
  - env() for NODE_ENV
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - Node.js and TypeScript familiarity
  - npm/pnpm basics
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| node | >= 20 | `node --version` |
| pnpm | >= 9.0 | `pnpm --version` |

## Learning Objectives

- **Apply** `env()` with defaults and `[group]` attributes to build a structured Node.js development workflow
- **Implement** testing, linting, formatting, and build recipes that integrate pnpm, vitest, eslint, and prettier
- **Design** a Docker multi-stage build recipe that produces optimized production images with just metadata

## Why Node.js Project Justfiles

Node.js projects define scripts in `package.json`, but `npm run` scripts have significant limitations. They cannot accept arguments cleanly, they lack dependency chains, and complex scripts become unreadable single-line strings. Teams work around this with `concurrently`, `npm-run-all`, or shell scripts, adding tool sprawl to an already complex ecosystem.

A justfile sits above `package.json` scripts, orchestrating them when convenient and calling tools directly when needed. For example, `pnpm vitest` can be called directly from a justfile recipe, with arguments passed through via `*args`. The recipe also handles pre-conditions (like running type-check before build) that `package.json` scripts cannot express.

The `NODE_ENV` variable is critical in Node.js applications, affecting everything from bundler behavior to dependency installation. Using `env("NODE_ENV", "development")` in the justfile provides a sensible default while allowing CI to override it. Combined with `[group]` attributes, the result is a professional developer experience that scales from solo projects to monorepos.

## Step 1 -- Project Structure

Create a minimal TypeScript project.

### `package.json`

```json
{
  "name": "myapp",
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "express": "^4.18.0"
  },
  "devDependencies": {
    "@types/express": "^4.17.0",
    "@types/node": "^20.0.0",
    "eslint": "^9.0.0",
    "prettier": "^3.3.0",
    "typescript": "^5.5.0",
    "vite": "^5.4.0",
    "vitest": "^2.0.0"
  }
}
```

### `tsconfig.json`

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "outDir": "dist",
    "rootDir": "src",
    "declaration": true
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist"]
}
```

### `src/index.ts`

```typescript
export function add(a: number, b: number): number {
  return a + b;
}

console.log(`Hello from myapp! 2 + 3 = ${add(2, 3)}`);
```

### `tests/index.test.ts`

```typescript
import { describe, it, expect } from "vitest";
import { add } from "../src/index";

describe("add", () => {
  it("adds two numbers", () => {
    expect(add(2, 3)).toBe(5);
  });

  it("handles negative numbers", () => {
    expect(add(-1, 1)).toBe(0);
  });
});
```

### `.env`

```
NODE_ENV=development
PORT=3000
```

## Step 2 -- Core Justfile with Settings

Create the justfile with Node.js-specific settings.

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Environment
node_env := env("NODE_ENV", "development")
port     := env("PORT", "3000")

# Project metadata (extracted from package.json)
pkg_name    := `node -p "require('./package.json').name" 2>/dev/null || echo "myapp"`
pkg_version := `node -p "require('./package.json').version" 2>/dev/null || echo "0.0.0"`
git_sha     := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Show available commands
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Package:{{NORMAL}} {{pkg_name}}@{{pkg_version}}\n'
    @printf '{{BOLD}}NODE_ENV:{{NORMAL}} {{node_env}}\n'
```

**Intermediate Verification:**

```bash
just
```

You should see the recipe list with package name, version, and NODE_ENV.

## Step 3 -- Dependency and Setup Recipes

Add recipes for dependency management.

### `justfile` (append)

```just
# Install dependencies
[group('setup')]
install:
    @printf '{{GREEN}}Installing dependencies...{{NORMAL}}\n'
    pnpm install

# Install production dependencies only
[group('setup')]
install-prod:
    @printf '{{GREEN}}Installing production dependencies...{{NORMAL}}\n'
    pnpm install --prod

# Update dependencies
[group('setup')]
update:
    pnpm update
    @printf '{{GREEN}}Dependencies updated.{{NORMAL}}\n'

# Clean node_modules and build artifacts
[group('setup')]
clean:
    rm -rf node_modules dist coverage .tsbuildinfo
    @printf '{{GREEN}}Cleaned.{{NORMAL}}\n'

# Bootstrap: install + typecheck
[group('setup')]
bootstrap: install typecheck
    @printf '{{GREEN}}{{BOLD}}Project ready.{{NORMAL}}\n'
```

## Step 4 -- TypeScript and Build Recipes

Add recipes for type checking and building.

### `justfile` (append)

```just
# Run TypeScript type checker (no emit)
[group('build')]
typecheck:
    @printf '{{GREEN}}Type checking...{{NORMAL}}\n'
    pnpm tsc --noEmit

# Build for production
[group('build')]
build: typecheck
    @printf '{{GREEN}}Building {{pkg_name}}@{{pkg_version}} ({{node_env}})...{{NORMAL}}\n'
    pnpm tsc
    @printf '{{GREEN}}Build output in dist/{{NORMAL}}\n'
    @ls -lh dist/ 2>/dev/null || true

# Build and bundle with Vite (if configured)
[group('build')]
build-bundle:
    @printf '{{GREEN}}Bundling for production...{{NORMAL}}\n'
    NODE_ENV=production pnpm vite build
```

**Intermediate Verification:**

```bash
just typecheck
```

You should see TypeScript checking complete without errors.

## Step 5 -- Testing Recipes

Add vitest testing recipes.

### `justfile` (append)

```just
# Run all tests
[group('test')]
test *args:
    @printf '{{GREEN}}Running tests...{{NORMAL}}\n'
    pnpm vitest run {{args}}

# Run tests in watch mode
[group('test')]
test-watch:
    pnpm vitest

# Run tests with coverage
[group('test')]
coverage:
    @printf '{{GREEN}}Running tests with coverage...{{NORMAL}}\n'
    pnpm vitest run --coverage
    @printf '{{GREEN}}Coverage report generated.{{NORMAL}}\n'

# Run tests matching a filter
[group('test')]
test-filter filter:
    @printf '{{GREEN}}Running tests matching "{{filter}}"...{{NORMAL}}\n'
    pnpm vitest run --reporter=verbose -t "{{filter}}"

# Run tests with verbose output
[group('test')]
test-verbose:
    pnpm vitest run --reporter=verbose
```

## Step 6 -- Linting and Formatting Recipes

Add eslint and prettier recipes.

### `justfile` (append)

```just
# Run ESLint
[group('lint')]
lint *args:
    @printf '{{GREEN}}Running ESLint...{{NORMAL}}\n'
    pnpm eslint src/ tests/ {{args}}

# Run ESLint with auto-fix
[group('lint')]
lint-fix:
    pnpm eslint --fix src/ tests/
    @printf '{{GREEN}}Lint issues fixed.{{NORMAL}}\n'

# Check formatting with Prettier
[group('lint')]
fmt-check:
    @printf '{{GREEN}}Checking formatting...{{NORMAL}}\n'
    pnpm prettier --check "src/**/*.ts" "tests/**/*.ts"

# Format code with Prettier
[group('lint')]
fmt:
    pnpm prettier --write "src/**/*.ts" "tests/**/*.ts"
    @printf '{{GREEN}}Formatted.{{NORMAL}}\n'

# All quality checks
[group('lint')]
quality: fmt-check lint typecheck
    @printf '{{GREEN}}{{BOLD}}All quality checks passed.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just --list --unsorted
```

You should see recipes organized under setup, build, test, lint groups.

## Step 7 -- Dev Server and Docker Recipes

Add development server and Docker build recipes.

### `justfile` (append)

```just
# Start development server
[group('dev')]
dev:
    @printf '{{GREEN}}Starting dev server on port {{port}}...{{NORMAL}}\n'
    pnpm vite --port {{port}}

# Start production preview
[group('dev')]
preview: build-bundle
    pnpm vite preview --port {{port}}

# Build Docker image
[group('docker')]
docker-build tag=git_sha:
    @printf '{{GREEN}}Building Docker image: {{pkg_name}}:{{tag}}{{NORMAL}}\n'
    docker build \
        --build-arg NODE_ENV=production \
        -t {{pkg_name}}:{{tag}} \
        -t {{pkg_name}}:latest .

# Run Docker image
[group('docker')]
docker-run tag=git_sha:
    docker run --rm -p {{port}}:{{port}} -e PORT={{port}} {{pkg_name}}:{{tag}}
```

### `Dockerfile`

```dockerfile
# Stage 1: Install dependencies
FROM node:20-alpine AS deps
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prod

# Stage 2: Build
FROM node:20-alpine AS builder
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm tsc

# Stage 3: Production
FROM node:20-alpine AS runner
RUN addgroup -g 1001 -S app && adduser -S app -u 1001
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY --from=builder /app/dist ./dist
COPY package.json ./
USER app
EXPOSE 3000
CMD ["node", "dist/index.js"]
```

## Step 8 -- CI Aggregate Recipe

Add CI pipeline recipes.

### `justfile` (append)

```just
# Full CI pipeline: install → quality → test → build
[group('ci')]
ci: install quality test build
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# Quick check (fast feedback)
[group('ci')]
check: typecheck lint test
    @printf '{{GREEN}}Quick check passed.{{NORMAL}}\n'

# Show project info
[group('help')]
info:
    @printf '{{BOLD}}Package:{{NORMAL}} {{pkg_name}}@{{pkg_version}}\n'
    @printf '{{BOLD}}Git:{{NORMAL}} {{git_sha}}\n'
    @printf '{{BOLD}}NODE_ENV:{{NORMAL}} {{node_env}}\n'
    @printf '{{BOLD}}Node:{{NORMAL}} '
    @node --version
    @printf '{{BOLD}}pnpm:{{NORMAL}} '
    @pnpm --version
```

## Common Mistakes

### Mistake 1: Using `npm run` Instead of Direct Tool Invocation

**Wrong:**

```just
test:
    npm run test
```

**What happens:** Arguments cannot be passed through cleanly. `just test --reporter=verbose` would try to pass `--reporter=verbose` to `npm run`, not to vitest.

**Fix:** Call the tool directly via pnpm:

```just
test *args:
    pnpm vitest run {{args}}
```

### Mistake 2: Forgetting NODE_ENV Affects `pnpm install`

**Wrong:**

```just
set export
# NODE_ENV=production in .env

install:
    pnpm install  # skips devDependencies!
```

**What happens:** When `NODE_ENV=production` is exported, `pnpm install` (and `npm install`) skips devDependencies. Your build tools, test frameworks, and linters are missing.

**Fix:** Either use `install-prod` explicitly for production, or override the variable:

```just
install:
    NODE_ENV=development pnpm install
```

## Verify What You Learned

```bash
# 1. Show project info
just info
# Expected: package name, version, git SHA, NODE_ENV, node and pnpm versions

# 2. Type check the project
just typecheck
# Expected: TypeScript checks pass with no errors

# 3. Run tests
just test
# Expected: vitest runs all tests, shows pass/fail

# 4. Run all quality checks
just quality
# Expected: fmt-check, lint, typecheck all pass

# 5. Build the project
just build
# Expected: TypeScript compiles to dist/ directory
```

## What's Next

In the next exercise, you will learn about just's module system using `mod` and `import` keywords to organize large justfiles into maintainable, namespaced components.

## Summary

- `env("NODE_ENV", "development")` provides sensible defaults that CI can override
- Package metadata is extracted from `package.json` using Node.js backtick one-liners
- Direct tool invocation (`pnpm vitest`) is preferred over `npm run` for proper argument passthrough
- `[group]` attributes organize recipes into setup, build, test, lint, dev, docker, and ci categories
- Docker multi-stage builds separate dependency installation, build, and production stages
- `NODE_ENV=production` affects `pnpm install` behavior -- be explicit about when you want production mode

## Reference

- [just manual -- env function](https://just.systems/man/en/env.html)
- [just manual -- groups](https://just.systems/man/en/groups.html)
- [just manual -- variadic parameters](https://just.systems/man/en/recipe-parameters.html)
- [just manual -- dotenv settings](https://just.systems/man/en/dotenv-settings.html)

## Additional Resources

- [pnpm documentation](https://pnpm.io/)
- [vitest documentation](https://vitest.dev/)
- [ESLint flat config guide](https://eslint.org/docs/latest/use/configure/configuration-files-new)
- [Docker multi-stage builds](https://docs.docker.com/build/building/multi-stage/)
