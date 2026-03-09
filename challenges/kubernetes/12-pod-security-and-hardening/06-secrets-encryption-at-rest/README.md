# Exercise 6: Secrets Encryption at Rest

<!--
difficulty: intermediate
concepts: [encryption-configuration, aescbc, secretbox, aesgcm, kms-provider, etcd, key-rotation]
tools: [kubectl, etcdctl, kind]
estimated_time: 35m
bloom_level: apply
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security]
-->

## Introduction

By default, Kubernetes stores Secrets in etcd as base64-encoded plaintext. Anyone with direct access to etcd can read every Secret in the cluster. **Encryption at rest** adds a layer of protection by encrypting Secret data before it is written to etcd.

Key concepts:

- **EncryptionConfiguration** -- defines which resources to encrypt and which providers (algorithms) to use
- **Providers**: `aescbc` (AES-CBC), `secretbox` (XSalsa20+Poly1305), `aesgcm` (AES-GCM, requires frequent rotation), `kms` (external Key Management Service)
- **identity** -- the no-encryption provider; placing it last allows reading unencrypted legacy data
- **Key rotation** -- adding a new key as the first provider and re-encrypting all Secrets

**Note**: This exercise requires control plane access (kubeadm or kind cluster). It does not apply directly to managed clusters (EKS, GKE, AKS) where the cloud provider handles encryption at rest.

## Step-by-Step

### 1. Generate an encryption key

```bash
ENCRYPTION_KEY=$(head -c 32 /dev/urandom | base64)
echo "Generated key: $ENCRYPTION_KEY"
```

### 2. Create the EncryptionConfiguration

```yaml
# encryption-config.yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - aescbc:
          keys:
            - name: key1
              secret: <BASE64_ENCODED_KEY>     # replace with $ENCRYPTION_KEY
      - identity: {}                            # fallback: read unencrypted data
```

Replace `<BASE64_ENCODED_KEY>` with the key generated in step 1.

### 3. Deploy to the control plane

```bash
# Copy to the control plane node
sudo cp encryption-config.yaml /etc/kubernetes/encryption-config.yaml
sudo chmod 600 /etc/kubernetes/encryption-config.yaml
```

### 4. Configure kube-apiserver

Add the encryption flag to the API server manifest:

```yaml
# /etc/kubernetes/manifests/kube-apiserver.yaml (relevant fragment)
apiVersion: v1
kind: Pod
metadata:
  name: kube-apiserver
  namespace: kube-system
spec:
  containers:
    - name: kube-apiserver
      command:
        - kube-apiserver
        # ... existing flags ...
        - --encryption-provider-config=/etc/kubernetes/encryption-config.yaml
      volumeMounts:
        # ... existing mounts ...
        - name: encryption-config
          mountPath: /etc/kubernetes/encryption-config.yaml
          readOnly: true
  volumes:
    # ... existing volumes ...
    - name: encryption-config
      hostPath:
        path: /etc/kubernetes/encryption-config.yaml
        type: File
```

### 5. Create test Secrets

```yaml
# secret-before.yaml
apiVersion: v1
kind: Secret
metadata:
  name: secret-before-encryption
  namespace: default
type: Opaque
data:
  username: YWRtaW4=
  password: c3VwZXJzZWNyZXQ=
```

```yaml
# secret-after.yaml
apiVersion: v1
kind: Secret
metadata:
  name: secret-after-encryption
  namespace: default
type: Opaque
data:
  username: YWRtaW4=
  password: ZW5jcnlwdGVkLXBhc3N3b3Jk
```

```bash
# Create a Secret BEFORE enabling encryption
kubectl apply -f secret-before.yaml

# After configuring the API server, wait for it to restart
kubectl wait --for=condition=ready pod \
  -l component=kube-apiserver -n kube-system --timeout=120s

# Create a Secret AFTER enabling encryption
kubectl apply -f secret-after.yaml
```

### 6. Key rotation configuration

```yaml
# encryption-config-rotated.yaml
apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - aescbc:
          keys:
            - name: key2
              secret: <NEW_BASE64_ENCODED_KEY>    # new key is first (used for writing)
            - name: key1
              secret: <OLD_BASE64_ENCODED_KEY>    # old key still present (for reading)
      - identity: {}
```

## Verify

```bash
# Confirm API server has the encryption flag
kubectl get pod kube-apiserver-* -n kube-system -o yaml | \
  grep encryption-provider-config

# Secrets are still accessible via kubectl
kubectl get secret secret-before-encryption -o yaml
kubectl get secret secret-after-encryption -o yaml

# Check etcd directly: new Secret should be encrypted
sudo ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/secret-after-encryption | hexdump -C | head -20
# Expected: prefix "k8s:enc:aescbc:v1:key1:" followed by binary data

# Check etcd: old Secret should still be plaintext
sudo ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/secret-before-encryption | hexdump -C | head -20

# Re-encrypt all existing Secrets
kubectl get secrets --all-namespaces -o json | kubectl replace -f -

# Verify old Secret is now encrypted
sudo ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/secret-before-encryption | hexdump -C | head -20
# Now shows "k8s:enc:aescbc:v1:key1:" prefix
```

## Cleanup

```bash
kubectl delete secret secret-before-encryption secret-after-encryption
```

## What's Next

The next exercise covers **AppArmor Profiles for Pods** -- mandatory access control that restricts what files and operations a container can access.
