# 9. Volume Cloning and Data Migration

<!--
difficulty: advanced
concepts: [volume-cloning, pvc-datasource, data-migration, cross-storageclass, rsync]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [09-01, 09-02, 09-07]
-->

## Prerequisites

- A running Kubernetes cluster with a CSI driver that supports cloning (minikube with `csi-hostpath-driver` addon)
- `kubectl` installed and configured
- Completion of exercises [01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md), [02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md), and [07](../07-volume-snapshots/07-volume-snapshots.md)

Enable CSI support on minikube:

```bash
minikube addons enable csi-hostpath-driver
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** the difference between volume cloning (PVC-to-PVC) and snapshot restore
- **Create** volume clones using the PVC `dataSource` field
- **Design** data migration strategies between StorageClasses using helper pods

## Architecture

Volume cloning creates a new PVC pre-populated with data from an existing PVC. Unlike snapshots, cloning is a direct copy operation without an intermediate snapshot object.

```
Source PVC (csi-hostpath-sc)          Clone PVC (csi-hostpath-sc)
+-------------------------+          +-------------------------+
| /data/app/metrics.json  |  clone   | /data/app/metrics.json  |
| /data/config/app.conf   | ------> | /data/config/app.conf   |
| /data/logs/app.log      |          | /data/logs/app.log      |
+-------------------------+          +-------------------------+

For cross-StorageClass migration (cloning not supported):
Source PVC (sc-a)       Helper Pod         Target PVC (sc-b)
+---------------+      +----------+       +---------------+
| /source/data  | ---> | rsync/cp | ----> | /target/data  |
+---------------+      +----------+       +---------------+
```

## The Challenge

### Task 1: Clone a Volume Using PVC dataSource

Create a source PVC with data, then clone it to a new PVC:

```yaml
# storageclass-csi.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-hostpath-sc
provisioner: hostpath.csi.k8s.io
reclaimPolicy: Delete
volumeBindingMode: Immediate
```

```yaml
# pvc-clone-source.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-clone-source
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-hostpath-sc
```

Populate the source with data, then create the clone:

```yaml
# pvc-clone-target.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-clone-target
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-hostpath-sc
  dataSource:
    name: pvc-clone-source            # Source PVC name
    kind: PersistentVolumeClaim       # Note: kind is PVC, not VolumeSnapshot
```

### Task 2: Verify Clone Independence

Write new data to the clone and verify it does not affect the source. Write new data to the source and verify the clone is unaffected. This proves they are independent copies.

### Task 3: Cross-StorageClass Data Migration

Clone only works within the same StorageClass and CSI driver. For cross-StorageClass migration, use a helper pod that mounts both PVCs:

```yaml
# pod-migrate.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-migrate
spec:
  containers:
    - name: migrator
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Migrating data..."
          cp -av /source/. /target/
          echo "Migration complete."
          echo "=== Source ==="
          find /source -type f
          echo "=== Target ==="
          find /target -type f
          sleep 30
      volumeMounts:
        - name: source
          mountPath: /source
          readOnly: true
        - name: target
          mountPath: /target
  volumes:
    - name: source
      persistentVolumeClaim:
        claimName: pvc-clone-source
    - name: target
      persistentVolumeClaim:
        claimName: pvc-migration-target
  restartPolicy: Never
```

### Task 4: Verify Migration Integrity

Create a verification pod that mounts the migration target and confirms all files match the source.

## Suggested Steps

1. Create the CSI StorageClass and source PVC
2. Populate the source PVC with test data using a helper pod
3. Create the clone PVC using `dataSource`
4. Verify the clone contains identical data
5. Write to the clone and verify the source is unaffected
6. Create a second StorageClass and PVC for cross-class migration
7. Run the migration pod and verify data integrity

## Verify What You Learned

```bash
kubectl get pvc -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,SC:.spec.storageClassName
kubectl exec <clone-pod> -- cat /data/app/metrics.json
kubectl exec <source-pod> -- cat /data/app/metrics.json
# Both should show the same content
```

## Cleanup

```bash
kubectl delete pod --all --ignore-not-found
kubectl delete pvc pvc-clone-source pvc-clone-target pvc-migration-target --ignore-not-found
kubectl delete storageclass csi-hostpath-sc --ignore-not-found
```

## What's Next

Now that you can clone and migrate volumes, the next exercise covers shared storage patterns with ReadWriteMany access. Continue to [exercise 10 (ReadWriteMany Shared Storage Patterns)](../10-rwx-shared-storage/10-rwx-shared-storage.md).

## Summary

- **Volume cloning** creates a new PVC from an existing PVC using `dataSource` with `kind: PersistentVolumeClaim`
- Cloning is a direct copy; no intermediate snapshot object is needed
- Clones are **independent** -- changes to one do not affect the other
- Cloning works only within the **same StorageClass and CSI driver**
- **Cross-StorageClass migration** requires a helper pod that mounts both PVCs and copies data

## Reference

- [CSI Volume Cloning](https://kubernetes.io/docs/concepts/storage/volume-pvc-datasource/)
- [Volume Snapshots](https://kubernetes.io/docs/concepts/storage/volume-snapshots/)
