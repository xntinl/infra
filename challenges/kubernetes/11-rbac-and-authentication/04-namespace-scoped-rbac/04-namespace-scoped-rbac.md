# Exercise 4: Namespace-Scoped RBAC for Teams

<!--
difficulty: intermediate
concepts: [namespace-isolation, role, rolebinding, multi-team, least-privilege]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/03-rbac-verbs-and-resources]
-->

## Introduction

In shared clusters, different teams need access to their own namespaces without being able to interfere with each other. This exercise walks you through creating isolated RBAC configurations for two teams: **frontend** and **backend**. Each team gets full control of their own namespace and zero access to the other.

## Step-by-Step

### 1. Create team namespaces

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-frontend
  labels:
    team: frontend
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-backend
  labels:
    team: backend
```

### 2. Create ServiceAccounts for each team

```yaml
# serviceaccounts.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: frontend-deployer
  namespace: team-frontend
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: backend-deployer
  namespace: team-backend
```

### 3. Create Roles granting full workload management

```yaml
# roles.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: team-deployer
  namespace: team-frontend
rules:
  - apiGroups: ["", "apps", "batch"]
    resources: ["pods", "deployments", "services", "configmaps", "jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods/log", "pods/exec"]
    verbs: ["get", "create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: team-deployer
  namespace: team-backend
rules:
  - apiGroups: ["", "apps", "batch"]
    resources: ["pods", "deployments", "services", "configmaps", "jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods/log", "pods/exec"]
    verbs: ["get", "create"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "create", "update", "delete"]
```

### 4. Bind Roles to ServiceAccounts

```yaml
# rolebindings.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: frontend-deployer-binding
  namespace: team-frontend
subjects:
  - kind: ServiceAccount
    name: frontend-deployer
    namespace: team-frontend
roleRef:
  kind: Role
  name: team-deployer
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: backend-deployer-binding
  namespace: team-backend
subjects:
  - kind: ServiceAccount
    name: backend-deployer
    namespace: team-backend
roleRef:
  kind: Role
  name: team-deployer
  apiGroup: rbac.authorization.k8s.io
```

### 5. Deploy test workloads

```yaml
# frontend-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: team-frontend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
---
# backend-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  namespace: team-backend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

### 6. Apply

```bash
kubectl apply -f namespaces.yaml
kubectl apply -f serviceaccounts.yaml
kubectl apply -f roles.yaml
kubectl apply -f rolebindings.yaml
kubectl apply -f frontend-deploy.yaml
kubectl apply -f backend-deploy.yaml
```

## Verify

```bash
# Frontend deployer CAN manage its namespace
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:team-frontend:frontend-deployer \
  -n team-frontend
# Expected: yes

# Frontend deployer CANNOT access backend namespace
kubectl auth can-i list pods \
  --as=system:serviceaccount:team-frontend:frontend-deployer \
  -n team-backend
# Expected: no

# Backend deployer CAN manage secrets (extra permission)
kubectl auth can-i create secrets \
  --as=system:serviceaccount:team-backend:backend-deployer \
  -n team-backend
# Expected: yes

# Frontend deployer CANNOT manage secrets
kubectl auth can-i create secrets \
  --as=system:serviceaccount:team-frontend:frontend-deployer \
  -n team-frontend
# Expected: no

# List all permissions for each team
kubectl auth can-i --list \
  --as=system:serviceaccount:team-frontend:frontend-deployer \
  -n team-frontend

kubectl auth can-i --list \
  --as=system:serviceaccount:team-backend:backend-deployer \
  -n team-backend
```

## Cleanup

```bash
kubectl delete namespace team-frontend team-backend
```

## What's Next

The next exercise covers **ClusterRoles and Aggregated ClusterRoles** -- reusable permission sets that can be combined across namespaces.
