# 7. TTL Controller and Job Cleanup

<!--
difficulty: advanced
concepts: [ttl-after-finished, job-cleanup, ttl-controller, resource-management, completed-pods]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: analyze
prerequisites: [04-01, 04-05]
-->

## Prerequisites

- A running Kubernetes cluster (Kubernetes 1.23+ for stable TTL-after-finished)
- `kubectl` installed and configured
- Completion of exercises [01](../01-jobs-and-cronjobs/) and [05](../05-job-failure-handling/)

## Learning Objectives

- **Analyze** the resource impact of completed Jobs and pods that are never cleaned up
- **Evaluate** appropriate TTL values for different workload types
- **Create** Jobs with `ttlSecondsAfterFinished` and observe automatic cleanup

## Architecture

When a Job completes (or fails), its pods remain in `Completed` or `Error` status. These zombie pods consume etcd storage, clutter `kubectl get pods` output, and count against ResourceQuotas in some configurations. The TTL-after-finished controller watches for Jobs with a `ttlSecondsAfterFinished` field and deletes them (along with their pods) after the specified duration.

The cleanup chain:
1. Job finishes (succeeds or fails)
2. TTL timer starts counting from the Job's completion time
3. After the TTL expires, the TTL controller deletes the Job
4. Cascading deletion removes the Job's pods and associated resources

## The Challenge

1. Create three Jobs with different TTL values: 30 seconds, 120 seconds, and 0 seconds (immediate cleanup)
2. Observe which Jobs and pods are cleaned up and when
3. Create a Job with `ttlSecondsAfterFinished: 0` and verify it is cleaned up within seconds of completion
4. Analyze what happens when a CronJob creates Jobs with TTL — does the CronJob's `successfulJobsHistoryLimit` conflict with the Job's TTL?
5. Design a cleanup strategy for a production workload that needs logs retained for 1 hour but cleaned up automatically after

<details>
<summary>Hint 1: TTL field placement</summary>

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: ttl-demo
spec:
  ttlSecondsAfterFinished: 30    # Delete 30 seconds after completion
  template:
    spec:
      containers:
        - name: worker
          image: busybox:1.37
          command: ["echo", "done"]
      restartPolicy: Never
```

</details>

<details>
<summary>Hint 2: TTL with CronJobs</summary>

Set `ttlSecondsAfterFinished` inside the CronJob's `jobTemplate`:

```yaml
spec:
  jobTemplate:
    spec:
      ttlSecondsAfterFinished: 300
      template: ...
```

The TTL controller and `successfulJobsHistoryLimit` both clean up Jobs. Whichever triggers first wins. If TTL is shorter than the interval between CronJob runs, the history limit is effectively bypassed.

</details>

<details>
<summary>Hint 3: Verifying cleanup</summary>

```bash
# Watch Jobs disappear
kubectl get jobs -w

# Check if pods still exist
kubectl get pods -l job-name=ttl-demo-0
```

</details>

## Verify What You Learned

```bash
# After TTL expires, the Job should be gone
kubectl get job ttl-demo-0
# Expected: Error from server (NotFound)

# Job with ttlSecondsAfterFinished: 0 should be cleaned up almost immediately
kubectl get jobs
# The immediate-cleanup Job should not appear

# Long-TTL Job should still exist if TTL has not expired
kubectl get job ttl-demo-120
# Should still exist if checked within 120 seconds
```

## Cleanup

```bash
# Most resources self-clean via TTL, but remove any remaining
kubectl delete job --all
```

## What's Next

Kubernetes 1.26 introduced Pod Failure Policies, which give fine-grained control over how Jobs respond to specific failure types. In [exercise 08 (Pod Failure Policy for Jobs)](../08-job-pod-failure-policy/), you will learn how to distinguish between retriable and non-retriable errors.

## Summary

- `ttlSecondsAfterFinished` triggers automatic cleanup of completed (or failed) Jobs
- Setting it to 0 causes near-immediate deletion after completion
- The TTL controller deletes the Job, which cascades to delete pods
- For CronJobs, TTL and `successfulJobsHistoryLimit` both manage cleanup — whichever fires first wins
- Without TTL, completed Jobs and pods persist until manually deleted
- Choose TTL values that balance log retention needs against resource consumption

## Reference

- [TTL Controller for Finished Resources](https://kubernetes.io/docs/concepts/workloads/controllers/ttlafterfinished/)
- [Job Termination and Cleanup](https://kubernetes.io/docs/concepts/workloads/controllers/job/#job-termination-and-cleanup)
- [Automatic Cleanup for Finished Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/#clean-up-finished-jobs-automatically)
