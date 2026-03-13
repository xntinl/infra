# 5. PVC Access Modes and Volume Binding

<!--
difficulty: intermediate
concepts: [access-modes, rwo, rox, rwx, volume-binding, pvc-selector, storage-capacity]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [09-01, 09-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md) and [exercise 02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** different PVC access modes and understand their effect on pod scheduling
- **Analyze** why a PVC stays `Pending` by inspecting binding requirements and PV compatibility
- **Evaluate** the tradeoffs between RWO, ROX, and RWX access modes for different workload patterns

## Why Access Modes Matter

Access modes determine how many nodes can mount a volume and whether they can write to it. Choosing the wrong mode causes pods to hang in `Pending` or `ContainerCreating` with no obvious error.

- **ReadWriteOnce (RWO)**: One node mounts read-write. Multiple pods on the same node can use it, but pods on other nodes cannot.
- **ReadOnlyMany (ROX)**: Many nodes mount read-only. Good for shared configuration or static assets.
- **ReadWriteMany (RWX)**: Many nodes mount read-write. Required when multiple pods across nodes need concurrent write access. Only certain storage backends (NFS, EFS, CephFS) support this.

## Step 1: Create PVs with Different Access Modes

```yaml
# pv-rwo.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-rwo
  labels:
    mode: rwo
spec:
  capacity:
    storage: 1Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: manual
  hostPath:
    path: /tmp/k8s-rwo
    type: DirectoryOrCreate
```

```yaml
# pv-rox.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-rox
  labels:
    mode: rox
spec:
  capacity:
    storage: 512Mi
  accessModes:
    - ReadOnlyMany
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: manual
  hostPath:
    path: /tmp/k8s-rox
    type: DirectoryOrCreate
```

```bash
kubectl apply -f pv-rwo.yaml
kubectl apply -f pv-rox.yaml
kubectl get pv
```

## Step 2: PVC Binding by Access Mode

```yaml
# pvc-rwo.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-rwo
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 500Mi
  storageClassName: manual
  selector:
    matchLabels:
      mode: rwo
```

```yaml
# pvc-rox.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-rox
spec:
  accessModes:
    - ReadOnlyMany
  resources:
    requests:
      storage: 256Mi
  storageClassName: manual
  selector:
    matchLabels:
      mode: rox
```

```bash
kubectl apply -f pvc-rwo.yaml
kubectl apply -f pvc-rox.yaml
kubectl get pvc
kubectl get pv
```

Both PVCs should be `Bound`. The RWO PVC binds to `pv-rwo` and the ROX PVC binds to `pv-rox`.

## Step 3: Test RWO Behavior

Create two pods that use the same RWO PVC:

```yaml
# pod-rwo-writer.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-rwo-writer
spec:
  containers:
    - name: writer
      image: busybox:1.37
      command: ["sh", "-c", "echo 'written by pod-rwo-writer' > /data/test.txt && sleep 3600"]
      volumeMounts:
        - name: rwo-vol
          mountPath: /data
  volumes:
    - name: rwo-vol
      persistentVolumeClaim:
        claimName: pvc-rwo
  restartPolicy: Never
```

```yaml
# pod-rwo-reader.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-rwo-reader
spec:
  containers:
    - name: reader
      image: busybox:1.37
      command: ["sh", "-c", "cat /data/test.txt && sleep 3600"]
      volumeMounts:
        - name: rwo-vol
          mountPath: /data
  volumes:
    - name: rwo-vol
      persistentVolumeClaim:
        claimName: pvc-rwo
  restartPolicy: Never
```

```bash
kubectl apply -f pod-rwo-writer.yaml
kubectl wait --for=condition=Ready pod/pod-rwo-writer --timeout=60s
kubectl apply -f pod-rwo-reader.yaml
kubectl wait --for=condition=Ready pod/pod-rwo-reader --timeout=60s
```

On a single-node cluster, both pods run because RWO means one **node**, not one pod. On a multi-node cluster, the reader would be stuck if scheduled on a different node.

## Step 4: Test ROX Behavior

Write data to the ROX-capable PV, then mount it read-only from multiple pods:

```yaml
# pod-rox-seed.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-rox-seed
spec:
  containers:
    - name: seeder
      image: busybox:1.37
      command: ["sh", "-c", "echo 'shared config data' > /data/config.txt && sleep 10"]
      volumeMounts:
        - name: shared
          mountPath: /data
  volumes:
    - name: shared
      persistentVolumeClaim:
        claimName: pvc-rox
  restartPolicy: Never
```

```bash
kubectl apply -f pod-rox-seed.yaml
kubectl wait --for=condition=Ready pod/pod-rox-seed --timeout=60s
sleep 12
kubectl delete pod pod-rox-seed
```

## Step 5: Diagnose a Pending PVC

Create a PVC that cannot bind and investigate why:

```yaml
# pvc-unbound.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-unbound
spec:
  accessModes:
    - ReadWriteMany              # No PV supports RWX with hostPath
  resources:
    requests:
      storage: 10Gi             # Larger than any available PV
  storageClassName: manual
```

```bash
kubectl apply -f pvc-unbound.yaml
kubectl get pvc pvc-unbound      # Pending
kubectl describe pvc pvc-unbound
```

The `Events` section tells you why: no PV matches the requested access mode and capacity.

## Verify What You Learned

```bash
kubectl get pv -o custom-columns=NAME:.metadata.name,ACCESS:.spec.accessModes,STATUS:.status.phase,CLAIM:.spec.claimRef.name
kubectl get pvc -o custom-columns=NAME:.metadata.name,ACCESS:.spec.accessModes,STATUS:.status.phase,VOLUME:.spec.volumeName
kubectl describe pvc pvc-unbound | grep -A 3 "Events"
```

## Cleanup

```bash
kubectl delete pod pod-rwo-writer pod-rwo-reader pod-rox-seed --ignore-not-found
kubectl delete pvc pvc-rwo pvc-rox pvc-unbound --ignore-not-found
kubectl delete pv pv-rwo pv-rox
```

## What's Next

Now that you understand access modes and binding, the next exercise covers expanding volumes after they have been created. Continue to [exercise 06 (Volume Expansion and Resize)](../06-volume-expansion/06-volume-expansion.md).

## Summary

- **ReadWriteOnce (RWO)** allows one node to mount read-write; multiple pods on the same node can share it
- **ReadOnlyMany (ROX)** allows many nodes to mount read-only; useful for shared static data
- **ReadWriteMany (RWX)** allows concurrent read-write from multiple nodes; requires specific storage backends
- A PVC stays **Pending** when no PV matches its access mode, capacity, or StorageClass requirements

## Reference

- [Persistent Volumes - Access Modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes)
- [Persistent Volume Claims](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims)
