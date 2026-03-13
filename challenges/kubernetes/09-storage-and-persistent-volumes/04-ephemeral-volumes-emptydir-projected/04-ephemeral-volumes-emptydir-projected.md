# 4. Ephemeral Volumes: emptyDir and Projected

<!--
difficulty: intermediate
concepts: [emptyDir, tmpfs, projected-volumes, downward-api, configmap-volume, secret-volume, init-container-pattern]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: apply
prerequisites: [09-01, 09-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md) and [exercise 03](../03-volume-basics-emptydir-hostpath/03-volume-basics-emptydir-hostpath.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** emptyDir with `medium: Memory` (tmpfs) for high-performance temporary storage
- **Analyze** how projected volumes combine ConfigMaps, Secrets, and the Downward API into a single mount
- **Create** init container patterns that populate emptyDir volumes for main containers

## Why Ephemeral Volumes?

Not every volume needs to persist. Caches should be fast and disposable. Configuration files should be assembled from multiple sources. Pod metadata should be available as files without calling the API server. Ephemeral volumes fill these roles.

An `emptyDir` with `medium: Memory` mounts a tmpfs filesystem backed by RAM. It is significantly faster than disk but counts against the container's memory limit and disappears on pod deletion. Projected volumes let you merge data from ConfigMaps, Secrets, the Downward API, and ServiceAccount tokens into a single directory, simplifying container configuration.

## Step 1: Create Supporting ConfigMap and Secret

```yaml
# configmap-app.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
data:
  app.properties: |
    environment=production
    feature.new_ui=true
    max_retries=3
    timeout_seconds=30
  logging.conf: |
    [loggers]
    keys=root,app
    [logger_root]
    level=WARNING
    handlers=console
```

```yaml
# secret-app.yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
type: Opaque
stringData:
  api-key: "sk-prod-a1b2c3d4e5f6g7h8i9j0"
  db-password: "Pr0duct10n_DB_P@ss!"
```

```bash
kubectl apply -f configmap-app.yaml
kubectl apply -f secret-app.yaml
```

## Step 2: emptyDir with tmpfs (Memory-Backed)

Create `pod-tmpfs.yaml`:

```yaml
# pod-tmpfs.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-tmpfs
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== tmpfs vs disk ==="
          echo "--- /cache is tmpfs ---"
          mount | grep /cache
          dd if=/dev/zero of=/cache/bench bs=1M count=5 2>&1
          df -h /cache
          echo "--- /scratch is disk ---"
          dd if=/dev/zero of=/scratch/bench bs=1M count=5 2>&1
          df -h /scratch
          sleep 3600
      volumeMounts:
        - name: cache-tmpfs
          mountPath: /cache
        - name: cache-disk
          mountPath: /scratch
      resources:
        requests:
          memory: "128Mi"
        limits:
          memory: "256Mi"        # tmpfs counts against this limit
  volumes:
    - name: cache-tmpfs
      emptyDir:
        medium: Memory           # Backed by RAM, not disk
        sizeLimit: 64Mi
    - name: cache-disk
      emptyDir:
        sizeLimit: 100Mi         # Disk-backed with eviction limit
  restartPolicy: Never
```

```bash
kubectl apply -f pod-tmpfs.yaml
kubectl wait --for=condition=Ready pod/pod-tmpfs --timeout=60s
kubectl logs pod-tmpfs
kubectl exec pod-tmpfs -- mount | grep cache
```

## Step 3: Projected Volume (ConfigMap + Secret + Downward API)

Create `pod-projected.yaml`:

```yaml
# pod-projected.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-projected
  labels:
    app: projected-demo
    version: "2.1.0"
  annotations:
    maintainer: "platform-team@example.com"
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Projected Volume Contents ==="
          echo "--- ConfigMap: app.properties ---"
          cat /etc/projected/app.properties
          echo "--- Secret: api-key ---"
          echo "API Key: $(cat /etc/projected/api-key)"
          echo "--- Downward API ---"
          echo "Pod Name: $(cat /etc/projected/pod-name)"
          echo "Pod Namespace: $(cat /etc/projected/pod-namespace)"
          echo "Labels:"
          cat /etc/projected/labels
          echo "--- CPU Request ---"
          cat /etc/projected/cpu-request
          echo ""
          echo "=== Full listing ==="
          ls -la /etc/projected/
          sleep 3600
      volumeMounts:
        - name: projected-volume
          mountPath: /etc/projected
          readOnly: true
      resources:
        requests:
          cpu: "100m"
          memory: "64Mi"
        limits:
          cpu: "200m"
          memory: "128Mi"
  volumes:
    - name: projected-volume
      projected:
        sources:
          - configMap:
              name: app-settings
              items:
                - key: app.properties
                  path: app.properties
                - key: logging.conf
                  path: logging.conf
          - secret:
              name: app-secrets
              items:
                - key: api-key
                  path: api-key
                - key: db-password
                  path: db-password
          - downwardAPI:
              items:
                - path: pod-name
                  fieldRef:
                    fieldPath: metadata.name
                - path: pod-namespace
                  fieldRef:
                    fieldPath: metadata.namespace
                - path: labels
                  fieldRef:
                    fieldPath: metadata.labels
                - path: cpu-request
                  resourceFieldRef:
                    containerName: app
                    resource: requests.cpu
  restartPolicy: Never
```

```bash
kubectl apply -f pod-projected.yaml
kubectl wait --for=condition=Ready pod/pod-projected --timeout=60s
kubectl logs pod-projected
```

## Step 4: Init Container Pattern with emptyDir

Create `pod-init-emptydir.yaml`:

```yaml
# pod-init-emptydir.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-init-emptydir
spec:
  initContainers:
    - name: init-config
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          cat > /config/app.json <<'CONF'
          {
            "server": {"port": 8080, "host": "0.0.0.0"},
            "database": {"host": "db.default.svc.cluster.local", "port": 5432}
          }
          CONF
          echo "Config generated."
      volumeMounts:
        - name: config-volume
          mountPath: /config
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Config from init container ==="
          cat /config/app.json
          sleep 3600
      volumeMounts:
        - name: config-volume
          mountPath: /config
          readOnly: true
  volumes:
    - name: config-volume
      emptyDir: {}
  restartPolicy: Never
```

```bash
kubectl apply -f pod-init-emptydir.yaml
kubectl wait --for=condition=Ready pod/pod-init-emptydir --timeout=60s
kubectl logs pod-init-emptydir -c init-config
kubectl logs pod-init-emptydir -c app
```

## Spot the Bug

This projected volume definition silently fails. **Why does the pod see an empty file instead of the Secret value?**

```yaml
volumes:
  - name: all-config
    projected:
      sources:
        - secret:
            name: app-secrets
            items:
              - key: api-key
                path: api-key
        - configMap:
            name: app-settings
            items:
              - key: app.properties
                path: api-key     # <-- BUG
```

<details>
<summary>Explanation</summary>

Both the Secret and ConfigMap project to the same `path: api-key`. The ConfigMap entry overwrites the Secret entry because sources are applied in order. The resulting file contains the ConfigMap's `app.properties` content, not the Secret's `api-key` value. Fix: use unique paths for each projected item.

</details>

## Verify What You Learned

```bash
kubectl exec pod-tmpfs -- mount | grep tmpfs | grep cache
kubectl exec pod-projected -- cat /etc/projected/pod-name
kubectl exec pod-projected -- ls /etc/projected/
kubectl exec pod-init-emptydir -- cat /config/app.json
```

## Cleanup

```bash
kubectl delete pod pod-tmpfs pod-projected pod-init-emptydir --ignore-not-found
kubectl delete configmap app-settings
kubectl delete secret app-secrets
```

## What's Next

Now that you understand ephemeral volumes, the next exercise explores PVC access modes and how they affect volume binding in multi-pod scenarios. Continue to [exercise 05 (PVC Access Modes and Volume Binding)](../05-pvc-access-modes-and-binding/05-pvc-access-modes-and-binding.md).

## Summary

- **emptyDir with `medium: Memory`** mounts a tmpfs volume backed by RAM, which is faster but counts against memory limits
- **Projected volumes** merge ConfigMaps, Secrets, Downward API, and ServiceAccount tokens into a single mount point
- The **Downward API** exposes pod metadata (name, namespace, labels, resource requests) as files without API calls
- **Init containers** can populate emptyDir volumes with generated configuration before the main container starts

## Reference

- [Volumes - emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir)
- [Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
- [Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/)
- [Expose Pod Information Through Files](https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/)
