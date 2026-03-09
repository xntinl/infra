# Exercise 5: ClusterRoles and Aggregated ClusterRoles

<!--
difficulty: intermediate
concepts: [clusterrole, clusterrolebinding, aggregation-rule, label-selector, reusable-roles]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/03-rbac-verbs-and-resources]
-->

## Introduction

ClusterRoles can be **aggregated**: a parent ClusterRole automatically inherits rules from any ClusterRole carrying a matching label. This is how Kubernetes builds the built-in `admin`, `edit`, and `view` ClusterRoles. You can extend them or create your own aggregation hierarchies.

## Step-by-Step

### 1. Examine built-in aggregated roles

```bash
# The "admin" ClusterRole aggregates all ClusterRoles with this label
kubectl get clusterrole admin -o yaml | grep -A5 aggregationRule
```

### 2. Create base ClusterRoles with aggregation labels

```yaml
# clusterroles.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-pod-reader
  labels:
    rbac.example.com/aggregate-to-viewer: "true"   # will be aggregated
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-deployment-manager
  labels:
    rbac.example.com/aggregate-to-viewer: "true"
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-secret-reader
  labels:
    rbac.example.com/aggregate-to-admin: "true"    # different aggregation target
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
```

### 3. Create aggregated parent ClusterRoles

```yaml
# aggregated-roles.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-viewer
aggregationRule:
  clusterRoleSelectors:
    - matchLabels:
        rbac.example.com/aggregate-to-viewer: "true"
rules: []   # rules are auto-populated by the controller
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-admin
aggregationRule:
  clusterRoleSelectors:
    - matchLabels:
        rbac.example.com/aggregate-to-viewer: "true"
    - matchLabels:
        rbac.example.com/aggregate-to-admin: "true"
rules: []   # includes viewer rules + admin-specific rules
```

### 4. Bind to ServiceAccounts in a test namespace

```yaml
# bindings.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: agg-lab
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: viewer-sa
  namespace: agg-lab
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: admin-sa
  namespace: agg-lab
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: viewer-binding
  namespace: agg-lab
subjects:
  - kind: ServiceAccount
    name: viewer-sa
    namespace: agg-lab
roleRef:
  kind: ClusterRole
  name: custom-viewer
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: admin-binding
  namespace: agg-lab
subjects:
  - kind: ServiceAccount
    name: admin-sa
    namespace: agg-lab
roleRef:
  kind: ClusterRole
  name: custom-admin
  apiGroup: rbac.authorization.k8s.io
```

### 5. Apply

```bash
kubectl apply -f clusterroles.yaml
kubectl apply -f aggregated-roles.yaml
kubectl apply -f bindings.yaml
```

## Spot the Bug

This ClusterRole is meant to be aggregated into `custom-viewer`, but it is not working. Why?

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-service-reader
  labels:
    rbac.example.com/aggregate-to-Viewer: "true"
rules:
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list"]
```

<details>
<summary>Answer</summary>

The label key uses `aggregate-to-Viewer` (capital V) but the aggregation selector expects `aggregate-to-viewer` (lowercase v). Labels are case-sensitive in Kubernetes.

</details>

## Verify

```bash
# Inspect aggregated rules -- custom-viewer should contain pod + deployment rules
kubectl get clusterrole custom-viewer -o yaml

# custom-admin should contain pod + deployment + secret rules
kubectl get clusterrole custom-admin -o yaml

# viewer-sa can list pods (expect "yes")
kubectl auth can-i list pods \
  --as=system:serviceaccount:agg-lab:viewer-sa -n agg-lab

# viewer-sa cannot read secrets (expect "no")
kubectl auth can-i get secrets \
  --as=system:serviceaccount:agg-lab:viewer-sa -n agg-lab

# admin-sa can read secrets (expect "yes")
kubectl auth can-i get secrets \
  --as=system:serviceaccount:agg-lab:admin-sa -n agg-lab

# admin-sa can also list pods (inherits viewer rules)
kubectl auth can-i list pods \
  --as=system:serviceaccount:agg-lab:admin-sa -n agg-lab
```

## Cleanup

```bash
kubectl delete namespace agg-lab
kubectl delete clusterrole custom-pod-reader custom-deployment-manager \
  custom-secret-reader custom-viewer custom-admin
```

## What's Next

The next exercise covers **RBAC Debugging with kubectl auth can-i** -- systematic techniques for diagnosing permission issues.
