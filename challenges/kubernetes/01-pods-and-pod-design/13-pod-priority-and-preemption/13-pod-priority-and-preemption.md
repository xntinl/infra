# 13. Pod Priority and Preemption

<!--
difficulty: advanced
concepts: [priority-class, preemption, scheduling, resource-pressure, non-preempting]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [01-02, 01-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube or kind with resource limits)
- `kubectl` installed and configured
- Completion of [exercise 02 (Pod Lifecycle and Restart Policies)](../02-pod-lifecycle-and-restart-policies/02-pod-lifecycle-and-restart-policies.md) and [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/03-labels-selectors-and-annotations.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** PriorityClasses to control Pod scheduling priority
- **Analyze** how preemption works when cluster resources are constrained
- **Evaluate** the trade-offs between preempting and non-preempting priority classes

## Architecture

When a cluster runs out of resources, the scheduler must decide which Pods get scheduled and which wait. PriorityClasses assign a numeric priority value to Pods. Higher-priority Pods can preempt (evict) lower-priority Pods to free up resources. The flow is:

1. A high-priority Pod cannot be scheduled due to insufficient resources
2. The scheduler identifies lower-priority Pods that, if evicted, would free enough resources
3. Those Pods are evicted (moved to `Terminating`)
4. The high-priority Pod is scheduled in the freed space

## Steps

### 1. Create PriorityClasses

```yaml
# priority-classes.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: critical
value: 1000000
globalDefault: false
preemptionPolicy: PreemptLowerPriority
description: "Critical workloads that can preempt others"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: standard
value: 100
globalDefault: true
preemptionPolicy: PreemptLowerPriority
description: "Default priority for standard workloads"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: background
value: 10
globalDefault: false
preemptionPolicy: Never
description: "Background tasks that never preempt other Pods"
```

```bash
kubectl apply -f priority-classes.yaml
kubectl get priorityclasses
```

Note: `preemptionPolicy: Never` means background Pods will never evict other Pods, even lower-priority ones.

### 2. Fill the Cluster with Low-Priority Pods

Create Pods that consume most of the cluster's resources:

```yaml
# low-priority-pods.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: background-workers
spec:
  replicas: 5
  selector:
    matchLabels:
      app: background-worker
  template:
    metadata:
      labels:
        app: background-worker
    spec:
      priorityClassName: background
      containers:
        - name: worker
          image: busybox:1.37
          command: ["sh", "-c", "while true; do echo working; sleep 60; done"]
          resources:
            requests:
              cpu: "200m"
              memory: "128Mi"
            limits:
              cpu: "200m"
              memory: "128Mi"
```

```bash
kubectl apply -f low-priority-pods.yaml
kubectl get pods -l app=background-worker
```

### 3. Deploy a Critical Pod

Create a Pod with the `critical` priority class:

```yaml
# critical-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: critical-task
  labels:
    app: critical-task
spec:
  priorityClassName: critical
  containers:
    - name: task
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Critical task running' && sleep 3600"]
      resources:
        requests:
          cpu: "200m"
          memory: "128Mi"
        limits:
          cpu: "200m"
          memory: "128Mi"
```

```bash
kubectl apply -f critical-pod.yaml
```

If the cluster has enough resources, the critical Pod is scheduled immediately. If resources are tight, watch for preemption:

```bash
kubectl get pods -w
kubectl get events --sort-by=.lastTimestamp | grep -i preempt
```

### 4. Observe the Non-Preempting Background Class

The `background` PriorityClass has `preemptionPolicy: Never`. Even if there are lower-priority Pods, background Pods will wait in Pending rather than evict them:

```bash
kubectl get priorityclass background -o yaml | grep preemptionPolicy
```

### 5. Inspect Priority on Running Pods

```bash
kubectl get pod critical-task -o jsonpath='{.spec.priority}'
# Expected: 1000000

kubectl get pods -l app=background-worker -o jsonpath='{.items[0].spec.priority}'
# Expected: 10
```

## Verify What You Learned

```bash
kubectl get priorityclasses
# Expected: critical (1000000), standard (100, default), background (10)

kubectl get pod critical-task -o jsonpath='{.spec.priorityClassName}'
# Expected: critical

kubectl describe pod critical-task | grep "Priority"
```

## Cleanup

```bash
kubectl delete pod critical-task
kubectl delete deployment background-workers
kubectl delete priorityclass critical standard background
```

## Summary

- **PriorityClasses** assign numeric priority values (higher = more important) to Pods
- **Preemption** evicts lower-priority Pods when resources are insufficient for higher-priority ones
- `preemptionPolicy: Never` creates non-preempting priorities for background workloads
- `globalDefault: true` makes a PriorityClass the default for Pods without an explicit class
- System-critical Pods (kube-system) use built-in `system-cluster-critical` and `system-node-critical` classes

## Reference

- [Pod Priority and Preemption](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/) — official concept documentation
- [PriorityClass](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/priority-class-v1/) — API reference
- [Scheduling Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/) — how the scheduler processes priorities
