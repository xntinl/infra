# 2. StatefulSet Scaling and Pod Management Policy

<!--
difficulty: intermediate
concepts: [statefulset-scaling, pod-management-policy, ordered-ready, scale-down-guarantees]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [03-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (StatefulSets and Persistent Storage)](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md)
- A default StorageClass (`kubectl get storageclass`)

## Learning Objectives

- **Understand** the ordered scaling guarantees of StatefulSets (scale-up and scale-down order)
- **Apply** manual scaling to observe pod creation and termination order
- **Analyze** the difference between `OrderedReady` and `Parallel` pod management policies

## Why Scaling Behavior Matters

When you scale a Deployment from 3 to 5 replicas, Kubernetes creates 2 new pods simultaneously. StatefulSets behave differently. By default, they use `OrderedReady` pod management, which creates pods sequentially and requires each pod to be Running and Ready before starting the next. Scale-down removes pods in reverse ordinal order. This matters for clustered systems where pod-0 might be a primary and must exist before replicas join.

## Step 1: Deploy a StatefulSet with OrderedReady

Create a headless Service and StatefulSet:

```yaml
# ordered-sts.yaml
apiVersion: v1
kind: Service
metadata:
  name: web-headless
spec:
  clusterIP: None
  selector:
    app: web-ordered
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web-ordered
spec:
  serviceName: web-headless
  replicas: 2
  podManagementPolicy: OrderedReady   # Default — pods created one at a time
  selector:
    matchLabels:
      app: web-ordered
  template:
    metadata:
      labels:
        app: web-ordered
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
```

Apply and watch the ordered creation:

```bash
kubectl apply -f ordered-sts.yaml
kubectl get pods -l app=web-ordered -w
```

You will see `web-ordered-0` reach Running before `web-ordered-1` starts.

## Step 2: Scale Up and Observe Ordering

Scale from 2 to 4 replicas:

```bash
kubectl scale statefulset web-ordered --replicas=4
kubectl get pods -l app=web-ordered -w
```

Expected behavior: `web-ordered-2` starts first, then after it is Ready, `web-ordered-3` begins.

## Step 3: Scale Down and Observe Reverse Order

Scale back to 2:

```bash
kubectl scale statefulset web-ordered --replicas=2
kubectl get pods -l app=web-ordered -w
```

Expected behavior: `web-ordered-3` terminates first, then `web-ordered-2`. The highest ordinal is always removed first.

## Step 4: Compare with Parallel Pod Management

Create a second StatefulSet using `Parallel` policy:

```yaml
# parallel-sts.yaml
apiVersion: v1
kind: Service
metadata:
  name: web-parallel-headless
spec:
  clusterIP: None
  selector:
    app: web-parallel
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web-parallel
spec:
  serviceName: web-parallel-headless
  replicas: 4
  podManagementPolicy: Parallel      # All pods created simultaneously
  selector:
    matchLabels:
      app: web-parallel
  template:
    metadata:
      labels:
        app: web-parallel
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
```

```bash
kubectl apply -f parallel-sts.yaml
kubectl get pods -l app=web-parallel -w
```

All 4 pods start simultaneously instead of waiting for the previous one.

## Step 5: Verify PVC Behavior During Scale-Down

Check PVCs after scaling `web-ordered` down:

```bash
kubectl get pvc
```

If the StatefulSet had `volumeClaimTemplates`, PVCs would remain after scale-down. This is intentional: scaling back up re-attaches existing PVCs. Data is preserved across scale operations.

## Spot the Bug

A teammate deploys a database cluster that requires pod-0 to initialize the schema before replicas connect:

```yaml
podManagementPolicy: Parallel
```

**What goes wrong?**

<details>
<summary>Explanation</summary>

With `Parallel`, all pods start simultaneously. The replicas attempt to connect to pod-0 before it finishes schema initialization, causing connection failures or data corruption. The fix is `OrderedReady` (the default), which ensures pod-0 is Ready before pod-1 starts.

</details>

## Verify What You Learned

```bash
# OrderedReady StatefulSet should have 2 pods
kubectl get statefulset web-ordered -o jsonpath='{.spec.replicas} {.spec.podManagementPolicy}'
# Expected: 2 OrderedReady

# Parallel StatefulSet should have 4 pods
kubectl get statefulset web-parallel -o jsonpath='{.spec.replicas} {.spec.podManagementPolicy}'
# Expected: 4 Parallel

# All pods running
kubectl get pods -l 'app in (web-ordered, web-parallel)'
```

## Cleanup

```bash
kubectl delete statefulset web-ordered web-parallel
kubectl delete svc web-headless web-parallel-headless
```

## What's Next

You have seen `OrderedReady` vs `Parallel` at a high level. In [exercise 03 (Ordered vs Parallel Pod Management)](../03-statefulset-ordered-vs-parallel/03-statefulset-ordered-vs-parallel.md), you will explore the details of when each policy is appropriate and how readiness gates interact with pod management.

## Summary

- `OrderedReady` (default) creates pods sequentially and removes them in reverse order
- `Parallel` creates and deletes all pods simultaneously, like a Deployment
- Scale-down always removes highest ordinal first in `OrderedReady` mode
- PVCs are retained after scale-down and re-attached on scale-up
- Use `OrderedReady` for databases and clustered systems with initialization dependencies

## Reference

- [StatefulSet Pod Management Policies](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-management-policies)
- [StatefulSet Scaling](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#deployment-and-scaling-guarantees)
