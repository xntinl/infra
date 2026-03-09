# Tilt: Local Kubernetes Development

<!--
difficulty: basic
concepts: [tilt, tiltfile, live-reload, kubernetes-development, port-forwarding, resource-grouping]
tools: [tilt, kubectl, docker]
estimated_time: 30m
bloom_level: understand
prerequisites: [docker-basics, kubectl-basics, deployments]
-->

## Overview

Tilt is a development tool that automates building, deploying, and managing microservices on Kubernetes. Like Skaffold, it provides a build-deploy-watch loop, but Tilt adds a web-based dashboard, live_update for sub-second rebuilds (syncing files without a full image rebuild), and a Starlark-based configuration language (Tiltfile) that supports programming logic.

## Why This Matters

When developing multiple microservices, you need visibility into what is building, what is deploying, and what is failing. Tilt's dashboard shows all services at a glance with logs, build status, and health. The `live_update` feature avoids full Docker rebuilds for interpreted languages, cutting feedback time from 30+ seconds to under 2 seconds.

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or Docker Desktop)
- Docker installed and running
- Tilt installed (`brew install tilt` or [download](https://docs.tilt.dev/install.html))

## Step-by-Step Instructions

### Step 1 -- Create a Sample Application

```bash
mkdir -p ~/tilt-demo/app && cd ~/tilt-demo
```

Create a Python web server:

```python
# app/server.py
from http.server import HTTPServer, BaseHTTPRequestHandler
import json

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'ok')
            return

        self.send_response(200)
        self.end_headers()
        response = {"message": "Hello from Tilt!", "version": "1.0"}
        self.wfile.write(json.dumps(response).encode())

if __name__ == '__main__':
    server = HTTPServer(('0.0.0.0', 8080), Handler)
    print('Server running on port 8080')
    server.serve_forever()
```

```dockerfile
# app/Dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY server.py .
EXPOSE 8080
CMD ["python", "server.py"]
```

### Step 2 -- Create Kubernetes Manifests

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tilt-demo
  labels:
    app: tilt-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tilt-demo
  template:
    metadata:
      labels:
        app: tilt-demo
    spec:
      containers:
        - name: app
          image: tilt-demo-image       # Tilt replaces this
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: tilt-demo
spec:
  selector:
    app: tilt-demo
  ports:
    - port: 80
      targetPort: 8080
```

### Step 3 -- Create the Tiltfile

The Tiltfile uses Starlark (a Python dialect) for configuration:

```python
# Tiltfile

# Build the Docker image
docker_build(
    'tilt-demo-image',                  # image name (matches deployment.yaml)
    './app',                            # build context
    dockerfile='./app/Dockerfile',
    live_update=[                       # sync files without full rebuild
        sync('./app/server.py', '/app/server.py'),
        # Restart the process when server.py changes
        run('kill -HUP 1 || true'),
    ],
)

# Deploy Kubernetes manifests
k8s_yaml('k8s/deployment.yaml')

# Configure the resource in Tilt's dashboard
k8s_resource(
    'tilt-demo',
    port_forwards='8080:8080',          # auto port-forward
    labels=['app'],                     # group in dashboard
)
```

### Step 4 -- Start Tilt

```bash
tilt up
```

Tilt starts and opens a web dashboard at `http://localhost:10350`. The dashboard shows:
- Build status for each image
- Deploy status for each Kubernetes resource
- Streaming logs
- Health status

```bash
# Test the application
curl http://localhost:8080
# Output: {"message": "Hello from Tilt!", "version": "1.0"}
```

### Step 5 -- Live Update

Edit `app/server.py` to change the version:

```python
response = {"message": "Hello from Tilt!", "version": "2.0"}
```

With `live_update`, Tilt syncs only the changed file into the running container without rebuilding the Docker image. The update appears in under 2 seconds.

```bash
curl http://localhost:8080
# Output: {"message": "Hello from Tilt!", "version": "2.0"}
```

### Step 6 -- Multi-Service Tiltfile

Tilt excels at managing multiple services. Here is a more realistic Tiltfile:

```python
# Tiltfile for a multi-service setup

# Frontend
docker_build('frontend-image', './frontend')
k8s_yaml('k8s/frontend.yaml')
k8s_resource('frontend', port_forwards='3000:3000', labels=['frontend'])

# Backend API
docker_build('api-image', './api')
k8s_yaml('k8s/api.yaml')
k8s_resource('api', port_forwards='8080:8080', labels=['backend'])

# Database (use a pre-built image, no build needed)
k8s_yaml('k8s/postgres.yaml')
k8s_resource('postgres', port_forwards='5432:5432', labels=['database'])

# Group resources in the dashboard
config.define_string_list("services")
cfg = config.parse()

# Only run selected services (tilt up -- --services=frontend)
if cfg.get("services"):
    config.set_enabled_resources(cfg.get("services"))
```

### Step 7 -- Useful Tiltfile Functions

```python
# Run a local command
local_resource(
    'run-tests',
    cmd='cd app && python -m pytest',
    deps=['app/'],               # re-run when these files change
    labels=['test'],
)

# Load extensions
load('ext://restart_process', 'docker_build_with_restart')

# Conditional logic
if os.getenv('CI'):
    # Use different settings in CI
    docker_build('app', '.', dockerfile='Dockerfile.ci')
```

## Common Mistakes

1. **Image name mismatch** -- the first argument to `docker_build()` must match the image name in the Kubernetes manifest.
2. **live_update without process restart** -- syncing files only copies them; if the application does not hot-reload, you need to restart the process.
3. **Not using `k8s_resource()`** -- without it, you miss port forwarding and dashboard organization.
4. **Running `tilt up` without a cluster** -- Tilt requires a Kubernetes cluster context. Check `kubectl cluster-info` first.

## Verify

```bash
# Application is accessible
curl http://localhost:8080

# Tilt dashboard is running
curl -s http://localhost:10350/api/view | python3 -m json.tool | head -20

# Pod is running
kubectl get pods -l app=tilt-demo
```

## Cleanup

```bash
# Press Ctrl+C to stop Tilt, then:
tilt down
cd ~ && rm -rf ~/tilt-demo
```

## What's Next

- **Exercise 05** -- Use Telepresence to intercept traffic from a remote cluster to your local machine
- **Exercise 03** -- Build CI/CD pipelines with Tekton

## Summary

- Tilt provides a build-deploy-watch loop with a web dashboard for visibility into all services
- The Tiltfile uses Starlark (Python dialect) enabling conditional logic, loops, and function calls
- `live_update` syncs file changes into running containers without full image rebuilds for sub-second feedback
- `k8s_resource()` configures port forwarding, dashboard grouping, and resource dependencies
- `local_resource()` runs local commands (tests, linters) as part of the dev loop
- Tilt is particularly effective for multi-service microservice development

## Reference

- [Tilt Documentation](https://docs.tilt.dev/)
- [Tiltfile API Reference](https://docs.tilt.dev/api.html)

## Additional Resources

- [Tilt Getting Started](https://docs.tilt.dev/tutorial.html)
- [Live Update Reference](https://docs.tilt.dev/live_update_reference.html)
- [Tiltfile Extensions](https://github.com/tilt-dev/tilt-extensions)
