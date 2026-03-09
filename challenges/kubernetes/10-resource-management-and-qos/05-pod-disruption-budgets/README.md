# 5. Pod Disruption Budgets

<!--
difficulty: intermediate
concepts: [pod-disruption-budget, min-available, max-unavailable, voluntary-disruption, node-drain, eviction-api]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: apply
prerequisites: [10-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Resource Requests and Limits)](../01-resource-requests-and-limits/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** PodDisruptionBudget resources with `minAvailable` and `maxUnavailable` configurations
- **Analyze** how PDBs interact with `kubectl drain`, rolling updates, and cluster autoscaling
- **Evaluate** the tradeoffs between absolute numbers and percentages for disruption budgets

## Why Pod Disruption Budgets?

Nodes need maintenance. Clusters need upgrades. Autoscalers remove underutilized nodes. All of these are voluntary disruptions -- planned operations that evict pods. Without guardrails, draining a node could take down all replicas of a critical service simultaneously.

A PodDisruptionBudget (PDB) tells Kubernetes how many pods of a given application must remain available during voluntary disruptions. The eviction API checks PDBs before removing any pod. If evicting a pod would violate the budget, the operation waits.

## Step 1: Create a Deployment and PDB with minAvailable

```yaml
# deployment-web.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web-app
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 1
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "128Mi"
              cpu: "200m"
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 5
```

```yaml
# pdb-min-available.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-app-pdb
spec:
  minAvailable: 2                    # At least 2 pods must remain running
  selector:
    matchLabels:
      app: web-app
```

```bash
kubectl apply -f deployment-web.yaml
kubectl apply -f pdb-min-available.yaml
kubectl wait --for=condition=Available deployment/web-app --timeout=120s
kubectl get pdb web-app-pdb
```

## Step 2: PDB with maxUnavailable

```yaml
# deployment-api.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  replicas: 5
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              memory: "64Mi"
              cpu: "100m"
            limits:
              memory: "128Mi"
              cpu: "200m"
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 5
```

```yaml
# pdb-max-unavailable.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: api-server-pdb
spec:
  maxUnavailable: 1                  # At most 1 pod can be unavailable
  selector:
    matchLabels:
      app: api-server
```

```bash
kubectl apply -f deployment-api.yaml
kubectl apply -f pdb-max-unavailable.yaml
kubectl wait --for=condition=Available deployment/api-server --timeout=120s
kubectl get pdb api-server-pdb
```

## Step 3: PDB with Percentage

```yaml
# pdb-percentage.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-app-pdb-pct
spec:
  minAvailable: "60%"               # Scales with replica count
  selector:
    matchLabels:
      app: web-app
```

With 5 replicas, `60%` means at least 3 must remain. With 10 replicas, at least 6. Percentages are rounded up.

## Step 4: Observe PDB in Action

Scale the web-app to exactly the minimum:

```bash
kubectl scale deployment web-app --replicas=2
kubectl get pdb web-app-pdb
```

The `ALLOWED DISRUPTIONS` column shows `0` because all 2 pods are needed to meet `minAvailable: 2`. No voluntary evictions are allowed.

Scale back:

```bash
kubectl scale deployment web-app --replicas=3
kubectl get pdb web-app-pdb
```

Now `ALLOWED DISRUPTIONS` shows `1`.

## Step 5: Test with Node Drain

On a multi-node cluster, drain a node and observe the PDB blocking:

```bash
kubectl get pods -l app=web-app -o wide
# Note which node each pod is on

# In a multi-node cluster:
# kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data
# Watch PDB status in another terminal:
# kubectl get pdb web-app-pdb -w
```

The drain operation respects the PDB and waits if necessary.

## Verify What You Learned

```bash
kubectl get pdb -o custom-columns=NAME:.metadata.name,MIN-AVAIL:.spec.minAvailable,MAX-UNAVAIL:.spec.maxUnavailable,CURRENT:.status.currentHealthy,DESIRED:.status.desiredHealthy,ALLOWED:.status.disruptionsAllowed
```

## Cleanup

```bash
kubectl delete pdb web-app-pdb api-server-pdb web-app-pdb-pct --ignore-not-found
kubectl delete deployment web-app api-server --ignore-not-found
```

## What's Next

PDBs protect against voluntary disruptions. The next exercise covers right-sizing resources using VPA recommendations. Continue to [exercise 06 (Resource Right-Sizing with VPA Recommendations)](../06-resource-right-sizing/).

## Summary

- **PodDisruptionBudget** controls how many pods can be evicted during voluntary disruptions (drain, upgrades, autoscaling)
- **minAvailable** sets the minimum number of healthy pods that must remain
- **maxUnavailable** sets the maximum number of pods that can be down at once
- Both accept **absolute numbers** or **percentages** (percentages scale with replica count)
- PDBs only apply to **voluntary disruptions**; involuntary failures (node crash) are not blocked

## Reference

- [Disruptions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)
- [Specifying a Disruption Budget](https://kubernetes.io/docs/tasks/run-application/configure-pdb/)
- [Safely Drain a Node](https://kubernetes.io/docs/tasks/administer-cluster/safely-drain-node/)
