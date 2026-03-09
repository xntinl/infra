<!--
difficulty: basic
concepts: [secrets, base64-encoding, stringData, secret-types, env-from-secret, volume-mount-secret, imagePullSecrets]
tools: [kubectl, openssl]
estimated_time: 30m
bloom_level: understand
prerequisites: [pods, configmaps-environment-and-files]
-->

# 8.02 Secrets Management

## What You Will Learn

- How to create Secrets using `data` (base64) and `stringData` (plain text)
- The different Secret types: Opaque, TLS, docker-registry
- How to consume Secrets as environment variables and volume mounts
- How to configure `imagePullSecrets` for private container registries

## Why This Matters

Applications need credentials -- database passwords, API keys, TLS certificates. Storing them in ConfigMaps or container images exposes them in plain text. Kubernetes Secrets provide a dedicated resource type with base64 encoding, restricted API access, and optional encryption at rest in etcd.

## Steps

### 1. Create an Opaque Secret with `stringData`

`stringData` lets you write values in plain text. Kubernetes encodes them to base64 automatically when storing.

```yaml
# secret-db-credentials.yaml
apiVersion: v1
kind: Secret
metadata:
  name: db-credentials
  labels:
    app: demo-app
    component: database
type: Opaque
stringData:
  DB_HOST: "postgres.database.svc.cluster.local"
  DB_PORT: "5432"
  DB_NAME: "myapp_production"
  DB_USER: "app_user"
  DB_PASSWORD: "S3cur3P@ssw0rd!2024"
  DB_SSL_MODE: "require"
  connection-string: "postgresql://app_user:S3cur3P@ssw0rd!2024@postgres.database.svc.cluster.local:5432/myapp_production?sslmode=require"
```

### 2. Create an Opaque Secret with base64-encoded `data`

```yaml
# secret-api-keys.yaml
apiVersion: v1
kind: Secret
metadata:
  name: api-keys
type: Opaque
data:
  # echo -n 'sk-live-abc123def456' | base64
  API_KEY: c2stbGl2ZS1hYmMxMjNkZWY0NTY=
  # echo -n 'whsec_xyz789' | base64
  WEBHOOK_SECRET: d2hzZWNfeHl6Nzg5
```

### 3. Pod consuming Secrets as environment variables

```yaml
# pod-secret-env.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-secret-env
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Database Credentials ==="
          echo "DB_HOST=$DB_HOST"
          echo "DB_PORT=$DB_PORT"
          echo "DB_NAME=$DB_NAME"
          echo "DB_USER=$DB_USER"
          echo "DB_PASSWORD=***hidden***"
          echo ""
          echo "=== API Keys ==="
          echo "API_KEY=$API_KEY"
          echo "WEBHOOK_SECRET=***hidden***"
          sleep 3600
      # Inject all keys from the Secret
      envFrom:
        - secretRef:
            name: db-credentials
      # Inject specific keys from another Secret
      env:
        - name: API_KEY
          valueFrom:
            secretKeyRef:
              name: api-keys
              key: API_KEY
        - name: WEBHOOK_SECRET
          valueFrom:
            secretKeyRef:
              name: api-keys
              key: WEBHOOK_SECRET
  restartPolicy: Never
```

### 4. Pod consuming Secrets as volume-mounted files

```yaml
# pod-secret-volume.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-secret-volume
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Credential files ==="
          ls -la /etc/db-credentials/
          echo ""
          echo "=== DB_HOST ==="
          cat /etc/db-credentials/DB_HOST
          echo ""
          echo "=== DB_USER ==="
          cat /etc/db-credentials/DB_USER
          echo ""
          echo "=== File permissions ==="
          stat /etc/db-credentials/DB_PASSWORD
          sleep 3600
      volumeMounts:
        - name: db-creds-volume
          mountPath: /etc/db-credentials
          readOnly: true                  # always mount secrets read-only
  volumes:
    - name: db-creds-volume
      secret:
        secretName: db-credentials
        defaultMode: 0400                 # restrictive file permissions
  restartPolicy: Never
```

### 5. TLS Secret

```bash
# Generate a self-signed certificate (for development)
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout tls.key -out tls.crt \
  -subj "/CN=myapp.example.com/O=MyOrg"

# Create the TLS Secret from the generated files
kubectl create secret tls myapp-tls \
  --cert=tls.crt \
  --key=tls.key
```

### 6. Docker registry Secret

```bash
kubectl create secret docker-registry regcred \
  --docker-server=registry.example.com \
  --docker-username=deploy-user \
  --docker-password='D0ck3rP@ss!' \
  --docker-email=deploy@example.com
```

```yaml
# pod-private-registry.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-private-image
spec:
  containers:
    - name: app
      image: registry.example.com/myorg/myapp:latest
      ports:
        - containerPort: 8080
  imagePullSecrets:
    - name: regcred               # references the docker-registry Secret
  restartPolicy: Never
```

### 7. Apply the manifests

```bash
kubectl apply -f secret-db-credentials.yaml
kubectl apply -f secret-api-keys.yaml
kubectl apply -f pod-secret-env.yaml
kubectl apply -f pod-secret-volume.yaml
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Committing Secrets YAML to git | Secrets are base64-encoded, not encrypted -- trivially reversible | Use `stringData` locally, manage via Sealed Secrets or external vaults |
| Using `data` with plain text values | API rejects the value if it is not valid base64 | Use `stringData` for plain text or encode with `echo -n 'value' \| base64` |
| Forgetting `-n` in `echo` when encoding | A trailing newline is included in the base64 output | Always use `echo -n` |
| Mounting with `defaultMode: 0644` | Other users in the container can read the credentials | Use `defaultMode: 0400` for secrets |

## Verify

1. List Secrets (note: `describe` shows key names and byte sizes, not values):

```bash
kubectl get secret
kubectl describe secret db-credentials
```

2. Decode a Secret value:

```bash
kubectl get secret db-credentials -o jsonpath='{.data.DB_USER}' | base64 -d
kubectl get secret db-credentials -o jsonpath='{.data.DB_PASSWORD}' | base64 -d
```

3. Check environment variables in the pod:

```bash
kubectl exec pod-secret-env -- env | grep DB_
kubectl exec pod-secret-env -- env | grep API_KEY
```

4. Check volume-mounted files and permissions:

```bash
kubectl exec pod-secret-volume -- ls -la /etc/db-credentials/
kubectl exec pod-secret-volume -- cat /etc/db-credentials/DB_HOST
kubectl exec pod-secret-volume -- stat /etc/db-credentials/DB_PASSWORD
```

## Cleanup

```bash
kubectl delete pod pod-secret-env pod-secret-volume
kubectl delete secret db-credentials api-keys myapp-tls
```

## What's Next

Continue to [8.03 ConfigMaps from Files, Directories, and Env Files](../03-configmap-from-files-and-directories/) to explore imperative ConfigMap creation methods.

## Summary

- Secrets store sensitive data with base64 encoding in etcd.
- Use `stringData` for plain text input; use `data` with pre-encoded base64 values.
- Secrets can be injected as environment variables (`secretRef`, `secretKeyRef`) or as files (volume mount).
- TLS Secrets require `tls.crt` and `tls.key` keys; docker-registry Secrets store `.dockerconfigjson`.
- Always mount Secrets with `readOnly: true` and restrictive `defaultMode` (e.g., 0400).
- `kubectl describe secret` shows byte sizes but never reveals values.

## References

- [Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Managing Secrets Using kubectl](https://kubernetes.io/docs/tasks/configmap-secret/managing-secret-using-kubectl/)
- [Pull an Image from a Private Registry](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/)

## Additional Resources

- [Encrypting Secrets at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
