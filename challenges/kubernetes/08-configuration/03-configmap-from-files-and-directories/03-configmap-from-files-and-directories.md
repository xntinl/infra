<!--
difficulty: basic
concepts: [configmap-from-file, configmap-from-directory, configmap-from-env-file, imperative-configmap, kubectl-create-configmap]
tools: [kubectl]
estimated_time: 20m
bloom_level: understand
prerequisites: [configmaps-environment-and-files]
-->

# 8.03 ConfigMaps from Files, Directories, and Env Files

## What You Will Learn

- How to create ConfigMaps imperatively from individual files, directories, and `.env` files
- The difference between `--from-file`, `--from-literal`, and `--from-env-file`
- How the ConfigMap key name is derived from each source
- When to use imperative creation vs declarative YAML

## Why This Matters

Not all configuration lives in YAML manifests. Real applications often ship with configuration files (nginx.conf, application.properties, .env) that need to be injected into containers. The `kubectl create configmap` command makes it easy to load these files directly without manual base64 encoding or YAML formatting.

## Steps

### 1. Create sample configuration files

```bash
# Create a working directory
mkdir -p /tmp/config-demo

# A properties file
cat > /tmp/config-demo/app.properties << 'EOF'
server.port=8080
server.host=0.0.0.0
database.pool.size=10
database.timeout=30s
EOF

# An nginx configuration file
cat > /tmp/config-demo/nginx.conf << 'EOF'
server {
    listen 80;
    server_name localhost;
    location / {
        root /usr/share/nginx/html;
    }
}
EOF

# A .env file
cat > /tmp/config-demo/app.env << 'EOF'
APP_ENV=production
APP_PORT=8080
APP_LOG_LEVEL=info
APP_DEBUG=false
EOF

# A JSON config file
cat > /tmp/config-demo/config.json << 'EOF'
{
  "database": {
    "host": "db.example.com",
    "port": 5432
  },
  "cache": {
    "ttl": 300
  }
}
EOF
```

### 2. Create ConfigMap from a single file

The filename becomes the key in the ConfigMap. The file content becomes the value.

```bash
# Key = "app.properties", value = file contents
kubectl create configmap config-from-file \
  --from-file=/tmp/config-demo/app.properties

# Verify the key name
kubectl get configmap config-from-file -o yaml
```

### 3. Create ConfigMap from a file with a custom key

```bash
# Key = "my-app-config" instead of the filename
kubectl create configmap config-custom-key \
  --from-file=my-app-config=/tmp/config-demo/app.properties

kubectl get configmap config-custom-key -o yaml
```

### 4. Create ConfigMap from an entire directory

Every file in the directory becomes a key in the ConfigMap.

```bash
# All files in /tmp/config-demo/ become keys
kubectl create configmap config-from-dir \
  --from-file=/tmp/config-demo/

# Verify -- should have app.properties, nginx.conf, app.env, config.json
kubectl get configmap config-from-dir -o yaml
```

### 5. Create ConfigMap from an env file

Each `KEY=VALUE` line becomes a separate key-value pair in the ConfigMap (not a single file).

```bash
# Each line becomes a top-level key
kubectl create configmap config-from-env \
  --from-env-file=/tmp/config-demo/app.env

# Verify -- should have APP_ENV, APP_PORT, APP_LOG_LEVEL, APP_DEBUG as separate keys
kubectl get configmap config-from-env -o yaml
```

### 6. Create ConfigMap from literals

```bash
kubectl create configmap config-from-literals \
  --from-literal=APP_ENV=staging \
  --from-literal=APP_PORT=3000 \
  --from-literal=APP_WORKERS=4
```

### 7. Use the ConfigMaps in a pod

```yaml
# pod-test.yaml
apiVersion: v1
kind: Pod
metadata:
  name: config-test
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== From env file (as env vars) ==="
          echo "APP_ENV=$APP_ENV"
          echo "APP_PORT=$APP_PORT"
          echo ""
          echo "=== From file (mounted) ==="
          cat /etc/config/app.properties
          echo ""
          echo "=== From directory (all files) ==="
          ls /etc/all-config/
          sleep 3600
      envFrom:
        - configMapRef:
            name: config-from-env        # env file -> env vars
      volumeMounts:
        - name: single-file
          mountPath: /etc/config
        - name: all-files
          mountPath: /etc/all-config
  volumes:
    - name: single-file
      configMap:
        name: config-from-file
    - name: all-files
      configMap:
        name: config-from-dir
  restartPolicy: Never
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Confusing `--from-file` with `--from-env-file` | `--from-file` creates one key per file; `--from-env-file` creates one key per line | Choose based on whether you need the file as-is or parsed into key-value pairs |
| Including hidden files in a directory | Files like `.DS_Store` become ConfigMap keys | Clean the directory before creating the ConfigMap |
| Using `--from-file` on a binary file | The content is stored but may not display correctly in YAML | Use `binaryData` in declarative YAML for binary content |
| Forgetting `--dry-run=client -o yaml` | Cannot version-control imperatively created ConfigMaps | Append `--dry-run=client -o yaml > configmap.yaml` to generate declarative YAML |

## Verify

```bash
# Compare all ConfigMaps
kubectl get configmap config-from-file -o yaml
kubectl get configmap config-from-dir -o yaml
kubectl get configmap config-from-env -o yaml
kubectl get configmap config-from-literals -o yaml

# Test the pod
kubectl apply -f pod-test.yaml
kubectl exec config-test -- env | grep APP_
kubectl exec config-test -- cat /etc/config/app.properties
kubectl exec config-test -- ls /etc/all-config/
```

## Cleanup

```bash
kubectl delete pod config-test
kubectl delete configmap config-from-file config-custom-key config-from-dir config-from-env config-from-literals
rm -rf /tmp/config-demo
```

## What's Next

Continue to [8.04 Secret Types: Opaque, TLS, Docker Registry, SSH](../04-secret-types/04-secret-types.md) to explore the full range of Kubernetes Secret types.

## Summary

- `--from-file=<path>` uses the filename as the key and file content as the value.
- `--from-file=<key>=<path>` overrides the key name.
- `--from-file=<directory>` creates one key per file in the directory.
- `--from-env-file=<path>` parses `KEY=VALUE` lines into separate ConfigMap entries.
- `--from-literal=KEY=VALUE` sets individual key-value pairs from the command line.
- Append `--dry-run=client -o yaml` to generate declarative YAML from imperative commands.

## References

- [Configure a Pod to Use a ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
- [kubectl create configmap](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_create/kubectl_create_configmap/)

## Additional Resources

- [Managing ConfigMaps](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-config-file/)
