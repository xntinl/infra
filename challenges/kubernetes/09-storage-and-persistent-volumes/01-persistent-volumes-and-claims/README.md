# 1. Persistent Volumes and Claims

<!--
difficulty: basic
concepts: [persistent-volume, persistent-volume-claim, hostPath, reclaim-policy, access-modes, volume-lifecycle]
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

- **Remember** the purpose of PersistentVolumes and PersistentVolumeClaims as Kubernetes storage abstractions
- **Understand** the lifecycle phases of PVs (Available, Bound, Released) and how PVCs bind to PVs
- **Apply** kubectl commands to create PVs and PVCs, mount them in pods, and verify data persistence

## Why Persistent Volumes?

Containers are ephemeral by design. When a pod is deleted, its filesystem is destroyed along with it. This is fine for stateless workloads, but any application that needs to keep data between restarts, like a database, a file upload service, or a log aggregator, needs storage that outlives the pod.

Kubernetes solves this with two abstractions. A PersistentVolume (PV) represents a piece of storage in the cluster, provisioned by an administrator or dynamically by a StorageClass. A PersistentVolumeClaim (PVC) is a request for storage by a user. The separation exists so that cluster operators can manage storage infrastructure independently from the developers who consume it. When a PVC is created, Kubernetes finds a matching PV based on capacity, access modes, and StorageClass, then binds them together.

PVs have a lifecycle independent of any pod. When a pod is deleted, the PVC and PV remain. The reclaim policy on the PV determines what happens when the PVC is eventually deleted: `Retain` keeps the data for manual cleanup, `Delete` removes the underlying storage, and the deprecated `Recycle` performs a basic scrub. Understanding this lifecycle is fundamental to managing stateful workloads in Kubernetes.

## Step 1: Create a PersistentVolume with hostPath

A `hostPath` PV mounts a directory from the node's filesystem. This is suitable for single-node development clusters like minikube but not for production.

Create `pv-hostpath.yaml`:

```yaml
# pv-hostpath.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-data                     # PVs are cluster-scoped, no namespace
  labels:
    type: local                     # Labels help PVCs select specific PVs
    environment: lab
spec:
  capacity:
    storage: 1Gi                    # How much storage this PV provides
  accessModes:
    - ReadWriteOnce                 # RWO: one node can mount read-write
  persistentVolumeReclaimPolicy: Retain   # Keep data after PVC is deleted
  storageClassName: manual          # Must match the PVC's storageClassName
  hostPath:
    path: /tmp/k8s-pv-data          # Directory on the host node
    type: DirectoryOrCreate          # Create the directory if it does not exist
```

Apply and inspect:

```bash
kubectl apply -f pv-hostpath.yaml
kubectl get pv
```

The PV should show status `Available` — it exists but no PVC has claimed it yet.

## Step 2: Create a PersistentVolumeClaim

A PVC requests storage with specific requirements. Kubernetes will bind it to a matching PV automatically.

Create `pvc-data.yaml`:

```yaml
# pvc-data.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-data                    # PVCs are namespaced
  namespace: default
spec:
  accessModes:
    - ReadWriteOnce                 # Must match the PV's access mode
  resources:
    requests:
      storage: 500Mi               # Can be less than PV capacity; PV must be >=
  storageClassName: manual          # Must match the PV's storageClassName
  selector:
    matchLabels:
      type: local                   # Only bind to PVs with this label
      environment: lab
```

Apply and check binding:

```bash
kubectl apply -f pvc-data.yaml
kubectl get pv
kubectl get pvc
```

The PV status should change from `Available` to `Bound`, and the PVC should show `Bound` with the PV name.

## Step 3: Mount the PVC in a Pod

Create a pod that writes data to the persistent volume.

Create `pod-writer.yaml`:

```yaml
# pod-writer.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-writer
  namespace: default
spec:
  containers:
    - name: writer
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Created: $(date)" > /data/info.txt
          echo "Hostname: $(hostname)" >> /data/info.txt
          for i in 1 2 3 4 5; do
            echo "Entry $i: $(date)" >> /data/log.txt
            sleep 2
          done
          echo "Data written successfully."
          cat /data/info.txt
          cat /data/log.txt
          sleep 3600
      volumeMounts:
        - name: persistent-data      # Matches the volume name below
          mountPath: /data            # Where the volume appears in the container
  volumes:
    - name: persistent-data
      persistentVolumeClaim:
        claimName: pvc-data           # References the PVC by name
  restartPolicy: Never
```

Apply and wait for the pod:

```bash
kubectl apply -f pod-writer.yaml
kubectl wait --for=condition=Ready pod/pod-writer --timeout=60s
kubectl exec pod-writer -- cat /data/info.txt
```

## Step 4: Verify Data Persists After Pod Deletion

Delete the writer pod and create a reader pod that uses the same PVC:

```bash
kubectl delete pod pod-writer
kubectl get pv   # Still Bound
kubectl get pvc  # Still Bound
```

Create `pod-reader.yaml`:

```yaml
# pod-reader.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-reader
  namespace: default
spec:
  containers:
    - name: reader
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Reading persistent data ==="
          if [ -f /data/info.txt ]; then
            cat /data/info.txt
          else
            echo "info.txt not found!"
          fi
          if [ -f /data/log.txt ]; then
            cat /data/log.txt
          else
            echo "log.txt not found!"
          fi
          ls -la /data/
          sleep 3600
      volumeMounts:
        - name: persistent-data
          mountPath: /data
  volumes:
    - name: persistent-data
      persistentVolumeClaim:
        claimName: pvc-data
  restartPolicy: Never
```

```bash
kubectl apply -f pod-reader.yaml
kubectl wait --for=condition=Ready pod/pod-reader --timeout=60s
kubectl exec pod-reader -- cat /data/info.txt
kubectl exec pod-reader -- cat /data/log.txt
```

The data written by the first pod is still there. This is the core value of persistent volumes.

## Step 5: Observe the Released State

When you delete a PVC bound to a PV with `Retain` policy, the PV moves to `Released`:

```bash
kubectl delete pod pod-reader
kubectl delete pvc pvc-data
kubectl get pv pv-data
```

The PV shows `Released`. It cannot be bound to a new PVC until you clear its `claimRef`:

```bash
kubectl patch pv pv-data -p '{"spec":{"claimRef":null}}'
kubectl get pv pv-data
```

Now it returns to `Available` and can be claimed again.

## Common Mistakes

### Mistake 1: StorageClassName Mismatch

If the PVC specifies `storageClassName: fast` but the PV has `storageClassName: manual`, they will never bind. The PVC stays `Pending` forever with no obvious error message.

**Fix:** Always verify that storageClassName matches between PV and PVC.

### Mistake 2: Requesting More Storage Than Available

A PVC requesting `2Gi` will not bind to a PV with `capacity: 1Gi`. The PV must have capacity greater than or equal to the request.

### Mistake 3: Confusing Reclaim Policies

With `Delete` policy, deleting the PVC also deletes the PV and its backing storage. Use `Retain` when data must survive PVC deletion.

## Verify What You Learned

Confirm the PV lifecycle:

```bash
kubectl apply -f pv-hostpath.yaml
kubectl get pv pv-data          # Available
kubectl apply -f pvc-data.yaml
kubectl get pv pv-data          # Bound
kubectl delete pvc pvc-data
kubectl get pv pv-data          # Released
```

Confirm access modes:

```bash
kubectl describe pv pv-data | grep "Access Modes"
```

## Cleanup

```bash
kubectl delete pod pod-writer pod-reader --ignore-not-found
kubectl delete pvc pvc-data --ignore-not-found
kubectl delete pv pv-data
```

## What's Next

Now that you understand manual PV provisioning, the next exercise covers StorageClasses and dynamic provisioning, where Kubernetes creates PVs automatically. Continue to [exercise 02 (Storage Classes and Dynamic Provisioning)](../02-storage-classes-dynamic-provisioning/).

## Summary

- A **PersistentVolume** (PV) is a cluster-scoped storage resource, provisioned by an admin or dynamically by a StorageClass.
- A **PersistentVolumeClaim** (PVC) is a namespaced request for storage that binds to a matching PV.
- PVs pass through the lifecycle phases: **Available**, **Bound**, and **Released**.
- The **reclaim policy** (`Retain`, `Delete`) controls what happens to the PV when the PVC is deleted.
- **Access modes** (RWO, ROX, RWX) define how many nodes can mount the volume simultaneously.
- Data persists across pod deletions as long as the PVC and PV remain.

## Reference

- [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) -- official concept documentation
- [Configure a Pod to Use a PersistentVolume for Storage](https://kubernetes.io/docs/tasks/configure-pod-container/configure-persistent-volume-storage/) -- step-by-step task guide

## Additional Resources

- [Volumes](https://kubernetes.io/docs/concepts/storage/volumes/) -- overview of all volume types
- [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/) -- dynamic provisioning
- [Kubernetes API Reference: PersistentVolume](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/persistent-volume-v1/)
