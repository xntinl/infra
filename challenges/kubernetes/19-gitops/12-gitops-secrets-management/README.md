<!--
difficulty: advanced
concepts: [gitops-secrets, sops, sealed-secrets, external-secrets-operator, age-encryption, secret-rotation]
tools: [kubectl, kubeseal, sops, age]
estimated_time: 45m
bloom_level: analyze
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/03-flux-basics]
-->

# 19.12 - GitOps Secrets Management: SOPS + Sealed Secrets

## Architecture

```
GitOps Secret Strategies
=========================

  Option A: Sealed Secrets
  ┌──────────┐    ┌──────────────┐    ┌──────────────┐
  │ kubeseal  │───►│ SealedSecret │───►│ Sealed       │
  │ (client)  │    │ (encrypted   │    │ Secrets      │
  │           │    │  in Git)     │    │ Controller   │
  └──────────┘    └──────────────┘    │ (decrypts    │
                                       │  in-cluster) │
                                       └──────┬───────┘
                                              ▼
                                       Regular Secret

  Option B: SOPS + Flux
  ┌──────────┐    ┌──────────────┐    ┌──────────────┐
  │ sops     │───►│ Encrypted    │───►│ Flux         │
  │ encrypt  │    │ Secret YAML  │    │ Kustomize    │
  │ (age/pgp)│    │ (in Git)     │    │ Controller   │
  └──────────┘    └──────────────┘    │ (decrypts    │
                                       │  before      │
                                       │  applying)   │
                                       └──────┬───────┘
                                              ▼
                                       Regular Secret

  Option C: External Secrets Operator
  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
  │ AWS Secrets  │    │ ExternalSecret│───►│ ESO          │
  │ Manager /    │◄───│ (in Git,     │    │ Controller   │
  │ Vault /      │    │  no secret   │    │ (fetches     │
  │ Azure KV     │    │  data)       │    │  from store) │
  └──────────────┘    └──────────────┘    └──────┬───────┘
                                                  ▼
                                           Regular Secret
```

Storing secrets in Git is the fundamental challenge of GitOps. Three proven approaches exist: **Sealed Secrets** encrypts secrets client-side with the cluster's public key; **SOPS** encrypts specific YAML fields using age/PGP/KMS keys; and **External Secrets Operator** references secrets stored in external vaults without ever putting secret data in Git.

## Suggested Steps

### Strategy A: Sealed Secrets

```bash
# Install Sealed Secrets controller
helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
helm install sealed-secrets sealed-secrets/sealed-secrets -n kube-system --wait

# Install kubeseal CLI
brew install kubeseal
```

```bash
# Create a regular Secret
kubectl create secret generic db-creds \
  --from-literal=username=admin \
  --from-literal=password=s3cr3t \
  --dry-run=client -o yaml > secret.yaml

# Seal it with the cluster's public key
kubeseal --format yaml < secret.yaml > sealed-secret.yaml
```

```yaml
# sealed-secret.yaml (safe to commit to Git)
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: db-creds
  namespace: default
spec:
  encryptedData:
    username: AgBz...   # encrypted with cluster's public key
    password: AgCx...
  template:
    type: Opaque
```

### Strategy B: SOPS with age Encryption

```bash
# Install sops and age
brew install sops age

# Generate an age key pair
age-keygen -o age.key
export SOPS_AGE_KEY_FILE=$(pwd)/age.key
AGE_PUBLIC_KEY=$(age-keygen -y age.key)
```

Create a `.sops.yaml` configuration:

```yaml
# .sops.yaml
creation_rules:
  - path_regex: .*\.enc\.yaml$
    encrypted_regex: "^(data|stringData)$"
    age: >-
      age1ql3z...your-public-key...
```

```bash
# Create a plain Secret
cat > secret.yaml <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: db-creds
  namespace: default
type: Opaque
stringData:
  username: admin
  password: s3cr3t
EOF

# Encrypt with SOPS
sops --encrypt secret.yaml > secret.enc.yaml

# Decrypt (for verification)
sops --decrypt secret.enc.yaml
```

Configure Flux to decrypt SOPS secrets:

```yaml
# flux-kustomization-with-sops.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app-secrets
  namespace: flux-system
spec:
  interval: 10m
  sourceRef:
    kind: GitRepository
    name: flux-system
  path: ./secrets
  prune: true
  decryption:
    provider: sops
    secretRef:
      name: sops-age                       # Secret containing the age private key
```

```bash
# Create the age key Secret for Flux
kubectl create secret generic sops-age \
  --namespace flux-system \
  --from-file=age.agekey=age.key
```

### Strategy C: External Secrets Operator

```bash
# Install ESO
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets \
  -n external-secrets --create-namespace --wait
```

```yaml
# cluster-secret-store.yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: aws-secrets-manager
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        jwt:
          serviceAccountRef:
            name: external-secrets-sa
            namespace: external-secrets
```

```yaml
# external-secret.yaml (safe to commit -- contains no secret data)
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: db-creds
  namespace: default
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: db-creds
    creationPolicy: Owner
  data:
    - secretKey: username
      remoteRef:
        key: /myapp/db-credentials
        property: username
    - secretKey: password
      remoteRef:
        key: /myapp/db-credentials
        property: password
```

## Verify

```bash
# Sealed Secrets
kubectl get sealedsecret db-creds
kubectl get secret db-creds -o jsonpath='{.data.password}' | base64 -d && echo ""

# SOPS
sops --decrypt secret.enc.yaml | grep password
flux get kustomization app-secrets

# External Secrets
kubectl get externalsecret db-creds
kubectl get secret db-creds -o jsonpath='{.data.password}' | base64 -d && echo ""
```

## Cleanup

```bash
# Sealed Secrets
helm uninstall sealed-secrets -n kube-system
kubectl delete sealedsecret db-creds

# SOPS
kubectl delete secret sops-age -n flux-system
rm age.key secret.yaml secret.enc.yaml

# External Secrets
helm uninstall external-secrets -n external-secrets
kubectl delete namespace external-secrets
kubectl delete externalsecret db-creds
kubectl delete clustersecretstore aws-secrets-manager
```

## References

- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
- [Mozilla SOPS](https://github.com/getsops/sops)
- [Flux SOPS Integration](https://fluxcd.io/flux/guides/mozilla-sops/)
- [External Secrets Operator](https://external-secrets.io/)
