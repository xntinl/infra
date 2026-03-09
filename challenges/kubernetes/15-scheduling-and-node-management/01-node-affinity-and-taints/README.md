<!--
difficulty: basic
concepts: [node-affinity, taints, tolerations, node-selector, pod-anti-affinity, scheduling]
tools: [kubectl]
estimated_time: 35m
bloom_level: understand
prerequisites: [pods, deployments, labels-and-selectors]
-->

# 15.01 - Node Affinity and Taints/Tolerations

## Why This Matters

In production, not all nodes are equal. Some have GPUs, some have SSDs, some are in specific availability zones. You need to direct pods to appropriate nodes (affinity) and keep pods away from dedicated nodes unless they are authorized (taints/tolerations). These two mechanisms together give you fine-grained control over pod placement.

## What You Will Learn

- How `nodeSelector` provides simple label-based node selection
- How `nodeAffinity` with `required` and `preferred` rules offers flexible placement
- How `taints` repel pods from nodes and `tolerations` exempt specific pods
- How `podAntiAffinity` spreads replicas across nodes for high availability

## Step-by-Step Guide

### 1. Label Your Nodes

```bash
# List nodes
kubectl get nodes

# Add custom labels to nodes
kubectl label nodes <node-1> disktype=ssd tier=frontend
kubectl label nodes <node-2> disktype=hdd tier=backend

# Verify labels
kubectl get nodes --show-labels
```

### 2. Pod with nodeSelector

The simplest form of node affinity. The pod will only schedule on nodes with **all** listed labels.

```yaml
# pod-nodeselector.yaml
apiVersion: v1
kind: Pod
metadata:
  name: ssd-pod
  labels:
    app: ssd-app
spec:
  nodeSelector:
    disktype: ssd                    # only nodes with disktype=ssd
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 3. Pod with Required Node Affinity

More expressive than `nodeSelector` -- supports `In`, `NotIn`, `Exists`, `DoesNotExist`, `Gt`, `Lt` operators.

```yaml
# pod-affinity-required.yaml
apiVersion: v1
kind: Pod
metadata:
  name: affinity-required-pod
  labels:
    app: affinity-required
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:    # hard requirement
        nodeSelectorTerms:
          - matchExpressions:
              - key: disktype
                operator: In
                values:
                  - ssd
                  - nvme            # matches ssd OR nvme
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 4. Pod with Preferred Node Affinity

A soft preference with weight. The scheduler tries to honor it but will schedule elsewhere if necessary.

```yaml
# pod-affinity-preferred.yaml
apiVersion: v1
kind: Pod
metadata:
  name: affinity-preferred-pod
  labels:
    app: affinity-preferred
spec:
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 80                 # higher weight = stronger preference
          preference:
            matchExpressions:
              - key: disktype
                operator: In
                values:
                  - ssd
        - weight: 20
          preference:
            matchExpressions:
              - key: tier
                operator: In
                values:
                  - frontend
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 5. Apply a Taint to a Node

```bash
# Apply taint -- pods without a matching toleration cannot schedule here
kubectl taint nodes <node-1> dedicated=gpu:NoSchedule

# Verify taints
kubectl describe node <node-1> | grep -A5 Taints
```

### 6. Pod WITHOUT Toleration (Will Not Schedule on Tainted Node)

```yaml
# pod-no-toleration.yaml
apiVersion: v1
kind: Pod
metadata:
  name: no-toleration-pod
  labels:
    app: no-toleration
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: kubernetes.io/hostname
                operator: In
                values:
                  - <node-1>         # forced to tainted node -- will stay Pending
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 7. Pod WITH Toleration

```yaml
# pod-with-toleration.yaml
apiVersion: v1
kind: Pod
metadata:
  name: toleration-pod
  labels:
    app: toleration
spec:
  tolerations:
    - key: dedicated
      operator: Equal              # exact match on key, value, and effect
      value: gpu
      effect: NoSchedule
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: kubernetes.io/hostname
                operator: In
                values:
                  - <node-1>
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 8. Toleration with Exists Operator

```yaml
# pod-toleration-exists.yaml
apiVersion: v1
kind: Pod
metadata:
  name: toleration-exists-pod
  labels:
    app: toleration-exists
spec:
  tolerations:
    - key: dedicated
      operator: Exists             # matches any value for the key
      effect: NoSchedule
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 9. Deployment with Pod Anti-Affinity

Ensures replicas land on different nodes for high availability.

```yaml
# deployment-anti-affinity.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: spread-app
  template:
    metadata:
      labels:
        app: spread-app
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - spread-app
              topologyKey: kubernetes.io/hostname    # one pod per node
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
```

### 10. Combined Affinity, Anti-Affinity, and Tolerations

```yaml
# deployment-combined.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: combined-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: combined-app
  template:
    metadata:
      labels:
        app: combined-app
    spec:
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              preference:
                matchExpressions:
                  - key: disktype
                    operator: In
                    values:
                      - ssd
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app
                      operator: In
                      values:
                        - combined-app
                topologyKey: kubernetes.io/hostname
      tolerations:
        - key: dedicated
          operator: Equal
          value: gpu
          effect: NoSchedule
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
```

### Apply

```bash
kubectl apply -f pod-nodeselector.yaml
kubectl apply -f pod-affinity-required.yaml
kubectl apply -f pod-affinity-preferred.yaml
kubectl apply -f pod-with-toleration.yaml
kubectl apply -f pod-toleration-exists.yaml
kubectl apply -f deployment-anti-affinity.yaml
kubectl apply -f deployment-combined.yaml
```

## Common Mistakes

1. **Using `nodeSelector` when you need OR logic** -- `nodeSelector` requires ALL labels to match. Use `nodeAffinity` with `In` and multiple values for OR conditions.
2. **Forgetting that taints are directional** -- A taint repels pods. Adding a toleration does not *attract* pods to the node; it only *allows* scheduling there. Combine with affinity to attract.
3. **Confusing `Equal` and `Exists` operators** -- `Equal` matches a specific key-value pair. `Exists` matches any pod that has the key, regardless of value.
4. **Setting anti-affinity as `required` with more replicas than nodes** -- If you require one pod per node but have 5 replicas and 3 nodes, 2 pods will stay Pending.

## Verify

```bash
# 1. Check node labels
kubectl get nodes --show-labels

# 2. Verify nodeSelector placement
kubectl get pod ssd-pod -o wide

# 3. Verify required affinity placement
kubectl get pod affinity-required-pod -o wide

# 4. Verify preferred affinity placement
kubectl get pod affinity-preferred-pod -o wide

# 5. Check node taints
kubectl describe node <node-1> | grep -A5 Taints

# 6. Verify pod without toleration stays Pending
kubectl apply -f pod-no-toleration.yaml
kubectl get pod no-toleration-pod
kubectl describe pod no-toleration-pod | grep -A5 Events

# 7. Verify pod with toleration is Running
kubectl get pod toleration-pod -o wide

# 8. Verify anti-affinity spreads pods across nodes
kubectl get pods -l app=spread-app -o wide

# 9. Verify combined deployment
kubectl get pods -l app=combined-app -o wide
```

## Cleanup

```bash
# Delete pods and deployments
kubectl delete pod ssd-pod affinity-required-pod affinity-preferred-pod \
  no-toleration-pod toleration-pod toleration-exists-pod
kubectl delete deployment spread-app combined-app

# Remove taints
kubectl taint nodes <node-1> dedicated=gpu:NoSchedule-

# Remove labels
kubectl label nodes <node-1> disktype- tier-
kubectl label nodes <node-2> disktype- tier-
```

## What's Next

Now that you understand affinity and taints, the next exercise focuses specifically on `nodeSelector` -- the simplest and most common placement mechanism: [15.02 - Node Selectors and Labels](../02-node-selectors/).

## Summary

- `nodeSelector` is the simplest node placement: all listed labels must match
- `nodeAffinity` supports required (hard) and preferred (soft) rules with flexible operators
- Taints repel pods; tolerations allow specific pods to schedule on tainted nodes
- Taint effects: `NoSchedule` (block new pods), `PreferNoSchedule` (try to avoid), `NoExecute` (evict existing)
- Pod anti-affinity distributes replicas across topology domains (nodes, zones)
- Combine affinity (attract) + taints/tolerations (gate) for dedicated node pools

## References

- [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)
- [Taints and Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)

## Additional Resources

- [Scheduling Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [Well-Known Labels, Annotations and Taints](https://kubernetes.io/docs/reference/labels-annotations-taints/)
