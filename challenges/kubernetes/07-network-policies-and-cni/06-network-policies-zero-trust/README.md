<!--
difficulty: intermediate
concepts: [zero-trust, microsegmentation, default-deny, whitelist-pattern, three-tier-architecture]
tools: [kubectl, network-policy-cni]
estimated_time: 35m
bloom_level: apply
prerequisites: [network-policies-pod-isolation, default-deny-policies, ingress-network-policies, egress-network-policies]
-->

# 7.06 Network Policies: Zero Trust Architecture

## What You Will Learn

- How to implement a zero-trust networking model with deny-all as the baseline
- How to build whitelist policies for a three-tier application (frontend, backend, database)
- How to ensure every traffic flow has both an egress and ingress rule
- How to allow DNS resolution in a fully locked-down namespace

**Note**: Network Policies require a CNI plugin that supports them (Calico, Cilium, Weave Net). The default CNI in some clusters (e.g., kind with kindnet) does not enforce them.

## Steps

### 1. Create the namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: zero-trust
  labels:
    name: zero-trust
```

### 2. Deploy the three tiers

```yaml
# frontend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: zero-trust
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frontend
      tier: frontend
  template:
    metadata:
      labels:
        app: frontend
        tier: frontend
    spec:
      containers:
        - name: frontend
          image: nginx:1.27
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: frontend
  namespace: zero-trust
spec:
  selector:
    app: frontend
  ports:
    - port: 80
      targetPort: 80
```

```yaml
# backend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: zero-trust
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
      tier: backend
  template:
    metadata:
      labels:
        app: backend
        tier: backend
    spec:
      containers:
        - name: backend
          image: nginx:1.27
          ports:
            - containerPort: 8080
          command:
            - sh
            - -c
            - |
              sed -i 's/listen  *80;/listen 8080;/' /etc/nginx/conf.d/default.conf
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: backend
  namespace: zero-trust
spec:
  selector:
    app: backend
  ports:
    - port: 8080
      targetPort: 8080
```

```yaml
# database.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: database
  namespace: zero-trust
spec:
  replicas: 1
  selector:
    matchLabels:
      app: database
      tier: database
  template:
    metadata:
      labels:
        app: database
        tier: database
    spec:
      containers:
        - name: database
          image: postgres:16
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_PASSWORD
              value: "testpassword"
            - name: POSTGRES_DB
              value: "appdb"
---
apiVersion: v1
kind: Service
metadata:
  name: database
  namespace: zero-trust
spec:
  selector:
    app: database
  ports:
    - port: 5432
      targetPort: 5432
```

### 3. Apply the default deny-all baseline

```yaml
# deny-all.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: zero-trust
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
```

### 4. Allow DNS for all pods

```yaml
# allow-dns.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: zero-trust
spec:
  podSelector: {}
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

### 5. Frontend to backend (egress + ingress pair)

```yaml
# allow-frontend-to-backend.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-frontend-egress-to-backend
  namespace: zero-trust
spec:
  podSelector:
    matchLabels:
      tier: frontend
  policyTypes:
    - Egress
  egress:
    - to:
        - podSelector:
            matchLabels:
              tier: backend
      ports:
        - protocol: TCP
          port: 8080
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-backend-ingress-from-frontend
  namespace: zero-trust
spec:
  podSelector:
    matchLabels:
      tier: backend
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              tier: frontend
      ports:
        - protocol: TCP
          port: 8080
```

### 6. Backend to database (egress + ingress pair)

```yaml
# allow-backend-to-database.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-backend-egress-to-database
  namespace: zero-trust
spec:
  podSelector:
    matchLabels:
      tier: backend
  policyTypes:
    - Egress
  egress:
    - to:
        - podSelector:
            matchLabels:
              tier: database
      ports:
        - protocol: TCP
          port: 5432
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-database-ingress-from-backend
  namespace: zero-trust
spec:
  podSelector:
    matchLabels:
      tier: database
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              tier: backend
      ports:
        - protocol: TCP
          port: 5432
```

### Apply everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f frontend.yaml
kubectl apply -f backend.yaml
kubectl apply -f database.yaml

# Wait for pods
kubectl wait --for=condition=ready pod --all -n zero-trust --timeout=120s

# Apply policies
kubectl apply -f deny-all.yaml
kubectl apply -f allow-dns.yaml
kubectl apply -f allow-frontend-to-backend.yaml
kubectl apply -f allow-backend-to-database.yaml
```

## Verify

```bash
# All policies created
kubectl get networkpolicies -n zero-trust

# Frontend CAN reach backend
kubectl exec -n zero-trust deploy/frontend -- curl -s --connect-timeout 5 http://backend:8080

# Frontend CANNOT reach database (blocked)
kubectl exec -n zero-trust deploy/frontend -- curl -s --connect-timeout 5 http://database:5432 2>&1 || echo "Blocked"

# Backend CAN reach database
kubectl exec -n zero-trust deploy/backend -- sh -c "echo 'SELECT 1' | nc -w 3 database 5432" 2>&1 || echo "Connection made"

# Database CANNOT reach frontend (blocked)
kubectl exec -n zero-trust deploy/database -- bash -c 'timeout 5 bash -c "echo > /dev/tcp/frontend/80"' 2>&1 || echo "Blocked"

# Database CANNOT reach backend (blocked)
kubectl exec -n zero-trust deploy/database -- bash -c 'timeout 5 bash -c "echo > /dev/tcp/backend/8080"' 2>&1 || echo "Blocked"

# DNS works from all pods
kubectl exec -n zero-trust deploy/frontend -- nslookup backend.zero-trust.svc.cluster.local
kubectl exec -n zero-trust deploy/backend -- nslookup database.zero-trust.svc.cluster.local
```

## Cleanup

```bash
kubectl delete namespace zero-trust
```

## What's Next

Continue to [7.07 Namespace Isolation Patterns](../07-namespace-isolation-patterns/) to scale zero-trust principles across multiple namespaces.

## Summary

- Zero-trust starts with a deny-all policy that blocks all ingress and egress.
- Each permitted traffic flow requires a pair of policies: an egress rule on the source and an ingress rule on the destination.
- DNS egress must be explicitly allowed or all service name resolution fails.
- Using tier labels (`tier: frontend`, `tier: backend`, `tier: database`) makes policies clear and maintainable.

## References

- [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Declare Network Policy](https://kubernetes.io/docs/tasks/administer-cluster/declare-network-policy/)
- [Network Policy Editor (Cilium)](https://editor.networkpolicy.io/)
