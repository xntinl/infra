# Exercise 6: RBAC Debugging with kubectl auth can-i

<!--
difficulty: intermediate
concepts: [kubectl-auth-can-i, selfsubjectaccessreview, selfsubjectrulesreview, impersonation, rbac-troubleshooting]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/03-rbac-verbs-and-resources]
-->

## Introduction

When a pod gets a `403 Forbidden` or a user cannot perform an expected action, you need systematic ways to debug RBAC. Kubernetes provides `kubectl auth can-i` for point checks, `--list` for full permission listings, and the `SelfSubjectAccessReview` / `SelfSubjectRulesReview` APIs for programmatic checks.

## Step-by-Step

### 1. Set up a scenario with intentional gaps

```yaml
# setup.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: debug-lab
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: limited-sa
  namespace: debug-lab
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: limited-role
  namespace: debug-lab
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: limited-binding
  namespace: debug-lab
subjects:
  - kind: ServiceAccount
    name: limited-sa
    namespace: debug-lab
roleRef:
  kind: Role
  name: limited-role
  apiGroup: rbac.authorization.k8s.io
```

```bash
kubectl apply -f setup.yaml
```

### 2. Point checks with `can-i`

```bash
# Basic check: can the SA get pods?
kubectl auth can-i get pods \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab

# Check a specific resource by name
kubectl auth can-i get pods/my-pod \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab

# Check sub-resources
kubectl auth can-i get pods/log \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab

# Check across namespaces
kubectl auth can-i get pods \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n default
```

### 3. List all permissions

```bash
# Full permission listing for the SA in its namespace
kubectl auth can-i --list \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab
```

### 4. Check your own permissions

```bash
# What can *I* do in the debug-lab namespace?
kubectl auth can-i --list -n debug-lab

# Can I create ClusterRoles? (cluster-scoped check)
kubectl auth can-i create clusterroles
```

### 5. Use the SelfSubjectAccessReview API directly

```yaml
# access-review.yaml
apiVersion: authorization.k8s.io/v1
kind: SelfSubjectAccessReview
spec:
  resourceAttributes:
    namespace: debug-lab
    verb: create
    group: apps
    resource: deployments
```

```bash
kubectl create -f access-review.yaml -o yaml \
  --as=system:serviceaccount:debug-lab:limited-sa
```

### 6. Debugging checklist

When a permission is denied, check these in order:

```bash
# 1. Does the ServiceAccount exist?
kubectl get sa limited-sa -n debug-lab

# 2. Does a RoleBinding reference this SA?
kubectl get rolebindings -n debug-lab -o yaml | grep -A3 "subjects"

# 3. Does the referenced Role have the needed rule?
kubectl describe role limited-role -n debug-lab

# 4. Is the resource in the correct API group?
kubectl api-resources | grep deployments

# 5. Is there a ClusterRoleBinding that might also apply?
kubectl get clusterrolebindings -o yaml | \
  grep -B5 "limited-sa" 2>/dev/null
```

## TODO Exercise

The `limited-sa` currently cannot create Deployments. Add the minimum RBAC change to allow it to create and update Deployments in the `debug-lab` namespace, without modifying the existing Role.

<details>
<summary>Hint</summary>

Create a second Role with just the missing verbs and bind it to the same ServiceAccount. Alternatively, update the existing Role to add `create` and `update` to the deployments rule.

</details>

<details>
<summary>Solution</summary>

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: deployment-writer
  namespace: debug-lab
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["create", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: deployment-writer-binding
  namespace: debug-lab
subjects:
  - kind: ServiceAccount
    name: limited-sa
    namespace: debug-lab
roleRef:
  kind: Role
  name: deployment-writer
  apiGroup: rbac.authorization.k8s.io
```

</details>

## Verify

```bash
# After applying the fix, verify:
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab
# Expected: yes

kubectl auth can-i --list \
  --as=system:serviceaccount:debug-lab:limited-sa \
  -n debug-lab
```

## Cleanup

```bash
kubectl delete namespace debug-lab
```

## What's Next

The next exercise covers **Service Account Token Projection and Rotation** -- controlling token lifetimes and audiences for workload identity.
