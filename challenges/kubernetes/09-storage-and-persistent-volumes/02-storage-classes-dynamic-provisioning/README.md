# 2. Storage Classes and Dynamic Provisioning

<!--
difficulty: basic
concepts: [storage-class, dynamic-provisioning, volume-binding-mode, default-storage-class, reclaim-policy]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [09-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Persistent Volumes and Claims)](../01-persistent-volumes-and-claims/)

Verify your cluster has a provisioner:

```bash
kubectl get storageclass
```

You should see at least one StorageClass (e.g., `standard` on minikube).

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** what a StorageClass is and how it enables dynamic volume provisioning
- **Understand** the difference between `Immediate` and `WaitForFirstConsumer` volume binding modes
- **Apply** StorageClasses to automatically create PVs when PVCs are submitted

## Why Storage Classes?

In the previous exercise, you manually created PersistentVolumes before any pod could use them. This approach does not scale. If a team needs fifty volumes, an administrator should not have to create fifty PV manifests by hand.

StorageClasses automate this process. A StorageClass defines a provisioner (the plugin that creates actual storage), a reclaim policy, and parameters specific to the storage backend. When a PVC references a StorageClass, Kubernetes calls the provisioner to create a PV automatically. This is called dynamic provisioning.

The volume binding mode controls when provisioning happens. `Immediate` creates the volume as soon as the PVC is submitted. `WaitForFirstConsumer` delays creation until a pod actually needs the volume, which allows the provisioner to create the volume in the same availability zone as the pod. This distinction matters in multi-zone clusters where a volume created in zone-a cannot be mounted by a pod in zone-b.

## Step 1: Explore Existing StorageClasses

Check what StorageClasses your cluster already provides:

```bash
kubectl get storageclass
kubectl describe storageclass standard
```

Note the provisioner name, reclaim policy, and binding mode. On minikube this is typically `k8s.io/minikube-hostpath` with `Immediate` binding.

## Step 2: Create a Custom StorageClass

Create `storageclass-delayed.yaml`:

```yaml
# storageclass-delayed.yaml
apiVersion: storage.k8s.io/v1          # StorageClasses live in the storage API group
kind: StorageClass
metadata:
  name: delayed-binding
provisioner: k8s.io/minikube-hostpath   # Must match your cluster's provisioner
reclaimPolicy: Delete                   # PV is deleted when PVC is deleted
volumeBindingMode: WaitForFirstConsumer # Delay provisioning until a pod needs it
```

Create `storageclass-retain.yaml`:

```yaml
# storageclass-retain.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: retain-storage
provisioner: k8s.io/minikube-hostpath
reclaimPolicy: Retain                   # PV survives PVC deletion
volumeBindingMode: Immediate
```

Apply both:

```bash
kubectl apply -f storageclass-delayed.yaml
kubectl apply -f storageclass-retain.yaml
kubectl get storageclass
```

## Step 3: Dynamic Provisioning with Immediate Binding

Create a PVC that references the `retain-storage` StorageClass. No manual PV creation needed.

Create `pvc-dynamic.yaml`:

```yaml
# pvc-dynamic.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-dynamic
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: retain-storage    # References the StorageClass
```

```bash
kubectl apply -f pvc-dynamic.yaml
kubectl get pvc pvc-dynamic           # Should be Bound immediately
kubectl get pv                        # A PV was created automatically
```

## Step 4: WaitForFirstConsumer Binding

Create a PVC with `delayed-binding` and observe it stays `Pending` until a pod uses it.

Create `pvc-delayed.yaml`:

```yaml
# pvc-delayed.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-delayed
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 500Mi
  storageClassName: delayed-binding
```

```bash
kubectl apply -f pvc-delayed.yaml
kubectl get pvc pvc-delayed           # Pending -- no pod is using it yet
```

Now create a pod that uses this PVC:

```yaml
# pod-delayed-volume.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-delayed-volume
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Volume bound!' > /data/test.txt && sleep 3600"]
      volumeMounts:
        - name: delayed-data
          mountPath: /data
  volumes:
    - name: delayed-data
      persistentVolumeClaim:
        claimName: pvc-delayed
  restartPolicy: Never
```

```bash
kubectl apply -f pod-delayed-volume.yaml
kubectl get pvc pvc-delayed           # Now Bound
```

## Step 5: Set a Default StorageClass

A default StorageClass is used when a PVC does not specify `storageClassName`.

Create `storageclass-default.yaml`:

```yaml
# storageclass-default.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: custom-default
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"  # Marks it as default
provisioner: k8s.io/minikube-hostpath
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
```

```bash
kubectl apply -f storageclass-default.yaml
kubectl get storageclass              # Look for "(default)" annotation
```

Create a PVC without specifying `storageClassName`:

```yaml
# pvc-default-class.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-default-class
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 256Mi
  # No storageClassName -- uses the default StorageClass
```

```bash
kubectl apply -f pvc-default-class.yaml
kubectl get pvc pvc-default-class -o jsonpath='{.spec.storageClassName}'
# Should show: custom-default
```

## Common Mistakes

### Mistake 1: Multiple Default StorageClasses

If two StorageClasses are both annotated as default, PVCs without an explicit class will fail with an ambiguous error. Always ensure only one default exists.

### Mistake 2: Wrong Provisioner Name

If the provisioner name does not match any installed driver, PVCs stay `Pending` indefinitely. Check `kubectl describe pvc` for provisioning errors.

### Mistake 3: Confusing Empty String with Omitted storageClassName

Setting `storageClassName: ""` explicitly requests no StorageClass (manual PV binding only). Omitting the field entirely uses the default StorageClass. These are different behaviors.

## Verify What You Learned

```bash
kubectl get storageclass
kubectl get pvc
kubectl get pv
kubectl describe pvc pvc-dynamic | grep "StorageClass"
```

## Cleanup

```bash
kubectl delete pod pod-delayed-volume --ignore-not-found
kubectl delete pvc pvc-dynamic pvc-delayed pvc-default-class --ignore-not-found
kubectl delete pv --all --ignore-not-found
kubectl delete storageclass delayed-binding retain-storage custom-default
```

## What's Next

Now that you understand dynamic provisioning, the next exercise covers the foundational volume types `emptyDir` and `hostPath` and when to use each. Continue to [exercise 03 (Volume Basics: emptyDir and hostPath)](../03-volume-basics-emptydir-hostpath/).

## Summary

- A **StorageClass** defines a provisioner, reclaim policy, and binding mode for dynamic volume creation.
- **Dynamic provisioning** eliminates manual PV creation -- a PV is created automatically when a PVC references a StorageClass.
- **Immediate** binding provisions the PV as soon as the PVC is created.
- **WaitForFirstConsumer** delays provisioning until a pod is scheduled, ensuring zone-aware placement.
- A **default StorageClass** is used when a PVC omits `storageClassName`.
- The **reclaim policy** on the StorageClass is inherited by dynamically provisioned PVs.

## Reference

- [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/) -- official concept documentation
- [Dynamic Volume Provisioning](https://kubernetes.io/docs/concepts/storage/dynamic-provisioning/) -- how provisioners work

## Additional Resources

- [Change the Default StorageClass](https://kubernetes.io/docs/tasks/administer-cluster/change-default-storage-class/)
- [Kubernetes API Reference: StorageClass](https://kubernetes.io/docs/reference/kubernetes-api/config-and-storage-resources/storage-class-v1/)
