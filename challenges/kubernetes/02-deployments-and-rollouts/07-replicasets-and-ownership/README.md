# 7. ReplicaSets and Ownership

<!--
difficulty: intermediate
concepts: [replicaset, owner-references, orphan-pods, adoption, garbage-collection]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [02-01, 02-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Your First Deployment)](../01-your-first-deployment/) and [exercise 03 (Declarative Deployment Updates)](../03-declarative-deployment-updates/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Analyze** the ownership chain between Deployments, ReplicaSets, and Pods
- **Apply** commands to inspect ownerReferences and understand garbage collection
- **Evaluate** what happens when you create, orphan, or manually scale ReplicaSets

## Why Understand ReplicaSets?

You rarely create ReplicaSets directly -- Deployments do it for you. But understanding ReplicaSets is essential for debugging. When a rollout behaves unexpectedly, stale ReplicaSets pile up, or Pods appear orphaned, you need to understand the ownership chain. ReplicaSets are the mechanism through which Deployments track revisions and perform rollbacks.

## Step 1: Inspect the ReplicaSet Chain

```yaml
# deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rs-demo
spec:
  replicas: 3
  selector:
    matchLabels:
      app: rs-demo
  template:
    metadata:
      labels:
        app: rs-demo
    spec:
      containers:
        - name: nginx
          image: nginx:1.25
```

```bash
kubectl apply -f deploy.yaml
kubectl rollout status deployment/rs-demo
```

Inspect the ReplicaSet:

```bash
RS_NAME=$(kubectl get rs -l app=rs-demo -o jsonpath='{.items[0].metadata.name}')
echo "ReplicaSet: $RS_NAME"
kubectl get rs "$RS_NAME" -o jsonpath='{.metadata.ownerReferences[0]}' | python3 -m json.tool
```

The `ownerReferences` field shows the Deployment as the owner with `controller: true`.

Inspect a Pod's owner:

```bash
POD_NAME=$(kubectl get pods -l app=rs-demo -o jsonpath='{.items[0].metadata.name}')
kubectl get pod "$POD_NAME" -o jsonpath='{.metadata.ownerReferences[0]}' | python3 -m json.tool
```

The Pod is owned by the ReplicaSet, not the Deployment directly.

## Step 2: Create Multiple ReplicaSets via Updates

Trigger two image updates:

```bash
kubectl set image deployment/rs-demo nginx=nginx:1.26
kubectl rollout status deployment/rs-demo
kubectl set image deployment/rs-demo nginx=nginx:1.27
kubectl rollout status deployment/rs-demo
```

List all ReplicaSets:

```bash
kubectl get rs -l app=rs-demo
```

You should see three ReplicaSets. Only the latest has `DESIRED > 0`. The older ones are kept at 0 replicas for rollback.

Check how many old ReplicaSets are retained:

```bash
kubectl get deployment rs-demo -o jsonpath='{.spec.revisionHistoryLimit}'
```

The default is 10. Setting this lower reduces the number of stale ReplicaSets.

## Step 3: Orphaning Pods

Delete the Deployment with `--cascade=orphan` to leave Pods running:

```bash
kubectl delete deployment rs-demo --cascade=orphan
```

Check what remains:

```bash
kubectl get rs -l app=rs-demo
kubectl get pods -l app=rs-demo
```

The ReplicaSets and Pods still exist but are now orphaned -- no Deployment controls them.

Recreate the Deployment:

```bash
kubectl apply -f deploy.yaml
```

Kubernetes adopts the existing ReplicaSet if the selector matches:

```bash
kubectl get rs -l app=rs-demo
kubectl get deployment rs-demo
```

The Deployment detects the existing ReplicaSet with matching labels and adopts it, performing a rollout if the Pod template differs.

## Step 4: Creating a Standalone ReplicaSet

```yaml
# standalone-rs.yaml
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: standalone-rs
spec:
  replicas: 2
  selector:
    matchLabels:
      app: standalone
  template:
    metadata:
      labels:
        app: standalone
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f standalone-rs.yaml
kubectl get rs standalone-rs
kubectl get pods -l app=standalone
```

A standalone ReplicaSet works but has no rollout or rollback capabilities. If you change the image, existing Pods are NOT replaced -- only new Pods get the new template. This is why Deployments exist on top of ReplicaSets.

## Spot the Bug

A developer deletes a ReplicaSet that belongs to a Deployment:

```bash
kubectl delete rs <deployment-replicaset-hash>
```

What happens?

<details>
<summary>Explanation</summary>

The Deployment controller immediately recreates the ReplicaSet because the Deployment's desired state still requires it. The ReplicaSet is then recreated, and it recreates its Pods. This effectively causes an unplanned restart of all Pods in that ReplicaSet. Never delete ReplicaSets that belong to Deployments -- let the Deployment controller manage them.

</details>

## Verify What You Learned

```bash
kubectl get deployment rs-demo
# Expected: 3/3 Ready

kubectl get rs -l app=rs-demo
# Expected: one or more ReplicaSets, active one has DESIRED=3

kubectl get rs standalone-rs
# Expected: 2/2 Ready, no ownerReferences
```

## Cleanup

```bash
kubectl delete deployment rs-demo
kubectl delete rs standalone-rs
```

## What's Next

You understand the ownership relationship between Deployments and ReplicaSets. The next exercise covers revision history management -- how to inspect, prune, and navigate through Deployment revisions. Continue to [exercise 08 (Deployment Revision History and Management)](../08-deployment-revision-history/).

## Summary

- **ReplicaSets** are created and managed by Deployments; you rarely create them directly
- The **ownership chain** is Deployment -> ReplicaSet -> Pods, tracked via `ownerReferences`
- Old ReplicaSets are kept (at 0 replicas) for rollback, controlled by `revisionHistoryLimit`
- Deleting a Deployment with `--cascade=orphan` leaves ReplicaSets and Pods running
- Standalone ReplicaSets lack rollout and rollback capabilities -- always prefer Deployments

## Reference

- [ReplicaSet](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/) — official concept documentation
- [Garbage Collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/) — owner references and cascading deletion
- [Deployment Revision History](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#revision-history-limit) — revisionHistoryLimit

## Additional Resources

- [Kubernetes API Reference: ReplicaSet v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/replica-set-v1/)
- [Cascading Deletion](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/) — foreground vs background vs orphan
- [Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/) — deletion hooks
