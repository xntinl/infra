# kubectl Mastery: Advanced Commands and Output

<!--
difficulty: basic
concepts: [kubectl, jsonpath, custom-columns, sorting, filtering, dry-run, diff, explain]
tools: [kubectl]
estimated_time: 30m
bloom_level: understand
prerequisites: [kubectl-basics]
-->

## Overview

kubectl is the primary CLI for interacting with Kubernetes clusters. Beyond basic `get` and `describe`, kubectl offers powerful output formatting, filtering, and resource management features. This exercise covers the advanced commands and output techniques that make day-to-day cluster operations efficient.

## Why This Matters

In production, you rarely just run `kubectl get pods`. You need to extract specific fields, sort by resource consumption, filter by labels, and script interactions. Mastering these techniques lets you diagnose issues faster and automate routine tasks.

## Step-by-Step Instructions

### Step 1 -- Set Up Test Resources

Create resources to practice with.

```yaml
# workloads.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kubectl-lab
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-frontend
  namespace: kubectl-lab
  labels:
    app: web
    tier: frontend      # label for filtering exercises
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
      tier: frontend
  template:
    metadata:
      labels:
        app: web
        tier: frontend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
          ports:
            - containerPort: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-backend
  namespace: kubectl-lab
  labels:
    app: api
    tier: backend       # different tier for filtering
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api
      tier: backend
  template:
    metadata:
      labels:
        app: api
        tier: backend
    spec:
      containers:
        - name: api
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 200m
              memory: 256Mi
---
apiVersion: v1
kind: Service
metadata:
  name: web-frontend
  namespace: kubectl-lab
spec:
  selector:
    app: web
    tier: frontend
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: kubectl-lab
data:
  LOG_LEVEL: "info"
  APP_MODE: "production"
```

```bash
kubectl apply -f workloads.yaml
kubectl rollout status deployment/web-frontend -n kubectl-lab
kubectl rollout status deployment/api-backend -n kubectl-lab
```

### Step 2 -- JSONPath Output

JSONPath lets you extract specific fields from Kubernetes resources.

```bash
# Get all pod names in the namespace
kubectl get pods -n kubectl-lab -o jsonpath='{.items[*].metadata.name}'

# Get pod names, one per line
kubectl get pods -n kubectl-lab -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}'

# Get pod name and its node in a table-like format
kubectl get pods -n kubectl-lab \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.nodeName}{"\n"}{end}'

# Get container images used across all pods
kubectl get pods -n kubectl-lab \
  -o jsonpath='{range .items[*]}{.spec.containers[*].image}{"\n"}{end}'

# Get the ClusterIP of a service
kubectl get svc web-frontend -n kubectl-lab \
  -o jsonpath='{.spec.clusterIP}'
```

### Step 3 -- Custom Columns

Custom columns provide clean tabular output without the complexity of JSONPath range expressions.

```bash
# Pod name and status
kubectl get pods -n kubectl-lab \
  -o custom-columns='NAME:.metadata.name,STATUS:.status.phase'

# Pod name, container image, and CPU request
kubectl get pods -n kubectl-lab \
  -o custom-columns='POD:.metadata.name,IMAGE:.spec.containers[0].image,CPU_REQ:.spec.containers[0].resources.requests.cpu'

# All pods across namespaces with their namespace
kubectl get pods -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,NODE:.spec.nodeName'
```

### Step 4 -- Sorting and Filtering

```bash
# Sort pods by creation timestamp
kubectl get pods -n kubectl-lab --sort-by='.metadata.creationTimestamp'

# Sort by restart count
kubectl get pods -n kubectl-lab --sort-by='.status.containerStatuses[0].restartCount'

# Filter by label
kubectl get pods -n kubectl-lab -l tier=frontend
kubectl get pods -n kubectl-lab -l 'tier in (frontend, backend)'
kubectl get pods -n kubectl-lab -l 'tier!=frontend'

# Field selectors (limited to specific fields)
kubectl get pods -n kubectl-lab --field-selector status.phase=Running
kubectl get events -n kubectl-lab --field-selector reason=Pulled
```

### Step 5 -- Dry Run and Diff

```bash
# Generate YAML without creating the resource
kubectl create deployment test-dry \
  --image=nginx:1.27 \
  --dry-run=client -o yaml

# Server-side dry run validates against the API server
kubectl apply -f workloads.yaml --dry-run=server

# See what would change before applying
kubectl diff -f workloads.yaml
```

### Step 6 -- kubectl explain

```bash
# Get documentation for a resource field
kubectl explain pod.spec.containers.resources

# Recursive explanation
kubectl explain pod.spec --recursive | head -60

# Specific API version
kubectl explain deployment.spec.strategy --api-version=apps/v1
```

### Step 7 -- Useful Operational Commands

```bash
# Watch resources in real time
kubectl get pods -n kubectl-lab -w

# Get resource consumption (requires metrics-server)
kubectl top pods -n kubectl-lab
kubectl top pods -n kubectl-lab --sort-by=cpu

# Copy files to/from a pod
kubectl cp kubectl-lab/$(kubectl get pod -n kubectl-lab -l app=web -o jsonpath='{.items[0].metadata.name}'):/etc/nginx/nginx.conf ./nginx.conf

# Execute commands in a pod
kubectl exec -n kubectl-lab deploy/web-frontend -- nginx -v

# Port-forward for local debugging
kubectl port-forward -n kubectl-lab svc/web-frontend 8080:80

# Get all resources in a namespace
kubectl api-resources --verbs=list --namespaced -o name \
  | xargs -n 1 kubectl get --show-kind --ignore-not-found -n kubectl-lab
```

## Common Mistakes

1. **Using `-o jsonpath` without quoting** -- always wrap JSONPath expressions in single quotes to prevent shell expansion.
2. **Forgetting `{"\n"}` in JSONPath range** -- without newlines, all values are concatenated on one line.
3. **Confusing `--dry-run=client` with `--dry-run=server`** -- client-side does no validation; server-side validates against the cluster but does not persist.
4. **Using field selectors for unsupported fields** -- only `metadata.name`, `metadata.namespace`, `status.phase`, and a few others are supported for field selectors.

## Verify

```bash
# Test: extract all pod IPs in the namespace
kubectl get pods -n kubectl-lab \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.status.podIP}{"\n"}{end}'

# Test: custom columns showing pod, tier label, and container count
kubectl get pods -n kubectl-lab \
  -o custom-columns='POD:.metadata.name,TIER:.metadata.labels.tier,CONTAINERS:.spec.containers[*].name'

# Test: list only frontend pods sorted by name
kubectl get pods -n kubectl-lab -l tier=frontend --sort-by='.metadata.name'
```

## Cleanup

```bash
kubectl delete namespace kubectl-lab
```

## What's Next

- **Exercise 03** -- Manage namespaces and their lifecycle
- **Exercise 07** -- Use kubectl drain and cordon for node maintenance

## Summary

- JSONPath (`-o jsonpath`) extracts specific fields from resource JSON for scripting and diagnostics
- Custom columns (`-o custom-columns`) produce clean tabular output with user-defined headers
- Label selectors (`-l`) filter resources by label; field selectors (`--field-selector`) filter by a limited set of fields
- `--dry-run=server` validates changes against the API server without persisting them
- `kubectl diff` shows what an apply would change before executing it
- `kubectl explain` provides inline API documentation for any resource field

## Reference

- [kubectl Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/)
- [JSONPath Support](https://kubernetes.io/docs/reference/kubectl/jsonpath/)
- [kubectl Reference](https://kubernetes.io/docs/reference/generated/kubectl/kubectl-commands)

## Additional Resources

- [Managing Resources](https://kubernetes.io/docs/concepts/cluster-administration/manage-deployment/)
- [kubectl Usage Conventions](https://kubernetes.io/docs/reference/kubectl/conventions/)
