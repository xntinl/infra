# Exercise 3: RBAC Verbs, Resources, and API Groups

<!--
difficulty: basic
concepts: [rbac-verbs, api-groups, resources, sub-resources, resourcenames]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings]
-->

## Introduction

Every RBAC rule is a combination of three fields: **apiGroups**, **resources**, and **verbs**. Understanding how these map to actual API requests is essential for writing precise, least-privilege Roles.

- **Verbs** map to HTTP methods: `get` (GET one), `list` (GET collection), `create` (POST), `update` (PUT), `patch` (PATCH), `delete` (DELETE), `watch` (GET with ?watch=true)
- **API Groups** identify which API a resource belongs to: `""` for core, `apps` for Deployments, `batch` for Jobs, etc.
- **Resources** are always plural: `pods`, `deployments`, `configmaps`
- **Sub-resources** use a slash: `pods/log`, `pods/exec`, `deployments/scale`

## Why This Matters

Overly broad RBAC rules (like granting `"*"` on all resources) create serious security holes. Precise rules that name exact verbs, resources, and API groups let you enforce least privilege -- the single most important principle in Kubernetes security.

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: rbac-verbs-lab
```

### 2. Create a ServiceAccount

```yaml
# serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-deployer
  namespace: rbac-verbs-lab
```

### 3. Create a Role with fine-grained rules

```yaml
# role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: app-deployer-role
  namespace: rbac-verbs-lab
rules:
  # Core API group: read pods and their logs
  - apiGroups: [""]                    # core API group (no prefix)
    resources: ["pods"]
    verbs: ["get", "list", "watch"]    # read-only
  - apiGroups: [""]
    resources: ["pods/log"]            # sub-resource for container logs
    verbs: ["get"]

  # Core API group: full CRUD on ConfigMaps
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "create", "update", "patch", "delete"]

  # Apps API group: manage Deployments
  - apiGroups: ["apps"]                # Deployments live in the "apps" group
    resources: ["deployments"]
    verbs: ["get", "list", "create", "update", "patch"]

  # Apps API group: scale Deployments (sub-resource)
  - apiGroups: ["apps"]
    resources: ["deployments/scale"]   # sub-resource for scaling
    verbs: ["get", "update", "patch"]

  # Batch API group: read-only on Jobs
  - apiGroups: ["batch"]               # Jobs and CronJobs live here
    resources: ["jobs"]
    verbs: ["get", "list"]

  # Restrict to a specific Secret by name
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["app-config"]      # only this one Secret
    verbs: ["get"]
```

### 4. Create the RoleBinding

```yaml
# rolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: app-deployer-binding
  namespace: rbac-verbs-lab
subjects:
  - kind: ServiceAccount
    name: app-deployer
    namespace: rbac-verbs-lab
roleRef:
  kind: Role
  name: app-deployer-role
  apiGroup: rbac.authorization.k8s.io
```

### 5. Create the named Secret

```yaml
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-config
  namespace: rbac-verbs-lab
type: Opaque
data:
  db-password: cGFzc3dvcmQxMjM=      # "password123" base64-encoded
```

### 6. Apply everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f serviceaccount.yaml
kubectl apply -f role.yaml
kubectl apply -f rolebinding.yaml
kubectl apply -f secret.yaml
```

## Common Mistakes

1. **Forgetting the API group** -- Deployments require `apiGroups: ["apps"]`, not `[""]`. Check with `kubectl api-resources` to find the correct group.
2. **Using singular resource names** -- It is `pods`, `deployments`, `configmaps` (plural), not `pod`, `deployment`, `configmap`.
3. **Confusing `resourceNames` scope** -- `resourceNames` restricts access to specific named objects but does not allow `list` (since list returns all objects).
4. **Missing sub-resources** -- `pods/exec` and `pods/log` are separate resources from `pods`. You must list them explicitly.

## Verify

```bash
# Discover API groups for common resources
kubectl api-resources --sort-by=name | head -30

# Check permissions: pods (expect "yes" for get, list, watch)
kubectl auth can-i get pods \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab
kubectl auth can-i list pods \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# Check permissions: pods/log (expect "yes")
kubectl auth can-i get pods/log \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# Check permissions: cannot delete pods (expect "no")
kubectl auth can-i delete pods \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# Check permissions: deployments in apps group (expect "yes")
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# Check permissions: deployments/scale (expect "yes" for update)
kubectl auth can-i update deployments.apps/scale \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# Check permissions: named secret (expect "yes" for get)
kubectl auth can-i get secrets \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab \
  --subresource="" 2>/dev/null

# Check permissions: cannot list all secrets (expect "no")
kubectl auth can-i list secrets \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab

# List all permissions for the SA
kubectl auth can-i --list \
  --as=system:serviceaccount:rbac-verbs-lab:app-deployer -n rbac-verbs-lab
```

## Cleanup

```bash
kubectl delete namespace rbac-verbs-lab
```

## What's Next

In the next exercise you will apply RBAC at scale with **Namespace-Scoped RBAC for Teams** -- isolating multiple teams within a shared cluster.

## Summary

- RBAC rules combine `apiGroups`, `resources`, and `verbs` to define precise permissions.
- Verbs map to HTTP methods: get, list, watch, create, update, patch, delete.
- API groups identify which API owns a resource (`""` for core, `apps`, `batch`, etc.).
- Sub-resources like `pods/log` and `deployments/scale` must be granted separately.
- `resourceNames` restricts access to specific named objects within a resource type.
- Use `kubectl api-resources` to discover the correct group and resource name.

## Reference

- [Using RBAC Authorization](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [API Groups](https://kubernetes.io/docs/reference/using-api/#api-groups)

## Additional Resources

- [kubectl api-resources](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_api-resources/)
- [kubectl auth can-i](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_auth_can-i/)
