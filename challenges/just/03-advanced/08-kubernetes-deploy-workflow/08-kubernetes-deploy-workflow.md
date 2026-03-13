# 28. Kubernetes Deploy Workflow

<!--
difficulty: advanced
concepts:
  - Docker image build, tag, push
  - kubectl apply and rolling updates
  - rollback with kubectl rollout undo
  - health check verification
  - namespace management (dev/staging/prod)
  - log tailing
  - port-forward for debugging
tools: [just, docker, kubectl, helm]
estimated_time: 50 minutes
bloom_level: evaluate
prerequisites:
  - just intermediate (dependencies, environment variables, conditional expressions)
  - Docker image building and registry concepts
  - Kubernetes fundamentals (pods, deployments, services, namespaces)
  - kubectl basics
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| docker | 24+ | `docker --version` |
| kubectl | 1.28+ | `kubectl version --client` |

## Learning Objectives

- **Evaluate** deployment strategies (rolling update vs. recreate) and their impact on availability during releases
- **Design** a namespace-per-environment workflow that isolates dev, staging, and production within a single cluster
- **Analyze** rollback procedures and determine when automated rollback is appropriate versus manual intervention

## Why Kubernetes Deployment Orchestration

Kubernetes provides powerful primitives for deployment, but the kubectl CLI requires precise flags, resource names, and awareness of the current context. Deploying the wrong image to the wrong namespace is easy when running commands from memory. A justfile encodes the correct workflow: build the image, push to the registry, apply manifests, wait for rollout, and verify health — all parameterized by environment.

Rolling updates are Kubernetes's default strategy, but they require monitoring. A deployment that passes `kubectl rollout status` may still serve errors if the health check endpoint is misconfigured. This exercise layers application-level health checks on top of Kubernetes rollout status, catching issues that the orchestrator cannot see.

Rollback is equally important. When a deploy goes wrong, `kubectl rollout undo` reverts to the previous revision instantly. But you need to know when to trigger it — and you need the rollback command at your fingertips, not buried in documentation. The justfile makes rollback a single command with the right namespace and deployment name pre-filled.

## Step 1 -- Configuration and Context Management

```just
# justfile

set dotenv-load
set export

# ─── Configuration ──────────────────────────────────────
project    := env("PROJECT_NAME", "myapp")
env_name   := env("K8S_ENV", "dev")
registry   := env("DOCKER_REGISTRY", "ghcr.io/myorg")
image      := registry + "/" + project
version    := trim(read("VERSION"))
image_tag  := image + ":" + version
namespace  := project + "-" + env_name
replicas   := if env_name == "prod" { "3" } else if env_name == "staging" { "2" } else { "1" }
manifests  := "k8s/" + env_name

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

The `namespace` is derived from the project name and environment, preventing cross-environment accidents. The `replicas` count scales with environment criticality.

## Step 2 -- Context and Namespace Management

```just
# ─── Context ────────────────────────────────────────────

# Show current kubectl context and verify alignment
context:
    #!/usr/bin/env bash
    set -euo pipefail
    ctx=$(kubectl config current-context)
    echo "{{ BOLD }}Kubernetes Context{{ NORMAL }}"
    echo "  Context:   $ctx"
    echo "  Namespace: {{ namespace }}"
    echo "  Env:       {{ env_name }}"
    echo "  Replicas:  {{ replicas }}"

# Ensure the target namespace exists
ensure-namespace:
    #!/usr/bin/env bash
    set -euo pipefail
    if kubectl get namespace {{ namespace }} >/dev/null 2>&1; then
        echo "{{ GREEN }}Namespace {{ namespace }} exists{{ NORMAL }}"
    else
        echo "{{ BLUE }}Creating namespace {{ namespace }}...{{ NORMAL }}"
        kubectl create namespace {{ namespace }}
        kubectl label namespace {{ namespace }} \
            app={{ project }} \
            environment={{ env_name }}
        echo "{{ GREEN }}Namespace {{ namespace }} created{{ NORMAL }}"
    fi

# List all project namespaces
namespaces:
    @kubectl get namespaces -l app={{ project }} -o wide
```

## Step 3 -- Docker Image Lifecycle

```just
# ─── Docker ─────────────────────────────────────────────

# Build the Docker image
image-build:
    @echo "{{ BLUE }}Building {{ image_tag }}...{{ NORMAL }}"
    docker build \
        --build-arg VERSION={{ version }} \
        --tag {{ image_tag }} \
        --tag {{ image }}:latest \
        .
    @echo "{{ GREEN }}Image built: {{ image_tag }}{{ NORMAL }}"

# Push image to registry
image-push: image-build
    @echo "{{ BLUE }}Pushing {{ image_tag }}...{{ NORMAL }}"
    docker push {{ image_tag }}
    docker push {{ image }}:latest
    @echo "{{ GREEN }}Image pushed{{ NORMAL }}"

# List recent image tags in the registry
image-list:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BOLD }}Recent images for {{ project }}{{ NORMAL }}"
    docker images {{ image }} --format "table {{{{.Tag}}}}\t{{{{.Size}}}}\t{{{{.CreatedSince}}}}"
```

Note the double-braces (`{{{{.Tag}}}}`) — Just uses `{{ }}` for its own interpolation, so Docker's Go template braces must be escaped by doubling them.

## Step 4 -- Deployment Recipes

```just
# ─── Deploy ─────────────────────────────────────────────

# Full deployment: build, push, apply, verify
deploy: _deploy-preflight image-push apply wait-rollout health-check
    @echo ""
    @echo "{{ GREEN }}{{ BOLD }}Deployed {{ project }} v{{ version }} to {{ namespace }}{{ NORMAL }}"

# Pre-flight checks
[private]
_deploy-preflight: context ensure-namespace
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ env_name }}" == "prod" ]]; then
        echo ""
        echo "{{ RED }}{{ BOLD }}PRODUCTION DEPLOYMENT{{ NORMAL }}"
        echo "  Image:     {{ image_tag }}"
        echo "  Namespace: {{ namespace }}"
        echo "  Replicas:  {{ replicas }}"
        echo ""
        read -p "Type the namespace name to confirm: " confirm
        if [[ "$confirm" != "{{ namespace }}" ]]; then
            echo "{{ RED }}Aborted{{ NORMAL }}"
            exit 1
        fi
    fi

# Apply Kubernetes manifests
apply: ensure-namespace
    @echo "{{ BLUE }}Applying manifests from {{ manifests }}/...{{ NORMAL }}"
    kubectl apply -f {{ manifests }}/ -n {{ namespace }}
    @echo "{{ BLUE }}Setting image to {{ image_tag }}...{{ NORMAL }}"
    kubectl set image deployment/{{ project }} \
        app={{ image_tag }} \
        -n {{ namespace }}
    @echo "{{ GREEN }}Manifests applied{{ NORMAL }}"

# Wait for rollout to complete
wait-rollout timeout='300':
    @echo "{{ BLUE }}Waiting for rollout (timeout: {{ timeout }}s)...{{ NORMAL }}"
    kubectl rollout status deployment/{{ project }} \
        -n {{ namespace }} \
        --timeout={{ timeout }}s
    @echo "{{ GREEN }}Rollout complete{{ NORMAL }}"

# Verify deployment health via application endpoint
health-check:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BLUE }}Running health check...{{ NORMAL }}"
    # Get the service endpoint
    # In a real cluster, this might be an ingress URL or LoadBalancer IP
    # For simplicity, we port-forward briefly and check
    kubectl port-forward svc/{{ project }} 9090:80 -n {{ namespace }} &
    pf_pid=$!
    trap "kill $pf_pid 2>/dev/null" EXIT
    sleep 3

    for i in $(seq 1 5); do
        status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/health 2>/dev/null || echo "000")
        if [[ "$status" == "200" ]]; then
            echo "{{ GREEN }}Health check passed{{ NORMAL }}"
            exit 0
        fi
        echo "  Attempt $i/5: HTTP $status"
        sleep 2
    done
    echo "{{ RED }}Health check failed — consider: just rollback{{ NORMAL }}"
    exit 1
```

The health check starts a temporary port-forward, checks the endpoint, and cleans up via `trap`. This works in any cluster without needing an ingress or external IP.

## Step 5 -- Rollback and History

```just
# ─── Rollback ───────────────────────────────────────────

# Rollback to the previous deployment revision
[confirm("Rollback {{ project }} in {{ namespace }}? (yes/no)")]
rollback:
    @echo "{{ YELLOW }}Rolling back deployment in {{ namespace }}...{{ NORMAL }}"
    kubectl rollout undo deployment/{{ project }} -n {{ namespace }}
    just wait-rollout
    @echo "{{ GREEN }}Rollback complete{{ NORMAL }}"

# Rollback to a specific revision
rollback-to revision:
    @echo "{{ YELLOW }}Rolling back to revision {{ revision }} in {{ namespace }}...{{ NORMAL }}"
    kubectl rollout undo deployment/{{ project }} \
        -n {{ namespace }} \
        --to-revision={{ revision }}
    just wait-rollout

# Show deployment revision history
history:
    @echo "{{ BOLD }}Deployment History ({{ namespace }}){{ NORMAL }}"
    @kubectl rollout history deployment/{{ project }} -n {{ namespace }}
```

## Step 6 -- Debugging and Observability

```just
# ─── Debug ──────────────────────────────────────────────

# Tail logs from all pods in the deployment
logs lines='100':
    kubectl logs deployment/{{ project }} \
        -n {{ namespace }} \
        --tail={{ lines }} \
        --follow \
        --all-containers

# Tail logs from a specific pod
logs-pod pod lines='100':
    kubectl logs {{ pod }} -n {{ namespace }} --tail={{ lines }} --follow

# Port-forward to the service for local debugging
port-forward local_port='8080' remote_port='80':
    @echo "{{ BLUE }}Forwarding localhost:{{ local_port }} → {{ project }}:{{ remote_port }}{{ NORMAL }}"
    kubectl port-forward svc/{{ project }} {{ local_port }}:{{ remote_port }} -n {{ namespace }}

# Open a shell in a running pod
exec pod *args='sh':
    kubectl exec -it {{ pod }} -n {{ namespace }} -- {{ args }}

# Get pod status and recent events
pods:
    @echo "{{ BOLD }}Pods in {{ namespace }}{{ NORMAL }}"
    @kubectl get pods -n {{ namespace }} -o wide
    @echo ""
    @echo "{{ BOLD }}Recent Events{{ NORMAL }}"
    @kubectl get events -n {{ namespace }} --sort-by=.lastTimestamp | tail -10

# Describe the deployment
describe:
    kubectl describe deployment/{{ project }} -n {{ namespace }}

# Get resource usage
top:
    @echo "{{ BOLD }}Resource Usage ({{ namespace }}){{ NORMAL }}"
    @kubectl top pods -n {{ namespace }} 2>/dev/null || echo "Metrics server not available"
```

## Step 7 -- Scaling and Cleanup

```just
# ─── Scale ──────────────────────────────────────────────

# Scale the deployment
scale count:
    @echo "{{ BLUE }}Scaling {{ project }} to {{ count }} replicas in {{ namespace }}...{{ NORMAL }}"
    kubectl scale deployment/{{ project }} --replicas={{ count }} -n {{ namespace }}
    just wait-rollout
    @echo "{{ GREEN }}Scaled to {{ count }} replicas{{ NORMAL }}"

# ─── Cleanup ────────────────────────────────────────────

# Delete all resources in the namespace
[confirm("Delete ALL resources in {{ namespace }}? (yes/no)")]
teardown:
    @echo "{{ RED }}Tearing down {{ namespace }}...{{ NORMAL }}"
    kubectl delete all --all -n {{ namespace }}
    @echo "{{ YELLOW }}Resources deleted. Namespace {{ namespace }} still exists.{{ NORMAL }}"

# Delete the namespace entirely
[confirm("Delete namespace {{ namespace }} and ALL its resources? (yes/no)")]
delete-namespace:
    @echo "{{ RED }}Deleting namespace {{ namespace }}...{{ NORMAL }}"
    kubectl delete namespace {{ namespace }}
    @echo "{{ YELLOW }}Namespace {{ namespace }} deleted{{ NORMAL }}"
```

## Common Mistakes

**Wrong: Deploying without waiting for rollout to complete**
```just
deploy: image-push apply
    @echo "Deployed!"
```
What happens: `kubectl apply` returns immediately. The new pods may be crashing (OOMKilled, bad config, missing secrets), but the recipe reports success. Subsequent pipeline stages (health checks, notifications) run against the old pods.
Fix: Always include `kubectl rollout status --timeout=Ns` after apply. This blocks until all new pods are running or the timeout expires. Pair it with a health check that verifies the application endpoint, not just pod readiness.

**Wrong: Hardcoding namespace instead of deriving from environment**
```just
apply:
    kubectl apply -f k8s/ -n production
```
What happens: Every deploy targets production regardless of the `K8S_ENV` variable. A developer testing a manifest change accidentally applies to production.
Fix: Derive the namespace from the environment: `namespace := project + "-" + env_name`. Use `{{ namespace }}` in every kubectl command. The operator controls the target by setting `K8S_ENV=staging just deploy`.

## Verify What You Learned

```bash
# Show current context and configuration
just context
# Expected: cluster context, namespace, environment, replica count

# Verify namespace creation
K8S_ENV=dev just ensure-namespace
# Expected: "Namespace myapp-dev exists" or "created"

# Show pods in namespace
K8S_ENV=dev just pods
# Expected: pod listing with status and events

# Check deployment history
K8S_ENV=dev just history
# Expected: revision history table

# Verify production deploy requires typed confirmation
K8S_ENV=prod just deploy
# Expected: "Type the namespace name to confirm:" prompt
```

## What's Next

The next exercise ([29. Dynamic Recipes and Conditionals](../09-dynamic-recipes-and-conditionals/09-dynamic-recipes-and-conditionals.md)) explores advanced conditional logic, feature flags, tool detection, and adaptive build systems that adjust behavior based on the environment.

## Summary

- Namespace-per-environment (`project-env`) prevents cross-environment accidents
- Deployment chain: build → push → apply → rollout status → health check
- `kubectl rollout undo` provides instant rollback; track revisions with `rollout history`
- Port-forward enables local debugging without exposing services externally
- Production deploys require typed confirmation (namespace name) for maximum safety
- Health checks via temporary port-forward verify application behavior, not just pod status
- Replica counts scale with environment: 1 for dev, 2 for staging, 3 for production
- `trap` in shebang recipes ensures cleanup of background processes (port-forward)

## Reference

- [Just Conditional Expressions](https://just.systems/man/en/conditional-expressions.html)
- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just Shebang Recipes](https://just.systems/man/en/shebang-recipes.html)

## Additional Resources

- [Kubernetes Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [kubectl Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/)
- [Rolling Update Strategy](https://kubernetes.io/docs/tutorials/kubernetes-basics/update/update-intro/)
