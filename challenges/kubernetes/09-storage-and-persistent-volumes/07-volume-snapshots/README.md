# 7. Volume Snapshots and Restore

<!--
difficulty: intermediate
concepts: [volume-snapshot, volume-snapshot-class, volume-snapshot-content, csi-snapshots, data-restore]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: apply
prerequisites: [09-01, 09-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-persistent-volumes-and-claims/) and [exercise 02](../02-storage-classes-dynamic-provisioning/)
- A CSI driver that supports snapshots

Enable snapshot support on minikube:

```bash
minikube addons enable volumesnapshots
minikube addons enable csi-hostpath-driver

kubectl get crd | grep volumesnapshot
# Should show: volumesnapshotclasses, volumesnapshotcontents, volumesnapshots
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** VolumeSnapshot and VolumeSnapshotClass resources to capture point-in-time volume state
- **Analyze** the relationship between VolumeSnapshot, VolumeSnapshotContent, and VolumeSnapshotClass
- **Create** new PVCs from snapshots to restore data or clone volumes

## Why Volume Snapshots?

Backups are essential for any stateful application. Volume snapshots capture the state of a PVC at a specific point in time without stopping the application. You can restore from a snapshot by creating a new PVC with the snapshot as its data source. This enables backup, disaster recovery, and creating test environments from production data.

## Step 1: Create the VolumeSnapshotClass

```yaml
# volumesnapshotclass.yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: csi-hostpath-snapclass
driver: hostpath.csi.k8s.io           # Must match the CSI driver
deletionPolicy: Delete                 # Delete snapshot when VolumeSnapshot is deleted
```

```bash
kubectl apply -f volumesnapshotclass.yaml
```

## Step 2: Create a CSI StorageClass and PVC with Data

```yaml
# storageclass-csi.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-hostpath-sc
provisioner: hostpath.csi.k8s.io
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
```

```yaml
# pvc-source.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-source-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-hostpath-sc
```

```yaml
# pod-populate.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-populate-data
spec:
  containers:
    - name: writer
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          mkdir -p /data/app /data/config
          echo "database_url=postgresql://db:5432/myapp" > /data/config/app.conf
          echo '{"users": 150, "orders": 4230}' > /data/app/metrics.json
          echo '{"version": "2.1.0", "build": "abc123"}' > /data/app/version.json
          echo "Data populated at $(date)"
          find /data -type f -exec echo "  {}" \;
          sleep 3600
      volumeMounts:
        - name: source-data
          mountPath: /data
  volumes:
    - name: source-data
      persistentVolumeClaim:
        claimName: pvc-source-data
  restartPolicy: Never
```

```bash
kubectl apply -f storageclass-csi.yaml
kubectl apply -f pvc-source.yaml
kubectl apply -f pod-populate.yaml
kubectl wait --for=condition=Ready pod/pod-populate-data --timeout=120s
kubectl exec pod-populate-data -- cat /data/app/metrics.json
```

## Step 3: Create a VolumeSnapshot

```yaml
# volumesnapshot.yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: snapshot-source-data
spec:
  volumeSnapshotClassName: csi-hostpath-snapclass
  source:
    persistentVolumeClaimName: pvc-source-data    # The PVC to snapshot
```

```bash
kubectl apply -f volumesnapshot.yaml
kubectl wait --for=jsonpath='{.status.readyToUse}'=true volumesnapshot/snapshot-source-data --timeout=120s
kubectl get volumesnapshot
kubectl describe volumesnapshot snapshot-source-data
```

## Step 4: Restore Data from Snapshot

Create a new PVC using the snapshot as a data source:

```yaml
# pvc-restored.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-restored-from-snapshot
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-hostpath-sc
  dataSource:
    name: snapshot-source-data         # Reference the VolumeSnapshot
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
```

```yaml
# pod-verify-restore.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-verify-restore
spec:
  containers:
    - name: reader
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Restored data ==="
          cat /data/config/app.conf
          cat /data/app/metrics.json
          cat /data/app/version.json
          find /data -type f
          sleep 3600
      volumeMounts:
        - name: restored-data
          mountPath: /data
  volumes:
    - name: restored-data
      persistentVolumeClaim:
        claimName: pvc-restored-from-snapshot
  restartPolicy: Never
```

```bash
kubectl apply -f pvc-restored.yaml
kubectl apply -f pod-verify-restore.yaml
kubectl wait --for=condition=Ready pod/pod-verify-restore --timeout=120s
kubectl exec pod-verify-restore -- cat /data/app/metrics.json
```

The restored PVC contains the exact same data as the original at the time of the snapshot.

## Step 5: Verify the VolumeSnapshotContent

```bash
kubectl get volumesnapshotcontent
kubectl describe volumesnapshotcontent $(kubectl get volumesnapshot snapshot-source-data -o jsonpath='{.status.boundVolumeSnapshotContentName}')
```

The VolumeSnapshotContent is the cluster-scoped object that represents the actual snapshot on the storage backend, similar to how a PV represents the actual volume.

## Spot the Bug

This PVC restore fails. **Why?**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-wrong-restore
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 512Mi              # <-- BUG
  storageClassName: csi-hostpath-sc
  dataSource:
    name: snapshot-source-data
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
```

<details>
<summary>Explanation</summary>

The restored PVC requests `512Mi` but the snapshot was taken from a `1Gi` volume. The restored PVC must be at least as large as the original. Most CSI drivers reject restore requests where the target size is smaller than the snapshot source.

</details>

## Verify What You Learned

```bash
kubectl get volumesnapshot snapshot-source-data -o jsonpath='{.status.readyToUse}'
kubectl exec pod-verify-restore -- cat /data/config/app.conf
kubectl exec pod-populate-data -- cat /data/config/app.conf
```

Both pods should show identical data.

## Cleanup

```bash
kubectl delete pod pod-populate-data pod-verify-restore --ignore-not-found
kubectl delete pvc pvc-restored-from-snapshot pvc-source-data --ignore-not-found
kubectl delete volumesnapshot snapshot-source-data --ignore-not-found
kubectl delete volumesnapshotclass csi-hostpath-snapclass
kubectl delete storageclass csi-hostpath-sc
```

## What's Next

Now that you can snapshot and restore volumes, the next exercise covers CSI drivers and how to use different storage backends. Continue to [exercise 08 (CSI Drivers: EBS, EFS, Local Path)](../08-csi-drivers/).

## Summary

- **VolumeSnapshot** captures the state of a PVC at a point in time without stopping the workload
- **VolumeSnapshotClass** defines the CSI driver and deletion policy for snapshots
- **VolumeSnapshotContent** is the cluster-scoped resource representing the actual snapshot on the backend
- Restore data by creating a new PVC with `dataSource` pointing to a VolumeSnapshot
- The restored PVC must be at least as large as the original snapshot source

## Reference

- [Volume Snapshots](https://kubernetes.io/docs/concepts/storage/volume-snapshots/)
- [CSI Volume Cloning](https://kubernetes.io/docs/concepts/storage/volume-pvc-datasource/)
- [Kubernetes CSI Developer Documentation](https://kubernetes-csi.github.io/docs/)
