<!--
difficulty: advanced
concepts: [sops, mozilla-sops, age-encryption, kms, gitops-secrets, encrypted-manifests]
tools: [sops, age, kubectl, kustomize]
estimated_time: 35m
bloom_level: analyze
prerequisites: [secrets-management, sealed-secrets]
-->

# 8.12 SOPS Encryption for Kubernetes Secrets

## Architecture

Mozilla SOPS (Secrets OPerationS) encrypts YAML and JSON files while preserving the file structure. Keys remain visible but values are encrypted, making diffs readable. SOPS supports multiple encryption backends: age, PGP, AWS KMS, GCP KMS, and Azure Key Vault.

```
  Developer Workstation              Git Repository              CI/CD Pipeline
  +------------------+            +------------------+        +------------------+
  | sops --encrypt   |--commit-->| secret.enc.yaml  |--sync->| sops --decrypt   |
  | (age/KMS key)    |           | (values encrypted,|       | kubectl apply    |
  +------------------+           |  keys visible)    |        +------------------+
                                 +------------------+
```

Unlike Sealed Secrets, SOPS:
- Encrypts/decrypts locally (no cluster-side controller needed for basic use)
- Preserves YAML structure (readable diffs)
- Supports multiple key management systems
- Can be integrated into Kustomize and Flux natively

## Suggested Steps

### 1. Install SOPS and age

```bash
# macOS
brew install sops age

# Linux
# Download from https://github.com/getsops/sops/releases
# and https://github.com/FiloSottile/age/releases
```

### 2. Generate an age key pair

```bash
age-keygen -o ~/.sops/age-key.txt
# Output: public key: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

export SOPS_AGE_KEY_FILE=~/.sops/age-key.txt
```

### 3. Create a `.sops.yaml` configuration

```yaml
# .sops.yaml (in the repository root)
creation_rules:
  - path_regex: \.enc\.yaml$
    age: >-
      age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

### 4. Create and encrypt a Secret manifest

```bash
# Create the plain Secret
cat > secret.yaml << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
type: Opaque
stringData:
  DB_USER: admin
  DB_PASSWORD: super-s3cret-password
  API_KEY: sk_live_abc123def456
EOF

# Encrypt it
sops --encrypt secret.yaml > secret.enc.yaml

# Delete the plaintext file
rm secret.yaml
```

The encrypted file preserves structure but encrypts values:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
type: Opaque
stringData:
  DB_USER: ENC[AES256_GCM,data:0mNqFw==,iv:...,tag:...,type:str]
  DB_PASSWORD: ENC[AES256_GCM,data:XnS0Y2FDh4...,iv:...,tag:...,type:str]
  API_KEY: ENC[AES256_GCM,data:m8RkJj...,iv:...,tag:...,type:str]
sops:
  age:
    - recipient: age1xxxx...
      enc: |
        -----BEGIN AGE ENCRYPTED FILE-----
        ...
```

### 5. Decrypt and apply

```bash
# Decrypt and apply in one step
sops --decrypt secret.enc.yaml | kubectl apply -f -

# Or edit in-place (decrypts, opens editor, re-encrypts on save)
sops secret.enc.yaml
```

### 6. Integration with Kustomize

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
generators:
  - secret-generator.yaml
```

```yaml
# secret-generator.yaml (using KSOPS plugin)
apiVersion: viaduct.ai/v1
kind: ksops
metadata:
  name: app-secrets
files:
  - secret.enc.yaml
```

### 7. Using AWS KMS instead of age

```yaml
# .sops.yaml with KMS
creation_rules:
  - path_regex: \.enc\.yaml$
    kms: arn:aws:kms:us-east-1:123456789012:key/mrk-abc123
```

```bash
sops --encrypt --kms arn:aws:kms:us-east-1:123456789012:key/mrk-abc123 secret.yaml > secret.enc.yaml
```

## Verify

```bash
# Verify encryption (values should be ENC[...])
cat secret.enc.yaml

# Verify decryption works
sops --decrypt secret.enc.yaml

# Verify the Secret was created in the cluster
kubectl get secret app-credentials
kubectl get secret app-credentials -o jsonpath='{.data.DB_USER}' | base64 -d
```

## Cleanup

```bash
kubectl delete secret app-credentials
rm -f secret.enc.yaml
```

## What's Next

Continue to [8.13 Dynamic Configuration Reload Without Restarts](../13-dynamic-config-reload-system/13-dynamic-config-reload-system.md) to build a system that reloads application configuration without pod restarts.

## Summary

- SOPS encrypts YAML values while preserving keys, making diffs readable in Git.
- age is the simplest encryption backend; AWS/GCP/Azure KMS provides team-level key management.
- The `.sops.yaml` file defines which encryption keys to use for which file paths.
- SOPS integrates with Kustomize (via KSOPS plugin) and Flux (native support).
- Unlike Sealed Secrets, SOPS does not require a cluster-side controller for basic encrypt/decrypt workflows.

## References

- [SOPS GitHub](https://github.com/getsops/sops)
- [age Encryption](https://github.com/FiloSottile/age)
- [KSOPS - Kustomize Plugin](https://github.com/viaduct-ai/kustomize-sops)
- [Flux SOPS Integration](https://fluxcd.io/flux/guides/mozilla-sops/)
