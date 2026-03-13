# 2. QoS Classes: Guaranteed, Burstable, BestEffort

<!--
difficulty: basic
concepts: [qos-classes, guaranteed, burstable, besteffort, eviction, oom-score]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [10-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Resource Requests and Limits)](../01-resource-requests-and-limits/01-resource-requests-and-limits.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the three QoS classes and the resource configurations that produce each one
- **Understand** how Kubernetes uses QoS classes to decide eviction order under resource pressure
- **Apply** resource specifications to achieve a specific QoS class for a pod

## Why QoS Classes?

When a node runs out of memory, Kubernetes must decide which pods to kill. It cannot kill them randomly -- a database pod is more important than a batch job. QoS classes provide the eviction priority hierarchy.

Kubernetes assigns one of three QoS classes automatically based on the resource configuration of a pod's containers:

- **Guaranteed**: Every container has both requests and limits set, and they are equal. These pods are evicted last. They get the lowest OOM score adjustment, meaning the kernel's OOM killer targets them last.
- **Burstable**: At least one container has a request or limit set, but they are not all equal. These pods are evicted after BestEffort pods.
- **BestEffort**: No container has any request or limit set. These pods are evicted first under pressure.

You do not set the QoS class directly. Kubernetes calculates it from the resource specifications. Understanding this mapping lets you deliberately choose the eviction priority for each workload.

## Step 1: Create a Guaranteed Pod

All containers must have requests equal to limits for both CPU and memory:

```yaml
# pod-guaranteed.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-guaranteed
  namespace: default
spec:
  containers:
    - name: app
      image: nginx:1.27
      resources:
        requests:
          memory: "128Mi"         # Same as limit
          cpu: "250m"             # Same as limit
        limits:
          memory: "128Mi"
          cpu: "250m"
  restartPolicy: Never
```

```bash
kubectl apply -f pod-guaranteed.yaml
kubectl get pod pod-guaranteed -o jsonpath='{.status.qosClass}'
# Output: Guaranteed
```

## Step 2: Create a Burstable Pod

At least one container has requests different from limits:

```yaml
# pod-burstable.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-burstable
  namespace: default
spec:
  containers:
    - name: app
      image: nginx:1.27
      resources:
        requests:
          memory: "64Mi"          # Lower than limit
          cpu: "100m"             # Lower than limit
        limits:
          memory: "256Mi"
          cpu: "500m"
  restartPolicy: Never
```

```bash
kubectl apply -f pod-burstable.yaml
kubectl get pod pod-burstable -o jsonpath='{.status.qosClass}'
# Output: Burstable
```

## Step 3: Create a BestEffort Pod

No resources specified at all:

```yaml
# pod-besteffort.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-besteffort
  namespace: default
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      # No resources block
  restartPolicy: Never
```

```bash
kubectl apply -f pod-besteffort.yaml
kubectl get pod pod-besteffort -o jsonpath='{.status.qosClass}'
# Output: BestEffort
```

## Step 4: Understand the Edge Cases

A pod with only limits (no requests) becomes Burstable, not Guaranteed, because Kubernetes sets requests equal to limits only when limits are specified without explicit requests. Check:

```yaml
# pod-limits-only.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-limits-only
  namespace: default
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        limits:
          memory: "128Mi"
          cpu: "250m"
        # No explicit requests -- Kubernetes auto-sets them equal to limits
  restartPolicy: Never
```

```bash
kubectl apply -f pod-limits-only.yaml
kubectl get pod pod-limits-only -o jsonpath='{.status.qosClass}'
# Output: Guaranteed (because K8s auto-sets requests = limits)
```

A pod with only requests (no limits) is Burstable:

```yaml
# pod-requests-only.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-requests-only
  namespace: default
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "64Mi"
          cpu: "100m"
        # No limits
  restartPolicy: Never
```

```bash
kubectl apply -f pod-requests-only.yaml
kubectl get pod pod-requests-only -o jsonpath='{.status.qosClass}'
# Output: Burstable
```

## Step 5: Compare All QoS Classes

```bash
kubectl get pods -o custom-columns=NAME:.metadata.name,QOS:.status.qosClass,CPU_REQ:.spec.containers[0].resources.requests.cpu,CPU_LIM:.spec.containers[0].resources.limits.cpu,MEM_REQ:.spec.containers[0].resources.requests.memory,MEM_LIM:.spec.containers[0].resources.limits.memory
```

## Common Mistakes

### Mistake 1: Thinking "Guaranteed" Means High Resources

A Guaranteed pod with `cpu: 50m` and `memory: 32Mi` is still Guaranteed. The class is about the relationship between requests and limits, not the absolute values. A pod with 50m CPU guaranteed can still be starved for performance -- it just will not be evicted first.

### Mistake 2: Partial Resource Specs in Multi-Container Pods

In a multi-container pod, ALL containers must have requests == limits for the pod to be Guaranteed. If even one container is missing limits, the entire pod becomes Burstable.

### Mistake 3: Assuming BestEffort Gets Zero Resources

BestEffort pods can use any available resources on the node. They are not restricted -- they simply have no guarantees and are evicted first when the node is under pressure.

## Verify What You Learned

```bash
kubectl get pods -o custom-columns=NAME:.metadata.name,QOS:.status.qosClass
```

Expected output:

```
NAME               QOS
pod-guaranteed     Guaranteed
pod-burstable      Burstable
pod-besteffort     BestEffort
pod-limits-only    Guaranteed
pod-requests-only  Burstable
```

## Cleanup

```bash
kubectl delete pod pod-guaranteed pod-burstable pod-besteffort pod-limits-only pod-requests-only --ignore-not-found
```

## What's Next

Now that you understand QoS classes, the next exercise covers LimitRanges, which set default and constrained resource values at the namespace level. Continue to [exercise 03 (LimitRanges: Default and Constraints Per Namespace)](../03-limitranges/03-limitranges.md).

## Summary

- **Guaranteed**: all containers have requests == limits for both CPU and memory. Evicted last.
- **Burstable**: at least one container has a request or limit, but they are not all equal. Evicted second.
- **BestEffort**: no container has any request or limit. Evicted first under memory pressure.
- QoS class is **calculated automatically** from resource specs -- you cannot set it directly.
- Setting only limits (no requests) results in **Guaranteed** because Kubernetes auto-sets requests equal to limits.
- In multi-container pods, **all containers** must meet the criteria for the pod to be Guaranteed.

## Reference

- [Pod Quality of Service Classes](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/) -- official concept documentation
- [Configure Quality of Service for Pods](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/) -- task guide

## Additional Resources

- [Node-pressure Eviction](https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/)
- [Resource Management for Pods and Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
