<!--
difficulty: basic
concepts: [taints, tolerations, noschedule, noexecute, prefernoschedule, taint-effects, dedicated-nodes]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [pods, deployments, node-affinity-and-taints]
-->

# 15.03 - Taints and Tolerations Deep Dive

## Why This Matters

While affinity *attracts* pods to nodes, taints *repel* them. This is the foundation for dedicated node pools (GPU nodes, high-memory nodes), maintenance operations (preventing new pods during a drain), and system-level isolation (the control plane taint that keeps workloads off master nodes). Understanding all three taint effects and how tolerations interact with them is essential for production operations.

## What You Will Learn

- The three taint effects: `NoSchedule`, `PreferNoSchedule`, `NoExecute`
- How `NoExecute` evicts already-running pods (with optional `tolerationSeconds`)
- How the `Exists` operator creates wildcard tolerations
- How to create dedicated node pools using taints and tolerations

## Step-by-Step Guide

### 1. Apply Taints with Different Effects

```bash
# NoSchedule: new pods without toleration cannot be scheduled here
kubectl taint nodes <node-1> dedicated=gpu:NoSchedule

# PreferNoSchedule: scheduler tries to avoid this node, but will use it if necessary
kubectl taint nodes <node-2> workload=batch:PreferNoSchedule

# NoExecute: existing pods without toleration are evicted immediately
kubectl taint nodes <node-1> maintenance=true:NoExecute

# Verify taints on a node
kubectl describe node <node-1> | grep -A10 Taints
```

### 2. Pod Tolerating NoSchedule

```yaml
# pod-tolerate-noschedule.yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  tolerations:
    - key: dedicated
      operator: Equal
      value: gpu
      effect: NoSchedule           # matches exactly
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 3. Pod Tolerating NoExecute with Timeout

```yaml
# pod-tolerate-noexecute.yaml
apiVersion: v1
kind: Pod
metadata:
  name: temporary-pod
spec:
  tolerations:
    - key: maintenance
      operator: Equal
      value: "true"
      effect: NoExecute
      tolerationSeconds: 3600      # survive on the tainted node for 1 hour, then evict
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 4. Wildcard Toleration (Tolerate Everything)

```yaml
# pod-tolerate-all.yaml
apiVersion: v1
kind: Pod
metadata:
  name: tolerate-all-pod
spec:
  tolerations:
    - operator: Exists             # no key = matches ALL taints
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 50m
          memory: 32Mi
```

This is how DaemonSet pods (like `kube-proxy`, `calico-node`) run on every node including tainted ones.

### 5. Dedicated Node Pool Pattern

```bash
# Label and taint nodes for a dedicated pool
kubectl label nodes <node-1> pool=gpu
kubectl taint nodes <node-1> pool=gpu:NoSchedule
```

```yaml
# deployment-gpu-pool.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gpu-workload
spec:
  replicas: 2
  selector:
    matchLabels:
      app: gpu-workload
  template:
    metadata:
      labels:
        app: gpu-workload
    spec:
      nodeSelector:
        pool: gpu                   # attract: only schedule on gpu nodes
      tolerations:
        - key: pool
          operator: Equal
          value: gpu
          effect: NoSchedule        # allow: pass the taint gate
      containers:
        - name: app
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
```

### 6. View Taints on All Nodes

```bash
# See all taints across the cluster
kubectl get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints'
```

### Apply

```bash
kubectl apply -f pod-tolerate-noschedule.yaml
kubectl apply -f pod-tolerate-noexecute.yaml
kubectl apply -f pod-tolerate-all.yaml
kubectl apply -f deployment-gpu-pool.yaml
```

## Common Mistakes

1. **Forgetting that tolerations do not attract** -- A toleration only *permits* scheduling on a tainted node. Without affinity or nodeSelector, the scheduler may place the pod on an untainted node instead. Always pair taints with affinity for dedicated pools.
2. **Not understanding NoExecute on existing pods** -- Applying a `NoExecute` taint to a node evicts all pods that lack a matching toleration, including pods that were running before the taint was added.
3. **Missing the `tolerationSeconds` field** -- Without it, a NoExecute-tolerating pod stays forever. With it, the pod is evicted after the timeout. This is useful for graceful migration during maintenance.
4. **Control plane taint confusion** -- Master nodes have `node-role.kubernetes.io/control-plane:NoSchedule` by default. User workloads do not need to tolerate this unless you want to run on master nodes.

## Verify

```bash
# 1. Check taints on all nodes
kubectl get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints'

# 2. Verify gpu-pod can schedule on tainted node
kubectl get pod gpu-pod -o wide

# 3. Verify temporary-pod with tolerationSeconds
kubectl get pod temporary-pod -o wide
kubectl describe pod temporary-pod | grep -A5 Tolerations

# 4. Verify tolerate-all-pod can go anywhere
kubectl get pod tolerate-all-pod -o wide

# 5. Verify dedicated pool deployment
kubectl get pods -l app=gpu-workload -o wide

# 6. Create a pod WITHOUT toleration and verify it stays Pending
kubectl run blocked-pod --image=nginx:1.27 --overrides='{"spec":{"nodeSelector":{"pool":"gpu"}}}'
kubectl get pod blocked-pod
kubectl describe pod blocked-pod | grep -A5 Events
kubectl delete pod blocked-pod
```

## Cleanup

```bash
kubectl delete pod gpu-pod temporary-pod tolerate-all-pod
kubectl delete deployment gpu-workload

# Remove taints
kubectl taint nodes <node-1> dedicated=gpu:NoSchedule-
kubectl taint nodes <node-1> maintenance=true:NoExecute-
kubectl taint nodes <node-2> workload=batch:PreferNoSchedule-

# Remove labels
kubectl label nodes <node-1> pool-
```

## What's Next

You have mastered node-level scheduling. The next exercise moves to pod-level scheduling with pod affinity and anti-affinity rules that co-locate or separate pods: [15.04 - Pod Affinity and Anti-Affinity](../04-pod-affinity-anti-affinity/04-pod-affinity-anti-affinity.md).

## Summary

- `NoSchedule` prevents new pods from scheduling on the node
- `PreferNoSchedule` is a soft version -- the scheduler avoids the node but may use it
- `NoExecute` evicts existing pods that lack a matching toleration
- `tolerationSeconds` on a NoExecute toleration sets a grace period before eviction
- `operator: Exists` with no key creates a wildcard toleration matching all taints
- Dedicated node pools combine taints (repel others) with nodeSelector/affinity (attract desired pods)

## References

- [Taints and Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)
- [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)

## Additional Resources

- [Well-Known Taints](https://kubernetes.io/docs/reference/labels-annotations-taints/)
- [Node Lifecycle Controller](https://kubernetes.io/docs/concepts/architecture/nodes/#node-controller)
