# 1. Jobs and CronJobs

<!--
difficulty: basic
concepts: [job, cronjob, completions, parallelism, backoff-limit, concurrency-policy]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the difference between Jobs (run-to-completion) and Deployments (long-running)
- **Understand** how completions, parallelism, and backoffLimit control Job behavior
- **Apply** a CronJob with schedule syntax and concurrencyPolicy

## Why Jobs and CronJobs?

Not everything in Kubernetes is a long-running service. Data migrations, report generation, image processing, database backups, and machine learning training runs all share a common pattern: they start, do their work, and exit. Running these workloads as Deployments would be wrong because a Deployment restarts pods that exit successfully, treating completion as a failure. Jobs are the correct abstraction for run-to-completion work.

A Job creates one or more pods and ensures that a specified number of them successfully terminate. If a pod fails, the Job creates a replacement (up to a configurable retry limit). You can also run multiple pods in parallel to process work faster. CronJobs build on this by creating Jobs on a time-based schedule, just like cron on Linux. The `concurrencyPolicy` field controls what happens when the next scheduled run fires while the previous one is still executing. Understanding these primitives is essential for anyone building data pipelines or operational automation on Kubernetes.

## Step 1: Run a Parallel Job

Create `job.yaml` with a Job that computes pi to 2000 decimal places. The Job requires 5 successful completions and runs 2 pods at a time:

```yaml
# job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: pi-calculator
spec:
  completions: 5         # Run 5 successful completions total
  parallelism: 2         # Run 2 pods at a time
  backoffLimit: 3        # Retry failed pods up to 3 times
  template:
    spec:
      containers:
        - name: pi
          image: perl:5.40
          command: ["perl", "-Mbignum=bpi", "-wle", "print bpi(2000)"]
      restartPolicy: Never   # Jobs must use Never or OnFailure
```

Apply the Job:

```bash
kubectl apply -f job.yaml
```

Watch the pods being created in parallel batches:

```bash
kubectl get pods -l job-name=pi-calculator -w
```

You will see 2 pods running at a time. As each completes, a new one starts until all 5 completions are done. Press `Ctrl+C` once the Job finishes.

Verify the Job completed successfully:

```bash
kubectl get job pi-calculator
```

Expected output:

```
NAME            STATUS     COMPLETIONS   DURATION   AGE
pi-calculator   Complete   5/5           45s        60s
```

Check the logs of one of the completed pods to see the result:

```bash
kubectl logs job/pi-calculator
```

You should see a long number starting with `3.14159265...`.

## Step 2: Understand Job Behavior

Inspect the Job details to understand how Kubernetes tracked the completions:

```bash
kubectl describe job pi-calculator
```

Key fields in the output:

- **Completions** — the target number of successful runs (5)
- **Parallelism** — maximum concurrent pods (2)
- **Succeeded** — how many pods completed successfully
- **Pods Statuses** — breakdown of running, succeeded, and failed pods

Notice that the pods are not deleted after completion. Kubernetes keeps them so you can retrieve their logs. The Job's `ttlSecondsAfterFinished` field (not set here) can automatically clean them up after a delay.

## Step 3: Fill in the CronJob and Deploy

Create `cronjob.yaml` with the following content. The `schedule` and `concurrencyPolicy` fields are left as TODOs for you to complete:

```yaml
# cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: hello-cron
spec:
  # TODO: Add schedule in cron format
  # Requirement: Run every 2 minutes
  # Format: "*/2 * * * *"
  # Docs: https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#schedule-syntax

  # TODO: Add concurrencyPolicy
  # Requirement: Forbid concurrent runs (if previous job is still running, skip)
  # Options: Allow, Forbid, Replace
  # Docs: https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#concurrency-policy

  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: hello
              image: busybox:1.37
              command: ["/bin/sh", "-c", "echo Hello from CronJob at $(date); sleep 30"]
          restartPolicy: OnFailure
```

Once you have filled in both TODO fields, apply the CronJob:

```bash
kubectl apply -f cronjob.yaml
```

Verify the CronJob was created:

```bash
kubectl get cronjob hello-cron
```

Expected output:

```
NAME         SCHEDULE      TIMEZONE   SUSPEND   ACTIVE   LAST SCHEDULE   AGE
hello-cron   */2 * * * *   <none>     False     0        <none>          5s
```

## Step 4: Watch the CronJob Execute

Wait for the first scheduled execution (up to 2 minutes). You can watch for Jobs being created:

```bash
kubectl get jobs -w
```

Once a Job appears, check its pods:

```bash
kubectl get pods -l job-name --show-labels
```

View the logs from the most recent CronJob execution:

```bash
kubectl logs job/$(kubectl get jobs -o jsonpath='{.items[0].metadata.name}')
```

Expected output:

```
Hello from CronJob at Sun Mar  8 12:00:00 UTC 2026
```

Wait for a second execution (another 2 minutes) and verify that the history limit is respected:

```bash
kubectl get jobs
```

You should see at most 3 successful Jobs retained (matching `successfulJobsHistoryLimit: 3`).

## Step 5: Understand ConcurrencyPolicy

The `concurrencyPolicy: Forbid` setting means that if the CronJob's schedule fires while a previous Job is still running, Kubernetes skips the new run entirely. This is critical for workloads like database backups where running two instances simultaneously could cause data corruption.

The three options are:

| Policy    | Behavior                                                 |
|-----------|----------------------------------------------------------|
| `Allow`   | Multiple Jobs can run concurrently (default)             |
| `Forbid`  | Skip the new Job if the previous one is still running    |
| `Replace` | Cancel the running Job and start a new one               |

Since the hello-cron container sleeps for 30 seconds and runs every 2 minutes, `Forbid` will never trigger in this exercise. In production, you would use `Forbid` for longer-running tasks where overlap is dangerous.

## Common Mistakes

### Mistake 1: Using restartPolicy: Always

```yaml
# WRONG
spec:
  template:
    spec:
      restartPolicy: Always
```

Kubernetes rejects this with:

```
The Job "broken-job" is invalid: spec.template.spec.restartPolicy:
Unsupported value: "Always": supported values: "OnFailure", "Never"
```

Jobs require `Never` or `OnFailure`. `Always` would cause the kubelet to restart a successfully completed container forever, contradicting run-to-completion semantics.

### Mistake 2: Missing completions field

If you omit `completions`, the Job runs exactly 1 pod and finishes when it succeeds. This is correct for single-run jobs, but if you expect parallel batch processing, you must set `completions` explicitly.

## Verify What You Learned

Job completed all 5 runs:

```bash
kubectl get job pi-calculator -o jsonpath='{.status.succeeded}'
```

Expected output:

```
5
```

CronJob is active and has the correct schedule:

```bash
kubectl get cronjob hello-cron -o jsonpath='{.spec.schedule} {.spec.concurrencyPolicy}'
```

Expected output:

```
*/2 * * * * Forbid
```

At least one CronJob execution has completed (wait up to 2 minutes if needed):

```bash
kubectl get jobs -l job-name
```

Expected output shows one or more Jobs with `COMPLETIONS 1/1`.

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete job pi-calculator
kubectl delete cronjob hello-cron
```

Verify nothing remains:

```bash
kubectl get jobs,cronjobs
```

Expected output:

```
No resources found in default namespace.
```

## What's Next

You have seen how `completions` and `parallelism` work together. In [exercise 02 (Parallel Jobs with Completions and Parallelism)](../02-parallel-jobs-completions/02-parallel-jobs-completions.md), you will dive deeper into parallel processing patterns and understand the different Job completion modes.

## Summary

- **Jobs** manage run-to-completion workloads. `completions` sets the target, `parallelism` controls concurrency, and `backoffLimit` caps retries.
- **CronJobs** create Jobs on a cron schedule. `concurrencyPolicy` controls overlap behavior: `Allow`, `Forbid`, or `Replace`.
- Job templates require `restartPolicy: Never` or `restartPolicy: OnFailure`. Using `Always` is rejected because it contradicts run-to-completion semantics.
- `successfulJobsHistoryLimit` and `failedJobsHistoryLimit` control how many completed Jobs are retained for log inspection.
- Kubernetes keeps completed pods around so you can retrieve their logs.

## Reference

- [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/) — official concept documentation
- [CronJobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/) — scheduling and concurrency policies
- [Cron Schedule Syntax](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#schedule-syntax) — format reference

## Additional Resources

- [Kubernetes API Reference: Job v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/job-v1/)
- [Kubernetes API Reference: CronJob v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/cron-job-v1/)
- [Running Automated Tasks with a CronJob](https://kubernetes.io/docs/tasks/job/automated-tasks-with-cron-jobs/) — tutorial walkthrough

---

<details>
<summary>TODO Solution: Schedule and ConcurrencyPolicy</summary>

```yaml
spec:
  schedule: "*/2 * * * *"
  concurrencyPolicy: Forbid
```

Place these two fields at the top of the CronJob spec, before `successfulJobsHistoryLimit`.

</details>
