# 38. Zero-Downtime Deployment

<!--
difficulty: advanced
concepts: [blue-green-deployment, canary-deployment, health-monitoring, automatic-rollback, deployment-locking, webhook-notifications]
tools: [just]
estimated_time: 1h
bloom_level: evaluate
prerequisites: [shebang-recipes, error-handling, recipe-dependencies, confirm-guards]
-->

## Prerequisites

- just >= 1.38.0
- bash with ANSI color support
- curl (for health checks and webhook notifications)

## Learning Objectives

- **Evaluate** trade-offs between blue-green and canary deployment strategies
- **Architect** a deployment system with health monitoring, automatic rollback, and concurrency locks
- **Create** a complete deployment orchestration that simulates real zero-downtime patterns

## Why Zero-Downtime Deployment

Every deployment is a risk window. Blue-green deployment eliminates downtime by running two identical environments and switching traffic atomically. Canary deployment reduces blast radius by shifting traffic gradually and rolling back at the first sign of trouble. Both strategies demand health checks, rollback automation, and concurrency guards. Implementing them in a justfile makes the deployment logic transparent, auditable, and executable without specialized tooling.

## The Challenge

Build a justfile implementing both blue-green and canary deployment strategies. Include: environment state tracking (which slot is live), health checks at every stage, automatic rollback on health failure, a deployment lock to prevent concurrent deploys, webhook notifications, and a deployment history log. The justfile should work end-to-end in simulation mode without requiring actual infrastructure.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]

project := "zdt-demo"
version := `git describe --tags --always 2>/dev/null || echo "1.0.0-dev"`
state_dir := ".deploy-state"
lock_file := ".deploy-state/deploy.lock"
history_file := ".deploy-state/history.log"
webhook_url := env("DEPLOY_WEBHOOK_URL", "")

# ═══════════════════════════════════════════════════════════
# State Management (private)
# ═══════════════════════════════════════════════════════════

[private]
_init:
    @mkdir -p {{ state_dir }}/blue {{ state_dir }}/green

[private]
_get-live-slot:
    #!/usr/bin/env bash
    cat {{ state_dir }}/live-slot 2>/dev/null || echo "none"

[private]
_set-live-slot slot:
    @echo "{{ slot }}" > {{ state_dir }}/live-slot

[private]
_get-slot-version slot:
    #!/usr/bin/env bash
    cat {{ state_dir }}/{{ slot }}/version 2>/dev/null || echo "none"

[private]
_set-slot-version slot version:
    @echo "{{ version }}" > {{ state_dir }}/{{ slot }}/version

[private]
_log event:
    #!/usr/bin/env bash
    echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) {{ event }}" >> {{ history_file }}

# ═══════════════════════════════════════════════════════════
# Deployment Lock
# ═══════════════════════════════════════════════════════════

[private]
_acquire-lock:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f "{{ lock_file }}" ]; then
        lock_owner=$(cat "{{ lock_file }}")
        lock_age=$(( $(date +%s) - $(stat -f %m "{{ lock_file }}" 2>/dev/null || stat -c %Y "{{ lock_file }}" 2>/dev/null || echo 0) ))
        printf '\033[31mERROR: Deploy lock held by: %s (age: %ds)\033[0m\n' "$lock_owner" "$lock_age"
        echo ""
        echo "If the lock is stale, remove it with: just unlock"
        exit 1
    fi
    echo "$$:$(whoami)@$(hostname):{{ version }}" > "{{ lock_file }}"
    printf '\033[32mDeploy lock acquired\033[0m\n'

[private]
_release-lock:
    @rm -f {{ lock_file }}

[group('lock')]
[doc('Force-remove a stale deployment lock')]
[confirm('Force-remove the deployment lock?')]
unlock:
    @rm -f {{ lock_file }}
    @printf '\033[32mDeploy lock removed.\033[0m\n'

# ═══════════════════════════════════════════════════════════
# Health Checks
# ═══════════════════════════════════════════════════════════

[private]
_health-check slot retries="5" interval="2":
    #!/usr/bin/env bash
    set -euo pipefail
    slot="{{ slot }}"
    max={{ retries }}
    interval={{ interval }}
    attempt=1

    printf '\033[36m  Health-checking %s slot...\033[0m\n' "$slot"

    while [ "$attempt" -le "$max" ]; do
        # Simulated health check — replace with real endpoint check
        # Read the simulated health status from state
        health_file="{{ state_dir }}/${slot}/health"
        if [ -f "$health_file" ]; then
            status=$(cat "$health_file")
        else
            status="healthy"
        fi

        if [ "$status" = "healthy" ]; then
            printf '    \033[32mAttempt %d/%d: healthy\033[0m\n' "$attempt" "$max"
            exit 0
        else
            printf '    \033[31mAttempt %d/%d: %s\033[0m\n' "$attempt" "$max" "$status"
        fi

        if [ "$attempt" -lt "$max" ]; then
            sleep "$interval"
        fi
        attempt=$((attempt + 1))
    done

    printf '  \033[31mHealth check failed after %d attempts\033[0m\n' "$max"
    exit 1

# ═══════════════════════════════════════════════════════════
# Webhook Notifications
# ═══════════════════════════════════════════════════════════

[private]
_notify status message:
    #!/usr/bin/env bash
    set -uo pipefail
    url="{{ webhook_url }}"
    if [ -z "$url" ]; then
        exit 0
    fi
    payload="{\"project\":\"{{ project }}\",\"version\":\"{{ version }}\",\"status\":\"{{ status }}\",\"message\":\"{{ message }}\"}"
    curl -sf -X POST -H "Content-Type: application/json" -d "$payload" "$url" >/dev/null 2>&1 || true

# ═══════════════════════════════════════════════════════════
# Blue-Green Deployment
# ═══════════════════════════════════════════════════════════

[group('blue-green')]
[doc('Deploy using blue-green strategy: deploy to idle slot, switch traffic')]
bg-deploy: _init _acquire-lock
    #!/usr/bin/env bash
    set -euo pipefail

    # Determine which slot is live and which is idle
    live_slot=$(just _get-live-slot)
    if [ "$live_slot" = "blue" ]; then
        target="green"
    else
        target="blue"
    fi

    printf '\n\033[1m════════════════════════════════════════════════\033[0m\n'
    printf '\033[1m  Blue-Green Deploy: {{ version }}\033[0m\n'
    printf '\033[1m  Live: %-6s  Target: %-6s\033[0m\n' "$live_slot" "$target"
    printf '\033[1m════════════════════════════════════════════════\033[0m\n\n'

    just _log "bg-deploy-start: version={{ version }} live=$live_slot target=$target"
    just _notify deploying "Blue-green deploy of {{ version }} to $target slot"

    # ── Stage 1: Deploy to target slot ──
    printf '\033[36m[1/4] Deploying to %s slot...\033[0m\n' "$target"
    just _set-slot-version "$target" "{{ version }}"
    echo "healthy" > {{ state_dir }}/${target}/health
    printf '\033[32m  Deployed {{ version }} to %s\033[0m\n' "$target"

    # ── Stage 2: Health check target ──
    printf '\033[36m[2/4] Verifying %s slot health...\033[0m\n' "$target"
    if ! just _health-check "$target"; then
        printf '\033[31m  Target slot unhealthy — aborting deploy\033[0m\n'
        just _log "bg-deploy-abort: target=$target health-check-failed"
        just _notify failure "Blue-green deploy aborted: $target slot unhealthy"
        just _release-lock
        exit 1
    fi

    # ── Stage 3: Switch traffic ──
    printf '\033[36m[3/4] Switching traffic to %s...\033[0m\n' "$target"
    just _set-live-slot "$target"
    printf '\033[32m  Traffic now routed to %s\033[0m\n' "$target"

    # ── Stage 4: Verify live slot ──
    printf '\033[36m[4/4] Verifying live traffic on %s...\033[0m\n' "$target"
    if ! just _health-check "$target" 3 1; then
        printf '\033[31m  Post-switch health check failed — rolling back to %s\033[0m\n' "$live_slot"
        just _set-live-slot "$live_slot"
        just _log "bg-deploy-rollback: reverted to $live_slot"
        just _notify failure "Blue-green deploy rolled back to $live_slot"
        just _release-lock
        exit 1
    fi

    just _release-lock
    just _log "bg-deploy-success: version={{ version }} slot=$target"
    just _notify success "Blue-green deploy of {{ version }} to $target complete"

    printf '\n\033[32m════════════════════════════════════════════════\033[0m\n'
    printf '\033[32m  Deploy successful\033[0m\n'
    printf '\033[32m  Live slot: %s  Version: {{ version }}\033[0m\n' "$target"
    printf '\033[32m════════════════════════════════════════════════\033[0m\n'

[group('blue-green')]
[doc('Instantly rollback by switching traffic to the previous slot')]
bg-rollback: _acquire-lock
    #!/usr/bin/env bash
    set -euo pipefail
    live_slot=$(just _get-live-slot)
    if [ "$live_slot" = "blue" ]; then
        rollback_to="green"
    else
        rollback_to="blue"
    fi

    rollback_version=$(just _get-slot-version "$rollback_to")
    if [ "$rollback_version" = "none" ]; then
        printf '\033[31mNo previous deployment to roll back to.\033[0m\n'
        just _release-lock
        exit 1
    fi

    printf '\033[33mRolling back: %s -> %s (version: %s)\033[0m\n' "$live_slot" "$rollback_to" "$rollback_version"

    # Health-check the rollback target first
    if ! just _health-check "$rollback_to" 3 1; then
        printf '\033[31mRollback target %s is also unhealthy. Manual intervention required.\033[0m\n' "$rollback_to"
        just _release-lock
        exit 1
    fi

    just _set-live-slot "$rollback_to"
    just _log "bg-rollback: $live_slot -> $rollback_to (version=$rollback_version)"
    just _notify rollback "Rolled back to $rollback_to ($rollback_version)"
    just _release-lock

    printf '\033[32mRollback complete. Live slot: %s (version: %s)\033[0m\n' "$rollback_to" "$rollback_version"

# ═══════════════════════════════════════════════════════════
# Canary Deployment
# ═══════════════════════════════════════════════════════════

[group('canary')]
[doc('Deploy using canary strategy: gradual traffic shift with health monitoring')]
canary-deploy: _init _acquire-lock
    #!/usr/bin/env bash
    set -euo pipefail

    steps="10 25 50 75 100"

    printf '\n\033[1m════════════════════════════════════════════════\033[0m\n'
    printf '\033[1m  Canary Deploy: {{ version }}\033[0m\n'
    printf '\033[1m  Steps: %s%%\033[0m\n' "$(echo $steps | tr ' ' ', ')"
    printf '\033[1m════════════════════════════════════════════════\033[0m\n\n'

    just _log "canary-deploy-start: version={{ version }}"
    just _notify deploying "Canary deploy of {{ version }} starting"

    # Deploy canary instance
    printf '\033[36m  Deploying canary instance...\033[0m\n'
    echo "{{ version }}" > {{ state_dir }}/canary-version
    echo "healthy" > {{ state_dir }}/canary-health

    for pct in $steps; do
        printf '\n\033[36m  ── Canary at %d%% ──\033[0m\n' "$pct"
        echo "$pct" > {{ state_dir }}/canary-weight

        # Simulate traffic shift
        printf '    Traffic: canary=%d%% stable=%d%%\n' "$pct" "$((100 - pct))"

        # Health check at this weight
        canary_health=$(cat {{ state_dir }}/canary-health 2>/dev/null || echo "unknown")
        if [ "$canary_health" != "healthy" ]; then
            printf '    \033[31mCanary unhealthy at %d%% — initiating rollback\033[0m\n' "$pct"
            just _log "canary-rollback: unhealthy at ${pct}%"
            just _notify failure "Canary deploy of {{ version }} failed at ${pct}%"

            # Rollback: set weight to 0
            echo "0" > {{ state_dir }}/canary-weight
            rm -f {{ state_dir }}/canary-version
            printf '    \033[32mRollback complete. All traffic on stable.\033[0m\n'
            just _release-lock
            exit 1
        fi

        printf '    \033[32mHealth: OK\033[0m\n'

        # Simulated monitoring window
        if [ "$pct" -lt 100 ]; then
            monitor_seconds=2
            printf '    Monitoring for %ds...\n' "$monitor_seconds"
            sleep "$monitor_seconds"
        fi
    done

    # Canary is now at 100% — promote to stable
    printf '\n\033[36m  Promoting canary to stable...\033[0m\n'
    live_slot=$(just _get-live-slot)
    target_slot="blue"
    [ "$live_slot" = "blue" ] && target_slot="green"

    just _set-slot-version "$target_slot" "{{ version }}"
    echo "healthy" > {{ state_dir }}/${target_slot}/health
    just _set-live-slot "$target_slot"

    rm -f {{ state_dir }}/canary-version {{ state_dir }}/canary-weight {{ state_dir }}/canary-health

    just _release-lock
    just _log "canary-deploy-success: version={{ version }} promoted to $target_slot"
    just _notify success "Canary deploy of {{ version }} promoted to stable"

    printf '\n\033[32m════════════════════════════════════════════════\033[0m\n'
    printf '\033[32m  Canary deploy successful\033[0m\n'
    printf '\033[32m  Version: {{ version }} promoted to %s\033[0m\n' "$target_slot"
    printf '\033[32m════════════════════════════════════════════════\033[0m\n'

[group('canary')]
[doc('Abort an in-progress canary and route all traffic to stable')]
canary-abort: _acquire-lock
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f {{ state_dir }}/canary-version ]; then
        printf '\033[33mNo active canary deployment.\033[0m\n'
        just _release-lock
        exit 0
    fi

    canary_version=$(cat {{ state_dir }}/canary-version)
    current_weight=$(cat {{ state_dir }}/canary-weight 2>/dev/null || echo "0")

    printf '\033[33mAborting canary: %s at %s%%\033[0m\n' "$canary_version" "$current_weight"
    echo "0" > {{ state_dir }}/canary-weight
    rm -f {{ state_dir }}/canary-version {{ state_dir }}/canary-health

    just _log "canary-abort: version=$canary_version at ${current_weight}%"
    just _notify rollback "Canary $canary_version aborted at ${current_weight}%"
    just _release-lock

    printf '\033[32mCanary aborted. All traffic on stable.\033[0m\n'

[group('canary')]
[doc('Simulate a canary health failure to test rollback')]
canary-inject-failure:
    @echo "unhealthy" > {{ state_dir }}/canary-health
    @printf '\033[33mInjected canary health failure.\033[0m\n'

# ═══════════════════════════════════════════════════════════
# Status & History
# ═══════════════════════════════════════════════════════════

[group('status')]
[doc('Show current deployment state for all slots')]
status: _init
    #!/usr/bin/env bash
    set -euo pipefail
    live=$(just _get-live-slot)
    blue_ver=$(just _get-slot-version blue)
    green_ver=$(just _get-slot-version green)
    canary_ver=$(cat {{ state_dir }}/canary-version 2>/dev/null || echo "none")
    canary_weight=$(cat {{ state_dir }}/canary-weight 2>/dev/null || echo "0")
    locked="no"
    [ -f {{ lock_file }} ] && locked="yes ($(cat {{ lock_file }}))"

    printf '\n\033[1m  Deployment Status\033[0m\n'
    printf '  ─────────────────────────────────────\n'
    printf '  Live slot:      %s\n' "$live"
    printf '  Blue:           %s %s\n' "$blue_ver" "$([ "$live" = "blue" ] && echo "(LIVE)" || echo "")"
    printf '  Green:          %s %s\n' "$green_ver" "$([ "$live" = "green" ] && echo "(LIVE)" || echo "")"
    if [ "$canary_ver" != "none" ]; then
        printf '  Canary:         %s (%s%% traffic)\n' "$canary_ver" "$canary_weight"
    fi
    printf '  Deploy lock:    %s\n' "$locked"
    printf '  ─────────────────────────────────────\n\n'

[group('status')]
[doc('Show deployment history log')]
history:
    #!/usr/bin/env bash
    if [ -f {{ history_file }} ]; then
        echo "=== Deployment History ==="
        cat {{ history_file }}
    else
        echo "No deployment history."
    fi

# ═══════════════════════════════════════════════════════════
# Cleanup
# ═══════════════════════════════════════════════════════════

[group('status')]
[doc('Remove all deployment state')]
[confirm('Remove ALL deployment state including history?')]
clean:
    rm -rf {{ state_dir }}
    @printf '\033[32mAll deployment state removed.\033[0m\n'
```

## Verify What You Learned

```bash
# Initialize and check status
just status

# Run a blue-green deploy
just bg-deploy

# Check which slot is now live
just status

# Run another deploy (switches to the other slot)
just bg-deploy

# View deployment history
just history

# Test blue-green rollback
just bg-rollback

# Run a canary deploy with gradual traffic shift
just canary-deploy

# Test canary failure handling:
# In one terminal: just canary-deploy
# In another terminal: just canary-inject-failure
# Observe automatic rollback

# Verify the lock prevents concurrent deploys
just bg-deploy &
just bg-deploy  # Should fail with lock error
```

## What's Next

This is the final exercise in the challenges series. You now have a comprehensive toolkit for building production-grade automation with just. Consider combining patterns from multiple exercises: the plugin system (Exercise 35) with the testing framework (Exercise 37), or the caching orchestrator (Exercise 36) with the deployment strategies from this exercise.

## Summary

- Blue-green deployment maintains two identical slots and switches traffic atomically between them
- Canary deployment shifts traffic incrementally (10% -> 25% -> 50% -> 75% -> 100%) with health checks at each stage
- A file-based lock prevents concurrent deployments from corrupting state
- Automatic rollback triggers when health checks fail at any stage
- Deployment history provides an audit trail of every deploy, rollback, and abort
- Webhook notifications keep the team informed regardless of outcome
- The entire system runs in simulation mode without real infrastructure, making it safe to experiment

## Reference

- [Shebang recipes](https://just.systems/man/en/shebang-recipes.html)
- [Confirm attribute](https://just.systems/man/en/confirm-attribute.html)
- [Environment variables](https://just.systems/man/en/environment-variables.html)

## Additional Resources

- [Blue-green deployment pattern (Martin Fowler)](https://martinfowler.com/bliki/BlueGreenDeployment.html)
- [Canary releases (Martin Fowler)](https://martinfowler.com/bliki/CanaryRelease.html)
- [AWS blue/green deployments](https://docs.aws.amazon.com/whitepapers/latest/blue-green-deployments/welcome.html)
