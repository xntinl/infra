<!--
difficulty: advanced
concepts: [descheduler, pod-eviction, rebalancing, node-utilization, topology-violation]
tools: [kubectl, descheduler]
estimated_time: 35m
bloom_level: analyze
prerequisites: [topology-spread-constraints, pod-affinity-anti-affinity, node-management]
-->

# 14.12 - Descheduler: Rebalancing Workloads

## Architecture

```
  +-------------------+     +-------------------+     +-------------------+
  |  Node A (85%)     |     |  Node B (20%)     |     |  Node C (90%)     |
  |  [pod][pod][pod]  |     |  [pod]            |     |  [pod][pod][pod]  |
  +-------------------+     +-------------------+     +-------------------+
           |                         |                         |
           +-----------+-------------+-----------+-------------+
                       |                         |
              +--------v--------+       +--------v--------+
              |  Descheduler    |       | kube-scheduler   |
              |  (evicts pods   | ----> | (reschedules     |
              |   from A & C)  |       |  evicted pods)   |
              +-----------------+       +-----------------+
```

The Kubernetes scheduler only runs when a pod is first created. Once placed, a pod stays on its node forever -- even if conditions change. The **Descheduler** periodically evicts pods that violate current scheduling constraints (topology spread, affinity, utilization balance), allowing the scheduler to place them optimally.

## What You Will Learn

- How the Descheduler identifies pods to evict using strategy plugins
- How `RemoveDuplicates`, `LowNodeUtilization`, and `RemovePodsViolatingTopologySpreadConstraint` work
- How PodDisruptionBudgets and priority classes protect critical pods
- How to run the Descheduler as a CronJob or Deployment

## Suggested Steps

1. Install the Descheduler via Helm or manifest
2. Configure a DeschedulerPolicy with `LowNodeUtilization` to rebalance across nodes
3. Create an imbalanced cluster by scheduling many pods on a single node
4. Run the Descheduler and observe pods being evicted and rescheduled
5. Enable `RemovePodsViolatingTopologySpreadConstraint` and test with a skewed Deployment
6. Verify that PDBs are respected during eviction

### Descheduler Policy (ConfigMap)

```yaml
# descheduler-policy.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: descheduler-policy
  namespace: kube-system
data:
  policy.yaml: |
    apiVersion: descheduler/v1alpha2
    kind: DeschedulerPolicy
    profiles:
      - name: default
        pluginConfig:
          - name: LowNodeUtilization
            args:
              thresholds:
                cpu: 20
                memory: 20
                pods: 20
              targetThresholds:
                cpu: 50
                memory: 50
                pods: 50
          - name: RemoveDuplicates
          - name: RemovePodsViolatingTopologySpreadConstraint
        plugins:
          balance:
            enabled:
              - LowNodeUtilization
              - RemoveDuplicates
              - RemovePodsViolatingTopologySpreadConstraint
```

### Descheduler CronJob

```yaml
# descheduler-cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: descheduler
  namespace: kube-system
spec:
  schedule: "*/5 * * * *"           # run every 5 minutes
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: descheduler
          containers:
            - name: descheduler
              image: registry.k8s.io/descheduler/descheduler:v0.30.1
              command:
                - /bin/descheduler
                - --policy-config-file=/policy/policy.yaml
                - --v=3
              volumeMounts:
                - name: policy
                  mountPath: /policy
          restartPolicy: Never
          volumes:
            - name: policy
              configMap:
                name: descheduler-policy
```

### Imbalanced Workload for Testing

```yaml
# imbalanced-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: imbalanced-app
spec:
  replicas: 10
  selector:
    matchLabels:
      app: imbalanced
  template:
    metadata:
      labels:
        app: imbalanced
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

## Verify

```bash
# 1. Check pod distribution before Descheduler runs
kubectl get pods -l app=imbalanced -o wide
kubectl get pods -o wide --no-headers | awk '{print $7}' | sort | uniq -c

# 2. Run the Descheduler manually (or wait for CronJob)
kubectl create job --from=cronjob/descheduler descheduler-manual -n kube-system

# 3. Watch pods being evicted and rescheduled
kubectl get pods -l app=imbalanced --watch

# 4. Check distribution after Descheduler runs
kubectl get pods -l app=imbalanced -o wide
kubectl get pods -o wide --no-headers | awk '{print $7}' | sort | uniq -c

# 5. Check Descheduler logs
kubectl logs -n kube-system -l job-name=descheduler-manual

# 6. Verify PDB was respected
kubectl get pdb
```

## Cleanup

```bash
kubectl delete deployment imbalanced-app
kubectl delete cronjob descheduler -n kube-system
kubectl delete configmap descheduler-policy -n kube-system
```

## What's Next

You now understand individual autoscaling components. The next exercise integrates all of them -- HPA, VPA, and Cluster Autoscaler -- into a cohesive autoscaling stack: [14.13 - Full Autoscaling Stack: HPA + VPA + Cluster Autoscaler](../13-autoscaling-stack-integration/).

## Summary

- The Descheduler evicts pods that no longer satisfy scheduling constraints
- `LowNodeUtilization` rebalances pods from over-utilized to under-utilized nodes
- `RemoveDuplicates` ensures replicas are spread across nodes
- `RemovePodsViolatingTopologySpreadConstraint` fixes topology spread drift
- PodDisruptionBudgets are respected; critical pods are never over-evicted
