# 1. Resource Requests and Limits

<!--
difficulty: basic
concepts: [resource-requests, resource-limits, cpu, memory, oomkilled, throttling]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Metrics server installed (optional, for `kubectl top`)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the difference between resource requests and limits for CPU and memory
- **Understand** how the scheduler uses requests for placement and how limits enforce resource caps
- **Apply** resource specifications to pods and observe the effects of exceeding them

## Why Resource Requests and Limits?

Without resource configuration, a single pod can consume all CPU and memory on a node, starving other workloads. Kubernetes uses two mechanisms to prevent this.

**Requests** define the minimum resources a container needs. The scheduler uses requests to decide which node has enough capacity to run a pod. A pod requesting 256Mi of memory will only be placed on a node with at least 256Mi available. Requests are a scheduling guarantee, not a cap.

**Limits** define the maximum resources a container can use. If a container exceeds its memory limit, the kernel's OOM killer terminates it (`OOMKilled`). If a container exceeds its CPU limit, it is throttled -- the kernel pauses the process until the next scheduling period. CPU throttling slows the application but does not kill it. Memory overuse kills the process immediately.

This distinction is critical. Setting requests too high wastes cluster capacity. Setting limits too low causes restarts and throttling. Getting the balance right is one of the most important operational skills in Kubernetes.

## Step 1: Create a Namespace for Resource Experiments

```yaml
# namespace-resources.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: resources-lab
  labels:
    purpose: resource-management-lab
```

```bash
kubectl apply -f namespace-resources.yaml
```

## Step 2: Pod with Explicit Requests and Limits

Create `pod-resources.yaml`:

```yaml
# pod-resources.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-resources
  namespace: resources-lab
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Running with defined resources' && sleep 3600"]
      resources:
        requests:
          memory: "64Mi"           # Scheduling guarantee: need at least 64Mi
          cpu: "100m"              # 100 millicores = 0.1 CPU core
        limits:
          memory: "128Mi"          # Hard cap: OOMKilled if exceeded
          cpu: "250m"              # Soft cap: throttled if exceeded
  restartPolicy: Never
```

```bash
kubectl apply -f pod-resources.yaml
kubectl get pod pod-resources -n resources-lab -o jsonpath='{.spec.containers[0].resources}' | python3 -m json.tool
```

CPU is measured in millicores: `100m` means 10% of one CPU core. `1` means one full core. Memory is measured in bytes with standard suffixes: `Mi` (mebibytes), `Gi` (gibibytes).

## Step 3: Pod Without Resources (BestEffort)

```yaml
# pod-no-resources.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-no-resources
  namespace: resources-lab
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'No resource constraints' && sleep 3600"]
      # No resources block at all
  restartPolicy: Never
```

```bash
kubectl apply -f pod-no-resources.yaml
```

This pod can use unlimited resources on the node. It will be the first to be evicted under memory pressure because Kubernetes assigns it the lowest QoS class (`BestEffort`).

## Step 4: Observe OOMKilled (Memory Limit Exceeded)

Create a pod that deliberately exceeds its memory limit:

```yaml
# pod-oomkilled.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-oomkilled
  namespace: resources-lab
spec:
  containers:
    - name: stress
      image: polinux/stress:latest
      command: ["stress"]
      args:
        - "--vm"
        - "1"
        - "--vm-bytes"
        - "200M"                   # Tries to allocate 200MB
        - "--vm-hang"
        - "1"
      resources:
        requests:
          memory: "50Mi"
          cpu: "100m"
        limits:
          memory: "100Mi"          # Only 100Mi allowed -- will be killed
          cpu: "200m"
  restartPolicy: Never
```

```bash
kubectl apply -f pod-oomkilled.yaml
kubectl get pod pod-oomkilled -n resources-lab -w
```

Watch the pod status change to `OOMKilled`. Inspect the reason:

```bash
kubectl describe pod pod-oomkilled -n resources-lab | grep -A 5 "State\|Reason\|Exit Code"
```

The exit code `137` confirms the OOM killer terminated the process (128 + signal 9).

## Step 5: Observe CPU Throttling

CPU limits do not kill containers -- they throttle them:

```yaml
# pod-cpu-throttled.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-cpu-throttled
  namespace: resources-lab
spec:
  containers:
    - name: cpu-hog
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Starting CPU-intensive loop..."
          i=0
          while [ $i -lt 100000000 ]; do
            i=$((i + 1))
          done
          echo "Done after throttling."
          sleep 3600
      resources:
        requests:
          cpu: "50m"
        limits:
          cpu: "100m"              # Heavily limited
          memory: "64Mi"
  restartPolicy: Never
```

```bash
kubectl apply -f pod-cpu-throttled.yaml
```

If metrics server is installed, observe the throttling:

```bash
kubectl top pod pod-cpu-throttled -n resources-lab
```

The pod will never exceed its CPU limit but will run slowly.

## Common Mistakes

### Mistake 1: Setting Requests Higher Than Limits

```yaml
resources:
  requests:
    memory: "512Mi"      # WRONG: request > limit
  limits:
    memory: "256Mi"
```

Kubernetes rejects this pod. Requests must be less than or equal to limits.

### Mistake 2: Using Decimal CPU Values Incorrectly

```yaml
resources:
  requests:
    cpu: "0.1"           # Valid: same as 100m
  limits:
    cpu: "250m"          # Valid: 250 millicores
```

Both formats work: `0.1` and `100m` are equivalent. Avoid `cpu: "100"` without the `m` suffix, which means 100 full cores.

### Mistake 3: Forgetting Memory Units

```yaml
resources:
  limits:
    memory: "128"        # Means 128 bytes, not 128 megabytes!
```

Always include the unit suffix: `Mi`, `Gi`, `Ki`. Plain numbers are interpreted as bytes.

## Verify What You Learned

```bash
kubectl get pod pod-resources -n resources-lab -o jsonpath='{.status.qosClass}'
# Burstable (requests != limits)

kubectl get pod pod-no-resources -n resources-lab -o jsonpath='{.status.qosClass}'
# BestEffort (no resources defined)

kubectl get pod pod-oomkilled -n resources-lab -o jsonpath='{.status.containerStatuses[0].state}'
# Shows terminated with reason OOMKilled
```

## Cleanup

```bash
kubectl delete namespace resources-lab
```

## What's Next

Now that you understand requests and limits, the next exercise explores the three QoS classes that Kubernetes automatically assigns based on resource configuration. Continue to [exercise 02 (QoS Classes: Guaranteed, Burstable, BestEffort)](../02-qos-classes/02-qos-classes.md).

## Summary

- **Requests** are the minimum resources guaranteed to a container; the scheduler uses them for pod placement.
- **Limits** are the maximum resources a container can use; exceeding memory limits causes `OOMKilled`, exceeding CPU limits causes throttling.
- CPU is measured in **millicores** (`100m` = 0.1 core); memory is measured in **bytes** with suffixes (`Mi`, `Gi`).
- A pod without any resource specifications gets the `BestEffort` QoS class and is first to be evicted.
- Requests must be **less than or equal to** limits; Kubernetes rejects pods where request > limit.

## Reference

- [Resource Management for Pods and Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) -- official concept documentation
- [Assign CPU Resources to Containers](https://kubernetes.io/docs/tasks/configure-pod-container/assign-cpu-resource/) -- task guide
- [Assign Memory Resources to Containers](https://kubernetes.io/docs/tasks/configure-pod-container/assign-memory-resource/) -- task guide

## Additional Resources

- [Pod Quality of Service Classes](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/)
- [Meaning of CPU](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#meaning-of-cpu)
- [Meaning of Memory](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/#meaning-of-memory)
