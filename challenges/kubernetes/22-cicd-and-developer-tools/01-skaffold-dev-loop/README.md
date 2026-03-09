# Skaffold: Kubernetes Dev Loop

<!--
difficulty: basic
concepts: [skaffold, dev-loop, hot-reload, image-building, kubernetes-deployment, port-forwarding]
tools: [skaffold, kubectl, docker]
estimated_time: 30m
bloom_level: understand
prerequisites: [docker-basics, kubectl-basics, deployments]
-->

## Overview

Skaffold automates the build-push-deploy cycle for Kubernetes development. When you run `skaffold dev`, it watches your source code, rebuilds container images on change, redeploys to your cluster, and streams logs -- all in a single command. This eliminates the tedious manual loop of `docker build`, `docker push`, `kubectl apply` during development.

## Why This Matters

Without an automated dev loop, Kubernetes development involves many manual steps for every code change. Skaffold reduces this to a single command, giving you a fast feedback loop comparable to local development. It supports multiple build tools (Docker, Jib, Buildpacks) and deploy tools (kubectl, Helm, Kustomize).

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or Docker Desktop)
- Docker installed and running
- skaffold CLI installed (`brew install skaffold` or [download](https://skaffold.dev/docs/install/))

## Step-by-Step Instructions

### Step 1 -- Create a Sample Application

```bash
mkdir -p ~/skaffold-demo/app && cd ~/skaffold-demo
```

Create a simple Go web server:

```go
// app/main.go
package main

import (
    "fmt"
    "net/http"
)

func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello from Skaffold! Version 1\n")
    })

    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, "ok\n")
    })

    fmt.Println("Server starting on :8080")
    http.ListenAndServe(":8080", nil)
}
```

```dockerfile
# app/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
```

### Step 2 -- Create Kubernetes Manifests

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: skaffold-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: skaffold-demo
  template:
    metadata:
      labels:
        app: skaffold-demo
    spec:
      containers:
        - name: app
          image: skaffold-demo        # Skaffold replaces this with the built image
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: skaffold-demo
spec:
  selector:
    app: skaffold-demo
  ports:
    - port: 80
      targetPort: 8080
```

### Step 3 -- Create the Skaffold Configuration

```yaml
# skaffold.yaml
apiVersion: skaffold/v4beta11
kind: Config
metadata:
  name: skaffold-demo
build:
  artifacts:
    - image: skaffold-demo             # matches the image name in deployment.yaml
      context: app                     # build context directory
      docker:
        dockerfile: Dockerfile
  local:
    push: false                        # don't push to remote registry (local cluster)
manifests:
  rawYaml:
    - k8s/*.yaml                       # Kubernetes manifests to deploy
portForward:
  - resourceType: service
    resourceName: skaffold-demo
    port: 80
    localPort: 8080                    # access at http://localhost:8080
```

### Step 4 -- Start the Dev Loop

```bash
# Start Skaffold in dev mode
skaffold dev

# Skaffold will:
# 1. Build the Docker image
# 2. Deploy to Kubernetes
# 3. Set up port forwarding
# 4. Stream logs
# 5. Watch for file changes
```

Open a browser or use curl to test:

```bash
curl http://localhost:8080
# Output: Hello from Skaffold! Version 1
```

### Step 5 -- Make a Code Change

While `skaffold dev` is running, edit the response message:

```go
// Change this line in app/main.go:
fmt.Fprintf(w, "Hello from Skaffold! Version 2\n")
```

Skaffold detects the change, rebuilds the image, redeploys, and the new version is live within seconds:

```bash
curl http://localhost:8080
# Output: Hello from Skaffold! Version 2
```

### Step 6 -- Explore Skaffold Commands

```bash
# One-time build and deploy (no watching)
skaffold run

# Build only (no deploy)
skaffold build

# Render manifests without deploying
skaffold render

# Delete deployed resources
skaffold delete

# Debug mode (adds debug agents for remote debugging)
skaffold debug
```

## Common Mistakes

1. **Image name mismatch** -- the `image` field in `skaffold.yaml` must exactly match the `image` field in your Kubernetes manifests.
2. **Pushing to a registry when using a local cluster** -- set `local.push: false` when using minikube, kind, or Docker Desktop to avoid unnecessary pushes.
3. **Missing `context` in artifact** -- without the correct build context, Docker cannot find the Dockerfile or source files.
4. **Not cleaning up** -- `skaffold dev` cleans up on Ctrl+C, but `skaffold run` does not. Use `skaffold delete` to remove deployed resources.

## Verify

```bash
# Application is running and accessible
curl http://localhost:8080

# Pod is healthy
kubectl get pods -l app=skaffold-demo

# Service exists
kubectl get svc skaffold-demo
```

## Cleanup

```bash
# If skaffold dev is running, press Ctrl+C (it cleans up automatically)
# Otherwise:
skaffold delete
cd ~ && rm -rf ~/skaffold-demo
```

## What's Next

- **Exercise 02** -- Try Tilt for a similar dev loop with a web UI
- **Exercise 05** -- Use Telepresence to debug services running in a remote cluster

## Summary

- Skaffold automates the build-push-deploy cycle, providing a fast feedback loop for Kubernetes development
- `skaffold dev` watches source files, rebuilds images, redeploys, and streams logs automatically
- Port forwarding provides local access to cluster services without manual `kubectl port-forward`
- The `skaffold.yaml` configuration ties together build artifacts, deploy manifests, and dev settings
- `skaffold run` does a one-time build and deploy; `skaffold dev` watches continuously
- Skaffold cleans up deployed resources when `skaffold dev` is interrupted

## Reference

- [Skaffold Documentation](https://skaffold.dev/docs/)
- [Skaffold Configuration](https://skaffold.dev/docs/references/yaml/)

## Additional Resources

- [Skaffold Quickstart](https://skaffold.dev/docs/quickstart/)
- [Custom Build Scripts](https://skaffold.dev/docs/builders/custom/)
- [Skaffold Profiles](https://skaffold.dev/docs/environment/profiles/)
