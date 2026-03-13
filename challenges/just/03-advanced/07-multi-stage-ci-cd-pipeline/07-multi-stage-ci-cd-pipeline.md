# 27. Multi-Stage CI/CD Pipeline

<!--
difficulty: advanced
concepts:
  - multi-stage pipeline (lint, test, build, package, deploy)
  - quality gates between stages
  - environment-specific deployment
  - artifact versioning
  - confirm for production
  - git tag on success
  - Docker image build and push
  - health check after deploy
tools: [just, docker, git, curl]
estimated_time: 50 minutes
bloom_level: evaluate
prerequisites:
  - just intermediate (dependencies, conditional expressions, environment variables)
  - Docker image building basics
  - CI/CD pipeline concepts
  - git tagging
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| docker | 24+ | `docker --version` |
| git | 2.30+ | `git --version` |
| curl | 7.0+ | `curl --version` |

## Learning Objectives

- **Evaluate** quality gate strategies that prevent broken code from advancing through pipeline stages
- **Design** a deployment pipeline with environment-specific configuration and progressive rollout
- **Justify** the safety mechanisms needed for production deployments versus development environments

## Why Model CI/CD in a Justfile

CI/CD platforms (GitHub Actions, GitLab CI, Jenkins) define pipelines in YAML. These are powerful but have a critical flaw: you cannot run them locally. When a pipeline fails in CI, you push a fix, wait for the runner, and hope. A justfile that mirrors the CI/CD pipeline lets developers run the exact same stages on their machine, catching failures in seconds instead of minutes.

The pipeline in this exercise follows a strict stage model: lint must pass before test, test before build, build before package, and package before deploy. Each stage is a quality gate — failure stops the pipeline. This pattern prevents deploying untested code, shipping binaries that do not compile, or pushing images that fail health checks.

Environment-specific deployment adds another dimension. Development deploys automatically, staging requires a passing test suite, and production demands explicit confirmation plus a git tag. The justfile encodes these policies as recipe dependencies and `[confirm]` attributes, making the rules visible and enforceable.

## Step 1 -- Pipeline Configuration

```just
# justfile

set dotenv-load
set export

# ─── Configuration ──────────────────────────────────────
project   := env("PROJECT_NAME", "myservice")
version   := trim(read("VERSION"))
env_name  := env("DEPLOY_ENV", "dev")
registry  := env("DOCKER_REGISTRY", "ghcr.io/myorg")
image     := registry + "/" + project
image_tag := image + ":" + version

# Environment-specific endpoints
deploy_host := if env_name == "prod" {
    "https://api.example.com"
} else if env_name == "staging" {
    "https://staging-api.example.com"
} else {
    "http://localhost:8080"
}

# ─── Colors ─────────────────────────────────────────────
RED    := '\033[0;31m'
GREEN  := '\033[0;32m'
YELLOW := '\033[1;33m'
BLUE   := '\033[0;34m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

default:
    @just --list --unsorted
```

## Step 2 -- Stage 1: Lint (Quality Gate)

```just
# ─── Stage 1: Lint ──────────────────────────────────────

# Run all linters
lint: lint-format lint-static lint-security
    @echo "{{ GREEN }}Stage 1 PASSED: Lint{{ NORMAL }}"

# Check code formatting
lint-format:
    @echo "{{ BLUE }}[lint] Checking format...{{ NORMAL }}"
    cargo fmt --check

# Static analysis
lint-static:
    @echo "{{ BLUE }}[lint] Running clippy...{{ NORMAL }}"
    cargo clippy --workspace -- -D warnings

# Security audit
lint-security:
    @echo "{{ BLUE }}[lint] Auditing dependencies...{{ NORMAL }}"
    cargo audit
```

Each lint sub-recipe runs independently, but all must pass for the `lint` gate to succeed. If `lint-format` fails, the developer gets immediate feedback without waiting for the slower `lint-security`.

## Step 3 -- Stage 2: Test (Quality Gate)

```just
# ─── Stage 2: Test ──────────────────────────────────────

# Run all tests
test: lint test-unit test-integration
    @echo "{{ GREEN }}Stage 2 PASSED: Test{{ NORMAL }}"

# Unit tests
test-unit:
    @echo "{{ BLUE }}[test] Running unit tests...{{ NORMAL }}"
    cargo test --workspace --lib

# Integration tests (may need services running)
test-integration:
    @echo "{{ BLUE }}[test] Running integration tests...{{ NORMAL }}"
    cargo test --workspace --test '*'

# Generate coverage report
test-coverage:
    @echo "{{ BLUE }}[test] Generating coverage...{{ NORMAL }}"
    cargo llvm-cov --workspace --html
    @echo "{{ GREEN }}Coverage report: target/llvm-cov/html/index.html{{ NORMAL }}"
```

Notice `test` depends on `lint` — you cannot run tests without passing lint first. This is the quality gate pattern: each stage includes the previous stage as a dependency.

## Step 4 -- Stage 3: Build (Quality Gate)

```just
# ─── Stage 3: Build ────────────────────────────────────

# Build release binary
build: test
    @echo "{{ BLUE }}[build] Compiling release binary...{{ NORMAL }}"
    cargo build --release
    @echo "{{ GREEN }}Stage 3 PASSED: Build{{ NORMAL }}"
    @echo "  Binary: target/release/{{ project }}"
```

## Step 5 -- Stage 4: Package (Docker Image)

```just
# ─── Stage 4: Package ──────────────────────────────────

# Build Docker image
package: build
    @echo "{{ BLUE }}[package] Building Docker image...{{ NORMAL }}"
    docker build \
        --build-arg VERSION={{ version }} \
        --tag {{ image_tag }} \
        --tag {{ image }}:latest \
        --file Dockerfile \
        .
    @echo "{{ GREEN }}Stage 4 PASSED: Package{{ NORMAL }}"
    @echo "  Image: {{ image_tag }}"

# Push image to registry
push-image: package
    @echo "{{ BLUE }}[package] Pushing {{ image_tag }}...{{ NORMAL }}"
    docker push {{ image_tag }}
    docker push {{ image }}:latest
    @echo "{{ GREEN }}Image pushed{{ NORMAL }}"

# Scan image for vulnerabilities
scan-image: package
    @echo "{{ BLUE }}[package] Scanning image...{{ NORMAL }}"
    docker scout cves {{ image_tag }}
```

## Step 6 -- Stage 5: Deploy (Environment-Specific)

This is where the pipeline diverges by environment. Dev deploys automatically, staging requires all tests, and production demands explicit confirmation and a git tag.

```just
# ─── Stage 5: Deploy ───────────────────────────────────

# Deploy to the target environment
deploy: _deploy-gate push-image _deploy-execute _health-check _deploy-tag
    @echo ""
    @echo "{{ GREEN }}{{ BOLD }}Stage 5 PASSED: Deploy to {{ env_name }}{{ NORMAL }}"
    @echo "  Version: {{ version }}"
    @echo "  Image:   {{ image_tag }}"
    @echo "  Host:    {{ deploy_host }}"

# Gate: production requires explicit confirmation
[private]
_deploy-gate:
    #!/usr/bin/env bash
    set -euo pipefail
    case "{{ env_name }}" in
        prod)
            echo "{{ RED }}{{ BOLD }}PRODUCTION DEPLOYMENT{{ NORMAL }}"
            echo "  Version: {{ version }}"
            echo "  Image:   {{ image_tag }}"
            echo ""
            read -p "Type 'deploy-prod' to confirm: " confirm
            if [[ "$confirm" != "deploy-prod" ]]; then
                echo "{{ RED }}Aborted{{ NORMAL }}"
                exit 1
            fi
            # Verify clean working tree for production
            if [[ -n "$(git status --porcelain)" ]]; then
                echo "{{ RED }}Working tree must be clean for production deploy{{ NORMAL }}"
                exit 1
            fi
            ;;
        staging)
            echo "{{ YELLOW }}Deploying to staging...{{ NORMAL }}"
            ;;
        dev)
            echo "{{ BLUE }}Deploying to dev...{{ NORMAL }}"
            ;;
        *)
            echo "{{ RED }}Unknown environment: {{ env_name }}{{ NORMAL }}"
            exit 1
            ;;
    esac

# Execute the deployment
[private]
_deploy-execute:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BLUE }}[deploy] Deploying {{ image_tag }} to {{ env_name }}...{{ NORMAL }}"
    # In a real project, this would be:
    #   kubectl set image deployment/{{ project }} app={{ image_tag }}
    #   or: aws ecs update-service ...
    #   or: docker compose pull && docker compose up -d
    echo "{{ GREEN }}Deployment initiated{{ NORMAL }}"

# Health check after deployment
[private]
_health-check:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BLUE }}[deploy] Running health check on {{ deploy_host }}...{{ NORMAL }}"
    max_retries=10
    for i in $(seq 1 $max_retries); do
        status=$(curl -s -o /dev/null -w "%{http_code}" "{{ deploy_host }}/health" 2>/dev/null || echo "000")
        if [[ "$status" == "200" ]]; then
            echo "{{ GREEN }}Health check passed (attempt $i/$max_retries){{ NORMAL }}"
            exit 0
        fi
        echo "  Attempt $i/$max_retries: HTTP $status, retrying..."
        sleep 3
    done
    echo "{{ RED }}Health check failed after $max_retries attempts{{ NORMAL }}"
    echo "{{ YELLOW }}Consider running: just rollback{{ NORMAL }}"
    exit 1

# Tag the release in git (only for staging and prod)
[private]
_deploy-tag:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ env_name }}" == "dev" ]]; then
        echo "Skipping git tag for dev environment"
        exit 0
    fi
    tag="v{{ version }}-{{ env_name }}"
    if git rev-parse "$tag" >/dev/null 2>&1; then
        echo "{{ YELLOW }}Tag $tag already exists, skipping{{ NORMAL }}"
    else
        git tag -a "$tag" -m "Deploy v{{ version }} to {{ env_name }}"
        git push origin "$tag"
        echo "{{ GREEN }}Tagged: $tag{{ NORMAL }}"
    fi
```

The production gate uses a typed confirmation ("deploy-prod") instead of `[confirm]`. This forces the operator to type a specific string, which is harder to confirm accidentally than answering "yes."

## Step 7 -- Rollback and Utility Recipes

```just
# ─── Rollback ───────────────────────────────────────────

# Rollback to the previous version
[confirm("Rollback {{ env_name }} to previous version? (yes/no)")]
rollback:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ YELLOW }}Rolling back {{ env_name }}...{{ NORMAL }}"
    # In a real project:
    #   kubectl rollout undo deployment/{{ project }}
    #   or: deploy the previous image tag
    echo "{{ GREEN }}Rollback complete{{ NORMAL }}"

# ─── Convenience ────────────────────────────────────────

# Run full pipeline for an environment (e.g., DEPLOY_ENV=staging just full-pipeline)
full-pipeline: deploy
    @echo "{{ GREEN }}{{ BOLD }}Full pipeline complete for {{ env_name }}{{ NORMAL }}"

# Quick deploy to dev (skips confirmation)
dev-deploy:
    DEPLOY_ENV=dev just deploy

# Show pipeline configuration
info:
    @echo "{{ BOLD }}Pipeline Configuration{{ NORMAL }}"
    @echo "  Project:    {{ project }}"
    @echo "  Version:    {{ version }}"
    @echo "  Environment:{{ env_name }}"
    @echo "  Image:      {{ image_tag }}"
    @echo "  Deploy Host:{{ deploy_host }}"

# Show which stage would run next given current state
status:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BOLD }}Pipeline Stage Status{{ NORMAL }}"
    echo "  1. Lint      → cargo fmt --check && clippy && audit"
    echo "  2. Test      → unit + integration"
    echo "  3. Build     → cargo build --release"
    echo "  4. Package   → docker build + tag"
    echo "  5. Deploy    → push + deploy to {{ env_name }} + health check"
```

## Common Mistakes

**Wrong: Allowing deploy to skip the test stage**
```just
deploy: push-image _deploy-execute
# Missing: test dependency
```
What happens: An operator runs `just deploy` with failing tests. The broken code ships because nothing enforced the quality gate. In this exercise, `deploy` depends on `push-image`, which depends on `package`, which depends on `build`, which depends on `test`, which depends on `lint`. The chain is unbroken.
Fix: Model quality gates as recipe dependencies. Never create shortcuts that bypass earlier stages. If a stage is slow, optimize the stage — do not skip it.

**Wrong: Using the same confirmation for dev and production deploys**
```just
[confirm("Deploy?")]
deploy:
    # same for all envs
```
What happens: Deploying to dev requires the same friction as production. Developers either disable the prompt or develop "yes" muscle memory, eroding the safety benefit when it matters most.
Fix: Scale confirmation friction with blast radius. Dev deploys need no confirmation. Staging gets a simple `[confirm]`. Production requires typing a specific phrase that includes the environment name.

## Verify What You Learned

```bash
# Show pipeline info
just info
# Expected: project, version, environment, image details

# Run lint stage only
just lint
# Expected: format + static + security checks

# Run through test stage (includes lint)
just test
# Expected: lint passes, then unit + integration tests

# Attempt dev deploy
DEPLOY_ENV=dev just deploy
# Expected: full pipeline → push → deploy → health check (no confirmation needed)

# Attempt prod deploy (should require typed confirmation)
DEPLOY_ENV=prod just deploy
# Expected: "Type 'deploy-prod' to confirm:" prompt
```

## What's Next

The next exercise ([28. Kubernetes Deploy Workflow](../08-kubernetes-deploy-workflow/08-kubernetes-deploy-workflow.md)) applies pipeline concepts to Kubernetes-specific deployment, including rolling updates, rollbacks, and namespace management.

## Summary

- Quality gates enforce stage ordering: lint → test → build → package → deploy
- Each stage depends on the previous stage, making bypass impossible
- `[confirm]` and typed confirmations scale with blast radius (none for dev, strict for prod)
- Environment-specific logic uses `if env_name == "prod"` for conditional behavior
- Health checks with retry loops verify deployments succeeded before declaring victory
- Git tags record what version was deployed to which environment
- Rollback recipes provide a quick escape when health checks fail
- Private recipes (`_deploy-gate`, `_health-check`) hide implementation details

## Reference

- [Just Dependencies](https://just.systems/man/en/dependencies.html)
- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just Private Recipes](https://just.systems/man/en/private-recipes.html)
- [Just Conditional Expressions](https://just.systems/man/en/conditional-expressions.html)

## Additional Resources

- [Continuous Delivery Principles](https://continuousdelivery.com/)
- [Docker Build Best Practices](https://docs.docker.com/build/building/best-practices/)
- [Git Tagging Strategies](https://git-scm.com/book/en/v2/Git-Basics-Tagging)
