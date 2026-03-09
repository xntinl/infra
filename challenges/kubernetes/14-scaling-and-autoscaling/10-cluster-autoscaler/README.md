<!--
difficulty: advanced
concepts: [cluster-autoscaler, node-scaling, pending-pods, scale-down, node-groups, expander]
tools: [kubectl, cluster-autoscaler]
estimated_time: 40m
bloom_level: analyze
prerequisites: [hpa-cpu-memory-autoscaling, resource-requests-and-limits, node-management]
-->

# 14.10 - Cluster Autoscaler: Node Scaling

## Architecture

```
  +-------------------+
  |   Pending Pods    |  Pods unschedulable due to
  | (Insufficient     |  insufficient node resources
  |  resources)       |
  +--------+----------+
           |
           | detects
           |
  +--------v----------+
  | Cluster Autoscaler|
  | - scale-up check  |  Runs every 10s (--scan-interval)
  | - scale-down check|  Checks for underutilized nodes
  +--------+----------+
           |
           | calls cloud API
           |
  +--------v----------+
  | Cloud Provider    |
  | (ASG, MIG, VMSS,  |
  |  Node Pool)       |
  +--------+----------+
           |
           | provisions / terminates
           |
  +--------v----------+
  |   Worker Nodes    |
  +-------------------+
```

The Cluster Autoscaler bridges pod-level scaling (HPA) and infrastructure-level scaling. When the HPA creates more pods than the cluster can schedule, the Cluster Autoscaler adds nodes. When nodes become underutilized, it cordons, drains, and removes them.

## What You Will Learn

- How the Cluster Autoscaler detects pending pods and triggers scale-up
- How the scale-down algorithm identifies underutilized nodes
- How `--expander` strategies choose which node group to scale (random, most-pods, least-waste, priority)
- How annotations and PodDisruptionBudgets affect scale-down decisions

## Suggested Steps

1. Deploy the Cluster Autoscaler in the `kube-system` namespace with appropriate cloud provider flags
2. Create a Deployment with large resource requests that cannot be scheduled on existing nodes
3. Observe the Cluster Autoscaler adding a new node
4. Scale down the Deployment and observe the Cluster Autoscaler removing the node
5. Add `cluster-autoscaler.kubernetes.io/safe-to-evict: "false"` annotations to prevent eviction
6. Test with a PodDisruptionBudget that blocks scale-down

### Cluster Autoscaler Deployment (AWS Example)

```yaml
# cluster-autoscaler.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cluster-autoscaler
  namespace: kube-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cluster-autoscaler
  template:
    metadata:
      labels:
        app: cluster-autoscaler
    spec:
      serviceAccountName: cluster-autoscaler
      containers:
        - name: cluster-autoscaler
          image: registry.k8s.io/autoscaling/cluster-autoscaler:v1.29.0
          command:
            - ./cluster-autoscaler
            - --v=4
            - --cloud-provider=aws
            - --skip-nodes-with-local-storage=false
            - --expander=least-waste
            - --node-group-auto-discovery=asg:tag=k8s.io/cluster-autoscaler/enabled,k8s.io/cluster-autoscaler/my-cluster
            - --balance-similar-node-groups
            - --scale-down-enabled=true
            - --scale-down-delay-after-add=10m
            - --scale-down-unneeded-time=10m
            - --scale-down-utilization-threshold=0.5
          resources:
            requests:
              cpu: 100m
              memory: 300Mi
            limits:
              cpu: 200m
              memory: 600Mi
```

### Workload to Trigger Scale-Up

```yaml
# inflate-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
  namespace: default
spec:
  replicas: 0
  selector:
    matchLabels:
      app: inflate
  template:
    metadata:
      labels:
        app: inflate
    spec:
      containers:
        - name: inflate
          image: nginx:1.27
          resources:
            requests:
              cpu: "1"             # 1 full CPU per pod
              memory: 1Gi
            limits:
              cpu: "1"
              memory: 1Gi
```

### Safe-to-Evict Annotation

```yaml
# no-evict-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: critical-pod
  annotations:
    cluster-autoscaler.kubernetes.io/safe-to-evict: "false"
spec:
  containers:
    - name: app
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

## Verify

```bash
# 1. Check Cluster Autoscaler status
kubectl get configmap cluster-autoscaler-status -n kube-system -o yaml

# 2. Check current node count
kubectl get nodes

# 3. Scale the inflate Deployment to trigger node provisioning
kubectl scale deployment inflate --replicas=10

# 4. Watch for pending pods
kubectl get pods -l app=inflate --watch

# 5. Watch for new nodes appearing
kubectl get nodes --watch

# 6. Check Cluster Autoscaler logs
kubectl logs -n kube-system -l app=cluster-autoscaler --tail=100

# 7. Scale down and observe node removal (after scale-down-delay)
kubectl scale deployment inflate --replicas=0
kubectl get nodes --watch

# 8. Check events
kubectl get events --sort-by='.lastTimestamp' | grep -i autoscaler
```

## Cleanup

```bash
kubectl delete deployment inflate
kubectl delete pod critical-pod --ignore-not-found
```

## What's Next

Cluster Autoscaler works but can be slow (minutes to provision nodes). The next exercise covers Karpenter, which provisions right-sized nodes directly without ASG intermediaries: [14.11 - Karpenter: Intelligent Node Provisioning](../11-karpenter/).

## Summary

- Cluster Autoscaler adds nodes when pods are unschedulable due to insufficient resources
- Scale-down removes nodes when utilization stays below the threshold for the configured duration
- `--expander` controls node group selection: `least-waste` minimizes wasted resources
- Pods with `safe-to-evict: "false"` annotations block node scale-down
- PodDisruptionBudgets are respected during node draining
