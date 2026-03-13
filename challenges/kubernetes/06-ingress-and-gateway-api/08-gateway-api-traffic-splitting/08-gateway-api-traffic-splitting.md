# 8. Gateway API Traffic Splitting and Canary

<!--
difficulty: advanced
concepts: [traffic-splitting, canary-deployment, weighted-backends, gradual-rollout]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [06-05, 06-06]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05](../05-gateway-api-fundamentals/05-gateway-api-fundamentals.md) and [exercise 06](../06-gateway-api-httproute/06-gateway-api-httproute.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** canary deployment strategies using HTTPRoute weighted backends
- **Analyze** traffic distribution patterns as weights change
- **Implement** a progressive rollout from 0% to 100% traffic to a new version

## Architecture

```
                    HTTPRoute
                  ┌──────────┐
                  │  rules:  │
                  │  weight: │
                  └────┬─────┘
                       │
         ┌─────────────┼──────────────┐
         │             │              │
    Phase 1:      Phase 2:       Phase 3:
    100% v1       80% v1         0% v1
    0% v2         20% v2         100% v2

    ┌──────┐      ┌──────┐      ┌──────┐
    │v1 svc│      │v1 svc│      │v2 svc│
    │(3 rep)│     │(3 rep)│     │(3 rep)│
    └──────┘      └──────┘      └──────┘
    ┌──────┐      ┌──────┐      ┌──────┐
    │v2 svc│      │v2 svc│      │v1 svc│
    │(3 rep)│     │(3 rep)│     │(0 rep)│
    └──────┘      └──────┘      └──────┘
```

## The Challenge

### Step 1: Install CRDs and Create Gateway

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

```yaml
# gateway.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: canary-gc
spec:
  controllerName: example.com/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: canary-gw
spec:
  gatewayClassName: canary-gc
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: Same
```

### Step 2: Deploy Both Versions

Deploy v1 (stable) and v2 (canary) as separate Deployments with their own Services:

```yaml
# versions.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v1
spec:
  replicas: 3
  selector:
    matchLabels:
      app: canary-app
      version: v1
  template:
    metadata:
      labels:
        app: canary-app
        version: v1
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"version":"v1","pod":"'$(hostname)'"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: app-v1-svc
spec:
  selector:
    app: canary-app
    version: v1
  ports: [{ port: 80, targetPort: 80, name: http }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v2
spec:
  replicas: 3
  selector:
    matchLabels:
      app: canary-app
      version: v2
  template:
    metadata:
      labels:
        app: canary-app
        version: v2
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"version":"v2","pod":"'$(hostname)'"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: app-v2-svc
spec:
  selector:
    app: canary-app
    version: v2
  ports: [{ port: 80, targetPort: 80, name: http }]
```

### Step 3: Create Phased HTTPRoutes

Build three HTTPRoute manifests representing the rollout phases. Apply them sequentially to simulate a progressive canary.

Phase 1 -- 100/0 (all traffic to v1):

```yaml
# phase1.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: canary-route
spec:
  parentRefs:
    - name: canary-gw
  rules:
    - backendRefs:
        - name: app-v1-svc
          port: 80
          weight: 100
        - name: app-v2-svc
          port: 80
          weight: 0
```

Phase 2 -- 80/20 (canary testing):

```yaml
# phase2.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: canary-route
spec:
  parentRefs:
    - name: canary-gw
  rules:
    - backendRefs:
        - name: app-v1-svc
          port: 80
          weight: 80
        - name: app-v2-svc
          port: 80
          weight: 20
```

Phase 3 -- 0/100 (full rollout):

```yaml
# phase3.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: canary-route
spec:
  parentRefs:
    - name: canary-gw
  rules:
    - backendRefs:
        - name: app-v1-svc
          port: 80
          weight: 0
        - name: app-v2-svc
          port: 80
          weight: 100
```

### Step 4: Simulate the Progressive Rollout

Apply each phase and inspect the route:

```bash
kubectl apply -f gateway.yaml
kubectl apply -f versions.yaml
kubectl apply -f phase1.yaml

# Verify phase 1
kubectl get httproute canary-route -o jsonpath='{range .spec.rules[0].backendRefs[*]}{.name}: {.weight}{"\n"}{end}'

# Advance to phase 2
kubectl apply -f phase2.yaml
kubectl get httproute canary-route -o jsonpath='{range .spec.rules[0].backendRefs[*]}{.name}: {.weight}{"\n"}{end}'

# Advance to phase 3
kubectl apply -f phase3.yaml
kubectl get httproute canary-route -o jsonpath='{range .spec.rules[0].backendRefs[*]}{.name}: {.weight}{"\n"}{end}'
```

### Step 5: Design a Rollback Strategy

If issues are detected at phase 2, reapply phase 1:

```bash
kubectl apply -f phase1.yaml
```

Consider adding a header-based override to always route specific users to v2 for testing, even during phase 1:

```yaml
# canary-with-override.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: canary-route
spec:
  parentRefs:
    - name: canary-gw
  rules:
    # Override: header forces v2
    - matches:
        - headers:
            - name: X-Canary
              value: "true"
      backendRefs:
        - name: app-v2-svc
          port: 80
    # Default: weighted split
    - backendRefs:
        - name: app-v1-svc
          port: 80
          weight: 80
        - name: app-v2-svc
          port: 80
          weight: 20
```

## Verify What You Learned

```bash
# HTTPRoute exists with weighted backends
kubectl describe httproute canary-route

# Both Deployments healthy
kubectl get deployment app-v1 app-v2

# Weight values are correct for current phase
kubectl get httproute canary-route -o yaml | grep -A2 "weight:"
```

## Cleanup

```bash
kubectl delete httproute canary-route
kubectl delete gateway canary-gw
kubectl delete gatewayclass canary-gc
kubectl delete deployment app-v1 app-v2
kubectl delete svc app-v1-svc app-v2-svc
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

## What's Next

Gateway API supports more than just HTTP. In [exercise 09 (Gateway API: TLSRoute, TCPRoute, GRPCRoute)](../09-gateway-api-tls-tcp-grpc/09-gateway-api-tls-tcp-grpc.md), you will explore routing for non-HTTP protocols.

## Summary

- Gateway API **weighted backendRefs** enable traffic splitting without external tools.
- Progressive canary rollouts adjust weights incrementally: 100/0 -> 80/20 -> 50/50 -> 0/100.
- **Header-based overrides** allow targeted testing during phased rollouts.
- Rollback is a single `kubectl apply` reverting the weight configuration.
- Unlike Ingress, Gateway API has native traffic splitting -- no annotations or controller-specific hacks needed.

## Reference

- [HTTPRoute - BackendRefs](https://gateway-api.sigs.k8s.io/api-types/httproute/#backendrefs)
- [Traffic Splitting](https://gateway-api.sigs.k8s.io/guides/traffic-splitting/)

## Additional Resources

- [Canary Deployments](https://gateway-api.sigs.k8s.io/guides/traffic-splitting/)
- [Gateway API Implementations](https://gateway-api.sigs.k8s.io/implementations/)
