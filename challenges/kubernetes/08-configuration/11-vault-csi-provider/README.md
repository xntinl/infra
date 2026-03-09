<!--
difficulty: advanced
concepts: [hashicorp-vault, csi-driver, secrets-store-csi, vault-agent-injector, kubernetes-auth]
tools: [kubectl, helm, vault]
estimated_time: 45m
bloom_level: analyze
prerequisites: [secrets-management, external-secrets-operator]
-->

# 8.11 HashiCorp Vault CSI Provider Integration

## Architecture

HashiCorp Vault provides centralized secret management with fine-grained access control, audit logging, and dynamic secrets. The Secrets Store CSI Driver mounts Vault secrets as volumes in pods, while the Vault Agent Injector uses a sidecar to render secrets into shared volumes.

```
  Vault Server                     Kubernetes Cluster
  +------------+                 +-------------------------+
  |  KV engine |<--- auth ----->| Vault CSI Provider      |
  | secret/    |    (K8s SA)    | (DaemonSet)             |
  | data/      |                 +----------+--------------+
  +------------+                            |
                                   mounts secrets as files
                                            |
                                 +----------v--------------+
                                 | Pod                     |
                                 | /mnt/secrets-store/     |
                                 |   db-password            |
                                 |   api-key                |
                                 +-------------------------+
```

## Suggested Steps

### 1. Install Vault (for development)

```bash
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update

helm install vault hashicorp/vault \
  --namespace vault \
  --create-namespace \
  --set "server.dev.enabled=true" \
  --set "injector.enabled=true" \
  --set "csi.enabled=true"
```

### 2. Configure Vault with secrets and Kubernetes auth

```bash
# Port-forward to Vault
kubectl port-forward -n vault svc/vault 8200:8200 &

export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=root

# Write test secrets
vault kv put secret/db-credentials \
  username=app_user \
  password=s3cur3-db-p@ss

vault kv put secret/api-keys \
  stripe-key=sk_live_abc123 \
  webhook-secret=whsec_xyz789

# Enable Kubernetes auth
vault auth enable kubernetes

vault write auth/kubernetes/config \
  kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443"

# Create a policy
vault policy write app-policy - <<EOF
path "secret/data/db-credentials" {
  capabilities = ["read"]
}
path "secret/data/api-keys" {
  capabilities = ["read"]
}
EOF

# Create a role bound to a K8s service account
vault write auth/kubernetes/role/app-role \
  bound_service_account_names=app-sa \
  bound_service_account_namespaces=default \
  policies=app-policy \
  ttl=1h
```

### 3. Install Secrets Store CSI Driver

```bash
helm repo add secrets-store-csi-driver https://kubernetes-sigs.github.io/secrets-store-csi-driver/charts
helm install csi-secrets-store secrets-store-csi-driver/secrets-store-csi-driver \
  --namespace kube-system \
  --set syncSecret.enabled=true
```

### 4. Create a SecretProviderClass

```yaml
# secret-provider-class.yaml
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: vault-db-credentials
spec:
  provider: vault
  parameters:
    vaultAddress: "http://vault.vault:8200"
    roleName: "app-role"
    objects: |
      - objectName: "db-username"
        secretPath: "secret/data/db-credentials"
        secretKey: "username"
      - objectName: "db-password"
        secretPath: "secret/data/db-credentials"
        secretKey: "password"
  secretObjects:                          # optionally sync to a K8s Secret
    - secretName: db-credentials-synced
      type: Opaque
      data:
        - objectName: db-username
          key: DB_USER
        - objectName: db-password
          key: DB_PASSWORD
```

### 5. Create a ServiceAccount and Pod

```yaml
# sa-and-pod.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-sa
---
apiVersion: v1
kind: Pod
metadata:
  name: app-with-vault
spec:
  serviceAccountName: app-sa
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "cat /mnt/secrets-store/* && env | grep DB_ && sleep 3600"]
      volumeMounts:
        - name: secrets-store
          mountPath: /mnt/secrets-store
          readOnly: true
      envFrom:
        - secretRef:
            name: db-credentials-synced   # synced K8s Secret
  volumes:
    - name: secrets-store
      csi:
        driver: secrets-store.csi.k8s.io
        readOnly: true
        volumeAttributes:
          secretProviderClass: vault-db-credentials
  restartPolicy: Never
```

### 6. Alternative: Vault Agent Injector (annotation-based)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: app-vault-injector
  annotations:
    vault.hashicorp.com/agent-inject: "true"
    vault.hashicorp.com/role: "app-role"
    vault.hashicorp.com/agent-inject-secret-db: "secret/data/db-credentials"
    vault.hashicorp.com/agent-inject-template-db: |
      {{- with secret "secret/data/db-credentials" -}}
      DB_USER={{ .Data.data.username }}
      DB_PASSWORD={{ .Data.data.password }}
      {{- end }}
spec:
  serviceAccountName: app-sa
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "cat /vault/secrets/db && sleep 3600"]
```

## Verify

```bash
# CSI Driver pods running
kubectl get pods -n kube-system -l app=secrets-store-csi-driver

# Vault pods running
kubectl get pods -n vault

# Secret files mounted
kubectl exec app-with-vault -- ls /mnt/secrets-store/
kubectl exec app-with-vault -- cat /mnt/secrets-store/db-password

# Synced K8s Secret created
kubectl get secret db-credentials-synced -o yaml
```

## Cleanup

```bash
kubectl delete pod app-with-vault app-vault-injector 2>/dev/null
kubectl delete secretproviderclass vault-db-credentials
kubectl delete sa app-sa
helm uninstall vault -n vault
helm uninstall csi-secrets-store -n kube-system
kubectl delete namespace vault
```

## What's Next

Continue to [8.12 SOPS Encryption for Kubernetes Secrets](../12-sops-with-kubernetes/) to learn how to encrypt Secret manifests for Git storage using Mozilla SOPS.

## Summary

- The Secrets Store CSI Driver mounts secrets from Vault (and other providers) as files in pod volumes.
- SecretProviderClass defines which secrets to fetch and optionally syncs them to Kubernetes Secrets.
- Vault Agent Injector is an annotation-based alternative using sidecar containers.
- Kubernetes auth binds Vault roles to ServiceAccounts for fine-grained access control.

## References

- [Vault CSI Provider](https://developer.hashicorp.com/vault/docs/platform/k8s/csi)
- [Vault Agent Injector](https://developer.hashicorp.com/vault/docs/platform/k8s/injector)
- [Secrets Store CSI Driver](https://secrets-store-csi-driver.sigs.k8s.io/)
