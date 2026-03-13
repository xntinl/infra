# 8. Pod Failure Policy for Jobs

<!--
difficulty: advanced
concepts: [pod-failure-policy, container-exit-codes, job-failure-reasons, retriable-errors, non-retriable-errors]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [04-05]
-->

## Prerequisites

- A running Kubernetes cluster (Kubernetes 1.31+ for stable Pod Failure Policy)
- `kubectl` installed and configured
- Completion of [exercise 05 (Job Failure Handling)](../05-job-failure-handling/05-job-failure-handling.md)
- Understanding of `backoffLimit` and Job failure behavior

## Learning Objectives

- **Analyze** how Pod Failure Policies distinguish between retriable and non-retriable errors
- **Evaluate** which exit codes and failure conditions warrant retries vs immediate Job failure
- **Create** Jobs with pod failure policies that handle different error types differently

## Architecture

Without a pod failure policy, all pod failures are treated equally: the Job retries up to `backoffLimit` regardless of the failure reason. A configuration error (exit code 42) gets the same treatment as a transient network timeout (exit code 1). Pod Failure Policies let you define rules that map specific failure conditions to actions:

- **FailJob**: Immediately fail the entire Job (non-retriable error)
- **Ignore**: Do not count this failure against `backoffLimit` (known transient issue)
- **Count** (default): Count toward `backoffLimit` as usual

Rules can match on:
- Container exit codes
- Pod conditions (e.g., `DisruptionTarget` for preemption)

## The Challenge

1. Create a Job with a pod failure policy that:
   - Treats exit code 42 as a non-retriable configuration error (FailJob immediately)
   - Treats exit code 137 (OOMKilled) as retriable (Count toward backoffLimit)
   - Ignores pods terminated due to preemption (DisruptionTarget condition)
2. Test each path by creating Jobs that simulate each failure type
3. Verify that the Job behavior matches the policy

<details>
<summary>Hint 1: Pod failure policy structure</summary>

```yaml
spec:
  backoffLimit: 3
  podFailurePolicy:
    rules:
      - action: FailJob
        onExitCodes:
          containerName: worker
          operator: In
          values: [42]
      - action: Ignore
        onPodConditions:
          - type: DisruptionTarget
      - action: Count
        onExitCodes:
          containerName: worker
          operator: In
          values: [137]
```

Rules are evaluated in order. The first matching rule wins.

</details>

<details>
<summary>Hint 2: Simulating exit codes</summary>

```yaml
containers:
  - name: worker
    image: busybox:1.37
    command: ["sh", "-c", "exit 42"]    # Simulate config error
```

</details>

<details>
<summary>Hint 3: Verifying FailJob behavior</summary>

```bash
kubectl get job config-error-job -o jsonpath='{.status.conditions[0]}'
```

Look for `type: Failed` with `reason: PodFailurePolicy` and `message` mentioning exit code 42.

</details>

## Verify What You Learned

```bash
# Job with exit 42 should fail immediately (no retries)
kubectl get job config-error-job -o jsonpath='{.status.conditions[0].type}'
# Expected: Failed

# Only 1 pod should exist (FailJob stopped retries)
kubectl get pods -l job-name=config-error-job --no-headers | wc -l
# Expected: 1

# Job with exit 137 should retry up to backoffLimit
kubectl get pods -l job-name=oom-error-job --no-headers | wc -l
# Expected: up to backoffLimit + 1

# Describe the failed job for policy details
kubectl describe job config-error-job | grep "PodFailurePolicy"
```

## Cleanup

```bash
kubectl delete job config-error-job oom-error-job --ignore-not-found
```

## What's Next

Individual Jobs process discrete work items, but real-world batch systems chain multiple Jobs together. In [exercise 09 (Building a Batch Processing Pipeline)](../09-batch-processing-pipeline/09-batch-processing-pipeline.md), you will build a multi-stage batch pipeline.

## Summary

- Pod Failure Policies map specific failure conditions to actions: FailJob, Ignore, or Count
- `FailJob` immediately terminates the Job without further retries (for non-retriable errors)
- `Ignore` does not count the failure against `backoffLimit` (for known transient disruptions)
- Rules can match on container exit codes or pod conditions like `DisruptionTarget`
- Rules are evaluated in order; the first matching rule takes effect
- Without a pod failure policy, all failures are treated equally and counted toward backoffLimit

## Reference

- [Pod Failure Policy](https://kubernetes.io/docs/concepts/workloads/controllers/job/#pod-failure-policy)
- [Handling Pod and Container Failures](https://kubernetes.io/docs/concepts/workloads/controllers/job/#handling-pod-and-container-failures)
- [Pod Disruption Conditions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#pod-disruption-conditions)
