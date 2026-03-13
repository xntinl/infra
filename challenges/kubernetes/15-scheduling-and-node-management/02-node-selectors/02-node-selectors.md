<!--
difficulty: basic
concepts: [node-selector, node-labels, well-known-labels, label-management, scheduling-basics]
tools: [kubectl]
estimated_time: 20m
bloom_level: understand
prerequisites: [pods, deployments, labels-and-selectors]
-->

# 15.02 - Node Selectors and Labels

## Why This Matters

Before reaching for complex affinity rules, most scheduling needs can be solved with `nodeSelector` and well-managed node labels. A label like `disktype=ssd` or `gpu=nvidia-a100` on a node, paired with a `nodeSelector` on a pod, is the simplest and most readable way to control placement. Understanding Kubernetes well-known labels is equally important -- they are set automatically and expose zone, architecture, and instance type information.

## What You Will Learn

- How to add, remove, and query node labels
- How `nodeSelector` restricts pod scheduling to labeled nodes
- The most important well-known node labels (`kubernetes.io/os`, `kubernetes.io/arch`, `topology.kubernetes.io/zone`)
- How to use labels for multi-architecture (amd64/arm64) scheduling

## Step-by-Step Guide

### 1. Explore Existing Node Labels

```bash
# List all labels on all nodes
kubectl get nodes --show-labels

# Query specific well-known labels
kubectl get nodes -L kubernetes.io/os -L kubernetes.io/arch -L topology.kubernetes.io/zone
```

### 2. Add Custom Labels

```bash
# Add labels
kubectl label nodes <node-1> environment=production workload=web
kubectl label nodes <node-2> environment=production workload=database

# Verify
kubectl get nodes -L environment -L workload
```

### 3. Pod with nodeSelector

```yaml
# pod-web.yaml
apiVersion: v1
kind: Pod
metadata:
  name: web-pod
spec:
  nodeSelector:
    workload: web                   # must match exactly
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 50m
          memory: 32Mi
```

### 4. Pod Targeting a Specific Architecture

```yaml
# pod-amd64.yaml
apiVersion: v1
kind: Pod
metadata:
  name: amd64-pod
spec:
  nodeSelector:
    kubernetes.io/arch: amd64       # use well-known label
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 50m
          memory: 32Mi
```

### 5. Deployment with nodeSelector for Database Nodes

```yaml
# deployment-database.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: database
spec:
  replicas: 1
  selector:
    matchLabels:
      app: database
  template:
    metadata:
      labels:
        app: database
    spec:
      nodeSelector:
        workload: database          # only on database-labeled nodes
      containers:
        - name: postgres
          image: postgres:16
          env:
            - name: POSTGRES_PASSWORD
              value: example
          ports:
            - containerPort: 5432
          resources:
            requests:
              cpu: 200m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
```

### 6. Filtering Nodes by Label

```bash
# Find all nodes with a specific label
kubectl get nodes -l workload=web

# Find nodes matching multiple labels (AND logic)
kubectl get nodes -l environment=production,workload=web

# Find nodes where a label exists (any value)
kubectl get nodes -l workload

# Find nodes where a label does NOT exist
kubectl get nodes -l '!gpu'
```

### Apply

```bash
kubectl apply -f pod-web.yaml
kubectl apply -f pod-amd64.yaml
kubectl apply -f deployment-database.yaml
```

## Common Mistakes

1. **Typo in label key or value** -- `nodeSelector` requires an exact match. A label `disktype=ssd` will not match a selector for `disk-type=ssd`. Use `kubectl get nodes --show-labels` to verify.
2. **Forgetting that nodeSelector is AND logic** -- If you specify `{disktype: ssd, zone: us-east-1a}`, the node must have BOTH labels. There is no OR with `nodeSelector`; use `nodeAffinity` for that.
3. **Pods stuck in Pending** -- If no node matches the `nodeSelector`, the pod stays Pending indefinitely with no timeout. Always check `kubectl describe pod` for scheduling failure events.
4. **Removing a label from a running node** -- Pods already scheduled on the node are NOT evicted. `nodeSelector` is only evaluated at scheduling time.

## Verify

```bash
# 1. Confirm pod placement
kubectl get pod web-pod -o wide
kubectl get pod amd64-pod -o wide
kubectl get pods -l app=database -o wide

# 2. Verify the web pod is on the correct node
kubectl get pod web-pod -o jsonpath='{.spec.nodeName}'

# 3. Test scheduling failure -- create a pod with a nonexistent label
kubectl run orphan-pod --image=nginx:1.27 --overrides='{"spec":{"nodeSelector":{"nonexistent":"label"}}}'
kubectl get pod orphan-pod
kubectl describe pod orphan-pod | grep -A3 Events

# 4. Clean up the orphan
kubectl delete pod orphan-pod
```

## Cleanup

```bash
kubectl delete pod web-pod amd64-pod
kubectl delete deployment database

# Remove custom labels
kubectl label nodes <node-1> environment- workload-
kubectl label nodes <node-2> environment- workload-
```

## What's Next

Node selectors handle simple cases. The next exercise takes a deeper look at taints and tolerations -- the mechanism that lets you *repel* pods from specific nodes: [15.03 - Taints and Tolerations Deep Dive](../03-taints-and-tolerations/03-taints-and-tolerations.md).

## Summary

- `nodeSelector` is the simplest scheduling constraint: a map of label key-value pairs that must all match
- Well-known labels like `kubernetes.io/arch` and `topology.kubernetes.io/zone` are set automatically
- Label filtering with `kubectl get nodes -l` is essential for verifying node readiness
- `nodeSelector` is AND-only; use `nodeAffinity` for OR conditions or range matching
- Pods stay Pending forever if no node matches; there is no scheduling timeout
- Changing node labels does not affect already-scheduled pods

## References

- [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)
- [Well-Known Labels, Annotations and Taints](https://kubernetes.io/docs/reference/labels-annotations-taints/)

## Additional Resources

- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
- [Node Management](https://kubernetes.io/docs/concepts/architecture/nodes/)
