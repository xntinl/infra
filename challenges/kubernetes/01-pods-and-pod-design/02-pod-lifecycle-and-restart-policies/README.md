# 2. Pod Lifecycle and Restart Policies

<!--
difficulty: basic
concepts: [pod-lifecycle, restart-policy, pod-phase, container-states, CrashLoopBackOff]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [01-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Pod)](../01-your-first-pod/) or equivalent understanding of Pods

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the five Pod phases: Pending, Running, Succeeded, Failed, Unknown
- **Understand** how the three restart policies (Always, OnFailure, Never) control container restart behavior
- **Apply** different restart policies and observe how the kubelet responds to container exits

## Why Pod Lifecycle?

When a Pod is created, it does not jump straight to `Running`. It passes through a series of phases that reflect what is happening under the hood: the scheduler assigns it to a node (`Pending`), the kubelet starts the containers (`Running`), and eventually the containers exit (`Succeeded` or `Failed`). Understanding these phases is essential for debugging. A Pod stuck in `Pending` means the scheduler cannot find a suitable node. A Pod in `Failed` means at least one container exited with a non-zero code and will not be restarted.

The restart policy is one of the most misunderstood Pod fields. It controls what the kubelet does when a container inside the Pod exits. `Always` (the default) restarts the container regardless of exit code. `OnFailure` restarts only on non-zero exit codes. `Never` leaves the container dead. Choosing the wrong restart policy leads to Pods stuck in `CrashLoopBackOff` (a container that keeps crashing and gets restarted with exponentially increasing delays) or Pods that silently disappear after completing their work.

This exercise walks you through each phase and restart policy combination so you can predict exactly what Kubernetes will do when a container exits.

## Step 1: Observe Pod Phases with a Short-Lived Container

Create a Pod that runs a command and exits successfully:

```yaml
# success-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: success-pod
  labels:
    exercise: lifecycle
spec:
  restartPolicy: Never           # Do not restart after exit
  containers:
    - name: worker
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Job done'; exit 0"]  # Exits with code 0
```

Apply and watch the phase transitions:

```bash
kubectl apply -f success-pod.yaml
kubectl get pod success-pod -w
```

You will see the Pod move through `Pending` -> `ContainerCreating` -> `Completed`. Press `Ctrl+C` to stop watching. Because the restart policy is `Never` and the container exited with code 0, the Pod phase becomes `Succeeded`.

Check the final state:

```bash
kubectl get pod success-pod -o jsonpath='{.status.phase}'
```

Expected output: `Succeeded`.

## Step 2: Observe a Failed Container

Create a Pod that exits with a non-zero code:

```yaml
# failure-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: failure-pod
  labels:
    exercise: lifecycle
spec:
  restartPolicy: Never           # Do not restart after exit
  containers:
    - name: worker
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Something went wrong'; exit 1"]  # Non-zero exit
```

Apply and check the status:

```bash
kubectl apply -f failure-pod.yaml
sleep 5
kubectl get pod failure-pod
```

Expected output shows `Error` status. The Pod phase is `Failed` because the container exited with a non-zero code and the restart policy is `Never`.

Inspect the exit code:

```bash
kubectl get pod failure-pod -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}'
```

Expected output: `1`.

## Step 3: Restart Policy Always (the Default)

Create a Pod with the default restart policy that runs a container which exits immediately:

```yaml
# always-restart-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: always-restart-pod
  labels:
    exercise: lifecycle
spec:
  restartPolicy: Always          # Default — restarts regardless of exit code
  containers:
    - name: worker
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Starting...'; sleep 3; exit 0"]
```

Apply and watch:

```bash
kubectl apply -f always-restart-pod.yaml
kubectl get pod always-restart-pod -w
```

The container runs for 3 seconds, exits with code 0, and gets restarted. After a few restarts, the status will show `CrashLoopBackOff`. This does not mean the container is crashing with an error -- it means Kubernetes is applying exponential backoff delays (10s, 20s, 40s, up to 5 minutes) between restarts because the container keeps exiting.

Press `Ctrl+C` and check the restart count:

```bash
kubectl get pod always-restart-pod
```

The `RESTARTS` column will show an increasing number. This is the correct behavior for `restartPolicy: Always` with a short-lived container, and it is why long-running services use `Always` while batch jobs use `OnFailure` or `Never`.

## Step 4: Restart Policy OnFailure

Create a Pod that uses `OnFailure` and exits successfully:

```yaml
# onfailure-success-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: onfailure-success-pod
  labels:
    exercise: lifecycle
spec:
  restartPolicy: OnFailure       # Only restart on non-zero exit codes
  containers:
    - name: worker
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Task completed successfully'; exit 0"]
```

```bash
kubectl apply -f onfailure-success-pod.yaml
sleep 5
kubectl get pod onfailure-success-pod
```

The Pod shows `Completed` status and the restart count stays at 0. Because the exit code was 0 and the policy is `OnFailure`, the kubelet does not restart the container.

Now create one that fails:

```yaml
# onfailure-fail-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: onfailure-fail-pod
  labels:
    exercise: lifecycle
spec:
  restartPolicy: OnFailure       # Restart on non-zero exit codes
  containers:
    - name: worker
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Failing...'; exit 1"]
```

```bash
kubectl apply -f onfailure-fail-pod.yaml
kubectl get pod onfailure-fail-pod -w
```

The container fails and gets restarted, eventually entering `CrashLoopBackOff`. The key difference from `Always` is that a successful exit (code 0) would stop the restarts.

Press `Ctrl+C` when you have seen at least one restart.

## Step 5: Examine Container State Details

Kubernetes tracks three possible container states: `Waiting`, `Running`, and `Terminated`. Inspect a terminated container:

```bash
kubectl describe pod failure-pod | grep -A 10 "State:"
```

Key fields in the output:

- `State: Terminated` — the container has exited
- `Reason: Completed` (exit 0) or `Error` (non-zero exit)
- `Exit Code` — the numeric exit code from the process
- `Started` and `Finished` — timestamps showing container duration

For the always-restart Pod, you will see both `State` (current) and `Last State` (previous run):

```bash
kubectl describe pod always-restart-pod | grep -A 15 "State:"
```

## Common Mistakes

### Mistake 1: Using restartPolicy Always for Batch Jobs

```yaml
# WRONG for a one-shot task
spec:
  restartPolicy: Always
  containers:
    - name: migrate
      image: busybox:1.37
      command: ["sh", "-c", "echo 'migration done'; exit 0"]
```

The container completes successfully but Kubernetes keeps restarting it, wasting resources and ending up in `CrashLoopBackOff`. Use `OnFailure` or `Never` for containers that are meant to exit.

**Fix:** Set `restartPolicy: OnFailure` for tasks that should retry on failure, or `restartPolicy: Never` for tasks that should run exactly once.

### Mistake 2: Confusing Pod Phase with Container Status

A Pod can be in phase `Running` while one of its containers is in state `Waiting` (CrashLoopBackOff). The Pod phase reflects the overall Pod, while container states reflect individual containers. Always check both:

```bash
kubectl get pod <name>                          # Pod-level status
kubectl describe pod <name> | grep -A 5 State   # Container-level state
```

## Verify What You Learned

Check the phase of each Pod:

```bash
kubectl get pods -l exercise=lifecycle
```

Expected output:

```
NAME                    READY   STATUS             RESTARTS      AGE
success-pod             0/1     Completed          0             3m
failure-pod             0/1     Error              0             2m
always-restart-pod      0/1     CrashLoopBackOff   4 (30s ago)   2m
onfailure-success-pod   0/1     Completed          0             1m
onfailure-fail-pod      0/1     CrashLoopBackOff   3 (20s ago)   1m
```

Verify the exit codes:

```bash
kubectl get pod success-pod -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}'
# Expected: 0

kubectl get pod failure-pod -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}'
# Expected: 1
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete pods -l exercise=lifecycle
```

Verify nothing remains:

```bash
kubectl get pods -l exercise=lifecycle
```

## What's Next

Now that you understand Pod lifecycle phases and restart policies, the next exercise introduces labels, selectors, and annotations -- the metadata system that lets you organize, filter, and annotate your Kubernetes resources. Continue to [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/).

## Summary

- Pods pass through five **phases**: Pending, Running, Succeeded, Failed, Unknown.
- **restartPolicy: Always** (default) restarts containers regardless of exit code -- use for long-running services.
- **restartPolicy: OnFailure** restarts only on non-zero exit codes -- use for retry-capable batch tasks.
- **restartPolicy: Never** does not restart containers -- use for one-shot tasks.
- **CrashLoopBackOff** means the kubelet is applying exponential backoff between restarts, not necessarily that there is a crash bug.

## Reference

- [Pod Lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/) — phases, conditions, and container states
- [Container States](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-states) — Waiting, Running, Terminated
- [Restart Policy](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#restart-policy) — Always, OnFailure, Never

## Additional Resources

- [Debugging Pods](https://kubernetes.io/docs/tasks/debug/debug-application/debug-pods/) — troubleshooting common Pod issues
- [Kubernetes API Reference: Pod v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/)
- [CrashLoopBackOff Explained](https://kubernetes.io/docs/tasks/debug/debug-application/debug-pods/#my-pod-keeps-crashing-or-is-otherwise-unhealthy)
