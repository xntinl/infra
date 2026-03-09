<!--
difficulty: intermediate
concepts: [immutable-configmap, immutable-secret, performance-optimization, change-protection]
tools: [kubectl]
estimated_time: 20m
bloom_level: apply
prerequisites: [configmaps-environment-and-files, secrets-management]
-->

# 8.06 Immutable ConfigMaps and Secrets

## What You Will Learn

- How to create immutable ConfigMaps and Secrets using the `immutable: true` field
- Why immutable resources improve cluster performance (reduced API server watch load)
- How immutable resources protect against accidental changes
- The delete-and-recreate workflow for updating immutable resources

## Steps

### 1. Create an immutable ConfigMap

```yaml
# immutable-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: db-config-v1
  labels:
    app: myapp
    version: v1
data:
  DB_HOST: "db.production.internal"
  DB_PORT: "5432"
  DB_POOL_SIZE: "20"
immutable: true                     # cannot be modified after creation
```

### 2. Create an immutable Secret

```yaml
# immutable-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: db-credentials-v1
  labels:
    app: myapp
    version: v1
type: Opaque
stringData:
  DB_USER: "prod_reader"
  DB_PASSWORD: "r3ad0nly-s3cret"
immutable: true
```

### 3. Try to modify them (expect failure)

```bash
kubectl apply -f immutable-configmap.yaml
kubectl apply -f immutable-secret.yaml

# Attempt to edit the ConfigMap
kubectl edit configmap db-config-v1
# Error: ConfigMap is immutable

# Attempt to patch the Secret
kubectl patch secret db-credentials-v1 --type merge -p '{"stringData":{"DB_PASSWORD":"new-pass"}}'
# Error: Secret is immutable
```

### 4. Versioned update workflow

Since immutable resources cannot be modified, use a versioned naming convention:

```yaml
# immutable-configmap-v2.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: db-config-v2
  labels:
    app: myapp
    version: v2
data:
  DB_HOST: "db.production.internal"
  DB_PORT: "5432"
  DB_POOL_SIZE: "50"              # increased pool size
immutable: true
```

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
        - name: app
          image: nginx:1.27
          envFrom:
            - configMapRef:
                name: db-config-v2    # update to v2 triggers a rolling update
            - secretRef:
                name: db-credentials-v1
```

Changing the ConfigMap reference in the Deployment triggers a rolling update, ensuring all pods pick up the new config.

### TODO: Create an immutable ConfigMap with a hash suffix

Many tools (Kustomize, Helm) append a content hash to the ConfigMap name so that any change produces a new name, automatically triggering pod restarts.

<details>
<summary>Hint</summary>

Use `kustomize` with a `configMapGenerator` that has `options.immutable: true`. Kustomize appends a hash suffix automatically.

</details>

<details>
<summary>Solution</summary>

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
configMapGenerator:
  - name: db-config
    literals:
      - DB_HOST=db.production.internal
      - DB_PORT=5432
    options:
      immutable: true
```

Running `kubectl kustomize .` produces a ConfigMap named `db-config-<hash>` with `immutable: true`.

</details>

## Verify

```bash
# Confirm immutability
kubectl get configmap db-config-v1 -o jsonpath='{.immutable}'
# Output: true

kubectl get secret db-credentials-v1 -o jsonpath='{.immutable}'
# Output: true

# Confirm edit fails
kubectl patch configmap db-config-v1 --type merge -p '{"data":{"DB_POOL_SIZE":"100"}}' 2>&1 | grep -i immutable
```

## Cleanup

```bash
kubectl delete configmap db-config-v1 db-config-v2 2>/dev/null
kubectl delete secret db-credentials-v1 2>/dev/null
kubectl delete deployment myapp 2>/dev/null
```

## What's Next

Continue to [8.07 SubPath Mounts for Selective File Injection](../07-subpath-mounts/) to learn how to inject individual files into existing directories.

## Summary

- Setting `immutable: true` prevents any modification after creation.
- Immutable resources reduce API server load because the kubelet does not need to watch for changes.
- Use versioned names (e.g., `config-v1`, `config-v2`) and update the Deployment reference to roll out changes.
- Kustomize can auto-generate hash-suffixed immutable ConfigMaps.

## References

- [Immutable ConfigMaps](https://kubernetes.io/docs/concepts/configuration/configmap/#configmap-immutable)
- [Immutable Secrets](https://kubernetes.io/docs/concepts/configuration/secret/#secret-immutable)
