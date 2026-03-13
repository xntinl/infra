# 4. ResourceQuotas: CPU, Memory, and Object Count

<!--
difficulty: intermediate
concepts: [resource-quota, cpu-quota, memory-quota, object-count-quota, namespace-limits]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [10-01, 10-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-resource-requests-and-limits/01-resource-requests-and-limits.md) and [exercise 03](../03-limitranges/03-limitranges.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** ResourceQuota objects to limit total resource consumption and object counts in a namespace
- **Analyze** how quotas interact with LimitRanges and why quotas require resource specs on all pods
- **Evaluate** quota configurations for multi-team namespace isolation

## Why ResourceQuotas?

LimitRanges constrain individual pods. ResourceQuotas constrain the entire namespace. Without quotas, a single team could create thousands of pods and consume all cluster resources, even if each pod individually has reasonable limits.

A ResourceQuota sets hard limits on the total sum of resource requests and limits across all pods in a namespace, plus the count of objects like pods, services, PVCs, and ConfigMaps. Once a quota is hit, new pod creation fails until existing resources are freed.

## Step 1: Create a Namespace with a ResourceQuota

```yaml
# namespace-quota.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: quota-ns
```

```yaml
# resourcequota.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: namespace-quota
  namespace: quota-ns
spec:
  hard:
    requests.cpu: "2"              # Total CPU requests across all pods
    requests.memory: "2Gi"         # Total memory requests across all pods
    limits.cpu: "4"                # Total CPU limits across all pods
    limits.memory: "4Gi"           # Total memory limits across all pods
    pods: "10"                     # Maximum number of pods
    persistentvolumeclaims: "5"    # Maximum number of PVCs
    services: "5"                  # Maximum number of services
    configmaps: "10"
    secrets: "10"
```

```bash
kubectl apply -f namespace-quota.yaml
kubectl apply -f resourcequota.yaml
kubectl describe resourcequota namespace-quota -n quota-ns
```

## Step 2: ResourceQuota Requires Resource Specs

When a ResourceQuota exists that tracks `requests.cpu` or `limits.memory`, every pod must specify those resources. Otherwise creation fails:

```yaml
# pod-no-resources-quota.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-no-resources
  namespace: quota-ns
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      # No resources -- will fail
  restartPolicy: Never
```

```bash
kubectl apply -f pod-no-resources-quota.yaml
# Error: must specify requests.cpu, requests.memory, limits.cpu, limits.memory
```

Fix this by either adding resources to the pod or adding a LimitRange with defaults:

```yaml
# limitrange-defaults.yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: quota-defaults
  namespace: quota-ns
spec:
  limits:
    - type: Container
      default:
        memory: "256Mi"
        cpu: "500m"
      defaultRequest:
        memory: "128Mi"
        cpu: "250m"
```

```bash
kubectl apply -f limitrange-defaults.yaml
kubectl apply -f pod-no-resources-quota.yaml
# Now succeeds because LimitRange injects defaults
```

## Step 3: Check Quota Usage

```bash
kubectl describe resourcequota namespace-quota -n quota-ns
```

You will see `Used` vs `Hard` columns showing current consumption vs limits.

## Step 4: Exceed the Quota

Create pods until the quota is exceeded:

```yaml
# deployment-fill.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: quota-filler
  namespace: quota-ns
spec:
  replicas: 8
  selector:
    matchLabels:
      app: filler
  template:
    metadata:
      labels:
        app: filler
    spec:
      containers:
        - name: app
          image: busybox:1.37
          command: ["sh", "-c", "sleep 3600"]
          resources:
            requests:
              memory: "256Mi"
              cpu: "300m"
            limits:
              memory: "512Mi"
              cpu: "600m"
```

```bash
kubectl apply -f deployment-fill.yaml
kubectl get deployment quota-filler -n quota-ns
kubectl describe resourcequota namespace-quota -n quota-ns
```

Some replicas will not be created because the quota is exhausted. Check the Deployment events:

```bash
kubectl describe deployment quota-filler -n quota-ns | grep -A 5 "Events"
```

## Step 5: Object Count Quotas

Object count quotas limit how many of each resource type can exist:

```yaml
# quota-objects.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: object-quota
  namespace: quota-ns
spec:
  hard:
    count/deployments.apps: "3"
    count/services: "5"
    count/configmaps: "10"
```

```bash
kubectl apply -f quota-objects.yaml
kubectl describe resourcequota object-quota -n quota-ns
```

## Spot the Bug

This quota seems correct but causes unexpected pod failures. **Why?**

```yaml
spec:
  hard:
    requests.cpu: "1"
    limits.cpu: "2"
    requests.memory: "1Gi"
    # limits.memory is missing    <-- BUG
```

<details>
<summary>Explanation</summary>

When a ResourceQuota specifies some resource constraints (like `requests.cpu`) but omits others (like `limits.memory`), pods that only specify `limits.memory` without `requests.memory` may fail unpredictably. However, the real issue is that if a LimitRange injects `limits.memory` defaults, those are not tracked by the quota and can consume unbounded memory. Always track both requests and limits for each resource type.

</details>

## Verify What You Learned

```bash
kubectl describe resourcequota namespace-quota -n quota-ns
kubectl get pods -n quota-ns
kubectl describe deployment quota-filler -n quota-ns | grep -A 3 "Replicas"
```

## Cleanup

```bash
kubectl delete namespace quota-ns
```

## What's Next

ResourceQuotas and LimitRanges protect the cluster, but disruptions during maintenance can still take down applications. The next exercise covers Pod Disruption Budgets. Continue to [exercise 05 (Pod Disruption Budgets)](../05-pod-disruption-budgets/05-pod-disruption-budgets.md).

## Summary

- **ResourceQuota** limits the total resource consumption and object counts in a namespace
- When a compute quota exists, **all pods must specify resources** (or a LimitRange must provide defaults)
- Quota tracks both **requests and limits** independently; always configure both
- Object count quotas (`count/deployments.apps`) limit how many resources of each type can be created
- Combine ResourceQuotas with LimitRanges: quotas for namespace totals, LimitRanges for per-pod defaults

## Reference

- [Resource Quotas](https://kubernetes.io/docs/concepts/policy/resource-quotas/)
- [Configure Memory and CPU Quotas for a Namespace](https://kubernetes.io/docs/tasks/administer-cluster/manage-resources/quota-memory-cpu-namespace/)
