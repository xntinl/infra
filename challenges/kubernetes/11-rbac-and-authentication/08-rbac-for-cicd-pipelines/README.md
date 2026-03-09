# Exercise 8: RBAC for CI/CD Service Accounts

<!--
difficulty: advanced
concepts: [cicd-rbac, service-account, clusterrole, namespace-scoping, deployment-automation, least-privilege-pipeline]
tools: [kubectl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/04-namespace-scoped-rbac]
-->

## Introduction

CI/CD pipelines need Kubernetes access to deploy workloads, but giving them `cluster-admin` is a common and dangerous anti-pattern. This exercise designs a least-privilege RBAC configuration for a CI/CD service account that can deploy to specific namespaces, roll back Deployments, and monitor rollout status -- without access to Secrets, RBAC objects, or other namespaces.

## Architecture

```
CI/CD Pipeline
    |
    v
ServiceAccount: cicd-deployer (namespace: cicd-system)
    |
    +-- RoleBinding in "staging" namespace
    |       -> ClusterRole: cicd-deploy-role
    |       Permissions: deployments, services, configmaps (full CRUD)
    |                    pods, replicasets (read-only)
    |                    deployments/rollback, deployments/scale (update)
    |
    +-- RoleBinding in "production" namespace
    |       -> ClusterRole: cicd-deploy-role
    |       (same ClusterRole reused across namespaces)
    |
    +-- ClusterRoleBinding
            -> ClusterRole: cicd-readonly-cluster
            Permissions: namespaces (list), nodes (list) -- read-only cluster info
```

## Suggested Steps

1. Create namespaces `cicd-system`, `staging`, and `production`.
2. Create a ServiceAccount `cicd-deployer` in `cicd-system`.
3. Create a ClusterRole `cicd-deploy-role` with permissions to manage Deployments, Services, and ConfigMaps, plus read-only access to Pods and ReplicaSets.
4. Include sub-resource access for `deployments/scale` and `deployments/rollback`.
5. Create RoleBindings in both `staging` and `production` that reference the ClusterRole and the cross-namespace ServiceAccount.
6. Create a ClusterRole `cicd-readonly-cluster` with read-only access to namespaces and nodes.
7. Create a ClusterRoleBinding for the cluster-level read access.
8. Verify the SA can deploy to staging and production but cannot read Secrets, modify RBAC, or access other namespaces.

Key YAML structures to use:

```yaml
# Cross-namespace subject reference in a RoleBinding
subjects:
  - kind: ServiceAccount
    name: cicd-deployer
    namespace: cicd-system      # SA lives in a different namespace
```

```yaml
# ClusterRole with deployment management
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments/scale", "deployments/rollback"]
    verbs: ["update", "patch"]
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["services", "configmaps"]
    verbs: ["get", "list", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
```

## Verify

```bash
# Can deploy to staging
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n staging

# Can deploy to production
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n production

# Cannot read secrets
kubectl auth can-i get secrets \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n staging

# Cannot modify RBAC
kubectl auth can-i create roles \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n staging

# Cannot access other namespaces for workloads
kubectl auth can-i list pods \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n kube-system

# Can list namespaces (cluster-level read)
kubectl auth can-i list namespaces \
  --as=system:serviceaccount:cicd-system:cicd-deployer

# Full permission listing
kubectl auth can-i --list \
  --as=system:serviceaccount:cicd-system:cicd-deployer \
  -n staging
```

## Cleanup

```bash
kubectl delete namespace cicd-system staging production
kubectl delete clusterrole cicd-deploy-role cicd-readonly-cluster
kubectl delete clusterrolebinding cicd-readonly-cluster-binding
```

## What's Next

The next exercise covers **OIDC Authentication Integration** -- connecting external identity providers to your Kubernetes cluster.
