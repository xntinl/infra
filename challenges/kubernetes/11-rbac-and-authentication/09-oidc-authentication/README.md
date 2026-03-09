# Exercise 9: OIDC Authentication Integration

<!--
difficulty: advanced
concepts: [oidc, authentication, kube-apiserver-flags, id-token, groups-claim, kubeconfig]
tools: [kubectl, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/04-namespace-scoped-rbac]
-->

## Introduction

OpenID Connect (OIDC) is the recommended way to authenticate humans to Kubernetes. Instead of managing client certificates or static tokens, you delegate authentication to an identity provider (IdP) like Keycloak, Dex, Azure AD, or Google. The API server validates the OIDC ID token on every request and extracts the username and groups from its claims.

## Architecture

```
User (browser)
    |
    v
Identity Provider (Keycloak / Dex)
    |  issues ID token (JWT)
    v
kubectl (--token=<id-token> or via kubeconfig OIDC plugin)
    |
    v
kube-apiserver
    |  validates JWT signature against IdP JWKS
    |  extracts username from --oidc-username-claim
    |  extracts groups from --oidc-groups-claim
    v
RBAC (ClusterRoleBindings / RoleBindings match extracted user/groups)
```

## Suggested Steps

1. **Configure kube-apiserver with OIDC flags.** In a kind cluster, use the `kubeadmConfigPatches` to pass extra API server arguments:

```yaml
# kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: ClusterConfiguration
        apiServer:
          extraArgs:
            oidc-issuer-url: "https://idp.example.com/realms/k8s"
            oidc-client-id: "kubernetes"
            oidc-username-claim: "email"
            oidc-groups-claim: "groups"
            oidc-username-prefix: "oidc:"
            oidc-groups-prefix: "oidc:"
```

2. **Create RBAC bindings for OIDC groups.** Map IdP groups to Kubernetes ClusterRoles:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: oidc-cluster-admins
subjects:
  - kind: Group
    name: "oidc:platform-admins"     # group from IdP, with prefix
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: oidc-developers
  namespace: dev
subjects:
  - kind: Group
    name: "oidc:developers"
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: edit
  apiGroup: rbac.authorization.k8s.io
```

3. **Configure kubeconfig for OIDC authentication.** Users configure their kubeconfig with the OIDC auth provider:

```yaml
# kubeconfig snippet
users:
  - name: oidc-user
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1beta1
        command: kubectl
        args:
          - oidc-login
          - get-token
          - --oidc-issuer-url=https://idp.example.com/realms/k8s
          - --oidc-client-id=kubernetes
          - --oidc-client-secret=<client-secret>
```

4. **Test authentication with impersonation** (without a real IdP). Use `--as` and `--as-group` to simulate OIDC-authenticated requests:

```bash
# Simulate an OIDC user in the platform-admins group
kubectl auth can-i --list \
  --as="oidc:admin@example.com" \
  --as-group="oidc:platform-admins"

# Simulate a developer
kubectl auth can-i create deployments.apps \
  --as="oidc:dev@example.com" \
  --as-group="oidc:developers" \
  -n dev
```

5. **Verify token validation** by examining API server logs for OIDC-related entries.

## Verify

```bash
# Check API server flags (in a kind/kubeadm cluster)
kubectl get pod kube-apiserver-* -n kube-system -o yaml | grep oidc

# Simulate OIDC group-based access
kubectl auth can-i get pods \
  --as="oidc:user@example.com" \
  --as-group="oidc:developers" \
  -n dev

# Verify cluster-admin access for platform-admins group
kubectl auth can-i '*' '*' \
  --as="oidc:admin@example.com" \
  --as-group="oidc:platform-admins"
```

## Cleanup

```bash
kubectl delete clusterrolebinding oidc-cluster-admins 2>/dev/null
kubectl delete rolebinding oidc-developers -n dev 2>/dev/null
kubectl delete namespace dev 2>/dev/null
# If using kind: kind delete cluster
```

## What's Next

The next exercise covers **User Impersonation and Audit Logging** -- how to test access as other identities and track who did what.
