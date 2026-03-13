# 6. Volume Expansion and Resize

<!--
difficulty: intermediate
concepts: [volume-expansion, allowVolumeExpansion, online-resize, pvc-resize, storage-class-expansion]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [09-01, 09-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md) and [exercise 02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md)
- A CSI driver or provisioner that supports volume expansion

Verify CSI driver support:

```bash
kubectl get storageclass -o custom-columns=NAME:.metadata.name,PROVISIONER:.provisioner,EXPANSION:.allowVolumeExpansion
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** volume expansion by editing a PVC's storage request
- **Analyze** the conditions required for expansion: StorageClass configuration and CSI driver support
- **Evaluate** whether online or offline expansion is needed based on the storage backend

## Why Volume Expansion?

Applications grow. A database that started with 10Gi will eventually need 50Gi. Without volume expansion, you would need to create a new larger volume, copy data, update all references, and delete the old volume. Volume expansion lets you simply edit the PVC's storage request and Kubernetes handles the rest.

Not all storage backends support expansion. The StorageClass must have `allowVolumeExpansion: true`, and the underlying CSI driver must implement the `ControllerExpandVolume` and optionally `NodeExpandVolume` RPCs.

## Step 1: Create an Expansion-Enabled StorageClass

```yaml
# storageclass-expandable.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: expandable-storage
provisioner: k8s.io/minikube-hostpath
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true            # Required for resize
```

```bash
kubectl apply -f storageclass-expandable.yaml
```

## Step 2: Create a PVC and Write Data

```yaml
# pvc-expandable.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-expandable
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: expandable-storage
```

```yaml
# pod-expand-test.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-expand-test
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Initial data written at $(date)" > /data/before-resize.txt
          echo "=== Filesystem info ==="
          df -h /data
          sleep 3600
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: pvc-expandable
  restartPolicy: Never
```

```bash
kubectl apply -f pvc-expandable.yaml
kubectl apply -f pod-expand-test.yaml
kubectl wait --for=condition=Ready pod/pod-expand-test --timeout=60s
kubectl exec pod-expand-test -- df -h /data
```

## Step 3: Expand the Volume

Edit the PVC to request more storage:

```bash
kubectl patch pvc pvc-expandable -p '{"spec":{"resources":{"requests":{"storage":"5Gi"}}}}'
```

Monitor the expansion:

```bash
kubectl get pvc pvc-expandable
kubectl describe pvc pvc-expandable
```

Look for the `FileSystemResizePending` condition, which indicates the controller has expanded the volume but the filesystem resize has not yet occurred. With some CSI drivers, the filesystem resize happens online (while the pod is running). Others require the pod to be restarted.

## Step 4: Verify Expansion and Data Integrity

```bash
kubectl exec pod-expand-test -- df -h /data
kubectl exec pod-expand-test -- cat /data/before-resize.txt
```

The filesystem should show the larger size, and the original data should be intact.

## Step 5: Attempt Expansion on a Non-Expandable StorageClass

```yaml
# storageclass-no-expand.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: no-expand-storage
provisioner: k8s.io/minikube-hostpath
reclaimPolicy: Delete
allowVolumeExpansion: false
```

```yaml
# pvc-no-expand.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-no-expand
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: no-expand-storage
```

```bash
kubectl apply -f storageclass-no-expand.yaml
kubectl apply -f pvc-no-expand.yaml
kubectl patch pvc pvc-no-expand -p '{"spec":{"resources":{"requests":{"storage":"5Gi"}}}}'
# Error: PVC does not support volume expansion
```

## Spot the Bug

This PVC expansion request fails silently. **Why does the volume size not increase?**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-shrink-attempt
spec:
  resources:
    requests:
      storage: 500Mi    # <-- BUG: smaller than current size
  storageClassName: expandable-storage
```

<details>
<summary>Explanation</summary>

Volume shrinking is not supported. You can only increase the storage request. Kubernetes silently ignores or rejects requests to reduce PVC size. Always expand, never shrink.

</details>

## Verify What You Learned

```bash
kubectl get pvc pvc-expandable -o jsonpath='{.spec.resources.requests.storage}'
kubectl get pvc pvc-expandable -o jsonpath='{.status.capacity.storage}'
kubectl get storageclass expandable-storage -o jsonpath='{.allowVolumeExpansion}'
```

## Cleanup

```bash
kubectl delete pod pod-expand-test --ignore-not-found
kubectl delete pvc pvc-expandable pvc-no-expand --ignore-not-found
kubectl delete storageclass expandable-storage no-expand-storage
```

## What's Next

Now that you can resize volumes, the next exercise covers creating point-in-time snapshots for backup and cloning. Continue to [exercise 07 (Volume Snapshots and Restore)](../07-volume-snapshots/07-volume-snapshots.md).

## Summary

- Volume expansion requires `allowVolumeExpansion: true` on the StorageClass and CSI driver support
- Expand a PVC by patching `spec.resources.requests.storage` to a larger value
- **Online expansion** resizes the filesystem while the pod is running; some drivers require pod restart
- Volume **shrinking is not supported**; you can only increase the size
- Always verify data integrity after expansion

## Reference

- [Expanding Persistent Volumes Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims)
- [Storage Classes - Allow Volume Expansion](https://kubernetes.io/docs/concepts/storage/storage-classes/#allow-volume-expansion)
