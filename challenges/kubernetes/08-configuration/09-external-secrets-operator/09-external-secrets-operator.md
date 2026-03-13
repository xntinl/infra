<!--
difficulty: intermediate
concepts: [external-secrets-operator, aws-secrets-manager, gcp-secret-manager, azure-key-vault, secret-store, external-secret]
tools: [kubectl, helm, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [secrets-management, secret-types]
-->

# 8.09 External Secrets Operator with Cloud Providers

## What You Will Learn

- How External Secrets Operator (ESO) syncs secrets from cloud providers into Kubernetes Secrets
- The relationship between `SecretStore`, `ClusterSecretStore`, and `ExternalSecret` resources
- How to configure ESO for AWS Secrets Manager (with notes for GCP and Azure)
- How to set up automatic secret rotation and refresh intervals

## Steps

### 1. Install External Secrets Operator

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm repo update
helm install external-secrets external-secrets/external-secrets \
  --namespace external-secrets \
  --create-namespace
```

### 2. Create a SecretStore for AWS Secrets Manager

The SecretStore defines how ESO authenticates with the cloud provider.

```yaml
# secret-store-aws.yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: aws-secrets-manager
  namespace: default
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        secretRef:
          accessKeyIDSecretRef:
            name: aws-credentials
            key: access-key-id
          secretAccessKeySecretRef:
            name: aws-credentials
            key: secret-access-key
```

Prerequisite: create the AWS credentials Secret:

```bash
kubectl create secret generic aws-credentials \
  --from-literal=access-key-id=AKIAIOSFODNN7EXAMPLE \
  --from-literal=secret-access-key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

### 3. Create an ExternalSecret

The ExternalSecret tells ESO which remote secret to fetch and how to map it to a Kubernetes Secret.

```yaml
# external-secret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: db-credentials
  namespace: default
spec:
  refreshInterval: 1h                    # how often to sync from the provider
  secretStoreRef:
    name: aws-secrets-manager
    kind: SecretStore
  target:
    name: db-credentials                 # name of the K8s Secret to create
    creationPolicy: Owner                # ESO owns and manages the Secret
  data:
    - secretKey: DB_HOST                 # key in the K8s Secret
      remoteRef:
        key: prod/database              # path in AWS Secrets Manager
        property: host                   # JSON property within the secret

    - secretKey: DB_PORT
      remoteRef:
        key: prod/database
        property: port

    - secretKey: DB_PASSWORD
      remoteRef:
        key: prod/database
        property: password
```

### 4. Use the synced Secret in a pod

```yaml
# pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: app-with-external-secret
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "env | grep DB_ && sleep 3600"]
      envFrom:
        - secretRef:
            name: db-credentials        # created by ESO
  restartPolicy: Never
```

### 5. ClusterSecretStore for shared access

A `ClusterSecretStore` is cluster-scoped and can be referenced from any namespace.

```yaml
# cluster-secret-store.yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: global-aws-sm
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

### GCP Secret Manager variant

```yaml
spec:
  provider:
    gcpsm:
      projectID: my-gcp-project
      auth:
        secretRef:
          secretAccessKeySecretRef:
            name: gcp-credentials
            key: service-account-key
```

### Azure Key Vault variant

```yaml
spec:
  provider:
    azurekv:
      vaultUrl: "https://my-vault.vault.azure.net"
      authSecretRef:
        clientId:
          name: azure-credentials
          key: client-id
        clientSecret:
          name: azure-credentials
          key: client-secret
      tenantId: "my-tenant-id"
```

## Verify

```bash
# Check ESO is running
kubectl get pods -n external-secrets

# Check SecretStore status
kubectl get secretstore
kubectl describe secretstore aws-secrets-manager

# Check ExternalSecret sync status
kubectl get externalsecret
kubectl describe externalsecret db-credentials

# Verify the K8s Secret was created
kubectl get secret db-credentials -o yaml
```

## Cleanup

```bash
kubectl delete externalsecret db-credentials
kubectl delete secretstore aws-secrets-manager
kubectl delete secret aws-credentials db-credentials
kubectl delete pod app-with-external-secret
helm uninstall external-secrets -n external-secrets
kubectl delete namespace external-secrets
```

## What's Next

Continue to [8.10 Sealed Secrets for GitOps](../10-sealed-secrets/10-sealed-secrets.md) to learn how to encrypt Secrets so they can be safely stored in Git.

## Summary

- External Secrets Operator syncs secrets from cloud providers into native Kubernetes Secrets.
- `SecretStore` (namespaced) or `ClusterSecretStore` (cluster-wide) configures provider authentication.
- `ExternalSecret` maps remote secret properties to Kubernetes Secret keys.
- `refreshInterval` controls how often ESO re-syncs from the provider.
- Supports AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, HashiCorp Vault, and more.

## References

- [External Secrets Operator Documentation](https://external-secrets.io/)
- [AWS Secrets Manager Provider](https://external-secrets.io/latest/provider/aws-secrets-manager/)
- [GCP Secret Manager Provider](https://external-secrets.io/latest/provider/google-secrets-manager/)
