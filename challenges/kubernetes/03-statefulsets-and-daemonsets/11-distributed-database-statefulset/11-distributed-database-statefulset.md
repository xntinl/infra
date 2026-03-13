# 11. Deploying a Distributed Database with StatefulSets

<!--
difficulty: insane
concepts: [distributed-database, statefulset-init-containers, replication, leader-election, headless-service-discovery]
tools: [kubectl, minikube]
estimated_time: 90m
bloom_level: create
prerequisites: [03-01, 03-04, 03-08, 03-09]
-->

## Prerequisites

- A running Kubernetes cluster with a default StorageClass and at least 4Gi allocatable storage
- `kubectl` installed and configured
- Completion of exercises 01, 04, 08, and 09 in this category
- Familiarity with PostgreSQL or Redis replication concepts

## The Scenario

You need to deploy a 3-node Redis cluster using StatefulSets where each node has persistent storage and the nodes discover each other via headless Service DNS. The primary (ordinal 0) accepts writes, and replicas (ordinals 1 and 2) replicate from the primary. An init container on each replica must wait for the primary to be available before starting the Redis process. The system must survive pod restarts without data loss and maintain replication after scaling.

This exercise combines nearly every StatefulSet concept: ordered deployment, headless DNS for peer discovery, per-pod PVCs, init containers for dependency ordering, partition-based rolling updates, and PVC retention policies.

## Constraints

1. Use `redis:7` as the container image for all pods.
2. Pod-0 acts as primary. Pods 1 and 2 are replicas configured with `replicaof redis-cluster-0.redis-cluster-svc 6379`.
3. Each pod has a 1Gi PVC mounted at `/data` for Redis persistence (`appendonly yes` in Redis config).
4. An init container on replicas (ordinals > 0) uses `busybox:1.37` to poll `redis-cluster-0.redis-cluster-svc` on port 6379 until it responds, ensuring the primary is ready before replicas start.
5. The headless Service must be named `redis-cluster-svc` and select pods with label `app: redis-cluster`.
6. Use `podManagementPolicy: OrderedReady` so the primary starts first.
7. Configure `updateStrategy` with `partition: 1` so updates roll out to replicas first, leaving the primary untouched until you lower the partition.
8. Set `persistentVolumeClaimRetentionPolicy` with `whenDeleted: Retain` and `whenScaled: Retain`.
9. Write a key to the primary, read it from a replica, delete pod-1, wait for it to restart, and verify the key is still readable from the restarted replica.
10. Scale to 4 replicas and verify the new replica joins as a follower with data replicated.

## Success Criteria

1. Three pods running: `redis-cluster-0` (primary), `redis-cluster-1` (replica), `redis-cluster-2` (replica).
2. `redis-cli -h redis-cluster-0.redis-cluster-svc SET testkey "hello"` succeeds on the primary.
3. `redis-cli -h redis-cluster-1.redis-cluster-svc GET testkey` returns `"hello"` on the replica.
4. After deleting pod-1, the replacement pod re-attaches its PVC and `GET testkey` still returns `"hello"`.
5. After scaling to 4 replicas, `redis-cluster-3` replicates from the primary and can read `testkey`.
6. `partition` is set to 1, so updating the pod template only affects ordinals >= 1.
7. All PVCs survive StatefulSet deletion (`whenDeleted: Retain`).

## Verification Commands

```bash
# Check replication status
kubectl exec redis-cluster-0 -- redis-cli INFO replication

# Write and read across nodes
kubectl exec redis-cluster-0 -- redis-cli SET testkey "hello"
kubectl exec redis-cluster-1 -- redis-cli GET testkey
kubectl exec redis-cluster-2 -- redis-cli GET testkey

# Pod restart data persistence
kubectl delete pod redis-cluster-1
kubectl wait --for=condition=ready pod/redis-cluster-1 --timeout=60s
kubectl exec redis-cluster-1 -- redis-cli GET testkey

# Scale and verify
kubectl scale statefulset redis-cluster --replicas=4
kubectl wait --for=condition=ready pod/redis-cluster-3 --timeout=60s
kubectl exec redis-cluster-3 -- redis-cli GET testkey

# PVC retention after deletion
kubectl delete statefulset redis-cluster
kubectl get pvc -l app=redis-cluster
# All PVCs should still exist
```

## Cleanup

```bash
kubectl delete statefulset redis-cluster --ignore-not-found
kubectl delete svc redis-cluster-svc --ignore-not-found
kubectl delete pvc -l app=redis-cluster
```
