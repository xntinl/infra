# 12. Pod Topology Spread Constraints

<!--
difficulty: advanced
concepts: [topology-spread-constraints, maxSkew, topologyKey, whenUnsatisfiable, zone-spreading]
tools: [kubectl, minikube, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [01-03, 01-06]
-->

## Prerequisites

- A multi-node Kubernetes cluster (kind with 3+ nodes recommended, or a cloud cluster with multiple zones)
- `kubectl` installed and configured
- Completion of [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/) and [exercise 06 (Multi-Container Patterns)](../06-multi-container-patterns/)

Create a 3-node kind cluster if needed:

```bash
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
  - role: worker
  - role: worker
EOF
```

Label the worker nodes to simulate zones:

```bash
kubectl label node kind-worker topology.kubernetes.io/zone=us-east-1a
kubectl label node kind-worker2 topology.kubernetes.io/zone=us-east-1b
kubectl label node kind-worker3 topology.kubernetes.io/zone=us-east-1c
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how `topologySpreadConstraints` distribute Pods across failure domains
- **Apply** maxSkew, topologyKey, and whenUnsatisfiable to control Pod spreading
- **Evaluate** the interaction between topology spread constraints and node affinity

## Architecture

Topology spread constraints tell the scheduler to distribute Pods evenly across topology domains (zones, nodes, racks). The key parameters are:

- **topologyKey**: the node label that defines domains (e.g., `topology.kubernetes.io/zone`)
- **maxSkew**: the maximum allowed difference in Pod count between any two domains
- **whenUnsatisfiable**: what to do if the constraint cannot be met (`DoNotSchedule` or `ScheduleAnyway`)
- **labelSelector**: which Pods to count when computing skew

## Steps

### 1. Deploy Without Topology Constraints

```yaml
# unconstrained.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: unconstrained-app
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-demo
      variant: unconstrained
  template:
    metadata:
      labels:
        app: spread-demo
        variant: unconstrained
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f unconstrained.yaml
kubectl get pods -l variant=unconstrained -o wide
```

Note which nodes the Pods land on. The scheduler optimizes for resource utilization, not even distribution. You may see uneven placement.

### 2. Add Topology Spread Constraints

```yaml
# spread-by-zone.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zone-spread-app
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-demo
      variant: zone-spread
  template:
    metadata:
      labels:
        app: spread-demo
        variant: zone-spread
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-demo
              variant: zone-spread
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f spread-by-zone.yaml
kubectl get pods -l variant=zone-spread -o wide
```

With 6 replicas across 3 zones and maxSkew 1, expect 2 Pods per zone. The maximum difference between any two zones is at most 1.

### 3. Test with Node-Level Spreading

```yaml
# spread-by-node.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: node-spread-app
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-demo
      variant: node-spread
  template:
    metadata:
      labels:
        app: spread-demo
        variant: node-spread
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: spread-demo
              variant: node-spread
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f spread-by-node.yaml
kubectl get pods -l variant=node-spread -o wide
```

### 4. Combine Zone and Node Constraints

```yaml
# dual-spread.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dual-spread-app
spec:
  replicas: 6
  selector:
    matchLabels:
      app: spread-demo
      variant: dual
  template:
    metadata:
      labels:
        app: spread-demo
        variant: dual
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              variant: dual
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              variant: dual
      containers:
        - name: nginx
          image: nginx:1.27
```

```bash
kubectl apply -f dual-spread.yaml
kubectl get pods -l variant=dual -o wide
```

The zone constraint is hard (`DoNotSchedule`), the node constraint is soft (`ScheduleAnyway`). The scheduler must satisfy zone spreading but only tries its best for node spreading.

### 5. Observe Unsatisfiable Behavior

Scale to a number that cannot be evenly spread:

```bash
kubectl scale deployment zone-spread-app --replicas=7
kubectl get pods -l variant=zone-spread -o wide
```

With maxSkew 1 and 3 zones, 7 replicas distributes as 3-2-2 or 2-3-2. The constraint is still satisfied because the maximum difference between any two domains is 1.

## Verify What You Learned

```bash
# Check zone distribution
for zone in us-east-1a us-east-1b us-east-1c; do
  count=$(kubectl get pods -l variant=zone-spread -o wide | grep "$zone" | wc -l)
  echo "Zone $zone: $count pods"
done
```

Expected: each zone has 2-3 Pods with a maximum difference of 1.

## Cleanup

```bash
kubectl delete deployment unconstrained-app zone-spread-app node-spread-app dual-spread-app
```

If you created a kind cluster for this exercise, optionally delete it:

```bash
kind delete cluster
```

## Summary

- **topologySpreadConstraints** distribute Pods across failure domains for high availability
- **maxSkew** controls the maximum allowed Pod count difference between any two domains
- **DoNotSchedule** is a hard constraint; **ScheduleAnyway** is a soft preference
- Multiple constraints can be combined (zone + node) with different enforcement levels
- The `labelSelector` determines which Pods are counted when computing skew

## Reference

- [Pod Topology Spread Constraints](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) — official concept documentation
- [Assigning Pods to Nodes](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/) — broader scheduling overview
- [Well-Known Labels](https://kubernetes.io/docs/reference/labels-annotations-taints/) — topology label reference
