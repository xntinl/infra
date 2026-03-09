# 3. CronJob Schedules and Concurrency Policies

<!--
difficulty: basic
concepts: [cronjob-schedule, cron-syntax, concurrency-policy, allow, forbid, replace, timezone]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [04-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Jobs and CronJobs)](../01-jobs-and-cronjobs/)

## Learning Objectives

- **Remember** cron schedule syntax: minute, hour, day-of-month, month, day-of-week
- **Understand** the behavior differences between `Allow`, `Forbid`, and `Replace` concurrency policies
- **Apply** CronJobs with specific schedules and observe concurrency policy effects

## Why Schedules and Concurrency Policies?

Cron schedule syntax is simple but easy to misconfigure. The difference between `*/5 * * * *` (every 5 minutes) and `5 * * * *` (at minute 5 of every hour) has caused countless production incidents. Concurrency policies add another dimension: what happens when a scheduled Job fires while the previous one is still running? Database backups must not overlap (`Forbid`). Report generators might benefit from starting fresh (`Replace`). Understanding both axes prevents data corruption and wasted resources.

## Step 1: Schedule Syntax Deep Dive

Cron format: `minute hour day-of-month month day-of-week`

| Schedule | Meaning |
|----------|---------|
| `* * * * *` | Every minute |
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | At minute 0 of every hour |
| `0 0 * * *` | Midnight daily |
| `0 0 * * 0` | Midnight every Sunday |
| `0 9 1 * *` | 9:00 AM on the 1st of every month |
| `30 2 * * 1-5` | 2:30 AM Monday through Friday |

Create a CronJob that runs every minute for testing:

```yaml
# every-minute.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: every-minute
spec:
  schedule: "* * * * *"
  concurrencyPolicy: Allow
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: tick
              image: busybox:1.37
              command: ["sh", "-c", "echo Tick at $(date); sleep 5"]
          restartPolicy: Never
```

```bash
kubectl apply -f every-minute.yaml
```

Wait 2-3 minutes, then check the Job history:

```bash
kubectl get jobs --sort-by=.metadata.creationTimestamp
```

You should see Jobs created one minute apart, with at most 3 retained.

## Step 2: Test Forbid Policy with a Long-Running Job

Create a CronJob that runs every minute but takes 90 seconds to complete:

```yaml
# forbid-demo.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: forbid-demo
spec:
  schedule: "* * * * *"
  concurrencyPolicy: Forbid      # Skip if previous job is running
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: slow
              image: busybox:1.37
              command: ["sh", "-c", "echo Started at $(date); sleep 90; echo Done"]
          restartPolicy: Never
```

```bash
kubectl apply -f forbid-demo.yaml
```

Wait 3 minutes and check:

```bash
kubectl get jobs -l job-name --sort-by=.metadata.creationTimestamp
kubectl get cronjob forbid-demo
```

With `Forbid`, the CronJob fires every minute, but since the Job takes 90 seconds, every other scheduled run is skipped. The ACTIVE column on the CronJob shows at most 1.

## Step 3: Test Replace Policy

```yaml
# replace-demo.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: replace-demo
spec:
  schedule: "* * * * *"
  concurrencyPolicy: Replace      # Kill running job, start new one
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: slow
              image: busybox:1.37
              command: ["sh", "-c", "echo Started at $(date); sleep 90; echo Done"]
          restartPolicy: Never
```

```bash
kubectl apply -f replace-demo.yaml
```

Wait 3 minutes and check:

```bash
kubectl get jobs -l job-name --sort-by=.metadata.creationTimestamp
```

With `Replace`, when the next schedule fires and the previous Job is still running, Kubernetes deletes the running Job and creates a new one. You will see Jobs that never completed because they were replaced.

## Step 4: Timezone Support

Kubernetes 1.27+ supports the `timeZone` field:

```yaml
spec:
  schedule: "0 9 * * *"
  timeZone: "America/New_York"     # Run at 9 AM Eastern
```

Without `timeZone`, the schedule uses the kube-controller-manager's timezone (usually UTC). Verify your cluster's default:

```bash
kubectl get cronjob every-minute -o jsonpath='{.spec.timeZone}'
```

An empty result means UTC.

## Common Mistakes

### Mistake 1: Confusing */5 and 5

```yaml
schedule: "5 * * * *"      # At minute 5 of every hour (1x/hour)
schedule: "*/5 * * * *"    # Every 5 minutes (12x/hour)
```

### Mistake 2: No Deadline for Long-Running Jobs

If a CronJob's Job hangs, subsequent runs pile up (with `Allow`) or are skipped indefinitely (with `Forbid`). Add `startingDeadlineSeconds` and `activeDeadlineSeconds` on the Job template:

```yaml
spec:
  startingDeadlineSeconds: 100   # On CronJob: max delay to start
  jobTemplate:
    spec:
      activeDeadlineSeconds: 300 # On Job: max runtime before killed
```

## Verify What You Learned

```bash
# every-minute has Allow policy
kubectl get cronjob every-minute -o jsonpath='{.spec.concurrencyPolicy}'
# Expected: Allow

# forbid-demo never has more than 1 active job
kubectl get cronjob forbid-demo -o jsonpath='Active: {.status.active}'

# replace-demo shows replaced (incomplete) jobs
kubectl get jobs --sort-by=.metadata.creationTimestamp -o custom-columns=NAME:.metadata.name,COMPLETIONS:.status.conditions[0].type
```

## Cleanup

```bash
kubectl delete cronjob every-minute forbid-demo replace-demo
```

## What's Next

Standard Jobs assign random work to each pod. In [exercise 04 (Indexed Jobs for Work Queues)](../04-indexed-jobs/), you will learn how Indexed Jobs assign a specific index to each pod, enabling partitioned processing.

## Summary

- Cron syntax: `minute hour day-of-month month day-of-week` with `*`, `*/N`, ranges, and lists
- `Allow` (default): multiple Jobs run concurrently if schedules overlap
- `Forbid`: skip the new Job if the previous one is still running
- `Replace`: kill the running Job and start a new one
- `timeZone` field (Kubernetes 1.27+) controls schedule timezone; default is UTC
- Always set `activeDeadlineSeconds` on Jobs and `startingDeadlineSeconds` on CronJobs for safety

## Reference

- [CronJob Schedule Syntax](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#schedule-syntax)
- [CronJob Concurrency Policy](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#concurrency-policy)

## Additional Resources

- [CronJob Limitations](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#cron-job-limitations)
- [Running Automated Tasks with CronJobs](https://kubernetes.io/docs/tasks/job/automated-tasks-with-cron-jobs/)
- [Time Zones in CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#time-zones)
