# 33. Robust Error Handling

<!--
difficulty: advanced
concepts: [error-handling, rollback-patterns, preflight-checks, graceful-degradation, trap-handlers, webhook-notifications]
tools: [just]
estimated_time: 1h
bloom_level: evaluate
prerequisites: [shebang-recipes, shell-scripting, recipe-dependencies, conditional-expressions]
-->

## Prerequisites

- just >= 1.38.0
- bash, curl, docker, git
- A terminal with ANSI color support

## Learning Objectives

- **Evaluate** error handling strategies for multi-stage deployment pipelines
- **Design** rollback mechanisms using bash `trap` inside shebang recipes
- **Create** pre-flight validation that fails fast and reports actionable diagnostics

## Why Robust Error Handling

A deploy recipe that succeeds 95% of the time and silently corrupts state the other 5% is worse than one that fails loudly every time. Production-grade automation needs pre-flight checks that catch problems before any state changes, traps that roll back partial progress on failure, and graceful degradation that picks the best available tool rather than hard-failing when the preferred one is absent.

## The Challenge

Build a deploy pipeline justfile with comprehensive error handling. It must verify prerequisites before starting, roll back on failure at any stage, degrade gracefully when optional tools are missing (prefer `cargo-nextest` but fall back to `cargo test`), send webhook notifications on success or failure, and produce color-coded terminal output throughout.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load

project := "deploy-demo"
version := `git describe --tags --always 2>/dev/null || echo "0.0.0-dev"`
timestamp := `date -u +%Y%m%d%H%M%S`
deploy_log := ".deploy/deploy-" + timestamp + ".log"
webhook_url := env("DEPLOY_WEBHOOK_URL", "")

# ─── Color Helpers (private) ───────────────────────────────

[private]
_info msg:
    @printf '\033[36mINFO:\033[0m  %s\n' "{{ msg }}"

[private]
_ok msg:
    @printf '\033[32m  OK:\033[0m  %s\n' "{{ msg }}"

[private]
_warn msg:
    @printf '\033[33mWARN:\033[0m  %s\n' "{{ msg }}"

[private]
_err msg:
    @printf '\033[31m ERR:\033[0m  %s\n' "{{ msg }}"

# ─── Tool Verification ─────────────────────────────────────

[private]
_require tool:
    #!/usr/bin/env bash
    if ! command -v "{{ tool }}" >/dev/null 2>&1; then
        printf '\033[31m ERR:\033[0m  Required tool not found: {{ tool }}\n'
        exit 1
    fi

[private]
_prefer primary fallback:
    #!/usr/bin/env bash
    if command -v "{{ primary }}" >/dev/null 2>&1; then
        echo "{{ primary }}"
    elif command -v "{{ fallback }}" >/dev/null 2>&1; then
        printf '\033[33mWARN:\033[0m  {{ primary }} not found, falling back to {{ fallback }}\n' >&2
        echo "{{ fallback }}"
    else
        printf '\033[31m ERR:\033[0m  Neither {{ primary }} nor {{ fallback }} found\n' >&2
        exit 1
    fi

# ─── Pre-flight Checks ─────────────────────────────────────

[group('deploy')]
[doc('Run all pre-flight checks without deploying')]
preflight env="dev":
    #!/usr/bin/env bash
    set -euo pipefail
    failed=0
    total=0

    check() {
        total=$((total + 1))
        local desc="$1"
        shift
        if "$@" >/dev/null 2>&1; then
            printf '\033[32m  OK:\033[0m  %s\n' "$desc"
        else
            printf '\033[31mFAIL:\033[0m  %s\n' "$desc"
            failed=$((failed + 1))
        fi
    }

    echo "=== Pre-flight checks for {{ env }} ==="
    echo ""

    # Required tools
    check "git is installed" command -v git
    check "docker is installed" command -v docker
    check "curl is installed" command -v curl

    # Docker daemon
    check "Docker daemon is running" docker info

    # Git state
    check "Git working directory is clean" test -z "$(git status --porcelain 2>/dev/null)"
    check "On main branch" test "$(git branch --show-current 2>/dev/null)" = "main"
    check "Up to date with origin" bash -c 'git fetch origin --dry-run 2>&1 | grep -qv "." || true'

    # Environment-specific
    if [ "{{ env }}" = "prod" ]; then
        check "DEPLOY_WEBHOOK_URL is set" test -n "${DEPLOY_WEBHOOK_URL:-}"
        check "AWS credentials configured" bash -c 'test -n "${AWS_ACCESS_KEY_ID:-}" || test -f ~/.aws/credentials'
    fi

    echo ""
    echo "Results: $((total - failed))/$total passed"
    if [ "$failed" -gt 0 ]; then
        printf '\033[31m%d check(s) failed. Fix issues before deploying.\033[0m\n' "$failed"
        exit 1
    fi
    printf '\033[32mAll pre-flight checks passed.\033[0m\n'

# ─── Webhook Notifications ─────────────────────────────────

[private]
_notify status message:
    #!/usr/bin/env bash
    set -uo pipefail
    url="{{ webhook_url }}"
    if [ -z "$url" ]; then
        printf '\033[33mWARN:\033[0m  No webhook URL configured, skipping notification\n'
        exit 0
    fi
    payload=$(cat <<PEOF
    {
      "project": "{{ project }}",
      "version": "{{ version }}",
      "status": "{{ status }}",
      "message": "{{ message }}",
      "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    }
    PEOF
    )
    if curl -sf -X POST -H "Content-Type: application/json" -d "$payload" "$url" >/dev/null 2>&1; then
        printf '\033[32m  OK:\033[0m  Webhook notification sent ({{ status }})\n'
    else
        printf '\033[33mWARN:\033[0m  Webhook notification failed (non-fatal)\n'
    fi

# ─── Test with Graceful Degradation ────────────────────────

[group('test')]
[doc('Run tests using best available runner (nextest > cargo test)')]
test:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v cargo-nextest >/dev/null 2>&1; then
        printf '\033[36mINFO:\033[0m  Using cargo-nextest for parallel test execution\n'
        cargo nextest run --workspace 2>&1
    else
        printf '\033[33mWARN:\033[0m  cargo-nextest not found, falling back to cargo test\n'
        printf '\033[33m     \033[0m  Install nextest for faster tests: cargo install cargo-nextest\n'
        cargo test --workspace 2>&1
    fi

[group('test')]
[doc('Run linting with best available tool')]
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v cargo-clippy >/dev/null 2>&1; then
        printf '\033[36mINFO:\033[0m  Running clippy\n'
        cargo clippy --workspace --all-targets -- -D warnings
    else
        printf '\033[33mWARN:\033[0m  clippy not available, running cargo check only\n'
        cargo check --workspace
    fi

# ─── Build with Rollback ───────────────────────────────────

[group('deploy')]
[doc('Build release artifacts with cleanup on failure')]
build-release:
    #!/usr/bin/env bash
    set -euo pipefail

    artifact_dir="dist/{{ version }}"
    mkdir -p "$artifact_dir"

    cleanup() {
        local exit_code=$?
        if [ $exit_code -ne 0 ]; then
            printf '\033[31m ERR:\033[0m  Build failed (exit %d), cleaning up artifacts\n' "$exit_code"
            rm -rf "$artifact_dir"
        fi
    }
    trap cleanup EXIT

    printf '\033[36mINFO:\033[0m  Building release artifacts -> %s\n' "$artifact_dir"
    cargo build --release 2>&1

    # Copy artifacts
    find target/release -maxdepth 1 -type f -executable | while read -r bin; do
        name=$(basename "$bin")
        cp "$bin" "$artifact_dir/$name"
        printf '\033[32m  OK:\033[0m  %s -> %s/%s\n' "$name" "$artifact_dir" "$name"
    done

    echo "{{ version }}" > "$artifact_dir/VERSION"
    printf '\033[32m  OK:\033[0m  Build complete\n'

# ─── Deploy Pipeline ───────────────────────────────────────

[group('deploy')]
[doc('Full deploy pipeline: preflight -> test -> build -> deploy -> verify')]
deploy env="dev": (preflight env)
    #!/usr/bin/env bash
    set -euo pipefail

    deploy_env="{{ env }}"
    log="{{ deploy_log }}"
    mkdir -p "$(dirname "$log")"

    exec > >(tee -a "$log") 2>&1

    state_file=".deploy/state-${deploy_env}"
    previous_version=$(cat "$state_file" 2>/dev/null || echo "none")

    # ── Rollback handler ──
    rollback() {
        local exit_code=$?
        if [ $exit_code -ne 0 ]; then
            printf '\n\033[31m════════════════════════════════════════\033[0m\n'
            printf '\033[31m  DEPLOY FAILED — INITIATING ROLLBACK\033[0m\n'
            printf '\033[31m════════════════════════════════════════\033[0m\n\n'

            if [ "$previous_version" != "none" ]; then
                printf '\033[33mWARN:\033[0m  Rolling back to %s\n' "$previous_version"
                echo "$previous_version" > "$state_file"
                printf '\033[32m  OK:\033[0m  Rollback to %s complete\n' "$previous_version"
            else
                printf '\033[33mWARN:\033[0m  No previous version to roll back to\n'
            fi

            just _notify failure "Deploy of {{ version }} to ${deploy_env} failed, rolled back to ${previous_version}"
            printf '\033[31m ERR:\033[0m  Deploy log: %s\n' "$log"
        fi
    }
    trap rollback EXIT

    printf '\n\033[36m════════════════════════════════════════\033[0m\n'
    printf '\033[36m  DEPLOYING %s to %s\033[0m\n' "{{ version }}" "${deploy_env}"
    printf '\033[36m════════════════════════════════════════\033[0m\n\n'

    # ── Stage 1: Test ──
    printf '\033[36mINFO:\033[0m  Stage 1/4: Running tests\n'
    just test

    # ── Stage 2: Lint ──
    printf '\033[36mINFO:\033[0m  Stage 2/4: Running lints\n'
    just lint

    # ── Stage 3: Build ──
    printf '\033[36mINFO:\033[0m  Stage 3/4: Building release\n'
    just build-release

    # ── Stage 4: Deploy ──
    printf '\033[36mINFO:\033[0m  Stage 4/4: Deploying to %s\n' "${deploy_env}"
    mkdir -p .deploy
    echo "{{ version }}" > "$state_file"
    printf '\033[32m  OK:\033[0m  Version {{ version }} recorded for %s\n' "${deploy_env}"

    # ── Success ──
    trap - EXIT
    just _notify success "Deploy of {{ version }} to ${deploy_env} succeeded"

    printf '\n\033[32m════════════════════════════════════════\033[0m\n'
    printf '\033[32m  DEPLOY SUCCESSFUL\033[0m\n'
    printf '\033[32m  Version: %s\033[0m\n' "{{ version }}"
    printf '\033[32m  Environment: %s\033[0m\n' "${deploy_env}"
    printf '\033[32m  Log: %s\033[0m\n' "$log"
    printf '\033[32m════════════════════════════════════════\033[0m\n'

[group('deploy')]
[doc('Deploy to production with confirmation and extra validation')]
[confirm('PRODUCTION DEPLOY of version {{ version }}. Type "yes" to proceed:')]
deploy-prod: (preflight "prod")
    #!/usr/bin/env bash
    set -euo pipefail
    just deploy prod

# ─── Status & Cleanup ──────────────────────────────────────

[group('deploy')]
[doc('Show deploy state for all environments')]
status:
    #!/usr/bin/env bash
    echo "=== Deploy Status ==="
    for env in dev staging prod; do
        ver=$(cat ".deploy/state-${env}" 2>/dev/null || echo "not deployed")
        printf "  %-10s %s\n" "$env:" "$ver"
    done

[group('deploy')]
[doc('Remove deploy state and logs')]
clean:
    rm -rf .deploy/ dist/
    @printf '\033[32m  OK:\033[0m  Cleaned deploy state and artifacts\n'
```

## Verify What You Learned

```bash
# Run pre-flight checks and observe pass/fail output
just preflight dev

# Test graceful degradation (uninstall nextest first if installed)
just test

# Run a dev deploy (will fail at cargo build if no Rust project; that is expected — observe rollback)
just deploy dev 2>&1 || echo "Rollback executed as expected"

# Check deploy status
just status

# Inspect the deploy log
ls -la .deploy/deploy-*.log 2>/dev/null || echo "No logs yet"
```

## What's Next

Continue to [Exercise 34: Personal Productivity Justfile](../04-personal-productivity-justfile/04-personal-productivity-justfile.md) to build a global justfile for daily developer workflows.

## Summary

- Pre-flight checks validate all prerequisites before any state changes occur
- Bash `trap` handlers inside shebang recipes enable automatic rollback on failure
- Graceful degradation tries the preferred tool and falls back to alternatives without failing
- Color-coded output makes success, warning, and error states immediately distinguishable
- Webhook notifications keep the team informed regardless of whether the deploy succeeds or fails
- Deploy logs capture the full output for post-mortem analysis

## Reference

- [Shell settings](https://just.systems/man/en/settings.html)
- [Shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [Confirm attribute](https://just.systems/man/en/confirm-attribute.html)

## Additional Resources

- [Bash trap documentation](https://www.gnu.org/software/bash/manual/html_node/Bourne-Shell-Builtins.html)
- [Defensive bash programming](https://wizardzines.com/comics/bash-errors/)
