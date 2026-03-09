<!--
difficulty: basic
concepts: [kubectl-scale, replicas, rolling-update, deployment-scaling, statefulset-scaling]
tools: [kubectl]
estimated_time: 20m
bloom_level: understand
prerequisites: [deployments, statefulsets]
-->

# 14.03 - Manual Scaling: replicas, kubectl scale

## Why This Matters

Not every workload needs autoscaling. Batch jobs, staging environments, and well-understood steady-state services may be best served by manual scaling. Understanding the mechanics of `spec.replicas` and `kubectl scale` is also foundational -- the HPA itself simply patches the replica count using the same mechanism.

## What You Will Learn

- How to scale Deployments and StatefulSets with `kubectl scale`
- The difference between imperative (`kubectl scale`) and declarative (`spec.replicas`) scaling
- How scaling interacts with rolling updates and PodDisruptionBudgets
- How StatefulSet scaling is ordered (scale-up and scale-down are sequential)

## Step-by-Step Guide

### 1. Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: manual-scaling
```

### 2. Deploy a Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: manual-scaling
spec:
  replicas: 2                       # declarative replica count
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
```

### 3. Deploy a StatefulSet

```yaml
# statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: cache
  namespace: manual-scaling
spec:
  serviceName: cache
  replicas: 2
  selector:
    matchLabels:
      app: cache
  template:
    metadata:
      labels:
        app: cache
    spec:
      containers:
        - name: redis
          image: redis:7
          ports:
            - containerPort: 6379
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: cache
  namespace: manual-scaling
spec:
  clusterIP: None                   # headless service for StatefulSet
  selector:
    app: cache
  ports:
    - port: 6379
      targetPort: 6379
```

### 4. Scale Imperatively

```bash
# Scale the Deployment up
kubectl scale deployment web -n manual-scaling --replicas=5

# Scale the StatefulSet up (notice sequential pod creation: cache-2, cache-3)
kubectl scale statefulset cache -n manual-scaling --replicas=4

# Scale down
kubectl scale deployment web -n manual-scaling --replicas=1

# Scale the StatefulSet down (pods removed in reverse order: cache-3 first)
kubectl scale statefulset cache -n manual-scaling --replicas=1
```

### 5. PodDisruptionBudget for Safe Scaling

```yaml
# pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-pdb
  namespace: manual-scaling
spec:
  minAvailable: 1                   # at least 1 pod must be available during disruptions
  selector:
    matchLabels:
      app: web
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
kubectl apply -f statefulset.yaml
kubectl apply -f pdb.yaml
```

## Common Mistakes

1. **Scaling to 0 replicas** -- This is allowed for Deployments but means zero availability. Use with caution and never in production without intent.
2. **Imperative scale overridden by GitOps** -- If a GitOps tool (Argo CD, Flux) manages `spec.replicas`, an imperative `kubectl scale` will be reverted on the next sync. Use the declarative approach instead.
3. **Scaling a StatefulSet and expecting parallel creation** -- StatefulSet pods are created one at a time by default (`podManagementPolicy: OrderedReady`). Use `Parallel` if order does not matter.
4. **Forgetting PDB during scale-down** -- A PodDisruptionBudget limits voluntary disruptions but does not prevent `kubectl scale` from reducing `spec.replicas`. Pods are terminated regardless; PDBs protect during evictions.

## Verify

```bash
# 1. Check initial state
kubectl get deployment web -n manual-scaling
kubectl get statefulset cache -n manual-scaling
kubectl get pods -n manual-scaling

# 2. Scale up the Deployment
kubectl scale deployment web -n manual-scaling --replicas=5
kubectl get pods -n manual-scaling -l app=web --watch

# 3. Scale up the StatefulSet and observe ordered creation
kubectl scale statefulset cache -n manual-scaling --replicas=4
kubectl get pods -n manual-scaling -l app=cache --watch

# 4. Scale down and observe reverse ordering for StatefulSet
kubectl scale statefulset cache -n manual-scaling --replicas=1
kubectl get pods -n manual-scaling -l app=cache --watch

# 5. Verify PDB
kubectl get pdb -n manual-scaling

# 6. Confirm final state
kubectl get pods -n manual-scaling
```

## Cleanup

```bash
kubectl delete namespace manual-scaling
```

## What's Next

You now understand the basics of manual and automatic scaling. The next exercise dives into HPA behavior tuning -- configuring scale-up and scale-down policies for production workloads: [14.04 - HPA Behavior: Scale-Up and Scale-Down Policies](../04-hpa-behavior-tuning/).

## Summary

- `kubectl scale` provides imperative, immediate scaling of Deployments and StatefulSets
- `spec.replicas` in the manifest is the declarative approach; GitOps tools enforce it
- StatefulSets scale pods sequentially (ordered by index) by default
- PodDisruptionBudgets protect pods during voluntary evictions, not replica count changes
- Scaling to zero is possible for Deployments but not with the standard HPA
- The HPA internally uses the same replica-patching mechanism as `kubectl scale`

## References

- [Scaling a Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#scaling-a-deployment)
- [StatefulSet Scaling](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#deployment-and-scaling-guarantees)

## Additional Resources

- [PodDisruptionBudgets](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)
- [kubectl scale Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_scale/)
