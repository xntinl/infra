# 3. LimitRanges: Default and Constraints Per Namespace

<!--
difficulty: intermediate
concepts: [limitrange, default-resources, min-max-constraints, container-limits, pod-limits]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [10-01, 10-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-resource-requests-and-limits/01-resource-requests-and-limits.md) and [exercise 02](../02-qos-classes/02-qos-classes.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** LimitRange objects to set default resource values and enforce min/max constraints per namespace
- **Analyze** how LimitRanges interact with pod specs: which values are injected and which are rejected
- **Evaluate** LimitRange configurations for common multi-tenant scenarios

## Why LimitRanges?

In a shared cluster, you cannot trust every developer to set resource requests and limits correctly. Some will forget entirely (creating BestEffort pods that destabilize nodes). Others will request far more than needed (wasting capacity). LimitRanges solve both problems at the namespace level.

A LimitRange defines default values, default requests, minimum values, and maximum values for containers and pods in a namespace. When a pod is created without resource specs, the LimitRange injects defaults. When a pod specifies values outside the allowed range, the API server rejects it.

## Step 1: Create a Namespace with a LimitRange

```yaml
# namespace-limited.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: limited-ns
```

```yaml
# limitrange.yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: default-limits
  namespace: limited-ns
spec:
  limits:
    - type: Container
      default:                     # Applied when limits are not specified
        memory: "256Mi"
        cpu: "500m"
      defaultRequest:              # Applied when requests are not specified
        memory: "128Mi"
        cpu: "250m"
      max:                         # Maximum allowed values
        memory: "1Gi"
        cpu: "1"
      min:                         # Minimum allowed values
        memory: "32Mi"
        cpu: "50m"
    - type: Pod
      max:                         # Maximum total resources for the pod
        memory: "2Gi"
        cpu: "2"
```

```bash
kubectl apply -f namespace-limited.yaml
kubectl apply -f limitrange.yaml
kubectl describe limitrange default-limits -n limited-ns
```

## Step 2: Pod Without Resources Gets Defaults

```yaml
# pod-defaults.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-defaults
  namespace: limited-ns
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      # No resources specified
  restartPolicy: Never
```

```bash
kubectl apply -f pod-defaults.yaml
kubectl describe pod pod-defaults -n limited-ns | grep -A 6 "Limits\|Requests"
```

The pod receives the default values from the LimitRange: `memory: 256Mi, cpu: 500m` (limits) and `memory: 128Mi, cpu: 250m` (requests).

## Step 3: Pod Exceeding Max Is Rejected

```yaml
# pod-too-big.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-too-big
  namespace: limited-ns
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "2Gi"            # Exceeds max of 1Gi
          cpu: "2"                 # Exceeds max of 1
  restartPolicy: Never
```

```bash
kubectl apply -f pod-too-big.yaml
# Error: maximum memory usage per Container is 1Gi, but limit is 2Gi
```

## Step 4: Pod Below Min Is Rejected

```yaml
# pod-too-small.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-too-small
  namespace: limited-ns
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "16Mi"           # Below min of 32Mi
          cpu: "10m"               # Below min of 50m
        limits:
          memory: "32Mi"
          cpu: "50m"
  restartPolicy: Never
```

```bash
kubectl apply -f pod-too-small.yaml
# Error: minimum memory usage per Container is 32Mi
```

## Step 5: LimitRange with Ratio Constraint

You can also constrain the ratio between requests and limits:

```yaml
# limitrange-ratio.yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: ratio-limits
  namespace: limited-ns
spec:
  limits:
    - type: Container
      maxLimitRequestRatio:
        memory: "2"               # Limit can be at most 2x the request
        cpu: "4"                  # Limit can be at most 4x the request
```

```bash
kubectl apply -f limitrange-ratio.yaml
```

A pod requesting `memory: 100Mi` with limit `memory: 500Mi` (5x ratio) would be rejected because the ratio exceeds the max of 2.

## Spot the Bug

This LimitRange causes all pods to fail. **Why?**

```yaml
spec:
  limits:
    - type: Container
      min:
        memory: "256Mi"
      default:
        memory: "128Mi"     # <-- BUG
      defaultRequest:
        memory: "64Mi"      # <-- BUG
```

<details>
<summary>Explanation</summary>

The `default` (limit) is 128Mi and `defaultRequest` is 64Mi, but the `min` is 256Mi. When a pod is created without resources, the injected defaults (128Mi limit, 64Mi request) are below the minimum (256Mi), causing the API server to reject the pod. Defaults must always fall within the min/max range.

</details>

## Verify What You Learned

```bash
kubectl describe limitrange -n limited-ns
kubectl get pod pod-defaults -n limited-ns -o jsonpath='{.spec.containers[0].resources}'
kubectl get pod pod-defaults -n limited-ns -o jsonpath='{.status.qosClass}'
```

## Cleanup

```bash
kubectl delete namespace limited-ns
```

## What's Next

LimitRanges control per-container and per-pod resources. The next exercise covers ResourceQuotas, which control total resource consumption across an entire namespace. Continue to [exercise 04 (ResourceQuotas: CPU, Memory, and Object Count)](../04-resource-quotas/04-resource-quotas.md).

## Summary

- **LimitRange** sets default, min, and max resource values for containers and pods in a namespace
- Pods created without resource specs receive **default values** from the LimitRange
- Pods with values outside the **min/max range** are rejected by the API server
- The **maxLimitRequestRatio** constrains how much limits can exceed requests
- Default values must fall within the min/max range or all pods without explicit resources will be rejected

## Reference

- [Limit Ranges](https://kubernetes.io/docs/concepts/policy/limit-range/)
- [Configure Default Memory/CPU Requests and Limits](https://kubernetes.io/docs/tasks/administer-cluster/manage-resources/memory-default-namespace/)
