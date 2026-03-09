<!--
difficulty: advanced
concepts: [sealed-secrets, gitops, asymmetric-encryption, kubeseal, secret-encryption-at-rest]
tools: [kubectl, kubeseal, helm]
estimated_time: 35m
bloom_level: analyze
prerequisites: [secrets-management, secret-types]
-->

# 8.10 Sealed Secrets for GitOps

## Architecture

Standard Kubernetes Secrets are base64-encoded, not encrypted -- they cannot be safely committed to Git. Sealed Secrets solves this by encrypting Secrets with a public key. Only the Sealed Secrets controller in the cluster has the private key to decrypt them.

```
Developer                          Git Repo                     Cluster
+----------+                    +-----------+              +------------------+
| kubeseal |--- encrypts ------>| SealedSecret |--- sync -->| Sealed Secrets   |
| (public  |    with public     | (safe to  |   (ArgoCD/   | Controller       |
|  key)    |    key              | commit)   |    Flux)     | (private key)    |
+----------+                    +-----------+              +--------+---------+
                                                                    |
                                                           decrypts into
                                                                    |
                                                           +--------v---------+
                                                           | Kubernetes Secret|
                                                           +------------------+
```

## Suggested Steps

### 1. Install Sealed Secrets controller

```bash
helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
helm repo update
helm install sealed-secrets sealed-secrets/sealed-secrets \
  --namespace kube-system
```

### 2. Install the `kubeseal` CLI

```bash
# macOS
brew install kubeseal

# Linux
wget https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.27.0/kubeseal-0.27.0-linux-amd64.tar.gz
tar -xvzf kubeseal-0.27.0-linux-amd64.tar.gz
sudo install -m 755 kubeseal /usr/local/bin/kubeseal
```

### 3. Create a regular Secret, then seal it

```bash
# Create the Secret manifest (do NOT apply it)
kubectl create secret generic db-credentials \
  --from-literal=DB_USER=admin \
  --from-literal=DB_PASSWORD='super-s3cret' \
  --dry-run=client -o yaml > secret.yaml

# Seal it with kubeseal
kubeseal --format yaml < secret.yaml > sealed-secret.yaml

# The sealed-secret.yaml is safe to commit to Git
cat sealed-secret.yaml
```

The output is a `SealedSecret` resource with encrypted `encryptedData`:

```yaml
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: db-credentials
  namespace: default
spec:
  encryptedData:
    DB_USER: AgBy3i4OJSWK+PiTySYZZA9rO...
    DB_PASSWORD: AgBu7kRfTOPL+qZmN3w...
  template:
    metadata:
      name: db-credentials
      namespace: default
    type: Opaque
```

### 4. Apply the SealedSecret

```bash
kubectl apply -f sealed-secret.yaml

# The controller decrypts it into a regular Secret
kubectl get secret db-credentials
kubectl get secret db-credentials -o jsonpath='{.data.DB_USER}' | base64 -d
```

### 5. Scope modes

Sealed Secrets supports three scoping modes that control where the SealedSecret can be decrypted:

- **`strict`** (default) -- bound to a specific name and namespace
- **`namespace-wide`** -- can be renamed within the same namespace
- **`cluster-wide`** -- can be used in any namespace

```bash
# Namespace-wide scope
kubeseal --format yaml --scope namespace-wide < secret.yaml > sealed-ns.yaml

# Cluster-wide scope
kubeseal --format yaml --scope cluster-wide < secret.yaml > sealed-cluster.yaml
```

### 6. Key rotation and backup

```bash
# Fetch the public key (for offline sealing)
kubeseal --fetch-cert > sealed-secrets-pub.pem

# Backup the private key (critical for disaster recovery)
kubectl get secret -n kube-system -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-backup.yaml
```

## Verify

```bash
# Confirm controller is running
kubectl get pods -n kube-system -l app.kubernetes.io/name=sealed-secrets

# Confirm SealedSecret was processed
kubectl get sealedsecret
kubectl describe sealedsecret db-credentials

# Confirm the K8s Secret was created
kubectl get secret db-credentials -o yaml

# Decode to verify
kubectl get secret db-credentials -o jsonpath='{.data.DB_PASSWORD}' | base64 -d
```

## Cleanup

```bash
kubectl delete sealedsecret db-credentials
kubectl delete secret db-credentials
helm uninstall sealed-secrets -n kube-system
rm -f secret.yaml sealed-secret.yaml
```

## What's Next

Continue to [8.11 HashiCorp Vault CSI Provider Integration](../11-vault-csi-provider/) to learn how to inject secrets directly from Vault into pods.

## Summary

- Sealed Secrets encrypts Kubernetes Secrets so they can be safely stored in Git repositories.
- Only the controller's private key can decrypt a SealedSecret -- developers only need the public key.
- Scope modes (strict, namespace-wide, cluster-wide) control where a SealedSecret can be decrypted.
- Back up the controller's private key -- losing it means losing the ability to decrypt existing SealedSecrets.

## References

- [Sealed Secrets GitHub](https://github.com/bitnami-labs/sealed-secrets)
- [Sealed Secrets Documentation](https://sealed-secrets.netlify.app/)
