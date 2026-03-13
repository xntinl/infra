# 7. Priority Classes and Preemption

<!--
difficulty: advanced
concepts: [priority-class, preemption, scheduling-order, preemption-policy, system-priority]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [10-01, 10-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-resource-requests-and-limits/01-resource-requests-and-limits.md) and [exercise 02](../02-qos-classes/02-qos-classes.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how PriorityClasses affect scheduling order and preemption decisions
- **Create** a priority hierarchy that ensures critical workloads always get resources
- **Evaluate** when to use `PreemptLowerPriority` versus `Never` preemption policies

## Architecture

Priority and preemption work together to ensure critical workloads run even when the cluster is full:

```
Scheduling Queue (ordered by priority):
  1. [10000] critical-service    -> Scheduled immediately
  2. [1000]  standard-app        -> Scheduled if resources available
  3. [100]   batch-job           -> Scheduled last, may be preempted

Preemption Flow:
  high-priority pod (Pending)
  -> Scheduler finds no node with capacity
  -> Identifies nodes where evicting low-priority pods frees enough resources
  -> Evicts low-priority pods
  -> Schedules high-priority pod on freed node
```

## The Challenge

Create a priority hierarchy and demonstrate preemption behavior:

### Task 1: Define Priority Classes

Create four PriorityClasses:

```yaml
# priorityclass-low.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: low-priority
value: 100
globalDefault: false
preemptionPolicy: PreemptLowerPriority
description: "Low priority for non-critical batch workloads."
```

```yaml
# priorityclass-medium.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: medium-priority
value: 1000
globalDefault: true
preemptionPolicy: PreemptLowerPriority
description: "Medium priority, assigned by default."
```

```yaml
# priorityclass-high.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: high-priority
value: 10000
globalDefault: false
preemptionPolicy: PreemptLowerPriority
description: "High priority for production services."
```

```yaml
# priorityclass-high-no-preempt.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: high-priority-no-preempt
value: 10000
globalDefault: false
preemptionPolicy: Never
description: "High scheduling priority but will not evict other pods."
```

### Task 2: Fill the Cluster with Low-Priority Pods

Create a Deployment with low-priority pods that consume significant resources:

```yaml
# deployment-low-priority.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: low-priority-workload
spec:
  replicas: 5
  selector:
    matchLabels:
      app: low-priority-workload
  template:
    metadata:
      labels:
        app: low-priority-workload
    spec:
      priorityClassName: low-priority
      containers:
        - name: worker
          image: busybox:1.37
          command: ["sh", "-c", "sleep 3600"]
          resources:
            requests:
              memory: "128Mi"
              cpu: "250m"
            limits:
              memory: "128Mi"
              cpu: "250m"
      terminationGracePeriodSeconds: 5
```

### Task 3: Trigger Preemption with a High-Priority Pod

Create a high-priority pod that requires resources currently held by low-priority pods:

```yaml
# pod-high-priority.yaml
apiVersion: v1
kind: Pod
metadata:
  name: high-priority-critical
spec:
  priorityClassName: high-priority
  containers:
    - name: critical-app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'High priority running' && sleep 3600"]
      resources:
        requests:
          memory: "256Mi"
          cpu: "500m"
        limits:
          memory: "256Mi"
          cpu: "500m"
  restartPolicy: Never
```

### Task 4: Test No-Preempt Priority

Deploy a high-priority-no-preempt pod and observe that it waits for resources instead of evicting:

```yaml
# pod-no-preempt.yaml
apiVersion: v1
kind: Pod
metadata:
  name: high-no-preempt
spec:
  priorityClassName: high-priority-no-preempt
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "256Mi"
          cpu: "500m"
        limits:
          memory: "256Mi"
          cpu: "500m"
  restartPolicy: Never
```

### Task 5: Verify Default Priority Assignment

Create a pod without specifying a PriorityClass and verify it gets the `globalDefault`:

```bash
kubectl run test-default --image=busybox:1.37 --command -- sleep 3600
kubectl get pod test-default -o jsonpath='{.spec.priorityClassName}'
# Should show: medium-priority
kubectl delete pod test-default
```

## Suggested Steps

1. Apply all PriorityClasses and verify with `kubectl get priorityclass`
2. Deploy the low-priority workload and wait for all pods to run
3. Apply the high-priority pod and watch for preemption events
4. Check `kubectl get events --sort-by='.lastTimestamp'` for preemption evidence
5. Test the no-preempt variant and verify it stays Pending when resources are unavailable
6. Verify globalDefault behavior with an unclassified pod

## Verify What You Learned

```bash
kubectl get priorityclass
kubectl get pods -o custom-columns=NAME:.metadata.name,PRIORITY:.spec.priority,CLASS:.spec.priorityClassName,STATUS:.status.phase
kubectl get events --sort-by='.lastTimestamp' | grep -i "preempt\|evict"
```

## Cleanup

```bash
kubectl delete deployment low-priority-workload --ignore-not-found
kubectl delete pod high-priority-critical high-no-preempt --ignore-not-found
kubectl delete priorityclass low-priority medium-priority high-priority high-priority-no-preempt
```

## What's Next

Priority Classes control scheduling order. The next exercise covers Pod Overhead and Runtime Classes, which account for the resource cost of the container runtime itself. Continue to [exercise 08 (Pod Overhead and Runtime Classes)](../08-pod-overhead-and-runtime-classes/08-pod-overhead-and-runtime-classes.md).

## Summary

- **PriorityClass** assigns a numeric priority value to pods; higher values mean higher priority
- The scheduler processes **higher-priority pods first** in the scheduling queue
- **Preemption** evicts lower-priority pods when a higher-priority pod cannot be scheduled
- **`preemptionPolicy: Never`** gives scheduling priority without evicting other pods
- **`globalDefault: true`** sets the default PriorityClass for pods that do not specify one
- Built-in classes `system-cluster-critical` and `system-node-critical` are reserved for system components

## Reference

- [Pod Priority and Preemption](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/)
- [Scheduling Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [API-initiated Eviction](https://kubernetes.io/docs/concepts/scheduling-eviction/api-eviction/)
