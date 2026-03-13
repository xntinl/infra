# 3. Volume Basics: emptyDir and hostPath

<!--
difficulty: basic
concepts: [emptyDir, hostPath, volume-mounts, pod-lifecycle, shared-volumes]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [09-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Persistent Volumes and Claims)](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the difference between `emptyDir` and `hostPath` volume types
- **Understand** when each volume type is appropriate and their lifecycle implications
- **Apply** both volume types in pod manifests to share data between containers and access host files

## Why Volume Basics?

Before diving into PersistentVolumes and StorageClasses, every Kubernetes practitioner needs to understand the two simplest volume types: `emptyDir` and `hostPath`. They appear constantly in real-world manifests and serve different purposes.

An `emptyDir` volume is created when a pod is assigned to a node and exists only as long as the pod runs. It starts empty. Its primary use is sharing temporary files between containers in the same pod. When the pod is deleted, the volume is erased. This makes it perfect for scratch space, caches, and inter-container communication, but terrible for data that must survive restarts.

A `hostPath` volume mounts a file or directory from the host node's filesystem into the pod. The data exists on the node independently of any pod. This is useful for system-level access, like reading host logs or interacting with the Docker socket, but it ties the pod to a specific node and introduces security risks. In production, `hostPath` is rarely used outside DaemonSets and system components.

## Step 1: emptyDir for Shared Data Between Containers

Create a pod where a producer container writes to a shared `emptyDir` and a consumer container reads from it.

Create `pod-emptydir.yaml`:

```yaml
# pod-emptydir.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-emptydir
spec:
  containers:
    - name: producer
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          counter=1
          while true; do
            echo "{\"id\": $counter, \"time\": \"$(date)\"}" >> /shared/events.log
            counter=$((counter + 1))
            sleep 3
          done
      volumeMounts:
        - name: shared-data           # Same volume name as consumer
          mountPath: /shared
    - name: consumer
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Waiting for data..."
          sleep 5
          tail -f /shared/events.log
      volumeMounts:
        - name: shared-data
          mountPath: /shared
  volumes:
    - name: shared-data
      emptyDir: {}                     # Created empty when pod starts
  restartPolicy: Never
```

```bash
kubectl apply -f pod-emptydir.yaml
kubectl wait --for=condition=Ready pod/pod-emptydir --timeout=60s
```

Verify both containers see the same data:

```bash
kubectl exec pod-emptydir -c producer -- cat /shared/events.log
kubectl exec pod-emptydir -c consumer -- cat /shared/events.log
kubectl logs pod-emptydir -c consumer --tail=5
```

## Step 2: emptyDir with Size Limit

You can limit how much disk space an `emptyDir` consumes:

Create `pod-emptydir-limited.yaml`:

```yaml
# pod-emptydir-limited.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-emptydir-limited
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Writing to size-limited emptyDir..."
          dd if=/dev/zero of=/cache/testfile bs=1M count=5 2>&1
          ls -lh /cache/testfile
          df -h /cache
          sleep 3600
      volumeMounts:
        - name: cache
          mountPath: /cache
  volumes:
    - name: cache
      emptyDir:
        sizeLimit: 50Mi               # Evicts the pod if exceeded
  restartPolicy: Never
```

```bash
kubectl apply -f pod-emptydir-limited.yaml
kubectl wait --for=condition=Ready pod/pod-emptydir-limited --timeout=60s
kubectl exec pod-emptydir-limited -- ls -lh /cache/testfile
```

## Step 3: hostPath to Access Host Files

Create a pod that reads the node's system logs via `hostPath`:

Create `pod-hostpath.yaml`:

```yaml
# pod-hostpath.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-hostpath
spec:
  containers:
    - name: log-reader
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Host log files ==="
          ls /host-logs/ 2>/dev/null | head -10
          echo "=== Reading host logs ==="
          if [ -f /host-logs/syslog ]; then
            tail -5 /host-logs/syslog
          elif [ -f /host-logs/messages ]; then
            tail -5 /host-logs/messages
          else
            echo "No standard log files found, listing directory:"
            ls -la /host-logs/ | head -10
          fi
          sleep 3600
      volumeMounts:
        - name: host-logs
          mountPath: /host-logs
          readOnly: true               # Always mount hostPath as read-only
  volumes:
    - name: host-logs
      hostPath:
        path: /var/log                 # Directory on the host node
        type: Directory                # Must already exist
  restartPolicy: Never
```

```bash
kubectl apply -f pod-hostpath.yaml
kubectl wait --for=condition=Ready pod/pod-hostpath --timeout=60s
kubectl exec pod-hostpath -- ls /host-logs/ | head -10
```

## Step 4: Prove emptyDir Data Does Not Survive Pod Deletion

```bash
kubectl exec pod-emptydir -c producer -- cat /shared/events.log | tail -3
kubectl delete pod pod-emptydir
kubectl apply -f pod-emptydir.yaml
kubectl wait --for=condition=Ready pod/pod-emptydir --timeout=60s
kubectl exec pod-emptydir -c producer -- cat /shared/events.log
```

The log starts fresh. All previous data is gone because `emptyDir` is tied to the pod lifecycle.

## Common Mistakes

### Mistake 1: Using hostPath in Production

`hostPath` binds a pod to a specific node. If the pod is rescheduled to another node, it will not find the same data. Use PVs and PVCs for portable persistent storage.

### Mistake 2: Forgetting readOnly on hostPath

Without `readOnly: true`, a container could accidentally modify or delete host system files. Always mount `hostPath` volumes as read-only unless you have a specific reason to write.

### Mistake 3: Assuming emptyDir Survives Restarts

`emptyDir` survives container restarts within the same pod, but it is destroyed when the pod is deleted or evicted. Do not store important data in `emptyDir`.

## Verify What You Learned

```bash
kubectl get pod pod-emptydir -o jsonpath='{.spec.volumes[0].emptyDir}'
# Shows: {}

kubectl get pod pod-hostpath -o jsonpath='{.spec.volumes[0].hostPath.path}'
# Shows: /var/log
```

## Cleanup

```bash
kubectl delete pod pod-emptydir pod-emptydir-limited pod-hostpath --ignore-not-found
```

## What's Next

You now understand the basic volume types. Next, explore how projected volumes combine multiple data sources into a single mount point. Continue to [exercise 04 (Ephemeral Volumes: emptyDir and Projected)](../04-ephemeral-volumes-emptydir-projected/04-ephemeral-volumes-emptydir-projected.md).

## Summary

- **emptyDir** creates an empty volume when a pod starts; it is destroyed when the pod is deleted.
- **emptyDir** is ideal for scratch space, caches, and sharing files between containers in the same pod.
- **hostPath** mounts a directory from the host node's filesystem into the pod.
- **hostPath** ties a pod to a specific node and should be used sparingly, typically only in DaemonSets.
- Always mount hostPath volumes as **readOnly** unless write access is explicitly required.
- Neither `emptyDir` nor `hostPath` is a substitute for proper persistent storage (PVs/PVCs).

## Reference

- [Volumes - emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) -- official documentation
- [Volumes - hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath) -- official documentation

## Additional Resources

- [Configure a Pod to Use a Volume for Storage](https://kubernetes.io/docs/tasks/configure-pod-container/configure-volume-storage/)
- [Types of Volumes](https://kubernetes.io/docs/concepts/storage/volumes/#volume-types)
