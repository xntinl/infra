<!--
difficulty: basic
concepts: [configmap, environment-variables, volume-mount, envFrom, configMapKeyRef]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [pods, volumes]
-->

# 8.01 ConfigMaps: Environment Variables and File Mounts

## What You Will Learn

- How to create ConfigMaps with key-value pairs and multi-line file data
- How to inject ConfigMap data as environment variables using `envFrom` and `valueFrom`
- How to mount ConfigMaps as files in a volume
- The difference between environment variable injection and volume mounting

## Why This Matters

Hardcoding configuration inside container images makes them inflexible -- every change requires a rebuild. ConfigMaps decouple configuration from images, letting you change settings without touching the application code. This is a core Kubernetes pattern for the Twelve-Factor App methodology.

## Steps

### 1. Create a ConfigMap with application settings

```yaml
# configmap-app.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  labels:
    app: demo-app
data:
  # Simple key-value pairs (will become env vars or individual files)
  APP_ENV: "production"
  APP_PORT: "8080"
  APP_LOG_LEVEL: "info"
  APP_MAX_CONNECTIONS: "100"

  # Multi-line value (typically mounted as a file)
  app.properties: |
    server.port=8080
    server.host=0.0.0.0
    database.pool.size=10
    database.timeout=30s
    cache.enabled=true
    cache.ttl=300

  # Another file-like entry
  nginx.conf: |
    server {
        listen 80;
        server_name localhost;
        location / {
            root /usr/share/nginx/html;
            index index.html;
        }
        location /health {
            return 200 'OK';
            add_header Content-Type text/plain;
        }
    }
```

### 2. Pod consuming ConfigMap as environment variables

```yaml
# pod-env-vars.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-env-from-configmap
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo '--- Environment Variables ---' && env | grep APP_ && sleep 3600"]
      # Method 1: inject ALL keys from the ConfigMap as env vars
      envFrom:
        - configMapRef:
            name: app-config
      # Method 2: inject specific keys with custom env var names
      env:
        - name: SPECIFIC_PORT          # custom env var name
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: APP_PORT            # key from the ConfigMap
        - name: SPECIFIC_ENV
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: APP_ENV
  restartPolicy: Never
```

### 3. Pod consuming ConfigMap as mounted files

```yaml
# pod-volume-mount.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-volume-from-configmap
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo '--- Mounted files ---'
          ls -la /etc/config/
          echo '--- app.properties ---'
          cat /etc/config/app.properties
          echo '--- nginx.conf ---'
          cat /etc/config/nginx.conf
          sleep 3600
      volumeMounts:
        - name: config-volume
          mountPath: /etc/config         # all keys become files here
        - name: config-volume
          mountPath: /etc/nginx/conf.d/default.conf
          subPath: nginx.conf            # mount single file without overwriting dir
  volumes:
    - name: config-volume
      configMap:
        name: app-config                 # reference the ConfigMap
  restartPolicy: Never
```

### 4. Pod combining both methods

```yaml
# pod-combined.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-combined-configmap
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Environment Variables ==="
          echo "APP_ENV=$APP_ENV"
          echo "APP_PORT=$APP_PORT"
          echo "APP_LOG_LEVEL=$APP_LOG_LEVEL"
          echo ""
          echo "=== Config File ==="
          cat /etc/config/app.properties
          echo ""
          echo "Watching for file updates every 30s..."
          while true; do
            echo "--- $(date) ---"
            echo "Env APP_LOG_LEVEL=$APP_LOG_LEVEL"
            echo "File content:"
            cat /etc/config/APP_LOG_LEVEL
            sleep 30
          done
      env:
        - name: APP_ENV
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: APP_ENV
        - name: APP_PORT
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: APP_PORT
        - name: APP_LOG_LEVEL
          valueFrom:
            configMapKeyRef:
              name: app-config
              key: APP_LOG_LEVEL
      volumeMounts:
        - name: config-volume
          mountPath: /etc/config
          readOnly: true
  volumes:
    - name: config-volume
      configMap:
        name: app-config
  restartPolicy: Never
```

### 5. Apply everything

```bash
kubectl apply -f configmap-app.yaml
kubectl apply -f pod-env-vars.yaml
kubectl apply -f pod-volume-mount.yaml
kubectl apply -f pod-combined.yaml
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Referencing a ConfigMap that does not exist | Pod stays in `CreateContainerConfigError` | Create the ConfigMap before the pod |
| Mounting a volume without `subPath` | The entire directory at `mountPath` is replaced | Use `subPath` to inject a single file |
| Expecting env vars to update when ConfigMap changes | Env vars are set at container start and never refresh | Restart the pod, or use volume mounts instead |
| Using `envFrom` with keys containing dots or dashes | These characters are invalid in shell variable names; those keys are skipped | Use `valueFrom` with a valid `name` |

## Verify

1. Check the ConfigMap:

```bash
kubectl get configmap app-config -o yaml
```

2. Check environment variables in the pod:

```bash
kubectl exec pod-env-from-configmap -- env | grep APP_
kubectl exec pod-env-from-configmap -- env | grep SPECIFIC_
```

3. Check mounted files:

```bash
kubectl exec pod-volume-from-configmap -- ls -la /etc/config/
kubectl exec pod-volume-from-configmap -- cat /etc/config/app.properties
kubectl exec pod-volume-from-configmap -- cat /etc/nginx/conf.d/default.conf
```

4. Observe auto-update behavior (volumes update, env vars do not):

```bash
# Edit the ConfigMap
kubectl patch configmap app-config --type merge -p '{"data":{"APP_LOG_LEVEL":"debug"}}'

# Wait ~30 seconds, then check the volume (updated)
kubectl exec pod-combined-configmap -- cat /etc/config/APP_LOG_LEVEL

# Check the env var (NOT updated -- still "info")
kubectl exec pod-combined-configmap -- sh -c 'echo $APP_LOG_LEVEL'
```

## Cleanup

```bash
kubectl delete pod pod-env-from-configmap pod-volume-from-configmap pod-combined-configmap
kubectl delete configmap app-config
```

## What's Next

Continue to [8.02 Secrets Management](../02-secrets-management/02-secrets-management.md) to learn how to handle sensitive data like passwords and API keys.

## Summary

- ConfigMaps store non-sensitive configuration as key-value pairs.
- `envFrom` injects all keys as environment variables; `valueFrom` maps individual keys.
- Volume mounts turn each ConfigMap key into a file at the specified `mountPath`.
- Use `subPath` to mount a single file without overwriting the entire directory.
- Volume-mounted ConfigMaps auto-update (with a delay); environment variables require a pod restart.

## References

- [ConfigMaps](https://kubernetes.io/docs/concepts/configuration/configmap/)
- [Configure a Pod to Use a ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)

## Additional Resources

- [Twelve-Factor App - Config](https://12factor.net/config)
- [Immutable ConfigMaps](https://kubernetes.io/docs/concepts/configuration/configmap/#configmap-immutable)
