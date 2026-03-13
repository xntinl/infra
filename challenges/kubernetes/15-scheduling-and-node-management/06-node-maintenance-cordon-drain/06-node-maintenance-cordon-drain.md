<!--
difficulty: intermediate
concepts: [cordon, drain, uncordon, node-maintenance, pod-eviction, pod-disruption-budget]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [deployments, pods, taints-and-tolerations]
-->

# 15.06 - Node Maintenance: cordon, drain, uncordon

## Why This Matters

Nodes need maintenance -- kernel upgrades, hardware replacement, Kubernetes version bumps. You cannot just power off a node; that would kill all running pods ungracefully. The `cordon` / `drain` / `uncordon` workflow lets you safely evacuate workloads before taking a node offline, respecting PodDisruptionBudgets and graceful termination periods.

## What You Will Learn

- How `kubectl cordon` marks a node as unschedulable without affecting running pods
- How `kubectl drain` evicts all pods (respecting PDBs and grace periods)
- How `kubectl uncordon` returns a node to service
- How PodDisruptionBudgets protect application availability during drains

## Guide

### 1. Create a Deployment and PDB

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: maintenance-lab
```

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: maintenance-lab
spec:
  replicas: 4
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      terminationGracePeriodSeconds: 30
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

```yaml
# pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-pdb
  namespace: maintenance-lab
spec:
  minAvailable: 2                 # at least 2 pods must remain available
  selector:
    matchLabels:
      app: web
```

### 2. Cordon a Node

```bash
# Mark node as unschedulable (existing pods are not affected)
kubectl cordon <node-name>

# Verify the node shows SchedulingDisabled
kubectl get nodes
```

### 3. Drain a Node

```bash
# Evict all pods from the node (respects PDBs and grace periods)
kubectl drain <node-name> \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --grace-period=30 \
  --timeout=120s

# Watch pods being evicted and rescheduled
kubectl get pods -n maintenance-lab -o wide --watch
```

### 4. Perform Maintenance

```bash
# The node is now empty of user workloads
# Perform your maintenance (kernel upgrade, etc.)

# Verify no user pods on the node
kubectl get pods --all-namespaces --field-selector spec.nodeName=<node-name>
```

### 5. Return the Node to Service

```bash
# Mark node as schedulable again
kubectl uncordon <node-name>

# Verify
kubectl get nodes
```

### 6. Drain with PDB Protection

```bash
# If PDB prevents draining, kubectl drain will wait
kubectl drain <node-name> \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --pod-selector='app=web'

# Check PDB status
kubectl get pdb -n maintenance-lab
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
kubectl apply -f pdb.yaml
```

## Spot the Bug

What is wrong with this drain command?

```bash
kubectl drain my-node --force --grace-period=0
```

<details>
<summary>Show Answer</summary>

Two problems: (1) `--force` deletes pods not managed by a ReplicaSet/Deployment, which means standalone pods will be permanently deleted with no replacement. (2) `--grace-period=0` sends SIGKILL immediately, not allowing containers to shut down gracefully. This can cause data corruption for stateful workloads. The correct approach is `kubectl drain my-node --ignore-daemonsets --delete-emptydir-data --grace-period=30`.

</details>

## Verify

```bash
# 1. Check initial pod distribution
kubectl get pods -n maintenance-lab -o wide

# 2. Cordon a node and verify
kubectl cordon <node-name>
kubectl get nodes

# 3. Scale up -- new pods should NOT go to the cordoned node
kubectl scale deployment web -n maintenance-lab --replicas=6
kubectl get pods -n maintenance-lab -o wide

# 4. Drain and watch eviction
kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data
kubectl get pods -n maintenance-lab -o wide

# 5. Check PDB was respected (disruptions allowed)
kubectl get pdb -n maintenance-lab

# 6. Uncordon
kubectl uncordon <node-name>
kubectl get nodes
```

## Cleanup

```bash
kubectl delete namespace maintenance-lab
kubectl uncordon <node-name>  # in case it is still cordoned
```

## What's Next

You have mastered day-to-day node operations. The next exercise goes deeper into the scheduler itself, exploring scheduler profiles and plugin configuration: [15.07 - Scheduler Profiles and Plugins](../07-scheduler-profiles/07-scheduler-profiles.md).

## Summary

- `kubectl cordon` sets the node as unschedulable; existing pods continue running
- `kubectl drain` evicts all pods, respecting PDBs and `terminationGracePeriodSeconds`
- `--ignore-daemonsets` is usually required since DaemonSet pods cannot be evicted
- `--delete-emptydir-data` is needed when pods use `emptyDir` volumes
- PodDisruptionBudgets prevent drain from evicting too many pods simultaneously
- `kubectl uncordon` returns the node to the scheduling pool
