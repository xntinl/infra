# 10. ReadWriteMany Shared Storage Patterns

<!--
difficulty: advanced
concepts: [rwx, nfs, shared-storage, multi-pod-write, efs, cephfs]
tools: [kubectl, minikube, helm]
estimated_time: 50m
bloom_level: analyze
prerequisites: [09-01, 09-02, 09-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of exercises [01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md), [02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md), and [05](../05-pvc-access-modes-and-binding/05-pvc-access-modes-and-binding.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** when ReadWriteMany storage is required versus ReadWriteOnce
- **Create** an NFS-based shared storage solution for multi-pod write access
- **Evaluate** the tradeoffs of different RWX storage backends (NFS, EFS, CephFS)

## Architecture

ReadWriteMany (RWX) storage allows multiple pods across different nodes to mount the same volume for concurrent read-write access. This is essential for workloads like shared file uploads, CMS content directories, and distributed build caches.

```
Node A                    Node B                    Node C
+------------------+     +------------------+     +------------------+
| Pod: web-0       |     | Pod: web-1       |     | Pod: web-2       |
| Mount: /uploads  |     | Mount: /uploads  |     | Mount: /uploads  |
+--------+---------+     +--------+---------+     +--------+---------+
         |                        |                        |
         +------------------------+------------------------+
                                  |
                     +------------+------------+
                     |   RWX Volume (NFS/EFS)  |
                     |   /shared/uploads       |
                     +-------------------------+
```

## The Challenge

### Task 1: Deploy an In-Cluster NFS Server

For development purposes, deploy a simple NFS server inside the cluster:

```yaml
# nfs-server.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nfs-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nfs-server
  template:
    metadata:
      labels:
        app: nfs-server
    spec:
      containers:
        - name: nfs
          image: itsthenetwork/nfs-server-alpine:12
          ports:
            - containerPort: 2049
          securityContext:
            privileged: true
          env:
            - name: SHARED_DIRECTORY
              value: /exports
          volumeMounts:
            - name: nfs-storage
              mountPath: /exports
      volumes:
        - name: nfs-storage
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: nfs-server
spec:
  selector:
    app: nfs-server
  ports:
    - port: 2049
      targetPort: 2049
  clusterIP: None
```

### Task 2: Create a PV and PVC with RWX Access

Create a PV backed by the NFS server and a PVC that requests RWX access:

```yaml
# pv-nfs.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-nfs-shared
spec:
  capacity:
    storage: 5Gi
  accessModes:
    - ReadWriteMany
  nfs:
    server: nfs-server.default.svc.cluster.local
    path: /exports
  persistentVolumeReclaimPolicy: Retain
  storageClassName: nfs
```

```yaml
# pvc-rwx.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-shared-uploads
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 5Gi
  storageClassName: nfs
```

### Task 3: Deploy Multiple Writer Pods

Create a Deployment with 3 replicas that all write to the shared volume:

```yaml
# deployment-writers.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shared-writers
spec:
  replicas: 3
  selector:
    matchLabels:
      app: shared-writers
  template:
    metadata:
      labels:
        app: shared-writers
    spec:
      containers:
        - name: writer
          image: busybox:1.37
          command:
            - sh
            - -c
            - |
              HOSTNAME=$(hostname)
              while true; do
                echo "$HOSTNAME: $(date)" >> /uploads/activity.log
                echo "heartbeat" > /uploads/$HOSTNAME.txt
                sleep 5
              done
          volumeMounts:
            - name: shared
              mountPath: /uploads
      volumes:
        - name: shared
          persistentVolumeClaim:
            claimName: pvc-shared-uploads
```

### Task 4: Verify Concurrent Writes

Exec into any pod and verify that all pods are writing to the same volume:

```bash
kubectl exec <any-writer-pod> -- cat /uploads/activity.log
kubectl exec <any-writer-pod> -- ls /uploads/
```

You should see entries from all three hostnames interleaved in the log.

### Task 5: Compare RWX Backends

Document the tradeoffs:

| Feature | NFS | EFS | CephFS |
|---------|-----|-----|--------|
| Cloud | Any | AWS | Any |
| Performance | Moderate | Elastic | High |
| Max IOPS | Limited | Bursting | High |
| Setup Complexity | Low | Low (managed) | High |
| Cost Model | Self-managed | Pay-per-use | Self-managed |

## Suggested Steps

1. Deploy the NFS server and verify it is running
2. Create the NFS-backed PV and RWX PVC
3. Deploy the writer Deployment with 3 replicas
4. Verify all pods can read and write to the shared volume concurrently
5. Scale the Deployment to 5 replicas and verify new pods join seamlessly
6. Test what happens when one writer pod is deleted (data persists, new pod picks up)

## Verify What You Learned

```bash
kubectl get pvc pvc-shared-uploads -o jsonpath='{.spec.accessModes}'
kubectl get pods -l app=shared-writers
kubectl exec $(kubectl get pod -l app=shared-writers -o name | head -1) -- cat /uploads/activity.log | tail -10
kubectl exec $(kubectl get pod -l app=shared-writers -o name | head -1) -- ls /uploads/
```

## Cleanup

```bash
kubectl delete deployment shared-writers nfs-server --ignore-not-found
kubectl delete service nfs-server --ignore-not-found
kubectl delete pvc pvc-shared-uploads --ignore-not-found
kubectl delete pv pv-nfs-shared --ignore-not-found
```

## What's Next

Now that you understand shared storage, the next exercise covers full namespace backup and restore using Velero. Continue to [exercise 11 (Namespace Backup and Restore with Velero)](../11-velero-backup-restore/11-velero-backup-restore.md).

## Summary

- **ReadWriteMany (RWX)** allows concurrent read-write from multiple nodes -- required for shared file access
- **NFS** is the most common RWX solution for on-premises and development clusters
- **EFS** (AWS) and **CephFS** provide managed RWX in cloud and distributed environments
- Multiple pods can write to the same volume simultaneously; applications must handle file locking
- RWX performance varies significantly by backend; NFS is simpler but slower than CephFS

## Reference

- [Persistent Volumes - Access Modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes)
- [NFS Volume](https://kubernetes.io/docs/concepts/storage/volumes/#nfs)
- [AWS EFS CSI Driver](https://github.com/kubernetes-sigs/aws-efs-csi-driver)
