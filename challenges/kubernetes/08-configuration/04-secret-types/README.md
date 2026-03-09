<!--
difficulty: basic
concepts: [secret-types, opaque-secret, tls-secret, docker-registry-secret, ssh-secret, basic-auth-secret]
tools: [kubectl, openssl]
estimated_time: 25m
bloom_level: understand
prerequisites: [secrets-management]
-->

# 8.04 Secret Types: Opaque, TLS, Docker Registry, SSH

## What You Will Learn

- The built-in Secret types in Kubernetes and when to use each
- How Kubernetes validates type-specific fields (e.g., `tls.crt` and `tls.key` for TLS)
- How to create each Secret type both imperatively and declaratively
- How each Secret type is consumed by pods and other resources

## Why This Matters

Kubernetes defines specific Secret types for common use cases. Using the correct type provides built-in validation (e.g., TLS Secrets must contain valid key pairs), clearer intent for operators reading the manifests, and compatibility with Kubernetes features like Ingress TLS termination and image pull authentication.

## Steps

### 1. Opaque Secret (generic key-value)

The default type for arbitrary user-defined data.

```yaml
# secret-opaque.yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
type: Opaque                     # default type, can be omitted
stringData:
  username: admin
  password: "s3cret-p@ssw0rd"
  api-token: "tok_abc123xyz"
```

```bash
# Imperative equivalent
kubectl create secret generic app-credentials \
  --from-literal=username=admin \
  --from-literal=password='s3cret-p@ssw0rd'
```

### 2. TLS Secret (`kubernetes.io/tls`)

Stores a TLS certificate and private key. Kubernetes validates that `tls.crt` and `tls.key` are present.

```bash
# Generate a self-signed certificate
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /tmp/tls.key -out /tmp/tls.crt \
  -subj "/CN=myapp.example.com"

# Create the Secret
kubectl create secret tls myapp-tls \
  --cert=/tmp/tls.crt \
  --key=/tmp/tls.key
```

```yaml
# Ingress referencing the TLS Secret
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
spec:
  tls:
    - hosts:
        - myapp.example.com
      secretName: myapp-tls       # Ingress reads tls.crt and tls.key
  rules:
    - host: myapp.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: myapp
                port:
                  number: 80
```

### 3. Docker registry Secret (`kubernetes.io/dockerconfigjson`)

Stores credentials for pulling images from private registries.

```bash
kubectl create secret docker-registry my-registry-creds \
  --docker-server=ghcr.io \
  --docker-username=my-user \
  --docker-password='ghp_abc123' \
  --docker-email=user@example.com
```

```yaml
# Pod using imagePullSecrets
apiVersion: v1
kind: Pod
metadata:
  name: private-app
spec:
  containers:
    - name: app
      image: ghcr.io/my-org/my-app:latest
  imagePullSecrets:
    - name: my-registry-creds    # references the docker-registry Secret
```

### 4. SSH auth Secret (`kubernetes.io/ssh-auth`)

Stores an SSH private key. Kubernetes validates that the `ssh-privatekey` key is present.

```bash
# Generate an SSH key pair (for demonstration)
ssh-keygen -t ed25519 -f /tmp/deploy-key -N "" -q

# Create the Secret
kubectl create secret generic deploy-ssh-key \
  --from-file=ssh-privatekey=/tmp/deploy-key \
  --type=kubernetes.io/ssh-auth
```

```yaml
# Pod using the SSH key
apiVersion: v1
kind: Pod
metadata:
  name: git-sync
spec:
  containers:
    - name: git-sync
      image: busybox:1.37
      command: ["sh", "-c", "ls -la /etc/ssh-keys/ && sleep 3600"]
      volumeMounts:
        - name: ssh-key
          mountPath: /etc/ssh-keys
          readOnly: true
  volumes:
    - name: ssh-key
      secret:
        secretName: deploy-ssh-key
        defaultMode: 0400             # SSH requires strict permissions
```

### 5. Basic auth Secret (`kubernetes.io/basic-auth`)

Stores a username and password. Kubernetes validates that `username` and `password` keys are present.

```yaml
# secret-basic-auth.yaml
apiVersion: v1
kind: Secret
metadata:
  name: basic-auth-creds
type: kubernetes.io/basic-auth
stringData:
  username: admin
  password: "my-password"
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Using type `kubernetes.io/tls` without `tls.crt` and `tls.key` | API rejects the Secret | Ensure both keys are present |
| Setting SSH key file permissions to 0644 | SSH clients refuse keys with open permissions | Use `defaultMode: 0400` |
| Using `Opaque` for all Secrets | Loses validation and semantic clarity | Use the specific type when one exists |
| Forgetting to create docker-registry Secret before the pod | Pod fails with `ImagePullBackOff` | Create the Secret first, or use a ServiceAccount with `imagePullSecrets` |

## Verify

```bash
# List all Secrets with their types
kubectl get secrets -o custom-columns='NAME:.metadata.name,TYPE:.type'

# Inspect a specific Secret
kubectl describe secret myapp-tls
kubectl describe secret my-registry-creds
kubectl describe secret deploy-ssh-key

# Decode a value
kubectl get secret app-credentials -o jsonpath='{.data.username}' | base64 -d
```

## Cleanup

```bash
kubectl delete secret app-credentials myapp-tls my-registry-creds deploy-ssh-key basic-auth-creds 2>/dev/null
kubectl delete pod private-app git-sync 2>/dev/null
rm -f /tmp/tls.key /tmp/tls.crt /tmp/deploy-key /tmp/deploy-key.pub
```

## What's Next

Continue to [8.05 ConfigMap Volume Updates and Propagation](../05-configmap-volume-updates/) to understand how Kubernetes propagates configuration changes to running pods.

## Summary

- `Opaque` is the default type for arbitrary key-value data.
- `kubernetes.io/tls` validates the presence of `tls.crt` and `tls.key` and integrates with Ingress.
- `kubernetes.io/dockerconfigjson` stores registry credentials used by `imagePullSecrets`.
- `kubernetes.io/ssh-auth` validates the `ssh-privatekey` key and should be mounted with `defaultMode: 0400`.
- `kubernetes.io/basic-auth` validates `username` and `password` keys.
- Using the correct type improves validation, readability, and integration with Kubernetes features.

## References

- [Secret Types](https://kubernetes.io/docs/concepts/configuration/secret/#secret-types)
- [Pull an Image from a Private Registry](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/)

## Additional Resources

- [TLS Secrets with cert-manager](https://cert-manager.io/docs/)
- [Encrypting Secrets at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
