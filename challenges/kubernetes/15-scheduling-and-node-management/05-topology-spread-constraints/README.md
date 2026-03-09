<!--
difficulty: intermediate
concepts: [topology-spread-constraints, max-skew, topology-key, when-unsatisfiable, zone-spreading]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [pod-affinity-anti-affinity, node-affinity-and-taints, deployments]
-->

# 15.05 - Topology Spread Constraints

## Why This Matters

Pod anti-affinity can spread replicas, but it is binary: either one pod per domain or no constraint at all. `topologySpreadConstraints` lets you define the **maximum allowed skew** between domains. With 6 replicas across 3 zones, a `maxSkew: 1` ensures a 2-2-2 distribution instead of allowing 4-1-1. This is the Kubernetes-native way to achieve even workload distribution.

## What You Will Learn

- How `maxSkew` controls the maximum imbalance between topology domains
- How `whenUnsatisfiable` chooses between `DoNotSchedule` (hard) and `ScheduleAnyway` (soft)
- How to combine multiple constraints (hostname + zone)
- How topology spread interacts with `nodeAffinity`

## Guide

### 1. Prepare Nodes with Zone Labels

```bash
# Label nodes to simulate availability zones (if not already labeled)
kubectl label nodes <node-1> topology.kubernetes.io/zone=zone-a
kubectl label nodes <node-2> topology.kubernetes.io/zone=zone-b
kubectl label nodes <node-3> topology.kubernetes.io/zone=zone-a

# Verify
kubectl get nodes -L topology.kubernetes.io/zone
```

### 2. Spread by Hostname (DoNotSchedule)

```yaml
# deployment-spread-hostname.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-hostname
spec:
  replicas: 3
  selector:
    matchLabels:
      app: spread-hostname
  template:
    metadata:
      labels:
        app: spread-hostname
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-hostname
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 3. Spread by Zone (DoNotSchedule)

```yaml
# deployment-spread-zone.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-zone
spec:
  replicas: 4
  selector:
    matchLabels:
      app: spread-zone
  template:
    metadata:
      labels:
        app: spread-zone
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-zone
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 4. Spread with ScheduleAnyway (Soft Constraint)

```yaml
# deployment-spread-anyway.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-anyway
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-anyway
  template:
    metadata:
      labels:
        app: spread-anyway
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway    # best effort
          labelSelector:
            matchLabels:
              app: spread-anyway
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 5. Multiple Constraints (Hostname + Zone)

```yaml
# deployment-spread-multi.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-multi
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-multi
  template:
    metadata:
      labels:
        app: spread-multi
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-multi
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: spread-multi
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 6. Combined with Node Affinity

```yaml
# deployment-spread-affinity.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-affinity
spec:
  replicas: 4
  selector:
    matchLabels:
      app: spread-affinity
  template:
    metadata:
      labels:
        app: spread-affinity
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: topology.kubernetes.io/zone
                    operator: In
                    values:
                      - zone-a
                      - zone-b
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-affinity
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: spread-affinity
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 7. Higher maxSkew (Tolerance for Imbalance)

```yaml
# deployment-spread-skew2.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-skew2
spec:
  replicas: 5
  selector:
    matchLabels:
      app: spread-skew2
  template:
    metadata:
      labels:
        app: spread-skew2
    spec:
      topologySpreadConstraints:
        - maxSkew: 2
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-skew2
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### Apply

```bash
kubectl apply -f deployment-spread-hostname.yaml
kubectl apply -f deployment-spread-zone.yaml
kubectl apply -f deployment-spread-anyway.yaml
kubectl apply -f deployment-spread-multi.yaml
kubectl apply -f deployment-spread-affinity.yaml
kubectl apply -f deployment-spread-skew2.yaml
```

## Verify

```bash
# 1. Check zone labels
kubectl get nodes -L topology.kubernetes.io/zone

# 2. Verify hostname spread
kubectl get pods -l app=spread-hostname -o wide
kubectl get pods -l app=spread-hostname -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort | uniq -c

# 3. Verify zone spread
kubectl get pods -l app=spread-zone -o wide

# 4. Verify ScheduleAnyway distribution
kubectl get pods -l app=spread-anyway -o wide
kubectl get pods -l app=spread-anyway -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort | uniq -c

# 5. Verify multi-constraint distribution
kubectl get pods -l app=spread-multi -o wide

# 6. Verify affinity + spread
kubectl get pods -l app=spread-affinity -o wide

# 7. Verify maxSkew 2 distribution
kubectl get pods -l app=spread-skew2 -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort | uniq -c

# 8. Scale up and verify distribution is maintained
kubectl scale deployment spread-hostname --replicas=6
kubectl get pods -l app=spread-hostname -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | sort | uniq -c
```

## Cleanup

```bash
kubectl delete deployment spread-hostname spread-zone spread-anyway \
  spread-multi spread-affinity spread-skew2

# Remove zone labels if manually added
kubectl label nodes <node-1> topology.kubernetes.io/zone-
kubectl label nodes <node-2> topology.kubernetes.io/zone-
kubectl label nodes <node-3> topology.kubernetes.io/zone-
```

## What's Next

Scheduling gets pods onto nodes, but nodes need maintenance. The next exercise covers the operational side -- cordoning, draining, and uncordoning nodes safely: [15.06 - Node Maintenance: cordon, drain, uncordon](../06-node-maintenance-cordon-drain/).

## Summary

- `maxSkew` defines the maximum allowed difference in pod count between topology domains
- `DoNotSchedule` is a hard constraint; `ScheduleAnyway` is a soft preference
- Multiple constraints are evaluated independently; all must be satisfied
- Combine with `nodeAffinity` to limit the set of eligible nodes before spreading
- Topology spread constraints are more scalable than pod anti-affinity for large clusters
