# 5. Job Failure Handling: backoffLimit and activeDeadlineSeconds

<!--
difficulty: intermediate
concepts: [backoff-limit, active-deadline-seconds, job-failure, restart-policy, exponential-backoff]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [04-01, 04-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of exercises [01](../01-jobs-and-cronjobs/) and [02](../02-parallel-jobs-completions/)

## Learning Objectives

- **Understand** how `backoffLimit` controls retry behavior and exponential backoff timing
- **Apply** `activeDeadlineSeconds` to enforce maximum Job runtime
- **Analyze** the interaction between `restartPolicy`, `backoffLimit`, and failure counting

## Why Failure Handling Matters

A Job without failure bounds can spawn pods indefinitely. A misconfigured batch process that crashes on every attempt creates pods with exponential backoff delays up to 6 minutes, consuming resources and filling logs. Understanding `backoffLimit` and `activeDeadlineSeconds` is essential for building robust batch workloads that fail fast and clean up after themselves.

## Step 1: Observe backoffLimit and Exponential Backoff

Create a Job that always fails:

```yaml
# failing-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: failing-job
spec:
  backoffLimit: 3              # Allow 3 retries before marking as Failed
  template:
    metadata:
      labels:
        app: failing-job
    spec:
      containers:
        - name: fail
          image: busybox:1.37
          command: ["sh", "-c", "echo Attempt starting; exit 1"]
      restartPolicy: Never     # Each failure creates a NEW pod
```

```bash
kubectl apply -f failing-job.yaml
kubectl get pods -l app=failing-job -w
```

Watch the pods. Kubernetes creates them with exponential backoff: ~10s, ~20s, ~40s between attempts. After 3 failures (plus the initial attempt = 4 total pods), the Job status becomes `Failed`.

```bash
kubectl get job failing-job
```

Expected output:

```
NAME         STATUS   COMPLETIONS   DURATION   AGE
failing-job  Failed   0/1           45s        50s
```

Inspect the failure:

```bash
kubectl describe job failing-job | grep -A5 "Pods Statuses"
```

## Step 2: Compare restartPolicy Never vs OnFailure

With `restartPolicy: Never`, each failure creates a NEW pod. With `OnFailure`, the kubelet restarts the container inside the SAME pod:

```yaml
# restart-onfailure.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: restart-job
spec:
  backoffLimit: 3
  template:
    metadata:
      labels:
        app: restart-job
    spec:
      containers:
        - name: fail
          image: busybox:1.37
          command: ["sh", "-c", "echo Attempt; exit 1"]
      restartPolicy: OnFailure   # Restart in same pod
```

```bash
kubectl apply -f restart-onfailure.yaml
kubectl get pods -l app=restart-job -w
```

You see only ONE pod, but its RESTARTS count increments. After reaching the backoff limit, the pod is terminated and the Job fails.

```bash
kubectl get pods -l app=restart-job
```

Expected: 1 pod with multiple restarts, eventually terminated.

Key difference:
- `Never`: `backoffLimit` counts pods. 3 retries = 4 pods total.
- `OnFailure`: `backoffLimit` counts container restarts within the pod.

## Step 3: Enforce Maximum Runtime with activeDeadlineSeconds

```yaml
# deadline-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: deadline-job
spec:
  activeDeadlineSeconds: 15     # Kill after 15 seconds regardless of status
  backoffLimit: 10              # High retry limit — deadline will trigger first
  template:
    metadata:
      labels:
        app: deadline-job
    spec:
      containers:
        - name: slow
          image: busybox:1.37
          command: ["sh", "-c", "echo Working...; sleep 30"]
      restartPolicy: Never
```

```bash
kubectl apply -f deadline-job.yaml
```

Wait 20 seconds:

```bash
kubectl get job deadline-job
```

Expected: Status is `Failed` with reason `DeadlineExceeded`.

```bash
kubectl describe job deadline-job | grep -A2 "Conditions"
```

The Job was terminated after 15 seconds even though `backoffLimit` would have allowed more retries.

## Step 4: Combine Both Safeguards

In production, always set both:

```yaml
spec:
  backoffLimit: 4                  # Max 4 retries
  activeDeadlineSeconds: 600       # Max 10 minutes total runtime
```

This ensures Jobs fail fast on persistent errors (backoffLimit) and do not run forever on hanging processes (deadline).

## Verify What You Learned

```bash
# Failing job hit backoff limit
kubectl get job failing-job -o jsonpath='{.status.conditions[0].type}'
# Expected: Failed

# Deadline job was killed by timeout
kubectl describe job deadline-job | grep "DeadlineExceeded"

# Count pods created by failing-job (should be backoffLimit + 1)
kubectl get pods -l app=failing-job --no-headers | wc -l
# Expected: 4
```

## Cleanup

```bash
kubectl delete job failing-job restart-job deadline-job
```

## What's Next

CronJobs manage the lifecycle of scheduled Jobs, but sometimes you need to pause them or control their history. In [exercise 06 (CronJob Suspend, Resume, and History Limits)](../06-cronjob-suspend-and-history/), you will learn how to manage CronJob lifecycle.

## Summary

- `backoffLimit` controls how many retries before a Job is marked Failed (default: 6)
- Kubernetes uses exponential backoff between retries: 10s, 20s, 40s, up to 6 minutes
- `restartPolicy: Never` creates new pods on failure; `OnFailure` restarts containers in-place
- `activeDeadlineSeconds` enforces a hard time limit on total Job runtime
- Always set both `backoffLimit` and `activeDeadlineSeconds` for production Jobs

## Reference

- [Job Failures and Retries](https://kubernetes.io/docs/concepts/workloads/controllers/job/#pod-backoff-failure-policy)
- [Job Termination and Cleanup](https://kubernetes.io/docs/concepts/workloads/controllers/job/#job-termination-and-cleanup)
