# 8. Pod Overhead and Runtime Classes

<!--
difficulty: advanced
concepts: [runtime-class, pod-overhead, kata-containers, gvisor, container-runtime, cri]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [10-01, 10-02, 10-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-resource-requests-and-limits/01-resource-requests-and-limits.md), [exercise 02](../02-qos-classes/02-qos-classes.md), and [exercise 07](../07-priority-classes-and-preemption/07-priority-classes-and-preemption.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how RuntimeClasses affect pod scheduling and resource accounting through pod overhead
- **Create** RuntimeClass configurations and understand how they map to container runtime handlers
- **Evaluate** the resource cost of different container runtimes (runc, gVisor, Kata) for workload placement

## Architecture

Different container runtimes provide different isolation levels at different resource costs:

```
+------------------+     +------------------+     +------------------+
|  runc (default)  |     |  gVisor (runsc)  |     |  Kata Containers |
|  Overhead: ~0    |     |  Overhead: ~50Mi |     |  Overhead: ~150Mi|
|  Isolation: ns   |     |  Isolation: user |     |  Isolation: VM   |
|  Startup: fast   |     |  Startup: fast   |     |  Startup: slow   |
+------------------+     +------------------+     +------------------+
        |                        |                        |
        +------------------------+------------------------+
                                 |
                    +------------+------------+
                    |   CRI (Container Runtime|
                    |   Interface)            |
                    +-------------------------+
                                 |
                    +------------+------------+
                    |   kubelet               |
                    +-------------------------+
```

Pod overhead tells the scheduler how much extra memory and CPU the runtime itself consumes, beyond what the containers request. Without overhead accounting, a node could be over-committed because the scheduler does not know about the runtime's resource cost.

## The Challenge

### Task 1: Explore the Default RuntimeClass

```bash
kubectl get runtimeclass
```

Most clusters have no explicit RuntimeClasses configured. The default runtime (usually `runc`) is used when no `runtimeClassName` is specified.

### Task 2: Create RuntimeClasses with Overhead

```yaml
# runtimeclass-standard.yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: standard
handler: runc                          # Maps to the CRI handler name
overhead:
  podFixed:
    memory: "0"                        # runc has negligible overhead
    cpu: "0"
```

```yaml
# runtimeclass-gvisor.yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc                         # gVisor handler name
overhead:
  podFixed:
    memory: "50Mi"                     # gVisor's user-space kernel uses ~50Mi
    cpu: "50m"
scheduling:                            # Optional: constrain to labeled nodes
  nodeSelector:
    runtime: gvisor
```

```yaml
# runtimeclass-kata.yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: kata
handler: kata-runtime                  # Kata handler name
overhead:
  podFixed:
    memory: "150Mi"                    # MicroVM overhead
    cpu: "100m"
scheduling:
  nodeSelector:
    runtime: kata
```

```bash
kubectl apply -f runtimeclass-standard.yaml
kubectl apply -f runtimeclass-gvisor.yaml
kubectl apply -f runtimeclass-kata.yaml
kubectl get runtimeclass
```

### Task 3: Observe Overhead Impact on Scheduling

Create pods with different RuntimeClasses and compare how the scheduler accounts for resources:

```yaml
# pod-standard.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-standard
spec:
  runtimeClassName: standard
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "64Mi"
          cpu: "100m"
  restartPolicy: Never
```

```yaml
# pod-with-overhead.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-with-overhead
  annotations:
    note: "This pod uses 64Mi + 50Mi overhead = 114Mi total"
spec:
  runtimeClassName: gvisor
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: "64Mi"
          cpu: "100m"
  restartPolicy: Never
```

Note: The gVisor pod will only schedule on nodes labeled `runtime: gvisor`. For testing purposes on a standard cluster, you can remove the `scheduling.nodeSelector` from the RuntimeClass or label your node.

### Task 4: Calculate Effective Resource Usage

When overhead is configured, the scheduler adds it to the container requests:

```
Effective requests = container requests + pod overhead
pod-standard:       64Mi memory,  100m CPU (no overhead)
pod-with-overhead:  114Mi memory, 150m CPU (64Mi + 50Mi, 100m + 50m)
```

Verify:

```bash
kubectl get pod pod-standard -o jsonpath='{.spec.overhead}'
kubectl describe node | grep -A 10 "Allocated resources"
```

### Task 5: Evaluate Runtime Selection Criteria

Create a decision matrix:

| Criteria | runc | gVisor | Kata |
|----------|------|--------|------|
| Multi-tenant isolation | Low | Medium | High |
| System call compatibility | Full | Partial | Full |
| Startup time | <1s | <1s | 2-5s |
| Memory overhead | ~0 | ~50Mi | ~150Mi |
| Use case | Standard workloads | Untrusted code | Strict isolation |

## Suggested Steps

1. List existing RuntimeClasses on your cluster
2. Create RuntimeClass resources with overhead specifications
3. Deploy pods using each RuntimeClass
4. Compare the effective resource accounting for each pod
5. Document when each runtime is appropriate based on isolation needs vs overhead cost
6. Test what happens when a pod references a non-existent RuntimeClass

## Verify What You Learned

```bash
kubectl get runtimeclass -o custom-columns=NAME:.metadata.name,HANDLER:.handler,MEM-OVERHEAD:.overhead.podFixed.memory,CPU-OVERHEAD:.overhead.podFixed.cpu
kubectl get pod pod-standard -o jsonpath='{.spec.runtimeClassName}'
```

## Cleanup

```bash
kubectl delete pod pod-standard pod-with-overhead --ignore-not-found
kubectl delete runtimeclass standard gvisor kata --ignore-not-found
```

## What's Next

Now that you understand runtime overhead, the next exercise covers building a comprehensive resource monitoring and optimization workflow. Continue to [exercise 09 (Resource Monitoring and Optimization Workflow)](../09-resource-monitoring-and-optimization/09-resource-monitoring-and-optimization.md).

## Summary

- **RuntimeClass** maps pods to specific container runtime handlers (runc, runsc, kata-runtime)
- **Pod overhead** tells the scheduler how much extra resource the runtime consumes beyond container requests
- The scheduler adds overhead to requests for **accurate node capacity accounting**
- **gVisor** provides user-space kernel isolation with ~50Mi overhead
- **Kata Containers** provides VM-level isolation with ~150Mi overhead
- RuntimeClasses can include **scheduling constraints** to target nodes with specific runtimes installed

## Reference

- [Runtime Class](https://kubernetes.io/docs/concepts/containers/runtime-class/)
- [Pod Overhead](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-overhead/)
- [Container Runtime Interface](https://kubernetes.io/docs/concepts/architecture/cri/)
