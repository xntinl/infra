# 6. CronJob Suspend, Resume, and History Limits

<!--
difficulty: intermediate
concepts: [cronjob-suspend, history-limits, cronjob-lifecycle, starting-deadline-seconds]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [04-01, 04-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of exercises [01](../01-jobs-and-cronjobs/) and [03](../03-cronjob-schedules-and-policies/)

## Learning Objectives

- **Understand** how `suspend` pauses CronJob scheduling without deleting the resource
- **Apply** history limits to control how many completed and failed Jobs are retained
- **Analyze** how `startingDeadlineSeconds` interacts with suspend and missed schedules

## Why Suspend and History Management?

During maintenance windows, incident response, or deployments, you often need to temporarily stop scheduled Jobs without losing the CronJob configuration. The `suspend` field provides a clean on/off switch. History limits prevent old Jobs from accumulating and consuming etcd storage. Together, these fields give operators fine-grained control over CronJob lifecycle.

## Step 1: Create a CronJob and Suspend It

```yaml
# managed-cron.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: managed-cron
spec:
  schedule: "* * * * *"
  suspend: false                        # Active by default
  successfulJobsHistoryLimit: 2         # Keep only 2 successful Jobs
  failedJobsHistoryLimit: 1             # Keep only 1 failed Job
  startingDeadlineSeconds: 120          # Must start within 120s of schedule
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: reporter
              image: busybox:1.37
              command: ["sh", "-c", "echo Report generated at $(date)"]
          restartPolicy: Never
```

```bash
kubectl apply -f managed-cron.yaml
```

Wait for 1-2 executions, then suspend:

```bash
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":true}}'
kubectl get cronjob managed-cron
```

Expected output shows `SUSPEND: True`:

```
NAME           SCHEDULE    TIMEZONE   SUSPEND   ACTIVE   LAST SCHEDULE   AGE
managed-cron   * * * * *   <none>     True      0        30s             90s
```

Wait 2 more minutes. No new Jobs are created while suspended.

```bash
kubectl get jobs
```

Only the Jobs from before suspension remain.

## Step 2: Resume the CronJob

```bash
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":false}}'
```

The CronJob resumes on the next schedule tick. Note that missed schedules during suspension are NOT retroactively executed (unless `startingDeadlineSeconds` allows catching up).

## Step 3: Observe History Limits

With `successfulJobsHistoryLimit: 2`, Kubernetes only retains the 2 most recent successful Jobs:

```bash
kubectl get jobs --sort-by=.metadata.creationTimestamp
```

Wait for 4-5 executions. You should never see more than 2 successful Jobs and 1 failed Job listed.

## Step 4: Understand startingDeadlineSeconds

The `startingDeadlineSeconds` field defines the window in which a missed CronJob can still be started. If the kube-controller-manager was down or the CronJob was suspended, Kubernetes counts how many schedules were missed. If more than 100 schedules were missed, the CronJob stops scheduling entirely and requires manual intervention.

Setting `startingDeadlineSeconds: 120` means:
- If the scheduled time was 2 minutes ago or less, the Job still starts
- If the scheduled time was more than 2 minutes ago, the Job is skipped

Test by suspending for 3+ minutes, then resuming:

```bash
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":true}}'
# Wait 3+ minutes
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":false}}'
kubectl get jobs -w
```

The missed schedule is skipped because it falls outside the 120-second deadline.

## Step 5: Imperative Suspend with kubectl

You can also use `kubectl` directly:

```bash
# Suspend
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":true}}'

# Resume
kubectl patch cronjob managed-cron -p '{"spec":{"suspend":false}}'

# Check status
kubectl get cronjob managed-cron -o jsonpath='Suspend: {.spec.suspend}, Last: {.status.lastScheduleTime}'
```

## Spot the Bug

A team sets `successfulJobsHistoryLimit: 0` to save storage, then cannot debug why their batch pipeline is failing.

<details>
<summary>Explanation</summary>

With `successfulJobsHistoryLimit: 0`, completed Jobs and their pods are deleted immediately. This means `kubectl logs` returns nothing because the pods no longer exist. A minimum of 1 is recommended for debugging. For production batch pipelines, set at least 3 to compare recent runs.

</details>

## Verify What You Learned

```bash
# CronJob should be active (not suspended)
kubectl get cronjob managed-cron -o jsonpath='{.spec.suspend}'
# Expected: false

# History limits
kubectl get cronjob managed-cron -o jsonpath='Success limit: {.spec.successfulJobsHistoryLimit}, Failed limit: {.spec.failedJobsHistoryLimit}'
# Expected: Success limit: 2, Failed limit: 1

# No more than 2 successful jobs exist
kubectl get jobs -o custom-columns=NAME:.metadata.name,STATUS:.status.conditions[0].type | grep -c Complete
# Expected: 2 or less
```

## Cleanup

```bash
kubectl delete cronjob managed-cron
```

## What's Next

Completed Jobs leave behind pods that consume resources. In [exercise 07 (TTL Controller and Job Cleanup)](../07-ttl-after-finished/), you will learn how the TTL-after-finished controller automatically cleans up completed Jobs.

## Summary

- `suspend: true` stops CronJob scheduling without deleting the resource; `suspend: false` resumes it
- Missed schedules during suspension are not retroactively executed
- `successfulJobsHistoryLimit` and `failedJobsHistoryLimit` control how many old Jobs are retained
- Setting history limit to 0 deletes Jobs immediately, preventing log access for debugging
- `startingDeadlineSeconds` defines the window for catching up on missed schedules
- If more than 100 schedules are missed, the CronJob requires manual intervention

## Reference

- [CronJob Suspend](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#schedule-suspension)
- [CronJob History Limits](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#jobs-history-limits)
