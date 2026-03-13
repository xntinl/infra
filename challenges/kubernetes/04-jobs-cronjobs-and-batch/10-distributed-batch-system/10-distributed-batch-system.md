# 10. Distributed Batch Processing with Work Queues

<!--
difficulty: insane
concepts: [work-queue, distributed-batch, redis-queue, consumer-jobs, fan-out-fan-in, coordination]
tools: [kubectl, minikube]
estimated_time: 90m
bloom_level: create
prerequisites: [04-02, 04-04, 04-05, 04-09]
-->

## Prerequisites

- A running Kubernetes cluster with at least 4Gi allocatable resources
- `kubectl` installed and configured
- Completion of exercises 02, 04, 05, and 09 in this category
- Basic familiarity with Redis commands (SET, GET, LPUSH, RPOP)

## The Scenario

You need to build a distributed batch processing system that handles a variable number of work items without knowing the count at Job creation time. Unlike Indexed Jobs (which require a fixed `completions` count), this system uses a Redis-based work queue where producer pods enqueue items and consumer pods dequeue and process them until the queue is empty.

The system has three components: a Redis instance for the work queue, a producer Job that populates the queue with work items, and a consumer Job with multiple parallel pods that race to dequeue and process items. Each consumer pod processes items one at a time until the queue is drained, then exits. The Job must detect when all work is complete and terminate cleanly.

This pattern is used in production for image processing pipelines, ETL systems, ML training data preparation, and any workload where the number of items is dynamic or discovered at runtime.

## Constraints

1. Deploy Redis as a single-pod Deployment with a ClusterIP Service named `redis-queue` on port 6379.
2. The producer Job creates 20 work items in a Redis list called `work-queue` using `LPUSH`. Each item is a JSON string like `{"id": N, "payload": "process-item-N"}`.
3. The consumer Job uses `completions` unset (single-completion mode) with `parallelism: 4`. Each consumer pod runs a loop that `RPOP`s from `work-queue`, processes the item (simulate with `sleep 1`), and writes a result to a Redis hash `results` using `HSET results item-N "completed"`.
4. Consumer pods must exit cleanly (exit 0) when `RPOP` returns nil (queue empty). The Job is complete when one pod succeeds.
5. Use `backoffLimit: 2` and `activeDeadlineSeconds: 300` on the consumer Job.
6. After all consumers finish, a verifier Job reads the `results` hash and confirms all 20 items were processed.
7. If a consumer pod crashes mid-processing, the item it was working on is lost (at-most-once). Document this limitation and describe how you would implement at-least-once processing.
8. Use `busybox:1.37` for the producer and verifier. Use `redis:7` for consumers (so `redis-cli` is available).

## Success Criteria

1. Redis is running and accessible via `redis-queue:6379`.
2. Producer Job completes with 20 items in the queue.
3. Four consumer pods run in parallel, draining the queue.
4. All consumers exit successfully once the queue is empty.
5. The verifier Job confirms all 20 items have results in the `results` hash.
6. Total processing time is approximately 5 seconds (20 items / 4 parallel consumers x 1 second each).
7. `kubectl get jobs` shows all three Jobs (producer, consumer, verifier) as Complete.

## Verification Commands

```bash
# Redis running
kubectl get deployment redis-queue
kubectl get svc redis-queue

# Producer completed
kubectl get job work-producer
kubectl exec deployment/redis-queue -- redis-cli LLEN work-queue
# Expected: 0 (all items consumed)

# Consumer completed
kubectl get job work-consumer
kubectl get pods -l job-name=work-consumer -o custom-columns=NAME:.metadata.name,STATUS:.status.phase

# All 20 results present
kubectl exec deployment/redis-queue -- redis-cli HLEN results
# Expected: 20

# Verify specific results
kubectl exec deployment/redis-queue -- redis-cli HGETALL results

# Verifier completed
kubectl get job work-verifier
kubectl logs job/work-verifier
```

## Cleanup

```bash
kubectl delete job work-producer work-consumer work-verifier --ignore-not-found
kubectl delete deployment redis-queue --ignore-not-found
kubectl delete svc redis-queue --ignore-not-found
```
