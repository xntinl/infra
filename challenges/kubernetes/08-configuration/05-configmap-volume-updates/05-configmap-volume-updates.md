<!--
difficulty: intermediate
concepts: [configmap-auto-update, kubelet-sync-period, volume-propagation, env-var-immutability, symlink-rotation]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [configmaps-environment-and-files]
-->

# 8.05 ConfigMap Volume Updates and Propagation

## What You Will Learn

- How Kubernetes automatically updates ConfigMap volumes in running pods
- The kubelet sync period and typical propagation delay
- Why environment variables sourced from ConfigMaps do NOT auto-update
- How the symlink-based rotation mechanism works under the hood
- The `subPath` exception that disables auto-updates

## Steps

### 1. Create a ConfigMap to observe updates

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: live-config
data:
  LOG_LEVEL: "info"
  config.yaml: |
    version: 1
    feature_flags:
      dark_mode: false
      beta_api: false
```

### 2. Deploy a pod that watches both env vars and files

```yaml
# watcher-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: config-watcher
spec:
  containers:
    - name: watcher
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "Starting config watcher..."
          while true; do
            echo "=== $(date) ==="
            echo "ENV LOG_LEVEL=$LOG_LEVEL"
            echo "FILE LOG_LEVEL=$(cat /etc/config/LOG_LEVEL)"
            echo "FILE config.yaml:"
            cat /etc/config/config.yaml
            echo "---"
            echo "SYMLINKS:"
            ls -la /etc/config/
            echo ""
            sleep 10
          done
      env:
        - name: LOG_LEVEL
          valueFrom:
            configMapKeyRef:
              name: live-config
              key: LOG_LEVEL
      volumeMounts:
        - name: config
          mountPath: /etc/config
  volumes:
    - name: config
      configMap:
        name: live-config
  restartPolicy: Never
```

### 3. Apply and watch the pod logs

```bash
kubectl apply -f configmap.yaml
kubectl apply -f watcher-pod.yaml
kubectl logs -f config-watcher
```

### 4. Update the ConfigMap and observe propagation

```bash
# Patch the ConfigMap
kubectl patch configmap live-config --type merge -p '{"data":{"LOG_LEVEL":"debug","config.yaml":"version: 2\nfeature_flags:\n  dark_mode: true\n  beta_api: true\n"}}'

# Watch the logs -- within ~60 seconds:
# - FILE LOG_LEVEL changes to "debug"
# - FILE config.yaml shows version: 2
# - ENV LOG_LEVEL remains "info" (never changes)
```

### 5. Inspect the symlink rotation mechanism

```bash
kubectl exec config-watcher -- ls -la /etc/config/
```

You will see entries like:

```
..data -> ..2024_01_15_10_30_00.123456789
..2024_01_15_10_30_00.123456789/
LOG_LEVEL -> ..data/LOG_LEVEL
config.yaml -> ..data/config.yaml
```

Kubernetes updates the `..data` symlink to point to a new timestamped directory atomically.

### Spot the Bug

This pod is expected to auto-update its nginx config when the ConfigMap changes. Why does it not work?

```yaml
volumeMounts:
  - name: config
    mountPath: /etc/nginx/conf.d/default.conf
    subPath: nginx.conf
```

<details>
<summary>Answer</summary>

Volumes mounted with `subPath` do not receive automatic updates. The file is copied at pod start and never refreshed. Remove `subPath` and mount the entire volume to a directory to get auto-updates.

</details>

## Verify

```bash
# Before update
kubectl exec config-watcher -- cat /etc/config/LOG_LEVEL
# Should show: info

# After update (wait ~60s)
kubectl exec config-watcher -- cat /etc/config/LOG_LEVEL
# Should show: debug

# Env var never changes
kubectl exec config-watcher -- sh -c 'echo $LOG_LEVEL'
# Always shows: info
```

## Cleanup

```bash
kubectl delete pod config-watcher
kubectl delete configmap live-config
```

## What's Next

Continue to [8.06 Immutable ConfigMaps and Secrets](../06-immutable-configmaps-and-secrets/06-immutable-configmaps-and-secrets.md) to learn how to prevent accidental configuration changes.

## Summary

- ConfigMap volumes auto-update when the ConfigMap is modified (typical delay: 30-60 seconds).
- Environment variables sourced from ConfigMaps are set at container start and never refresh.
- The update mechanism uses atomic symlink rotation via the `..data` symlink.
- Volumes mounted with `subPath` do NOT receive auto-updates.

## References

- [ConfigMap - Mounted ConfigMaps are updated automatically](https://kubernetes.io/docs/concepts/configuration/configmap/#mounted-configmaps-are-updated-automatically)
- [Configure a Pod to Use a ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
