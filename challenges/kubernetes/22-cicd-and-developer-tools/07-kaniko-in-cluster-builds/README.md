# Kaniko: In-Cluster Container Image Builds

<!--
difficulty: advanced
concepts: [kaniko, in-cluster-builds, container-images, dockerfile, registry-authentication, build-caching]
tools: [kubectl, kaniko, docker]
estimated_time: 35m
bloom_level: analyze
prerequisites: [docker-basics, kubectl-basics]
-->

## Overview

Kaniko builds container images inside a Kubernetes cluster without requiring a Docker daemon. It executes Dockerfile commands in userspace, making it secure for multi-tenant clusters where giving containers access to the Docker socket is not acceptable. Kaniko is commonly used in Tekton pipelines and other Kubernetes-native CI/CD systems.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                Traditional Build (Docker daemon)              │
│                                                                │
│  Build Pod ──docker.sock──▶ Docker Daemon ──▶ Registry        │
│  (security risk: host access)                                 │
│                                                                │
├──────────────────────────────────────────────────────────────┤
│                Kaniko Build (daemonless)                       │
│                                                                │
│  Kaniko Pod ──builds in userspace──▶ Registry                 │
│  (no docker.sock, no host access, no privilege escalation)    │
└──────────────────────────────────────────────────────────────┘
```

Kaniko executes each Dockerfile instruction inside the container, snapshots the filesystem after each layer, and pushes the final image to a registry. No Docker daemon is needed.

## Suggested Steps

### 1. Set Up Registry Credentials

Kaniko needs credentials to push images to a container registry.

```bash
# Create a secret with registry credentials
kubectl create secret docker-registry kaniko-registry-creds \
  --docker-server=ghcr.io \
  --docker-username=YOUR_USERNAME \
  --docker-password=YOUR_TOKEN \
  --docker-email=your@email.com

# For local testing with a cluster-internal registry:
kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry
  template:
    metadata:
      labels:
        app: registry
    spec:
      containers:
        - name: registry
          image: registry:2
          ports:
            - containerPort: 5000
---
apiVersion: v1
kind: Service
metadata:
  name: registry
spec:
  selector:
    app: registry
  ports:
    - port: 5000
      targetPort: 5000
EOF
```

### 2. Create a Build Context

Kaniko needs a Dockerfile and build context. You can provide it via a ConfigMap, Git repository, or cloud storage.

```bash
# Create a simple Dockerfile in a ConfigMap
kubectl create configmap build-context \
  --from-literal=Dockerfile='FROM nginx:1.27
COPY index.html /usr/share/nginx/html/index.html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]' \
  --from-literal=index.html='<html><body><h1>Built by Kaniko!</h1></body></html>'
```

### 3. Run a Kaniko Build as a Pod

```yaml
# kaniko-build.yaml
apiVersion: v1
kind: Pod
metadata:
  name: kaniko-build
spec:
  restartPolicy: Never
  containers:
    - name: kaniko
      image: gcr.io/kaniko-project/executor:latest
      args:
        - --dockerfile=Dockerfile
        - --context=dir:///workspace               # build context directory
        - --destination=registry.default.svc:5000/myapp:v1  # push to in-cluster registry
        - --insecure                                # allow HTTP registry (for local registry)
        - --cache=true                              # enable layer caching
        - --cache-repo=registry.default.svc:5000/cache  # cache repository
        - --verbosity=info
      volumeMounts:
        - name: build-context
          mountPath: /workspace
        # For external registries, mount credentials:
        # - name: kaniko-secret
        #   mountPath: /kaniko/.docker
  volumes:
    - name: build-context
      configMap:
        name: build-context
    # For external registries:
    # - name: kaniko-secret
    #   secret:
    #     secretName: kaniko-registry-creds
    #     items:
    #       - key: .dockerconfigjson
    #         path: config.json
```

```bash
kubectl apply -f kaniko-build.yaml

# Watch the build progress
kubectl logs kaniko-build -f

# Wait for completion
kubectl wait --for=condition=ready=false pod/kaniko-build --timeout=300s
kubectl get pod kaniko-build
```

### 4. Build from a Git Repository

```yaml
# kaniko-git-build.yaml
apiVersion: v1
kind: Pod
metadata:
  name: kaniko-git-build
spec:
  restartPolicy: Never
  initContainers:
    - name: git-clone
      image: alpine/git:2.43.0
      command: ["git", "clone", "https://github.com/GoogleContainerTools/kaniko.git", "/workspace"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  containers:
    - name: kaniko
      image: gcr.io/kaniko-project/executor:latest
      args:
        - --dockerfile=/workspace/deploy/Dockerfile
        - --context=dir:///workspace
        - --destination=registry.default.svc:5000/kaniko-demo:latest
        - --insecure
        - --single-snapshot                    # faster: single snapshot instead of per-layer
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  volumes:
    - name: workspace
      emptyDir: {}
```

### 5. Integrate with Tekton

```yaml
# kaniko-tekton-task.yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: kaniko-build
spec:
  params:
    - name: IMAGE
      description: "Full image name including registry and tag"
    - name: DOCKERFILE
      default: Dockerfile
    - name: CONTEXT
      default: .
  workspaces:
    - name: source
  results:
    - name: IMAGE_DIGEST
      description: "Digest of the built image"
  steps:
    - name: build-and-push
      image: gcr.io/kaniko-project/executor:latest
      args:
        - --dockerfile=$(params.DOCKERFILE)
        - --context=$(workspaces.source.path)/$(params.CONTEXT)
        - --destination=$(params.IMAGE)
        - --digest-file=$(results.IMAGE_DIGEST.path)
        - --cache=true
```

### 6. Multi-Stage Builds and Optimization

```bash
# Kaniko supports multi-stage builds natively
kubectl create configmap multi-stage-context \
  --from-literal=Dockerfile='
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
' \
  --from-literal=main.go='package main
import "fmt"
import "net/http"
func main() {
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "OK") })
    http.ListenAndServe(":8080", nil)
}'
```

Kaniko flags for optimization:

| Flag | Purpose |
|------|---------|
| `--cache=true` | Enable layer caching |
| `--cache-repo` | Registry path for cache layers |
| `--single-snapshot` | Single snapshot (faster, but no layer reuse) |
| `--snapshotMode=redo` | Faster snapshots for large images |
| `--compressed-caching=false` | Speed up builds at the cost of cache size |
| `--use-new-run` | Experimental faster RUN execution |

## Verify

```bash
# Build completed successfully
kubectl get pod kaniko-build -o jsonpath='{.status.phase}'
# Expected: Succeeded

# Image exists in the registry
kubectl run verify-image --image=registry.default.svc:5000/myapp:v1 --restart=Never --port=80
kubectl wait --for=condition=ready pod/verify-image --timeout=60s
kubectl port-forward pod/verify-image 8080:80 &
curl http://localhost:8080
# Expected: "Built by Kaniko!"
kill %1
kubectl delete pod verify-image
```

## Cleanup

```bash
kubectl delete pod kaniko-build kaniko-git-build --ignore-not-found
kubectl delete configmap build-context multi-stage-context --ignore-not-found
kubectl delete deployment registry --ignore-not-found
kubectl delete service registry --ignore-not-found
kubectl delete secret kaniko-registry-creds --ignore-not-found
```

## Reference

- [Kaniko](https://github.com/GoogleContainerTools/kaniko)
- [Kaniko Build Contexts](https://github.com/GoogleContainerTools/kaniko#kaniko-build-contexts)
- [Kaniko Caching](https://github.com/GoogleContainerTools/kaniko#caching)
