<!--
difficulty: intermediate
concepts: [subpath, selective-mount, volume-mount, configmap-single-file, directory-preservation]
tools: [kubectl]
estimated_time: 20m
bloom_level: apply
prerequisites: [configmaps-environment-and-files, configmap-volume-updates]
-->

# 8.07 SubPath Mounts for Selective File Injection

## What You Will Learn

- How `subPath` mounts a single file from a ConfigMap or Secret into an existing directory
- Why a normal volume mount overwrites the entire directory
- The trade-off: `subPath` preserves the directory but disables auto-updates
- How to combine `subPath` with `items` for precise file placement

## Steps

### 1. Create a ConfigMap with configuration files

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-config
data:
  default.conf: |
    server {
        listen 80;
        location / {
            return 200 'custom config loaded\n';
            add_header Content-Type text/plain;
        }
    }
  extra.conf: |
    # Additional upstream configuration
    upstream backend {
        server backend:8080;
    }
```

### 2. Without subPath -- entire directory is replaced

```yaml
# pod-no-subpath.yaml
apiVersion: v1
kind: Pod
metadata:
  name: no-subpath
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      volumeMounts:
        - name: config
          mountPath: /etc/nginx/conf.d    # replaces the entire directory
  volumes:
    - name: config
      configMap:
        name: nginx-config
  restartPolicy: Never
```

```bash
kubectl apply -f configmap.yaml -f pod-no-subpath.yaml
# The default nginx conf.d/ directory is gone -- only default.conf and extra.conf exist
kubectl exec no-subpath -- ls /etc/nginx/conf.d/
```

### 3. With subPath -- inject a single file

```yaml
# pod-with-subpath.yaml
apiVersion: v1
kind: Pod
metadata:
  name: with-subpath
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      volumeMounts:
        - name: config
          mountPath: /etc/nginx/conf.d/custom.conf   # target file path
          subPath: default.conf                       # key from ConfigMap
  volumes:
    - name: config
      configMap:
        name: nginx-config
  restartPolicy: Never
```

```bash
kubectl apply -f pod-with-subpath.yaml
# The original /etc/nginx/conf.d/ contents are preserved, custom.conf is added
kubectl exec with-subpath -- ls /etc/nginx/conf.d/
```

### 4. Combining `items` with `subPath` for renaming

Use `items` to select and rename specific keys, then `subPath` to place them precisely.

```yaml
# pod-items-subpath.yaml
apiVersion: v1
kind: Pod
metadata:
  name: items-subpath
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      volumeMounts:
        - name: config
          mountPath: /etc/nginx/conf.d/site.conf
          subPath: site.conf
  volumes:
    - name: config
      configMap:
        name: nginx-config
        items:
          - key: default.conf        # ConfigMap key
            path: site.conf           # file name in the volume
  restartPolicy: Never
```

### Spot the Bug

This pod mounts a ConfigMap as a single file. The operator expects the file to auto-update when the ConfigMap changes. Why will it not?

```yaml
volumeMounts:
  - name: config
    mountPath: /app/config.yaml
    subPath: config.yaml
```

<details>
<summary>Answer</summary>

Volumes mounted with `subPath` do not receive automatic updates from the kubelet. The file is projected once at pod startup. To get auto-updates, mount the entire ConfigMap as a directory and read from there, or restart the pod after updating.

</details>

## Verify

```bash
# Without subPath: only ConfigMap files exist
kubectl exec no-subpath -- ls /etc/nginx/conf.d/
# Output: default.conf  extra.conf

# With subPath: original files are preserved, custom.conf is added
kubectl exec with-subpath -- ls /etc/nginx/conf.d/
# Output: custom.conf  default.conf (the original nginx default.conf is preserved)

# Content check
kubectl exec with-subpath -- cat /etc/nginx/conf.d/custom.conf
```

## Cleanup

```bash
kubectl delete pod no-subpath with-subpath items-subpath
kubectl delete configmap nginx-config
```

## What's Next

Continue to [8.08 Environment Variable Patterns: fieldRef, resourceFieldRef](../08-environment-variable-patterns/) to learn how to inject pod metadata and resource limits as env vars.

## Summary

- Without `subPath`, a volume mount replaces the entire target directory.
- With `subPath`, a single file is injected without disturbing existing directory contents.
- `subPath` mounts do NOT auto-update when the ConfigMap changes.
- Use `items` to select and rename specific keys from a ConfigMap volume.

## References

- [Using subPath](https://kubernetes.io/docs/concepts/storage/volumes/#using-subpath)
- [Configure a Pod to Use a ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
