# 4. Indexed Jobs for Work Queues

<!--
difficulty: intermediate
concepts: [indexed-job, completion-mode, job-completion-index, work-partitioning, batch-processing]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [04-01, 04-02]
-->

## Prerequisites

- A running Kubernetes cluster (Kubernetes 1.24+ for stable Indexed Jobs)
- `kubectl` installed and configured
- Completion of exercises [01](../01-jobs-and-cronjobs/01-jobs-and-cronjobs.md) and [02](../02-parallel-jobs-completions/02-parallel-jobs-completions.md)

## Learning Objectives

- **Understand** how Indexed completion mode differs from NonIndexed mode
- **Apply** Indexed Jobs that assign a unique index to each pod via `JOB_COMPLETION_INDEX`
- **Analyze** how indexed work partitioning enables fan-out processing patterns

## Why Indexed Jobs?

Standard (NonIndexed) Jobs create `completions` pods, but each pod is identical — there is no built-in way to assign different work items to different pods. Indexed Jobs solve this by setting the `JOB_COMPLETION_INDEX` environment variable in each pod to a unique integer from 0 to `completions-1`. Pod 0 processes shard 0, pod 1 processes shard 1, and so on. This eliminates the need for an external work queue for embarrassingly parallel workloads.

## Step 1: Create an Indexed Job

```yaml
# indexed-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: indexed-processor
spec:
  completionMode: Indexed     # Each pod gets a unique index
  completions: 5              # 5 indexed work items
  parallelism: 3              # Process up to 3 at a time
  template:
    metadata:
      labels:
        app: indexed-processor
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command:
            - sh
            - -c
            - |
              echo "Pod index: $JOB_COMPLETION_INDEX"
              echo "Processing data partition $JOB_COMPLETION_INDEX of 5"
              sleep $(( JOB_COMPLETION_INDEX + 2 ))
              echo "Partition $JOB_COMPLETION_INDEX complete"
      restartPolicy: Never
```

```bash
kubectl apply -f indexed-job.yaml
kubectl get pods -l app=indexed-processor -w
```

## Step 2: Verify Index Assignment

Check logs from each completed pod:

```bash
kubectl get pods -l app=indexed-processor --sort-by=.status.startTime
```

View logs per index:

```bash
for i in 0 1 2 3 4; do
  echo "=== Index $i ==="
  kubectl logs -l batch.kubernetes.io/job-completion-index=$i -l app=indexed-processor 2>/dev/null
done
```

Each pod received a unique `JOB_COMPLETION_INDEX` from 0 to 4.

## Step 3: Use the Index for Work Partitioning

A more realistic example: process different files based on the index:

```yaml
# file-processor.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: file-processor
spec:
  completionMode: Indexed
  completions: 4
  parallelism: 4
  template:
    spec:
      containers:
        - name: processor
          image: busybox:1.37
          command:
            - sh
            - -c
            - |
              # Map index to a specific file/shard
              FILES=("users.csv" "orders.csv" "products.csv" "inventory.csv")
              FILE=${FILES[$JOB_COMPLETION_INDEX]}
              echo "Processing file: $FILE (index $JOB_COMPLETION_INDEX)"
              sleep 3
              echo "File $FILE processed successfully"
      restartPolicy: Never
```

```bash
kubectl apply -f file-processor.yaml
kubectl wait --for=condition=complete job/file-processor --timeout=60s
```

Check logs:

```bash
kubectl logs -l job-name=file-processor --prefix
```

Each pod processed a different file based on its index.

## Step 4: Understand Index Stability

If an indexed pod fails, Kubernetes creates a replacement with the SAME index. The index is tied to the completion slot, not the pod. This guarantees that index 2 is always processed, even if the first attempt fails.

Test this by inspecting:

```bash
kubectl get job indexed-processor -o jsonpath='{.status.completedIndexes}'
```

Expected output: `0-4` (or comma-separated list like `0,1,2,3,4`).

## Spot the Bug

A developer creates this Indexed Job:

```yaml
spec:
  completionMode: Indexed
  completions: 10
  parallelism: 10
  template:
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command: ["sh", "-c", "echo $INDEX"]   # <-- Wrong variable
```

**Why does every pod print an empty line?**

<details>
<summary>Explanation</summary>

The environment variable is `JOB_COMPLETION_INDEX`, not `INDEX`. Kubernetes only sets this specific variable name. The command should be:

```bash
echo $JOB_COMPLETION_INDEX
```

There is no way to customize the variable name. If your application expects a different variable, use an init script to map it:

```bash
export INDEX=$JOB_COMPLETION_INDEX && your-app
```

</details>

## Verify What You Learned

```bash
# Indexed Job completed all indexes
kubectl get job indexed-processor -o jsonpath='Completed: {.status.completedIndexes}'
# Expected: Completed: 0-4

# Completion mode is Indexed
kubectl get job indexed-processor -o jsonpath='Mode: {.spec.completionMode}'
# Expected: Mode: Indexed
```

## Cleanup

```bash
kubectl delete job indexed-processor file-processor
```

## What's Next

Jobs can fail, and how Kubernetes handles those failures matters. In [exercise 05 (Job Failure Handling)](../05-job-failure-handling/05-job-failure-handling.md), you will explore `backoffLimit`, `activeDeadlineSeconds`, and what happens when Jobs exhaust their retries.

## Summary

- Indexed Jobs assign a unique `JOB_COMPLETION_INDEX` (0 to completions-1) to each pod
- Set `completionMode: Indexed` to enable this behavior
- Failed indexed pods are replaced with the same index, ensuring all partitions are processed
- `status.completedIndexes` tracks which indexes have finished
- Indexed Jobs eliminate the need for external work queues in fan-out processing patterns

## Reference

- [Indexed Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/#completion-mode)
- [Indexed Job for Parallel Processing](https://kubernetes.io/docs/tasks/job/indexed-parallel-processing-static/)
