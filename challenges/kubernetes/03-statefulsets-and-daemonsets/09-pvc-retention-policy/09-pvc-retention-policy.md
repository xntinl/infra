# 9. PersistentVolumeClaim Retention Policies

<!--
difficulty: advanced
concepts: [pvc-retention-policy, statefulset-lifecycle, volume-cleanup, when-deleted, when-scaled]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [03-01, 03-02]
-->

## Prerequisites

- A running Kubernetes cluster (Kubernetes 1.27+ for stable `persistentVolumeClaimRetentionPolicy`)
- `kubectl` installed and configured
- A default StorageClass (`kubectl get storageclass`)
- Completion of exercises [01](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md) and [02](../02-statefulset-scaling-behavior/02-statefulset-scaling-behavior.md)
- The `StatefulSetAutoDeletePVC` feature gate must be enabled (enabled by default since 1.27)

## Learning Objectives

- **Analyze** the four combinations of `whenDeleted` and `whenScaled` retention policies
- **Evaluate** which retention policy is appropriate for different workload types
- **Create** StatefulSets with explicit PVC retention policies for both scale-down and deletion scenarios

## Architecture

By default, StatefulSet PVCs are never automatically deleted. The `persistentVolumeClaimRetentionPolicy` field controls this with two independent settings:

| Field | Controls | Options |
|-------|----------|---------|
| `whenDeleted` | PVC behavior when StatefulSet is deleted | `Retain` (default), `Delete` |
| `whenScaled` | PVC behavior when replicas are scaled down | `Retain` (default), `Delete` |

The four combinations create different lifecycle behaviors:

- **Retain/Retain** (default): PVCs survive everything. Manual cleanup required.
- **Delete/Retain**: PVCs deleted with StatefulSet, but scale-down preserves them for scale-up reattachment.
- **Retain/Delete**: Scale-down cleans up PVCs, but deleting the StatefulSet leaves PVCs for manual recovery.
- **Delete/Delete**: Full automatic cleanup in all cases. Data loss risk.

## The Challenge

1. Create a StatefulSet with 3 replicas and `volumeClaimTemplates` that provisions 1Gi PVCs
2. Configure `persistentVolumeClaimRetentionPolicy` with `whenDeleted: Delete` and `whenScaled: Retain`
3. Write unique data to each pod's PVC
4. Scale down to 1 replica and verify PVCs for pods 1 and 2 are retained
5. Scale back up and verify pods 1 and 2 re-attach to their existing PVCs with data intact
6. Delete the StatefulSet and verify all PVCs are automatically deleted
7. Create a second StatefulSet with `whenScaled: Delete` and demonstrate that scale-down removes the PVC

<details>
<summary>Hint 1: Retention policy placement</summary>

```yaml
spec:
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Delete
    whenScaled: Retain
```

This goes at the StatefulSet spec level, alongside `serviceName` and `template`.

</details>

<details>
<summary>Hint 2: Verifying PVC ownership</summary>

```bash
kubectl get pvc -l app=retention-demo -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,OWNER:.metadata.ownerReferences[0].name
```

When retention is `Delete`, the PVC will have an `ownerReference` pointing to the StatefulSet or the specific pod. When `Retain`, no ownerReference is set.

</details>

## Verify What You Learned

```bash
# After scale-down to 1: PVCs for ordinals 1 and 2 should still exist
kubectl get pvc -l app=retention-demo

# After scale-up back to 3: data should be intact
for i in 0 1 2; do kubectl exec retention-demo-$i -- cat /data/identity; done

# After StatefulSet deletion: all PVCs should be gone
kubectl delete statefulset retention-demo
kubectl get pvc -l app=retention-demo
# Expected: No resources found
```

## Cleanup

```bash
kubectl delete statefulset retention-demo --ignore-not-found
kubectl delete svc retention-demo-svc --ignore-not-found
kubectl delete pvc -l app=retention-demo --ignore-not-found
```

## What's Next

Retention policies handle PVC lifecycle, but what about migrating data between storage backends? In [exercise 10 (StatefulSet Data Migration Between Storage Backends)](../10-statefulset-data-migration/10-statefulset-data-migration.md), you will tackle moving data from one StorageClass to another.

## Summary

- `persistentVolumeClaimRetentionPolicy` controls automatic PVC cleanup for StatefulSets
- `whenDeleted` governs PVC behavior when the StatefulSet is deleted
- `whenScaled` governs PVC behavior when replicas are scaled down
- The default (`Retain`/`Retain`) never deletes PVCs automatically
- PVC ownership references change based on the retention policy setting
- Choose policies carefully: `Delete` means data loss; `Retain` means manual cleanup

## Reference

- [StatefulSet PVC Retention](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#persistentvolumeclaim-retention)
- [StatefulSet Volume Claim Templates](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#volume-claim-templates)
- [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
