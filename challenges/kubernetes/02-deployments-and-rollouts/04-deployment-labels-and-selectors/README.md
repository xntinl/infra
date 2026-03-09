# 4. Deployment Labels and Selectors

<!--
difficulty: basic
concepts: [labels, selectors, matchLabels, deployment-pod-relationship, label-immutability]
tools: [kubectl, minikube]
estimated_time: 20m
bloom_level: understand
prerequisites: [02-01, 02-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Deployment)](../01-your-first-deployment/) and [exercise 03 (Declarative Deployment Updates)](../03-declarative-deployment-updates/)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the three levels of labels in a Deployment: Deployment metadata, selector, and Pod template
- **Understand** how `spec.selector.matchLabels` connects a Deployment to its Pods through the ReplicaSet
- **Apply** label-based queries to identify which Pods belong to which Deployment

## Why Labels and Selectors in Deployments?

A Deployment does not manage Pods directly. It creates a ReplicaSet, and the ReplicaSet creates Pods. The glue between these objects is labels. The Deployment's `spec.selector.matchLabels` defines which Pods the Deployment (via its ReplicaSet) claims ownership of. The Pod template's `metadata.labels` must include all the labels in the selector.

This relationship is strict by design. If you could change the selector after creation, a Deployment might accidentally adopt Pods from another Deployment or orphan its own Pods. That is why `spec.selector` is immutable after creation in `apps/v1`. Understanding this label chain prevents common debugging headaches like "my Deployment shows 0 Pods even though I can see Pods running."

## Step 1: Create Two Deployments with Distinct Labels

```yaml
# two-deployments.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-frontend
  labels:
    component: frontend          # Label on the Deployment object
    team: ui
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp                 # Selector — connects to Pods
      component: frontend
  template:
    metadata:
      labels:
        app: myapp               # Pod labels — MUST include selector labels
        component: frontend
        version: v1              # Extra label — not in selector, but useful for filtering
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-backend
  labels:
    component: backend
    team: api
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
      component: backend
  template:
    metadata:
      labels:
        app: myapp
        component: backend
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f two-deployments.yaml
kubectl get deployments --show-labels
```

## Step 2: Query Pods by Labels

Get all Pods for the overall application:

```bash
kubectl get pods -l app=myapp
```

Expected: 4 Pods (2 frontend + 2 backend).

Get only frontend Pods:

```bash
kubectl get pods -l app=myapp,component=frontend
```

Expected: 2 Pods.

Get only backend Pods:

```bash
kubectl get pods -l app=myapp,component=backend
```

Expected: 2 Pods.

## Step 3: Understand the Three Label Levels

There are three distinct places labels appear in a Deployment:

**Level 1: Deployment metadata labels** -- used to organize and query Deployments themselves:

```bash
kubectl get deployments -l team=ui
```

Expected: `app-frontend` only.

**Level 2: Selector matchLabels** -- used by the Deployment/ReplicaSet to find owned Pods. Immutable after creation.

**Level 3: Pod template labels** -- applied to every Pod. Must be a superset of the selector labels. Can include additional labels like `version`.

```bash
# Show Pods with the extra "version" label
kubectl get pods -l version=v1
```

Expected: all 4 Pods.

## Step 4: See the Selector Immutability

Try to change the selector on an existing Deployment:

```bash
kubectl patch deployment app-frontend -p '{"spec":{"selector":{"matchLabels":{"app":"myapp","component":"frontend","tier":"web"}}}}'
```

Error message:

```
The Deployment "app-frontend" is invalid: spec.selector: Invalid value: ...: field is immutable
```

The selector cannot be changed after the Deployment is created. If you need a different selector, you must delete and recreate the Deployment.

## Step 5: What Happens If You Manually Label a Pod

Add a label to a Pod that makes it match the OTHER Deployment's selector:

```bash
FRONTEND_POD=$(kubectl get pods -l component=frontend -o jsonpath='{.items[0].metadata.name}')
kubectl label pod "$FRONTEND_POD" component=backend --overwrite
```

Check what happens:

```bash
kubectl get pods -l component=frontend
kubectl get pods -l component=backend
kubectl get deployment app-frontend
kubectl get deployment app-backend
```

The frontend Deployment sees only 1 Pod matching its selector and creates a replacement. The backend Deployment briefly sees 3 Pods matching its selector and may terminate one. The manually relabeled Pod's `ownerReferences` still point to the frontend ReplicaSet, so this creates confusion.

Fix the label back:

```bash
kubectl label pod "$FRONTEND_POD" component=frontend --overwrite
```

Wait a moment for the controllers to reconcile.

## Common Mistakes

### Mistake 1: Selector Labels Missing from Pod Template

```yaml
# WRONG — selector requires "component: frontend" but Pod template only has "app: myapp"
spec:
  selector:
    matchLabels:
      app: myapp
      component: frontend
  template:
    metadata:
      labels:
        app: myapp
        # Missing: component: frontend
```

Kubernetes rejects this with a validation error. The Pod template labels must be a superset of the selector labels.

**Fix:** Ensure every key-value pair in `selector.matchLabels` also appears in `template.metadata.labels`.

### Mistake 2: Two Deployments with Overlapping Selectors

If two Deployments use the same selector, they will fight over the same Pods, creating an unstable loop of creation and deletion.

**Fix:** Always give each Deployment a unique set of selector labels. A common pattern is to include the component or Deployment name in the selector.

## Verify What You Learned

Confirm both Deployments are healthy:

```bash
kubectl get deployments -l app=myapp
```

Expected:

```
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
app-frontend   2/2     2            2           5m
app-backend    2/2     2            2           5m
```

Confirm label-based filtering works:

```bash
kubectl get pods -l 'component in (frontend, backend)' --show-labels
```

Expected: 4 Pods with clear label distinction.

## Cleanup

Remove all resources:

```bash
kubectl delete deployment app-frontend app-backend
```

Verify:

```bash
kubectl get deployment,pods -l app=myapp
```

## What's Next

You now understand the label-selector mechanism that connects Deployments to Pods. The next exercise covers rolling updates and rollbacks -- how Kubernetes transitions Pods from one version to another without downtime. Continue to [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/).

## Summary

- Deployments have **three levels of labels**: Deployment metadata, selector, and Pod template.
- `spec.selector.matchLabels` is **immutable** after creation and must be a subset of `template.metadata.labels`.
- Use additional Pod template labels (beyond the selector) for versioning, filtering, and canary routing.
- Two Deployments with **overlapping selectors** will conflict. Always use unique selectors.
- Manually changing a Pod's labels can confuse the ownership chain -- let controllers manage labels.

## Reference

- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) — official concept documentation
- [Deployments: Label Selector](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#label-selector-updates) — selector immutability
- [ReplicaSet: Pod Selector](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/#pod-selector) — how ReplicaSets use selectors

## Additional Resources

- [Recommended Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/) — standardized label conventions
- [Well-Known Labels](https://kubernetes.io/docs/reference/labels-annotations-taints/) — Kubernetes system labels
- [Garbage Collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/) — owner references and cascading deletion
