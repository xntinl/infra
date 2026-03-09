# 9. Gateway API: TLSRoute, TCPRoute, GRPCRoute

<!--
difficulty: advanced
concepts: [tlsroute, tcproute, grpcroute, protocol-routing, sni-matching]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [06-05, 06-06]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05](../05-gateway-api-fundamentals/) and [exercise 06](../06-gateway-api-httproute/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** Gateway configurations with multiple protocol listeners
- **Analyze** how TLSRoute uses SNI for backend selection without terminating TLS
- **Configure** TCPRoute for raw TCP services and GRPCRoute for gRPC-specific matching

## Architecture

```
                    Gateway (multi-protocol)
          ┌──────────────────────────────────────┐
          │ Listener: http (port 80)             │──▶ HTTPRoute
          │ Listener: https (port 443, TLS term) │──▶ HTTPRoute
          │ Listener: tls-pass (port 8443, pass) │──▶ TLSRoute (SNI-based)
          │ Listener: tcp (port 5432)            │──▶ TCPRoute
          │ Listener: grpc (port 50051)          │──▶ GRPCRoute
          └──────────────────────────────────────┘
```

## The Challenge

### Step 1: Install Gateway API CRDs (Including Experimental)

TLSRoute, TCPRoute, and GRPCRoute require the experimental channel:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/experimental-install.yaml
```

Verify the route CRDs:

```bash
kubectl get crd | grep -E "(tlsroute|tcproute|grpcroute)"
```

### Step 2: Deploy Protocol-Specific Backends

```yaml
# backends.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web-server
  template:
    metadata:
      labels:
        app: web-server
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
---
apiVersion: v1
kind: Service
metadata:
  name: web-server-svc
spec:
  selector:
    app: web-server
  ports: [{ port: 80, targetPort: 80, name: http }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres-db
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres-db
  template:
    metadata:
      labels:
        app: postgres-db
    spec:
      containers:
        - name: postgres
          image: postgres:16
          ports: [{ containerPort: 5432 }]
          env:
            - name: POSTGRES_PASSWORD
              value: "changeme"
---
apiVersion: v1
kind: Service
metadata:
  name: postgres-svc
spec:
  selector:
    app: postgres-db
  ports: [{ port: 5432, targetPort: 5432, name: tcp }]
```

```bash
kubectl apply -f backends.yaml
```

### Step 3: Create a Multi-Protocol Gateway

```yaml
# gateway.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: multi-protocol-gc
spec:
  controllerName: example.com/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: multi-gw
spec:
  gatewayClassName: multi-protocol-gc
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        kinds:
          - kind: HTTPRoute
    - name: tls-passthrough
      protocol: TLS
      port: 8443
      tls:
        mode: Passthrough               # TLS is NOT terminated -- passed to backend
      allowedRoutes:
        kinds:
          - kind: TLSRoute
    - name: tcp-postgres
      protocol: TCP
      port: 5432
      allowedRoutes:
        kinds:
          - kind: TCPRoute
    - name: grpc
      protocol: HTTP
      port: 50051
      allowedRoutes:
        kinds:
          - kind: GRPCRoute
```

### Step 4: Create TLSRoute (SNI-Based Passthrough)

```yaml
# tlsroute.yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: tls-passthrough-route
spec:
  parentRefs:
    - name: multi-gw
      sectionName: tls-passthrough      # Attaches to the tls-passthrough listener
  hostnames:
    - "secure-app.example.com"          # SNI matching -- routes based on TLS ClientHello
  rules:
    - backendRefs:
        - name: web-server-svc
          port: 80
```

### Step 5: Create TCPRoute (Raw TCP)

```yaml
# tcproute.yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: postgres-route
spec:
  parentRefs:
    - name: multi-gw
      sectionName: tcp-postgres         # Attaches to the tcp-postgres listener
  rules:
    - backendRefs:
        - name: postgres-svc
          port: 5432
```

### Step 6: Create GRPCRoute

```yaml
# grpcroute.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: grpc-route
spec:
  parentRefs:
    - name: multi-gw
      sectionName: grpc
  rules:
    - matches:
        - method:
            service: "myapp.UserService"   # gRPC service name matching
            method: "GetUser"              # gRPC method matching
      backendRefs:
        - name: web-server-svc
          port: 80
    - matches:
        - method:
            service: "myapp.UserService"   # All methods of this service
      backendRefs:
        - name: web-server-svc
          port: 80
```

### Step 7: Apply and Inspect

```bash
kubectl apply -f gateway.yaml
kubectl apply -f tlsroute.yaml
kubectl apply -f tcproute.yaml
kubectl apply -f grpcroute.yaml

kubectl get gateway multi-gw
kubectl describe gateway multi-gw
kubectl get tlsroute,tcproute,grpcroute
```

Analyze the Gateway listeners and their attached routes:

```bash
kubectl get gateway multi-gw -o yaml | grep -A10 "listeners:"
kubectl describe tlsroute tls-passthrough-route
kubectl describe tcproute postgres-route
kubectl describe grpcroute grpc-route
```

## Verify What You Learned

```bash
# All route CRDs exist
kubectl get crd | grep -E "(tlsroute|tcproute|grpcroute|httproute)"

# Gateway has multiple listeners
kubectl get gateway multi-gw -o jsonpath='{range .spec.listeners[*]}{.name}{"\t"}{.protocol}{"\t"}{.port}{"\n"}{end}'

# All routes are created and reference correct listeners
kubectl get tlsroute,tcproute,grpcroute

# TLSRoute has SNI hostname
kubectl get tlsroute tls-passthrough-route -o jsonpath='{.spec.hostnames}'

# GRPCRoute has service/method matching
kubectl describe grpcroute grpc-route
```

## Cleanup

```bash
kubectl delete grpcroute grpc-route
kubectl delete tcproute postgres-route
kubectl delete tlsroute tls-passthrough-route
kubectl delete gateway multi-gw
kubectl delete gatewayclass multi-protocol-gc
kubectl delete deployment web-server postgres-db
kubectl delete svc web-server-svc postgres-svc
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/experimental-install.yaml
```

## What's Next

In [exercise 10 (Gateway API with Different Implementations)](../10-gateway-api-implementations/), you will deploy a real Gateway Controller and see these routes actually routing traffic end-to-end.

## Summary

- **TLSRoute** routes TLS connections based on SNI without terminating the TLS connection (passthrough mode).
- **TCPRoute** routes raw TCP traffic to backends, useful for databases, message brokers, and custom protocols.
- **GRPCRoute** provides gRPC-specific matching on service and method names with proper status code handling.
- A single Gateway can have **multiple listeners** for different protocols on different ports.
- TLSRoute and TCPRoute are in the **experimental channel** and require experimental CRD installation.
- The `sectionName` in `parentRefs` attaches a route to a specific listener.

## Reference

- [TLSRoute](https://gateway-api.sigs.k8s.io/api-types/tlsroute/)
- [TCPRoute](https://gateway-api.sigs.k8s.io/api-types/tcproute/)
- [GRPCRoute](https://gateway-api.sigs.k8s.io/api-types/grpcroute/)

## Additional Resources

- [Gateway Listeners](https://gateway-api.sigs.k8s.io/api-types/gateway/#listeners)
- [TLS Configuration](https://gateway-api.sigs.k8s.io/guides/tls/)
- [Gateway API Experimental Channel](https://gateway-api.sigs.k8s.io/concepts/versioning/#release-channels)
