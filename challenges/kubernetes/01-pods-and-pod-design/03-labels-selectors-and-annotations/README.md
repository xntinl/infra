# 3. Labels, Selectors, and Annotations

<!--
difficulty: basic
concepts: [labels, selectors, annotations, matchLabels, matchExpressions, kubectl-filtering]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [01-01, 01-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Pod)](../01-your-first-pod/) and [exercise 02 (Pod Lifecycle and Restart Policies)](../02-pod-lifecycle-and-restart-policies/)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the difference between labels (for selection) and annotations (for metadata)
- **Understand** how equality-based and set-based selectors filter Kubernetes resources
- **Apply** labels to Pods and use selectors to query, group, and manage them

## Why Labels, Selectors, and Annotations?

Every Kubernetes resource has a `metadata` section that can carry labels and annotations. Labels are the primary mechanism for organizing and selecting resources. A Service finds its target Pods through label selectors. A Deployment manages Pods whose labels match its selector. Without labels, there is no connection between a controller and the resources it manages.

Labels are key-value pairs with strict naming rules: keys must be 63 characters or fewer (with an optional prefix), and values must be 63 characters or fewer. They are indexed by the API server, which is why they are fast to query. Annotations, on the other hand, are for storing non-identifying metadata. Build timestamps, git commit hashes, configuration hints for tools -- these belong in annotations. Annotations are not indexed and cannot be used in selectors, but they have no size limit on values.

Understanding the label-selector mechanism is fundamental. It is how Services route traffic, how Deployments manage Pods, how NetworkPolicies target workloads, and how you organize resources across namespaces. This exercise gives you hands-on practice with both concepts.

## Step 1: Create Labeled Pods

Create a set of Pods representing a microservices application:

```yaml
# labeled-pods.yaml
apiVersion: v1
kind: Pod
metadata:
  name: frontend-v1
  labels:
    app: frontend                # Application name
    version: v1                  # Version identifier
    tier: web                    # Architecture tier
    environment: production      # Deployment environment
spec:
  containers:
    - name: nginx
      image: nginx:1.27
---
apiVersion: v1
kind: Pod
metadata:
  name: frontend-v2
  labels:
    app: frontend
    version: v2
    tier: web
    environment: staging
spec:
  containers:
    - name: nginx
      image: nginx:1.27
---
apiVersion: v1
kind: Pod
metadata:
  name: backend-v1
  labels:
    app: backend
    version: v1
    tier: api
    environment: production
spec:
  containers:
    - name: nginx
      image: nginx:1.27
---
apiVersion: v1
kind: Pod
metadata:
  name: cache-v1
  labels:
    app: cache
    version: v1
    tier: data
    environment: production
spec:
  containers:
    - name: redis
      image: redis:7
```

Apply the manifest:

```bash
kubectl apply -f labeled-pods.yaml
```

Verify all 4 Pods are running:

```bash
kubectl get pods --show-labels
```

The `--show-labels` flag displays all labels for each Pod in the output.

## Step 2: Filter with Equality-Based Selectors

Equality-based selectors use `=`, `==`, and `!=`:

```bash
# Find all production Pods
kubectl get pods -l environment=production
```

Expected: `frontend-v1`, `backend-v1`, `cache-v1`.

```bash
# Find all Pods that are NOT production
kubectl get pods -l environment!=production
```

Expected: `frontend-v2`.

```bash
# Combine selectors (AND logic) — production web tier
kubectl get pods -l environment=production,tier=web
```

Expected: `frontend-v1` only.

## Step 3: Filter with Set-Based Selectors

Set-based selectors use `in`, `notin`, and `exists`:

```bash
# Find Pods in the web or api tier
kubectl get pods -l 'tier in (web, api)'
```

Expected: `frontend-v1`, `frontend-v2`, `backend-v1`.

```bash
# Find Pods NOT in the data tier
kubectl get pods -l 'tier notin (data)'
```

Expected: `frontend-v1`, `frontend-v2`, `backend-v1`.

```bash
# Find all Pods that have a version label (regardless of value)
kubectl get pods -l version
```

Expected: all 4 Pods.

## Step 4: Modify Labels on Running Pods

Add a new label to an existing Pod:

```bash
kubectl label pod frontend-v1 team=platform
```

Verify:

```bash
kubectl get pod frontend-v1 --show-labels
```

Overwrite an existing label (requires `--overwrite`):

```bash
kubectl label pod frontend-v2 environment=production --overwrite
```

Remove a label by appending a minus sign:

```bash
kubectl label pod frontend-v2 environment-
```

Verify the label is gone:

```bash
kubectl get pod frontend-v2 --show-labels
```

Restore the label for the remaining exercises:

```bash
kubectl label pod frontend-v2 environment=staging
```

## Step 5: Annotations

Annotations store non-identifying metadata. Add annotations to a Pod:

```bash
kubectl annotate pod frontend-v1 description="Main frontend serving user traffic"
kubectl annotate pod frontend-v1 git-commit="a1b2c3d"
kubectl annotate pod frontend-v1 build-timestamp="2026-03-09T10:30:00Z"
```

View annotations:

```bash
kubectl describe pod frontend-v1 | grep -A 5 "Annotations:"
```

You cannot filter by annotations with `kubectl get -l`. This is the key difference from labels: annotations are for metadata storage, not selection.

Remove an annotation:

```bash
kubectl annotate pod frontend-v1 git-commit-
```

## Step 6: Labels in YAML Selectors

Labels become powerful in controller specs. Here is how a Service would use them:

```yaml
# This is for reference — do not apply
apiVersion: v1
kind: Service
metadata:
  name: frontend-service
spec:
  selector:
    app: frontend              # Matches Pods with this label
    tier: web                  # AND this label
  ports:
    - port: 80
```

And a Deployment uses `matchLabels` or `matchExpressions`:

```yaml
# matchLabels — equality-based
selector:
  matchLabels:
    app: frontend

# matchExpressions — set-based
selector:
  matchExpressions:
    - key: tier
      operator: In
      values: [web, api]
    - key: environment
      operator: NotIn
      values: [staging]
```

## Common Mistakes

### Mistake 1: Using Annotations Where Labels Are Needed

```yaml
# WRONG — annotations cannot be used in selectors
metadata:
  annotations:
    app: frontend
```

If you try to select Pods with `kubectl get pods -l app=frontend`, Pods labeled only in annotations will not appear. Selectors only work with labels.

**Fix:** Use `labels` for any value you need to filter or select by. Use `annotations` for informational metadata that tools or humans read but that Kubernetes itself does not query.

### Mistake 2: Label Key Too Long

```yaml
# WRONG — key exceeds 63 characters
metadata:
  labels:
    this-is-an-extremely-long-label-key-that-exceeds-the-sixty-three-char-limit: "true"
```

Error message:

```
metadata.labels: Invalid value: "this-is-an-extremely-long...": must be no more than 63 characters
```

**Fix:** Keep label keys (the name portion, after any prefix) to 63 characters or fewer. Use a prefix like `mycompany.io/` for organization-specific labels.

## Verify What You Learned

Confirm you can filter by multiple criteria:

```bash
kubectl get pods -l 'tier in (web, api),environment=production' --show-labels
```

Expected: `frontend-v1` and `backend-v1`.

Confirm annotations exist but are not selectable:

```bash
kubectl describe pod frontend-v1 | grep "description"
```

Expected: `description: Main frontend serving user traffic`.

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete pods frontend-v1 frontend-v2 backend-v1 cache-v1
```

Verify nothing remains:

```bash
kubectl get pods
```

## What's Next

Now that you understand how to organize resources with labels and annotations, the next exercise introduces namespaces -- the mechanism for isolating groups of resources within a single cluster. Continue to [exercise 04 (Working with Namespaces)](../04-working-with-namespaces/).

## Summary

- **Labels** are key-value pairs used for identification and selection. They are indexed and queryable.
- **Annotations** store non-identifying metadata (build info, descriptions, tool hints). They are not indexed or selectable.
- **Equality-based selectors** use `=`, `==`, `!=` for exact matching.
- **Set-based selectors** use `in`, `notin`, and `exists` for flexible matching.
- Every controller-to-Pod relationship (Deployment, Service, etc.) is built on label selectors.

## Reference

- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) — official concept documentation
- [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) — non-identifying metadata
- [Recommended Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/) — standardized label conventions

## Additional Resources

- [Well-Known Labels](https://kubernetes.io/docs/reference/labels-annotations-taints/) — labels used by Kubernetes components
- [Field Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/) — querying by spec/status fields
- [kubectl label Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_label/) — full command documentation
