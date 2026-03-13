# 2. Parallel Jobs with Completions and Parallelism

<!--
difficulty: basic
concepts: [parallel-jobs, completions, parallelism, job-completion-modes, non-indexed-job]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [04-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Jobs and CronJobs)](../01-jobs-and-cronjobs/01-jobs-and-cronjobs.md)

## Learning Objectives

- **Remember** the relationship between `completions`, `parallelism`, and total pod count
- **Understand** how Kubernetes schedules parallel pods and tracks completion progress
- **Apply** different completions/parallelism ratios to observe scheduling behavior

## Why Parallel Jobs?

Many batch workloads consist of independent units of work: processing 100 images, running tests across 20 configurations, or sending notifications to thousands of users. Running these sequentially wastes time. Kubernetes Jobs let you set `completions` (total successful runs needed) and `parallelism` (maximum concurrent pods). The Job controller creates pods up to the parallelism limit and replaces completed ones until the completion target is met.

Understanding the interplay between these two fields prevents common mistakes like accidentally creating hundreds of pods simultaneously or running everything sequentially when it could be parallel.

## Step 1: Create a Job with High Parallelism

This Job simulates processing 8 work items with up to 4 pods running simultaneously:

```yaml
# parallel-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: batch-processor
spec:
  completions: 8          # 8 total successful completions needed
  parallelism: 4          # Run up to 4 pods concurrently
  backoffLimit: 2          # Max 2 retries per pod
  template:
    metadata:
      labels:
        app: batch-processor
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command:
            - sh
            - -c
            - |
              WORK_ID=$RANDOM
              echo "Processing item $WORK_ID"
              sleep $(( RANDOM % 5 + 2 ))
              echo "Completed item $WORK_ID"
      restartPolicy: Never
```

Apply and watch:

```bash
kubectl apply -f parallel-job.yaml
kubectl get pods -l app=batch-processor -w
```

Expected behavior: 4 pods start immediately. As each completes, a new one launches until 8 completions are reached. You will see two waves of 4 pods.

## Step 2: Monitor Completion Progress

Track the Job's progress:

```bash
kubectl get job batch-processor -w
```

Watch the `COMPLETIONS` column increment from `0/8` to `8/8`.

Inspect detailed status:

```bash
kubectl describe job batch-processor
```

Look for:
- **Pods Statuses**: shows running, succeeded, and failed counts
- **Events**: shows each pod creation

## Step 3: Compare Different Ratios

Delete the first Job and try different configurations:

```bash
kubectl delete job batch-processor
```

### Ratio 1: completions=6, parallelism=1 (Sequential)

```yaml
# sequential-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: sequential-processor
spec:
  completions: 6
  parallelism: 1            # One pod at a time
  template:
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command: ["sh", "-c", "echo Processing; sleep 2"]
      restartPolicy: Never
```

```bash
kubectl apply -f sequential-job.yaml
kubectl get pods -l job-name=sequential-processor -w
```

Pods run one at a time. Total duration is approximately 6 x 2 = 12 seconds of work.

### Ratio 2: completions=6, parallelism=6 (Fully Parallel)

```yaml
# fully-parallel-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: fully-parallel-processor
spec:
  completions: 6
  parallelism: 6            # All pods at once
  template:
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command: ["sh", "-c", "echo Processing; sleep 2"]
      restartPolicy: Never
```

```bash
kubectl apply -f fully-parallel-job.yaml
kubectl get pods -l job-name=fully-parallel-processor -w
```

All 6 pods start simultaneously. Total duration is approximately 2 seconds.

## Step 4: Understand the Default Behavior

When you omit `completions`, Kubernetes defaults to 1 completion. When you omit `parallelism`, Kubernetes defaults to 1. This means a bare Job with no fields runs exactly one pod:

```yaml
spec:
  # completions: 1    (implicit default)
  # parallelism: 1    (implicit default)
  template: ...
```

If you set `parallelism` without `completions`, the Job runs pods until one succeeds.

## Common Mistakes

### Mistake 1: Setting parallelism Higher Than completions

```yaml
completions: 3
parallelism: 10
```

Kubernetes caps actual parallelism at `completions`. Only 3 pods run. This is not an error, but it wastes no resources — Kubernetes is smart about the cap.

### Mistake 2: Forgetting backoffLimit

Without `backoffLimit`, the default is 6 retries. A bug that causes every pod to fail generates 6 x `completions` pods before the Job gives up. Set `backoffLimit` explicitly to control failure behavior.

## Verify What You Learned

```bash
# Sequential job completed
kubectl get job sequential-processor -o jsonpath='{.status.succeeded}'
# Expected: 6

# Fully parallel job completed
kubectl get job fully-parallel-processor -o jsonpath='{.status.succeeded}'
# Expected: 6

# Check that fully-parallel was faster (compare ages of completed pods)
kubectl get pods -l job-name=sequential-processor -o custom-columns=NAME:.metadata.name,START:.status.startTime
kubectl get pods -l job-name=fully-parallel-processor -o custom-columns=NAME:.metadata.name,START:.status.startTime
```

## Cleanup

```bash
kubectl delete job sequential-processor fully-parallel-processor
```

## What's Next

Jobs run on demand, but CronJobs run on schedules. In [exercise 03 (CronJob Schedules and Concurrency Policies)](../03-cronjob-schedules-and-policies/03-cronjob-schedules-and-policies.md), you will explore schedule syntax in depth and test all three concurrency policies.

## Summary

- `completions` sets the total number of successful pod runs; `parallelism` sets the max concurrent pods
- Kubernetes caps actual parallelism at `completions` — setting parallelism higher has no effect
- Omitting both fields gives you a single-pod, single-run Job
- Higher parallelism reduces total duration but increases cluster resource usage
- Always set `backoffLimit` explicitly to control failure behavior
- Pods are created and tracked by the Job controller, not by you

## Reference

- [Jobs: Parallel Execution](https://kubernetes.io/docs/concepts/workloads/controllers/job/#parallel-jobs)
- [Job Completions and Parallelism](https://kubernetes.io/docs/concepts/workloads/controllers/job/#controlling-parallelism)

## Additional Resources

- [Kubernetes API Reference: Job v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)
- [Coarse Parallel Processing Using a Work Queue](https://kubernetes.io/docs/tasks/job/coarse-parallel-processing-work-queue/)
- [Fine Parallel Processing Using a Work Queue](https://kubernetes.io/docs/tasks/job/fine-parallel-processing-work-queue/)
