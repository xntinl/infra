# Exercise 1: RBAC: Roles and Bindings

<!--
difficulty: basic
concepts: [role, clusterrole, rolebinding, clusterrolebinding, serviceaccount, rbac]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: []
-->

## Introduction

Role-Based Access Control (RBAC) is the primary authorization mechanism in Kubernetes. It lets you define **who** (subjects) can do **what** (verbs) on **which resources**. RBAC uses four objects:

- **Role** -- grants permissions within a single namespace
- **ClusterRole** -- grants permissions cluster-wide or across all namespaces
- **RoleBinding** -- binds a Role or ClusterRole to subjects within a namespace
- **ClusterRoleBinding** -- binds a ClusterRole to subjects cluster-wide

## Why This Matters

Every production cluster must enforce least-privilege access. Without RBAC, any pod or user that reaches the API server can read Secrets, delete Deployments, or even escalate to cluster-admin. Understanding Roles and Bindings is the foundation for everything else in Kubernetes security.

RBAC is enabled by default in all modern Kubernetes distributions (1.8+). The API server flag `--authorization-mode=RBAC` activates it. When multiple authorization modes are configured (e.g., `Node,RBAC`), a request is authorized if any mode approves it.

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: rbac-lab            # dedicated namespace for this exercise
```

### 2. Create a ServiceAccount

```yaml
# serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: pod-reader           # identity that will be granted read access
  namespace: rbac-lab
```

### 3. Create a Role

```yaml
# role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader-role
  namespace: rbac-lab
rules:
  - apiGroups: [""]          # core API group (pods, services, secrets, etc.)
    resources: ["pods"]      # only pods -- not deployments, secrets, etc.
    verbs: ["get", "list"]   # read-only -- no create, update, delete
  - apiGroups: [""]
    resources: ["pods/log"]  # sub-resource: allows reading container logs
    verbs: ["get"]
```

### 4. Create a RoleBinding

```yaml
# rolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pod-reader-binding
  namespace: rbac-lab
subjects:
  - kind: ServiceAccount
    name: pod-reader           # must match the ServiceAccount name exactly
    namespace: rbac-lab        # must match the ServiceAccount namespace
roleRef:
  kind: Role                   # can also be ClusterRole
  name: pod-reader-role        # must match the Role name exactly
  apiGroup: rbac.authorization.k8s.io
```

### 5. Deploy a test pod

```yaml
# test-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: rbac-lab
  labels:
    app: test
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      ports:
        - containerPort: 80
```

### 6. Apply everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f serviceaccount.yaml
kubectl apply -f role.yaml
kubectl apply -f rolebinding.yaml
kubectl apply -f test-pod.yaml
```

## Common Mistakes

1. **Forgetting `namespace` in subjects** -- If you omit `namespace` from the ServiceAccount subject, the binding looks for the SA in the default namespace.
2. **Using RoleBinding with ClusterRole but expecting cluster-wide access** -- A RoleBinding scopes a ClusterRole down to a single namespace. You need a ClusterRoleBinding for cluster-wide access.
3. **Confusing `apiGroups: [""]` with `apiGroups: ["*"]`** -- The empty string `""` means the core API group only. The wildcard `"*"` means all API groups (dangerous).
4. **Typos in resource names** -- Kubernetes resources are plural (`pods`, not `pod`; `deployments`, not `deployment`).

## Verify

```bash
# Confirm all resources exist
kubectl get namespaces rbac-lab
kubectl get sa pod-reader -n rbac-lab
kubectl get roles -n rbac-lab
kubectl get rolebindings -n rbac-lab

# Verify the test pod is running
kubectl get pod test-pod -n rbac-lab

# Inspect the Role rules
kubectl describe role pod-reader-role -n rbac-lab

# Inspect the RoleBinding subjects and roleRef
kubectl describe rolebinding pod-reader-binding -n rbac-lab

# ServiceAccount CAN list pods (expect "yes")
kubectl auth can-i list pods \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab

# ServiceAccount CAN get pods (expect "yes")
kubectl auth can-i get pods \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab

# ServiceAccount CAN read pod logs (expect "yes")
kubectl auth can-i get pods/log \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab

# ServiceAccount CANNOT create pods (expect "no")
kubectl auth can-i create pods \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab

# ServiceAccount CANNOT delete pods (expect "no")
kubectl auth can-i delete pods \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab

# ServiceAccount has NO access in other namespaces (expect "no")
kubectl auth can-i list pods \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n default

# List ALL permissions the ServiceAccount has in this namespace
kubectl auth can-i --list \
  --as=system:serviceaccount:rbac-lab:pod-reader \
  -n rbac-lab
```

## Cleanup

```bash
kubectl delete namespace rbac-lab
```

## What's Next

In the next exercise you will explore **Service Accounts and Tokens** -- how pods get their identity and how projected tokens work in modern Kubernetes.

## Summary

- A **Role** defines a set of permissions (verbs on resources) within a namespace.
- A **ClusterRole** works the same way but applies cluster-wide.
- A **RoleBinding** connects a Role (or ClusterRole) to one or more subjects in a namespace.
- Subjects can be ServiceAccounts, users, or groups.
- `kubectl auth can-i` is the primary tool for verifying RBAC permissions.
- Always follow the principle of least privilege: grant only the verbs and resources actually needed.

## Reference

- [Using RBAC Authorization](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [RBAC Good Practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)

## Additional Resources

- [kubectl auth can-i](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_auth_can-i/)
- [Authorization Overview](https://kubernetes.io/docs/reference/access-authn-authz/authorization/)
