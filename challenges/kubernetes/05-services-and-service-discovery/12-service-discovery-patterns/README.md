# 12. Cross-Namespace Service Discovery Patterns

<!--
difficulty: advanced
concepts: [cross-namespace, externalname-proxy, service-delegation, namespace-isolation]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [05-05, 05-08]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05](../05-dns-and-service-discovery/) and [exercise 08](../08-externalname-services/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** different patterns for cross-namespace service discovery
- **Design** namespace-scoped service delegation using ExternalName proxies
- **Evaluate** tradeoffs between direct FQDN references and local namespace aliases

## Architecture

```
  Namespace: team-frontend           Namespace: team-backend
  ┌─────────────────────┐           ┌─────────────────────┐
  │ frontend-app        │           │ api-server           │
  │   connects to:      │           │   (Deployment)       │
  │   "backend-api"     │           │                      │
  │                     │           │ api-svc              │
  │ backend-api (svc)   │──CNAME──▶│   (ClusterIP)        │
  │  type: ExternalName │           │                      │
  │  externalName:      │           └─────────────────────┘
  │   api-svc.team-     │
  │   backend.svc.      │           Namespace: shared-services
  │   cluster.local     │           ┌─────────────────────┐
  │                     │           │ redis (StatefulSet)  │
  │ cache (svc)         │──CNAME──▶│ redis-svc            │
  │  type: ExternalName │           │   (ClusterIP)        │
  └─────────────────────┘           └─────────────────────┘
```

In multi-team environments, applications often need to discover Services owned by other teams in different namespaces. There are three main patterns: direct FQDN references in application config, ExternalName proxy Services, and shared-namespace delegation. Each has different coupling, visibility, and maintenance tradeoffs.

## The Challenge

### Step 1: Set Up the Multi-Namespace Environment

Create three namespaces representing different team boundaries:

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-frontend
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-backend
---
apiVersion: v1
kind: Namespace
metadata:
  name: shared-services
```

### Step 2: Deploy Backend Services

```yaml
# backend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  namespace: team-backend
spec:
  replicas: 2
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
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: api-svc
  namespace: team-backend
spec:
  selector:
    app: api-server
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```yaml
# shared.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: shared-services
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
        - name: redis
          image: redis:7
          ports:
            - containerPort: 6379
---
apiVersion: v1
kind: Service
metadata:
  name: redis-svc
  namespace: shared-services
spec:
  selector:
    app: redis
  ports:
    - port: 6379
      targetPort: 6379
      name: redis
```

### Step 3: Pattern 1 -- Direct FQDN Reference

The simplest pattern: applications use the full DNS name directly.

```bash
# From team-frontend namespace, use FQDN
kubectl run direct-test -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  wget -qO- http://api-svc.team-backend.svc.cluster.local
```

Pros: no extra resources. Cons: the consuming team must know the target namespace and Service name -- tight coupling.

### Step 4: Pattern 2 -- ExternalName Proxy

Create namespace-local aliases that hide the target namespace:

```yaml
# frontend-proxies.yaml
apiVersion: v1
kind: Service
metadata:
  name: backend-api
  namespace: team-frontend
spec:
  type: ExternalName
  externalName: api-svc.team-backend.svc.cluster.local
---
apiVersion: v1
kind: Service
metadata:
  name: cache
  namespace: team-frontend
spec:
  type: ExternalName
  externalName: redis-svc.shared-services.svc.cluster.local
```

Now the frontend application connects to `backend-api` and `cache` using simple names, with no knowledge of the target namespaces.

### Step 5: Pattern 3 -- Shared Services Namespace

For truly shared infrastructure, place Services in a well-known namespace that all teams reference:

```bash
# All teams know to look in shared-services
kubectl run shared-test -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  wget -qO- -T3 http://redis-svc.shared-services:6379 || echo "Expected: Redis protocol error (not HTTP)"
```

Apply everything and test the ExternalName pattern:

```bash
kubectl apply -f namespaces.yaml
kubectl apply -f backend.yaml
kubectl apply -f shared.yaml
kubectl apply -f frontend-proxies.yaml

# Test ExternalName proxy
kubectl run proxy-test -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  wget -qO- http://backend-api
```

### Step 6: Analyze the Tradeoffs

Evaluate each pattern for your environment. Consider:

- **Coupling**: ExternalName proxies decouple consumers from provider namespace structure
- **Visibility**: ExternalName Services are visible in `kubectl get svc -n team-frontend`, documenting dependencies
- **Maintenance**: FQDN references in app config require coordinated changes if namespaces move
- **Network Policy**: cross-namespace traffic still requires NetworkPolicy to allow it

## Verify What You Learned

```bash
# Direct FQDN works
kubectl run v1 -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  wget -qO- http://api-svc.team-backend.svc.cluster.local

# ExternalName proxy works with simple name
kubectl run v2 -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  wget -qO- http://backend-api

# ExternalName shows CNAME
kubectl run v3 -n team-frontend --image=busybox:1.37 --rm -it --restart=Never -- \
  nslookup backend-api.team-frontend.svc.cluster.local
```

## Cleanup

```bash
kubectl delete namespace team-frontend team-backend shared-services
```

## What's Next

You have mastered Service discovery patterns at the namespace level. The insane-level [exercise 13 (Building Service Mesh Patterns Without a Mesh)](../13-service-mesh-without-mesh/) challenges you to implement service mesh features using only native Kubernetes primitives.

## Summary

- **Direct FQDN** (`svc.namespace.svc.cluster.local`) is simple but tightly couples consumers to provider namespace structure.
- **ExternalName proxies** create namespace-local aliases, decoupling consumers and documenting dependencies.
- **Shared Services namespaces** centralize common infrastructure with a well-known naming convention.
- Cross-namespace discovery works by default in Kubernetes; NetworkPolicies control whether traffic is actually allowed.
- ExternalName Services add no runtime overhead -- they are resolved entirely at the DNS level.

## Reference

- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
- [Namespaces](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/)

## Additional Resources

- [ExternalName Service](https://kubernetes.io/docs/concepts/services-networking/service/#externalname)
- [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
