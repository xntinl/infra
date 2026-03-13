# 8. CSI Drivers: EBS, EFS, Local Path

<!--
difficulty: advanced
concepts: [csi-driver, ebs-csi, efs-csi, local-path-provisioner, csi-node-info, storage-topology]
tools: [kubectl, minikube, helm]
estimated_time: 50m
bloom_level: analyze
prerequisites: [09-01, 09-02, 09-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of exercises [01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md), [02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md), and [07](../07-volume-snapshots/07-volume-snapshots.md)
- `helm` installed (for CSI driver installation)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** the CSI architecture and how drivers plug into Kubernetes storage
- **Evaluate** which CSI driver is appropriate for different workload requirements
- **Create** StorageClasses backed by different CSI drivers and verify their capabilities

## Architecture

The Container Storage Interface (CSI) is the standard mechanism for exposing block and file storage to Kubernetes. It replaced the in-tree volume plugins that were compiled directly into Kubernetes binaries.

```
+-------------------+     +-------------------+     +-------------------+
|   Kubernetes      |     |   CSI Controller  |     |   Storage Backend |
|   API Server      |---->|   Plugin          |---->|   (EBS, EFS,      |
|                   |     |   (Deployment)    |     |    Local Disk)    |
+-------------------+     +-------------------+     +-------------------+
                                                           ^
+-------------------+     +-------------------+            |
|   kubelet         |---->|   CSI Node Plugin  |-----------+
|   (per node)      |     |   (DaemonSet)     |
+-------------------+     +-------------------+
```

A CSI driver has two components:
- **Controller plugin** (Deployment): handles volume creation, deletion, snapshots, and expansion
- **Node plugin** (DaemonSet): handles mounting/unmounting volumes on nodes

## The Challenge

Explore CSI driver capabilities on your cluster. Complete these tasks:

### Task 1: Inspect Available CSI Drivers

```bash
kubectl get csidrivers
kubectl describe csidriver <driver-name>
kubectl get csinodes
```

Examine what capabilities each driver advertises (volume expansion, snapshots, cloning).

### Task 2: Install Local Path Provisioner (kind/k3d)

If using kind or a cluster without a default provisioner, install Rancher's local-path-provisioner:

```bash
kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.26/deploy/local-path-storage.yaml
kubectl get storageclass local-path
```

Create a StorageClass and PVC that uses it:

```yaml
# pvc-local-path.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-local-path
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 2Gi
  storageClassName: local-path
```

### Task 3: Compare Driver Capabilities

Create a comparison matrix for your cluster. For each installed CSI driver, determine:

1. Does it support dynamic provisioning?
2. Does it support volume expansion?
3. Does it support volume snapshots?
4. What access modes does it support (RWO, ROX, RWX)?
5. Does it support topology-aware provisioning?

### Task 4: Create a Pod Using a CSI-Provisioned Volume

```yaml
# pod-csi-test.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-csi-test
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "CSI driver test - $(date)" > /data/csi-test.txt
          echo "StorageClass: $(mount | grep /data | head -1)"
          df -h /data
          sleep 3600
      volumeMounts:
        - name: csi-vol
          mountPath: /data
  volumes:
    - name: csi-vol
      persistentVolumeClaim:
        claimName: pvc-local-path
  restartPolicy: Never
```

### Task 5: Verify CSI Node Information

```bash
kubectl get csinodes -o yaml
```

Examine the `topologyKeys` and `allocatable` fields to understand node-level storage constraints.

## Suggested Steps

1. List all CSI drivers in your cluster with `kubectl get csidrivers`
2. Install a provisioner if your cluster lacks one (local-path-provisioner for kind)
3. Create a StorageClass referencing the CSI driver
4. Create a PVC and pod to verify provisioning works
5. Test volume expansion if the driver supports it
6. Document which features each driver supports

## Verify What You Learned

```bash
kubectl get csidrivers
kubectl get storageclass
kubectl get pvc pvc-local-path
kubectl exec pod-csi-test -- cat /data/csi-test.txt
kubectl get csinodes -o jsonpath='{.items[*].spec.drivers[*].name}'
```

## Cleanup

```bash
kubectl delete pod pod-csi-test --ignore-not-found
kubectl delete pvc pvc-local-path --ignore-not-found
```

## What's Next

Now that you understand CSI drivers, the next exercise covers volume cloning and data migration between volumes. Continue to [exercise 09 (Volume Cloning and Data Migration)](../09-volume-cloning-and-migration/09-volume-cloning-and-migration.md).

## Summary

- **CSI** is the standard interface for storage plugins in Kubernetes, replacing in-tree volume plugins
- CSI drivers consist of a **controller plugin** (Deployment) and a **node plugin** (DaemonSet)
- Different drivers support different capabilities: expansion, snapshots, cloning, and access modes
- **Local path provisioner** provides simple dynamic provisioning for development clusters
- Use `kubectl get csidrivers` and `kubectl get csinodes` to inspect storage capabilities

## Reference

- [Kubernetes CSI Developer Documentation](https://kubernetes-csi.github.io/docs/)
- [Storage Drivers](https://kubernetes.io/docs/concepts/storage/volumes/#csi)
- [CSI Drivers List](https://kubernetes-csi.github.io/docs/drivers.html)
