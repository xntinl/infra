# Node Maintenance: cordon, drain, uncordon with PDB

<!--
difficulty: intermediate
concepts: [cordon, drain, uncordon, pod-disruption-budget, eviction, node-maintenance]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [kubectl-basics, deployments]
-->

## Overview

When performing node maintenance (OS updates, kernel upgrades, hardware replacement), you need to safely move workloads off the node before taking it offline. Kubernetes provides `cordon`, `drain`, and `uncordon` commands for this workflow. Pod Disruption Budgets (PDBs) protect application availability during the process.

## Why This Matters

Draining nodes without PDBs can cause application downtime if all replicas of a service are evicted simultaneously. The cordon/drain/uncordon workflow combined with PDBs ensures zero-downtime maintenance windows.

## Step-by-Step Instructions

### Step 1 -- Deploy a Test Application

```yaml
# maintenance-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: critical-app
  namespace: default
spec:
  replicas: 4
  selector:
    matchLabels:
      app: critical
  template:
    metadata:
      labels:
        app: critical
    spec:
      containers:
        - name: app
          image: nginx:1.27
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
      # Spread pods across nodes for realistic testing
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: critical
```

```bash
kubectl apply -f maintenance-app.yaml
kubectl rollout status deployment/critical-app

# Verify pods are spread across nodes
kubectl get pods -l app=critical -o wide
```

### Step 2 -- Create a Pod Disruption Budget

```yaml
# pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: critical-app-pdb
spec:
  minAvailable: 2          # at least 2 pods must remain running during eviction
  selector:
    matchLabels:
      app: critical
```

```bash
kubectl apply -f pdb.yaml
kubectl get pdb critical-app-pdb
```

### Step 3 -- Cordon a Node

Cordoning marks a node as unschedulable. Existing pods remain running but no new pods are scheduled there.

```bash
# Pick a node with critical-app pods
NODE=$(kubectl get pods -l app=critical -o jsonpath='{.items[0].spec.nodeName}')

kubectl cordon $NODE

# Verify the node is SchedulingDisabled
kubectl get nodes
# The node shows SchedulingDisabled under STATUS
```

### Step 4 -- Drain the Node

Draining evicts all pods from the node, respecting PDBs.

```bash
kubectl drain $NODE --ignore-daemonsets --delete-emptydir-data

# Watch pods being rescheduled
kubectl get pods -l app=critical -o wide -w
```

If the drain blocks, it is because the PDB would be violated. The drain waits until evicting a pod is safe.

### Step 5 -- Perform Maintenance

With the node drained, you can safely perform maintenance.

```bash
# Example: SSH to the node and apply OS updates
# ssh $NODE
# sudo apt-get update && sudo apt-get upgrade -y
# sudo reboot
```

### Step 6 -- Uncordon the Node

After maintenance is complete and the node is back, mark it as schedulable again.

```bash
kubectl uncordon $NODE

# Verify the node is Ready and schedulable
kubectl get nodes

# New pods can now be scheduled on this node
# Existing pods do NOT automatically move back -- the scheduler only places new pods
```

### Step 7 -- PDB Variations

```yaml
# Using maxUnavailable instead of minAvailable
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: pdb-max-unavailable
spec:
  maxUnavailable: 1        # at most 1 pod can be unavailable at a time
  selector:
    matchLabels:
      app: critical
---
# Using percentage
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: pdb-percentage
spec:
  minAvailable: "50%"      # at least 50% of pods must remain available
  selector:
    matchLabels:
      app: critical
```

## Verify

```bash
# Node should be Ready (no SchedulingDisabled)
kubectl get nodes

# All critical-app pods should be Running
kubectl get pods -l app=critical

# PDB should show ALLOWED DISRUPTIONS > 0
kubectl get pdb critical-app-pdb
```

## Cleanup

```bash
kubectl delete deployment critical-app
kubectl delete pdb critical-app-pdb
```

## Reference

- [Safely Drain a Node](https://kubernetes.io/docs/tasks/administer-cluster/safely-drain-node/)
- [Pod Disruption Budgets](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#pod-disruption-budgets)
