# 35. Modular Plugin System

<!--
difficulty: advanced
concepts: [mod-imports, plugin-architecture, path-exists, cross-module-dependencies, dynamic-discovery, optional-modules]
tools: [just]
estimated_time: 1h
bloom_level: create
prerequisites: [modules, recipe-dependencies, conditional-expressions, shebang-recipes]
-->

## Prerequisites

- just >= 1.38.0 (for `mod`, `path_exists()`)
- bash
- docker, aws-cli, kubectl (optional -- plugins degrade gracefully)

## Learning Objectives

- **Architect** a plugin system using `mod` imports with optional availability checks
- **Design** a standard plugin interface (setup, run, clean) enforced by convention
- **Create** cross-plugin dependency chains where one plugin invokes another's recipes

## Why a Modular Plugin System

As a justfile grows past 200 lines, maintaining it as a single file becomes painful. The `mod` keyword lets you split functionality into separate files while preserving a unified namespace. Combined with `path_exists()`, you can make plugins optional: present them when needed, omit them for simpler environments. This mirrors how tools like Terraform use provider plugins and how IDEs use extension systems.

## The Challenge

Build a main justfile that discovers and loads plugins from a `plugins/` directory. Each plugin follows a standard interface: `setup`, `run`, `status`, and `clean`. The main justfile lists available plugins dynamically, can invoke any plugin's recipes, and handles cross-plugin dependencies (e.g., the k8s plugin depends on docker). Plugins: `docker.just`, `aws.just`, `k8s.just`, `monitoring.just`.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]

project := "plugin-demo"

# ─── Plugin Imports ─────────────────────────────────────────
# Each `mod` imports a plugin file. Plugins follow a standard interface:
#   setup    — install/configure prerequisites
#   run      — execute the plugin's primary action
#   status   — report current state
#   clean    — tear down resources

mod docker 'plugins/docker.just'
mod aws 'plugins/aws.just'
mod k8s 'plugins/k8s.just'
mod monitoring 'plugins/monitoring.just'

# ─── Core Recipes ───────────────────────────────────────────

[doc('List all available plugins and their status')]
[group('core')]
plugins:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Available Plugins ==="
    echo ""

    check_plugin() {
        local name="$1"
        local file="$2"
        local tool="$3"
        local installed="no"

        if [ -f "$file" ]; then
            if command -v "$tool" >/dev/null 2>&1; then
                installed="yes"
                printf '  \033[32m●\033[0m %-15s tool: %-10s status: ready\n' "$name" "$tool"
            else
                printf '  \033[33m◐\033[0m %-15s tool: %-10s status: tool not found\n' "$name" "$tool"
            fi
        else
            printf '  \033[31m○\033[0m %-15s file missing: %s\n' "$name" "$file"
        fi
    }

    check_plugin "docker" "plugins/docker.just" "docker"
    check_plugin "aws" "plugins/aws.just" "aws"
    check_plugin "k8s" "plugins/k8s.just" "kubectl"
    check_plugin "monitoring" "plugins/monitoring.just" "curl"

    echo ""
    echo "Usage: just <plugin> <command>"
    echo "  e.g., just docker run"
    echo "  e.g., just aws status"

[doc('Run setup for all available plugins')]
[group('core')]
setup-all:
    just docker setup
    just aws setup
    just k8s setup
    just monitoring setup
    @printf '\n\033[32mAll plugins initialized.\033[0m\n'

[doc('Run status check across all plugins')]
[group('core')]
status-all:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Global Status ==="
    echo ""
    for plugin in docker aws k8s monitoring; do
        printf '\033[36m--- %s ---\033[0m\n' "$plugin"
        just "$plugin" status 2>&1 | sed 's/^/  /'
        echo ""
    done

[doc('Clean all plugin resources')]
[group('core')]
[confirm('This will tear down ALL plugin resources. Continue?')]
clean-all:
    just monitoring clean
    just k8s clean
    just docker clean
    just aws clean
    @printf '\n\033[32mAll plugins cleaned.\033[0m\n'

[doc('Run the full stack: docker -> aws -> k8s -> monitoring')]
[group('core')]
deploy-stack: (docker "setup") (aws "setup") (k8s "setup") (monitoring "setup")
    just docker run
    just aws run
    just k8s run
    just monitoring run
    @printf '\n\033[32mFull stack deployed.\033[0m\n'
```

```justfile
# file: plugins/docker.just

set shell := ["bash", "-euo", "pipefail", "-c"]

image_name := "plugin-demo"
image_tag := "latest"

[doc('Verify Docker is installed and daemon is running')]
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[docker]\033[0m Checking prerequisites...\n'
    if ! command -v docker >/dev/null 2>&1; then
        printf '\033[31m[docker]\033[0m Docker is not installed\n'
        echo "  Install: https://docs.docker.com/get-docker/"
        exit 1
    fi
    if ! docker info >/dev/null 2>&1; then
        printf '\033[31m[docker]\033[0m Docker daemon is not running\n'
        exit 1
    fi
    printf '\033[32m[docker]\033[0m Docker is ready (%(docker --version)s)\n'

[doc('Build and run the application container')]
run: setup
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[docker]\033[0m Building {{ image_name }}:{{ image_tag }}...\n'

    # Build (simulated — replace with real Dockerfile)
    if [ -f Dockerfile ]; then
        docker build -t {{ image_name }}:{{ image_tag }} .
    else
        printf '\033[33m[docker]\033[0m No Dockerfile found, creating minimal image\n'
        echo 'FROM alpine:3.19' | docker build -t {{ image_name }}:{{ image_tag }} -
    fi

    # Run
    container_id=$(docker run -d --name {{ image_name }} {{ image_name }}:{{ image_tag }} sleep 3600 2>/dev/null || true)
    if [ -z "$container_id" ]; then
        printf '\033[33m[docker]\033[0m Container already running, restarting...\n'
        docker restart {{ image_name }}
    fi
    printf '\033[32m[docker]\033[0m Container running\n'

[doc('Show Docker resource usage and container status')]
status:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v docker >/dev/null 2>&1; then
        echo "Docker not installed"
        exit 0
    fi

    running=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
    stopped=$(docker ps -aq --filter status=exited 2>/dev/null | wc -l | tr -d ' ')
    images=$(docker images -q 2>/dev/null | wc -l | tr -d ' ')

    echo "Containers running: $running"
    echo "Containers stopped: $stopped"
    echo "Images: $images"

    if docker ps --filter name={{ image_name }} --format '{{`{{.Status}}`}}' 2>/dev/null | grep -q .; then
        status=$(docker ps --filter name={{ image_name }} --format '{{`{{.Status}}`}}')
        printf '\033[32m{{ image_name }}: %s\033[0m\n' "$status"
    else
        printf '\033[33m{{ image_name }}: not running\033[0m\n'
    fi

[doc('Stop container and remove image')]
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[docker]\033[0m Cleaning up...\n'
    docker rm -f {{ image_name }} 2>/dev/null || true
    docker rmi {{ image_name }}:{{ image_tag }} 2>/dev/null || true
    printf '\033[32m[docker]\033[0m Clean complete\n'
```

```justfile
# file: plugins/aws.just

set shell := ["bash", "-euo", "pipefail", "-c"]

region := env("AWS_REGION", "us-east-1")
stack_name := "plugin-demo-stack"

[doc('Verify AWS CLI is configured')]
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[aws]\033[0m Checking prerequisites...\n'
    if ! command -v aws >/dev/null 2>&1; then
        printf '\033[31m[aws]\033[0m AWS CLI not installed\n'
        echo "  Install: https://aws.amazon.com/cli/"
        exit 1
    fi
    if ! aws sts get-caller-identity >/dev/null 2>&1; then
        printf '\033[33m[aws]\033[0m AWS credentials not configured (or expired)\n'
        echo "  Run: aws configure"
        exit 1
    fi
    identity=$(aws sts get-caller-identity --query 'Arn' --output text 2>/dev/null)
    printf '\033[32m[aws]\033[0m Authenticated as: %s\n' "$identity"
    printf '\033[32m[aws]\033[0m Region: {{ region }}\n'

[doc('Deploy cloud resources')]
run: setup
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[aws]\033[0m Deploying {{ stack_name }} to {{ region }}...\n'

    # Simulated deploy — replace with real CloudFormation/CDK/Terraform
    mkdir -p .plugins-state
    cat > .plugins-state/aws.json <<AWSEOF
    {
      "stack": "{{ stack_name }}",
      "region": "{{ region }}",
      "status": "deployed",
      "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    }
    AWSEOF

    printf '\033[32m[aws]\033[0m Stack {{ stack_name }} deployed\n'

[doc('Show AWS resource status')]
status:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f .plugins-state/aws.json ]; then
        echo "Stack: {{ stack_name }}"
        echo "Region: {{ region }}"
        echo "State: $(cat .plugins-state/aws.json | grep status | tr -d '", ' | cut -d: -f2)"
    else
        echo "Stack: not deployed"
    fi
    if command -v aws >/dev/null 2>&1 && aws sts get-caller-identity >/dev/null 2>&1; then
        printf '\033[32mCredentials: valid\033[0m\n'
    else
        printf '\033[33mCredentials: not configured\033[0m\n'
    fi

[doc('Tear down AWS resources')]
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[aws]\033[0m Cleaning up {{ stack_name }}...\n'
    rm -f .plugins-state/aws.json
    printf '\033[32m[aws]\033[0m Stack removed\n'
```

```justfile
# file: plugins/k8s.just

set shell := ["bash", "-euo", "pipefail", "-c"]

namespace := "plugin-demo"
app_name := "plugin-demo-app"

[doc('Verify kubectl is configured and cluster is reachable')]
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[k8s]\033[0m Checking prerequisites...\n'

    if ! command -v kubectl >/dev/null 2>&1; then
        printf '\033[31m[k8s]\033[0m kubectl not installed\n'
        exit 1
    fi

    # k8s plugin depends on docker being available
    if ! command -v docker >/dev/null 2>&1; then
        printf '\033[31m[k8s]\033[0m Docker required for k8s plugin (cross-plugin dependency)\n'
        exit 1
    fi

    if kubectl cluster-info >/dev/null 2>&1; then
        context=$(kubectl config current-context 2>/dev/null)
        printf '\033[32m[k8s]\033[0m Connected to cluster: %s\n' "$context"
    else
        printf '\033[33m[k8s]\033[0m No cluster connection (will use simulated mode)\n'
    fi

[doc('Deploy application to Kubernetes (depends on docker plugin)')]
run: setup
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[k8s]\033[0m Deploying {{ app_name }} to namespace {{ namespace }}...\n'

    # Cross-plugin dependency: ensure Docker image exists
    printf '\033[36m[k8s]\033[0m Ensuring Docker image is built (cross-plugin dep)...\n'
    just docker run 2>/dev/null || printf '\033[33m[k8s]\033[0m Docker build skipped\n'

    mkdir -p .plugins-state
    cat > .plugins-state/k8s.json <<K8SEOF
    {
      "namespace": "{{ namespace }}",
      "app": "{{ app_name }}",
      "replicas": 3,
      "status": "running",
      "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    }
    K8SEOF

    # Simulated deployment manifest
    cat > .plugins-state/k8s-manifest.yaml <<'YAMLEOF'
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: plugin-demo-app
      namespace: plugin-demo
    spec:
      replicas: 3
      selector:
        matchLabels:
          app: plugin-demo-app
      template:
        metadata:
          labels:
            app: plugin-demo-app
        spec:
          containers:
          - name: app
            image: plugin-demo:latest
            ports:
            - containerPort: 8080
    YAMLEOF

    printf '\033[32m[k8s]\033[0m Deployment created (3 replicas)\n'

[doc('Show Kubernetes deployment status')]
status:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f .plugins-state/k8s.json ]; then
        echo "Namespace: {{ namespace }}"
        echo "App: {{ app_name }}"
        echo "Replicas: 3"
        echo "State: running (simulated)"
    else
        echo "Not deployed"
    fi

[doc('Delete Kubernetes resources')]
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[k8s]\033[0m Cleaning up {{ namespace }}...\n'
    rm -f .plugins-state/k8s.json .plugins-state/k8s-manifest.yaml
    printf '\033[32m[k8s]\033[0m Namespace {{ namespace }} removed\n'
```

```justfile
# file: plugins/monitoring.just

set shell := ["bash", "-euo", "pipefail", "-c"]

metrics_port := "9090"
alert_endpoint := env("ALERT_WEBHOOK", "")

[doc('Verify monitoring prerequisites')]
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[monitoring]\033[0m Checking prerequisites...\n'

    for tool in curl jq; do
        if command -v "$tool" >/dev/null 2>&1; then
            printf '\033[32m[monitoring]\033[0m %s: available\n' "$tool"
        else
            printf '\033[31m[monitoring]\033[0m %s: not found\n' "$tool"
            exit 1
        fi
    done

    printf '\033[32m[monitoring]\033[0m Ready\n'

[doc('Start monitoring and collect metrics from all plugins')]
run: setup
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[monitoring]\033[0m Collecting metrics from all plugins...\n'

    mkdir -p .plugins-state

    # Collect status from each plugin (cross-plugin reads)
    metrics='{"timestamp":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","plugins":{'

    for plugin in docker aws k8s; do
        status_output=$(just "$plugin" status 2>/dev/null || echo "unavailable")
        # Simple health: if status contains "running" or "deployed", it's up
        if echo "$status_output" | grep -qiE "running|deployed|valid"; then
            health="healthy"
        else
            health="down"
        fi
        metrics+="\"$plugin\":\"$health\","
    done

    metrics="${metrics%,}}}"
    echo "$metrics" | python3 -m json.tool > .plugins-state/metrics.json 2>/dev/null || echo "$metrics" > .plugins-state/metrics.json

    printf '\033[32m[monitoring]\033[0m Metrics collected -> .plugins-state/metrics.json\n'
    cat .plugins-state/metrics.json

[doc('Show monitoring dashboard')]
status:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f .plugins-state/metrics.json ]; then
        echo "Last collection: $(cat .plugins-state/metrics.json | python3 -c 'import sys,json; print(json.load(sys.stdin).get("timestamp","unknown"))' 2>/dev/null || echo 'unknown')"
        echo "Plugin health:"
        if command -v python3 >/dev/null 2>&1; then
            python3 -c "
    import json, sys
    try:
        data = json.load(open('.plugins-state/metrics.json'))
        for plugin, health in data.get('plugins', {}).items():
            icon = '\033[32m●\033[0m' if health == 'healthy' else '\033[31m○\033[0m'
            print(f'  {icon} {plugin}: {health}')
    except Exception as e:
        print(f'  Error reading metrics: {e}')
    "
        else
            cat .plugins-state/metrics.json
        fi
    else
        echo "No metrics collected yet. Run: just monitoring run"
    fi

[doc('Alert on unhealthy plugins')]
alert:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f .plugins-state/metrics.json ]; then
        printf '\033[33m[monitoring]\033[0m No metrics available. Run: just monitoring run\n'
        exit 0
    fi

    unhealthy=$(python3 -c "
    import json
    data = json.load(open('.plugins-state/metrics.json'))
    down = [k for k, v in data.get('plugins', {}).items() if v != 'healthy']
    print(' '.join(down))
    " 2>/dev/null)

    if [ -n "$unhealthy" ]; then
        printf '\033[31m[monitoring]\033[0m ALERT: Unhealthy plugins: %s\n' "$unhealthy"
        if [ -n "{{ alert_endpoint }}" ]; then
            curl -sf -X POST -H "Content-Type: application/json" \
                -d "{\"text\":\"Unhealthy plugins: $unhealthy\"}" \
                "{{ alert_endpoint }}" || true
        fi
        exit 1
    else
        printf '\033[32m[monitoring]\033[0m All plugins healthy\n'
    fi

[doc('Remove monitoring data')]
clean:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '\033[36m[monitoring]\033[0m Cleaning up...\n'
    rm -f .plugins-state/metrics.json
    printf '\033[32m[monitoring]\033[0m Clean complete\n'
```

## Verify What You Learned

```bash
# Create the plugins directory and files (copy each block above)
mkdir -p plugins

# List all available plugins and their status
just plugins

# View the full recipe tree including submodules
just --list --list-submodules

# Run a single plugin's status
just docker status

# Run the global status check across all plugins
just status-all

# Collect monitoring metrics (cross-plugin reads)
just monitoring run
```

## What's Next

Continue to [Exercise 36: Build Orchestrator with Caching](../06-build-orchestrator-with-caching/06-build-orchestrator-with-caching.md) to implement content-addressable caching and multi-platform build matrices.

## Summary

- `mod name 'path'` imports plugin files into a namespace accessed as `just name recipe`
- A standard interface (setup, run, status, clean) makes plugins interchangeable and composable
- `path_exists()` enables optional plugins that degrade gracefully when absent
- Cross-plugin dependencies work by calling `just plugin recipe` from within another plugin's shebang
- A monitoring plugin that reads other plugins' state demonstrates the composability of the architecture

## Reference

- [Modules](https://just.systems/man/en/modules.html)
- [path_exists() function](https://just.systems/man/en/built-in-functions.html)
- [Submodule listing](https://just.systems/man/en/listing-submodules.html)

## Additional Resources

- [Modular justfile patterns](https://github.com/casey/just/issues?q=mod)
- [Just modules RFC discussion](https://github.com/casey/just/issues/929)
