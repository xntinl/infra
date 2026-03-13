# 3. Ordered vs Parallel Pod Management

<!--
difficulty: basic
concepts: [pod-management-policy, ordered-ready, parallel, statefulset-guarantees, readiness-probes]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [03-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (StatefulSets and Persistent Storage)](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md)

## Learning Objectives

- **Remember** the two pod management policies available for StatefulSets
- **Understand** how `OrderedReady` blocks pod creation on readiness and how `Parallel` skips this
- **Apply** both policies and observe the timing differences

## Why Pod Management Policies?

StatefulSets default to `OrderedReady` pod management, which creates pods one at a time in ordinal order (0, 1, 2, ...) and waits for each to become Ready before starting the next. This is essential for clustered databases where the first pod must initialize before replicas join. However, some stateful workloads have no initialization dependencies between pods — they just need stable identities and storage. For those, `Parallel` policy launches all pods at once, dramatically reducing startup time for large replica counts.

Understanding when to use each policy prevents two common problems: unnecessary slow startups (using `OrderedReady` when pods are independent) and initialization races (using `Parallel` when pods have dependencies).

## Step 1: Create an OrderedReady StatefulSet with a Slow Readiness Probe

This StatefulSet simulates a workload that takes time to become ready. The readiness probe delays each pod's Ready status by approximately 10 seconds:

```yaml
# ordered-demo.yaml
apiVersion: v1
kind: Service
metadata:
  name: ordered-demo-svc
spec:
  clusterIP: None
  selector:
    app: ordered-demo
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ordered-demo
spec:
  serviceName: ordered-demo-svc
  replicas: 3
  podManagementPolicy: OrderedReady
  selector:
    matchLabels:
      app: ordered-demo
  template:
    metadata:
      labels:
        app: ordered-demo
    spec:
      containers:
        - name: web
          image: nginx:1.27
          ports:
            - containerPort: 80
          # Simulate slow initialization — takes ~10s to become ready
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 10
            periodSeconds: 2
```

Apply and time the rollout:

```bash
kubectl apply -f ordered-demo.yaml
kubectl get pods -l app=ordered-demo -w
```

Expected behavior: Each pod takes about 10 seconds to become Ready. Total startup time is approximately 30 seconds (10s per pod, sequentially).

## Step 2: Create a Parallel StatefulSet with the Same Probe

```yaml
# parallel-demo.yaml
apiVersion: v1
kind: Service
metadata:
  name: parallel-demo-svc
spec:
  clusterIP: None
  selector:
    app: parallel-demo
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: parallel-demo
spec:
  serviceName: parallel-demo-svc
  replicas: 3
  podManagementPolicy: Parallel
  selector:
    matchLabels:
      app: parallel-demo
  template:
    metadata:
      labels:
        app: parallel-demo
    spec:
      containers:
        - name: web
          image: nginx:1.27
          ports:
            - containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 10
            periodSeconds: 2
```

Apply and watch:

```bash
kubectl apply -f parallel-demo.yaml
kubectl get pods -l app=parallel-demo -w
```

Expected behavior: All 3 pods start simultaneously. Total startup time is approximately 10 seconds (all pods initialize in parallel).

## Step 3: Compare the Timing

List both StatefulSets' pods with their ages:

```bash
kubectl get pods -l 'app in (ordered-demo, parallel-demo)' -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,AGE:.metadata.creationTimestamp --sort-by=.metadata.creationTimestamp
```

The `ordered-demo` pods have creation timestamps spaced ~10 seconds apart. The `parallel-demo` pods have nearly identical timestamps.

## Step 4: Understand the Trade-offs

| Aspect | OrderedReady | Parallel |
|--------|-------------|----------|
| Startup time (N pods) | N x readiness time | 1 x readiness time |
| Scale-down order | Reverse ordinal | All at once |
| Safe for init dependencies | Yes | No |
| Use case | Databases, leader/follower | Caches, independent workers |

## Common Mistakes

### Mistake 1: Assuming Parallel Means No Ordering At All

Pods still get ordinal names (0, 1, 2). `Parallel` only changes creation and deletion timing. Stable identities and per-pod PVCs still work exactly as with `OrderedReady`.

### Mistake 2: Changing podManagementPolicy on an Existing StatefulSet

`podManagementPolicy` is immutable after creation. You must delete and recreate the StatefulSet to change it:

```bash
kubectl delete statefulset ordered-demo --cascade=orphan  # Keep pods running
# Recreate with new policy
kubectl apply -f updated-statefulset.yaml
```

## Verify What You Learned

```bash
# Confirm policies
kubectl get statefulset ordered-demo -o jsonpath='OrderedReady: {.spec.podManagementPolicy}'
kubectl get statefulset parallel-demo -o jsonpath='Parallel: {.spec.podManagementPolicy}'

# All pods running
kubectl get pods -l 'app in (ordered-demo, parallel-demo)' --sort-by=.metadata.name
```

## Cleanup

```bash
kubectl delete statefulset ordered-demo parallel-demo
kubectl delete svc ordered-demo-svc parallel-demo-svc
```

## What's Next

StatefulSets also support rolling updates with a `partition` parameter that lets you update a subset of pods. In [exercise 04 (Partition Rolling Updates)](../04-statefulset-partition-rolling-updates/04-statefulset-partition-rolling-updates.md), you will learn how to stage updates gradually across StatefulSet pods.

## Summary

- `OrderedReady` creates pods one at a time in ordinal order, waiting for readiness between each
- `Parallel` creates all pods simultaneously, dramatically reducing startup time
- Both policies maintain stable identities and per-pod PVCs
- `podManagementPolicy` is immutable after StatefulSet creation
- Use `OrderedReady` when pods have initialization dependencies; use `Parallel` when they are independent
- Scale-down under `OrderedReady` removes pods in reverse ordinal order

## Reference

- [StatefulSet Pod Management Policies](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-management-policies)
- [StatefulSet Basics Tutorial](https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/)

## Additional Resources

- [Kubernetes API Reference: StatefulSet v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/stateful-set-v1/)
- [Forced Rollback](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback)
- [StatefulSet Deployment and Scaling Guarantees](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#deployment-and-scaling-guarantees)
