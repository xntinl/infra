# 10. StatefulSet Data Migration Between Storage Backends

<!--
difficulty: advanced
concepts: [storage-migration, storage-class, pvc-data-copy, statefulset-recreation, volume-snapshot]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [03-01, 03-09]
-->

## Prerequisites

- A running Kubernetes cluster with at least one StorageClass
- `kubectl` installed and configured
- Completion of exercises [01](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md) and [09](../09-pvc-retention-policy/09-pvc-retention-policy.md)
- Understanding of PVCs, PVs, and StorageClasses

## Learning Objectives

- **Analyze** the constraints that make StatefulSet storage migration non-trivial (immutable volumeClaimTemplates)
- **Evaluate** different migration strategies: copy-pod, volume snapshot, and parallel StatefulSet
- **Create** a migration plan that preserves data integrity during storage backend changes

## Architecture

StatefulSet `volumeClaimTemplates` are immutable after creation. Changing the StorageClass requires:

1. Provisioning new PVCs with the target StorageClass
2. Copying data from old PVCs to new PVCs
3. Deleting and recreating the StatefulSet (with `--cascade=orphan` to preserve running pods during planning)
4. Verifying data integrity on the new storage

Three approaches exist:

- **Copy pod**: Create a temporary pod that mounts both old and new PVCs and copies data with `cp` or `rsync`
- **Volume snapshot**: If the CSI driver supports it, snapshot the old PV and restore into a new PVC with a different StorageClass
- **Parallel StatefulSet**: Run a new StatefulSet alongside the old one, replicate data at the application level

## The Challenge

You have a 3-replica StatefulSet using the `standard` StorageClass. You need to migrate all data to PVCs using a different StorageClass (or the same class with different parameters — the process is identical).

1. Deploy the original StatefulSet with 3 replicas, each writing unique data to its PVC
2. Create new PVCs with the target StorageClass, matching the naming convention `{vct-name}-{sts-name}-{ordinal}`
3. For each ordinal, run a copy pod that mounts both the old and new PVC and copies data
4. Delete the original StatefulSet with `--cascade=orphan` to keep pods running
5. Delete old pods one at a time
6. Create the new StatefulSet pointing to the new PVCs
7. Verify data integrity on all replicas

<details>
<summary>Hint 1: Copy pod template</summary>

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: copy-data-0
spec:
  containers:
    - name: copy
      image: busybox:1.37
      command: ["sh", "-c", "cp -av /old-data/* /new-data/ && echo Done"]
      volumeMounts:
        - name: old-vol
          mountPath: /old-data
        - name: new-vol
          mountPath: /new-data
  volumes:
    - name: old-vol
      persistentVolumeClaim:
        claimName: data-old-sts-0
    - name: new-vol
      persistentVolumeClaim:
        claimName: data-new-sts-0
  restartPolicy: Never
```

</details>

<details>
<summary>Hint 2: Creating PVCs manually</summary>

Create PVCs that match the naming pattern the new StatefulSet expects:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-new-sts-0          # Must match: {vct-name}-{sts-name}-{ordinal}
  labels:
    app: new-sts
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: standard     # Target StorageClass
  resources:
    requests:
      storage: 1Gi
```

</details>

<details>
<summary>Hint 3: Orphan cascade delete</summary>

```bash
kubectl delete statefulset old-sts --cascade=orphan
```

This deletes the StatefulSet object but leaves pods and PVCs running. You can then create a new StatefulSet that adopts the existing pods (if labels match) or delete pods individually.

</details>

## Verify What You Learned

```bash
# All new pods running
kubectl get pods -l app=new-sts

# Data integrity check
for i in 0 1 2; do kubectl exec new-sts-$i -- cat /data/identity; done
# Should show the same data written by the original StatefulSet

# New PVCs bound
kubectl get pvc -l app=new-sts

# Old PVCs can be cleaned up
kubectl get pvc -l app=old-sts
```

## Cleanup

```bash
kubectl delete statefulset new-sts --ignore-not-found
kubectl delete svc old-sts-svc new-sts-svc --ignore-not-found
kubectl delete pvc -l app=old-sts --ignore-not-found
kubectl delete pvc -l app=new-sts --ignore-not-found
kubectl delete pod copy-data-0 copy-data-1 copy-data-2 --ignore-not-found
```

## What's Next

You have migrated data between storage backends for a single StatefulSet. In [exercise 11 (Deploying a Distributed Database with StatefulSets)](../11-distributed-database-statefulset/11-distributed-database-statefulset.md), you will combine everything you have learned to deploy a real distributed database.

## Summary

- StatefulSet `volumeClaimTemplates` are immutable — changing StorageClass requires recreation
- The copy-pod approach mounts both old and new PVCs to transfer data
- `kubectl delete statefulset --cascade=orphan` preserves pods and PVCs during migration
- New PVCs must follow the `{vct-name}-{sts-name}-{ordinal}` naming convention
- Always verify data integrity after migration before deleting old PVCs
- Volume snapshots offer a faster alternative when the CSI driver supports them

## Reference

- [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Volume Snapshots](https://kubernetes.io/docs/concepts/storage/volume-snapshots/)
