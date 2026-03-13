# 5. Gateway API Fundamentals

<!--
difficulty: intermediate
concepts: [gateway-api, gatewayclass, gateway, httproute, header-routing]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: apply
prerequisites: [06-01, 06-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-ingress-controller-routing/01-ingress-controller-routing.md) and [exercise 02](../02-ingress-host-and-path-routing/02-ingress-host-and-path-routing.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** GatewayClass, Gateway, and HTTPRoute resources
- **Apply** path-based and header-based routing with Gateway API
- **Differentiate** between Gateway API's role-oriented model and Ingress's flat model

## The Challenge

Install the Gateway API CRDs, create a Gateway with listeners, and define HTTPRoutes with advanced matching rules including header-based routing for A/B testing. Deploy two versions of an application and configure routing so that requests with a specific header reach the new version.

### Step 1: Install Gateway API CRDs

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

Verify CRDs are installed:

```bash
kubectl get crd | grep gateway
```

### Step 2: Deploy Two Application Versions

```yaml
# app-v1.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v1
spec:
  replicas: 2
  selector:
    matchLabels:
      app: demo
      version: v1
  template:
    metadata:
      labels:
        app: demo
        version: v1
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo '{"version":"v1","pod":"'$(hostname)'"}' > /usr/share/nginx/html/index.html
              mkdir -p /usr/share/nginx/html/api
              echo '{"version":"v1","endpoint":"api"}' > /usr/share/nginx/html/api/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: app-v1-svc
spec:
  selector:
    app: demo
    version: v1
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```yaml
# app-v2.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v2
spec:
  replicas: 2
  selector:
    matchLabels:
      app: demo
      version: v2
  template:
    metadata:
      labels:
        app: demo
        version: v2
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo '{"version":"v2","pod":"'$(hostname)'"}' > /usr/share/nginx/html/index.html
              mkdir -p /usr/share/nginx/html/api
              echo '{"version":"v2","endpoint":"api"}' > /usr/share/nginx/html/api/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: app-v2-svc
spec:
  selector:
    app: demo
    version: v2
  ports:
    - port: 80
      targetPort: 80
      name: http
```

### Step 3: Create GatewayClass and Gateway

```yaml
# gateway.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: demo-gateway-class
spec:
  controllerName: example.com/gateway-controller
  description: "Demo GatewayClass for exercises"
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: demo-gateway
spec:
  gatewayClassName: demo-gateway-class
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      hostname: "*.example.local"       # Wildcard -- accepts any subdomain
      allowedRoutes:
        namespaces:
          from: Same                     # Only routes in this namespace can attach
```

### Step 4: Create HTTPRoute with Advanced Matching

```yaml
# httproute.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: app-route
spec:
  parentRefs:
    - name: demo-gateway                # Attaches to the Gateway
  hostnames:
    - "app.example.local"
  rules:
    # Rule 1: /api with header X-App-Version: v2 -> app-v2
    - matches:
        - path:
            type: PathPrefix
            value: /api
          headers:
            - name: X-App-Version
              value: v2
      backendRefs:
        - name: app-v2-svc
          port: 80
    # Rule 2: /api without header -> app-v1
    - matches:
        - path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: app-v1-svc
          port: 80
    # Rule 3: Header v2 on any path -> app-v2
    - matches:
        - headers:
            - name: X-App-Version
              value: v2
      backendRefs:
        - name: app-v2-svc
          port: 80
    # Rule 4: Default -> weighted split (80/20)
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: app-v1-svc
          port: 80
          weight: 80
        - name: app-v2-svc
          port: 80
          weight: 20
```

### Step 5: Apply Everything

```bash
kubectl apply -f app-v1.yaml
kubectl apply -f app-v2.yaml
kubectl apply -f gateway.yaml
kubectl apply -f httproute.yaml
```

### Step 6: Inspect the Resources

```bash
kubectl get gatewayclass demo-gateway-class
kubectl get gateway demo-gateway
kubectl get httproute app-route
kubectl describe httproute app-route
```

The HTTPRoute description shows all rules with their matches and backendRefs. Note that the resources are accepted even without a real controller -- the CRDs define the schema, but a controller is needed to implement the routing.

<details>
<summary>How does Gateway API differ from Ingress?</summary>

Gateway API separates concerns by role:
- **Infrastructure provider** manages the GatewayClass (defines which controller to use)
- **Cluster operator** manages the Gateway (defines listeners, ports, TLS)
- **Application developer** manages the HTTPRoute (defines routing rules)

Ingress combines all these into a single resource, making it harder to delegate.

</details>

## Verify What You Learned

```bash
# CRDs installed
kubectl get crd | grep gateway

# GatewayClass, Gateway, HTTPRoute all created
kubectl get gatewayclass,gateway,httproute

# HTTPRoute shows correct rules
kubectl describe httproute app-route

# Backends are healthy
kubectl get deployments app-v1 app-v2
kubectl get svc app-v1-svc app-v2-svc
```

Note: without a real Gateway Controller implementation (like Envoy Gateway, Cilium, or nginx Gateway Fabric), traffic will not actually flow through the Gateway. The resources validate and store correctly, demonstrating the API model. Exercise 10 covers using a real implementation.

## Cleanup

```bash
kubectl delete httproute app-route
kubectl delete gateway demo-gateway
kubectl delete gatewayclass demo-gateway-class
kubectl delete deployment app-v1 app-v2
kubectl delete svc app-v1-svc app-v2-svc
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

## What's Next

You have defined Gateway API resources with basic matching. In [exercise 06 (Gateway API: HTTPRoute Advanced Matching)](../06-gateway-api-httproute/06-gateway-api-httproute.md), you will explore method-based matching, query parameter matching, and request/response header modification filters.

## Summary

- Gateway API uses three resource layers: **GatewayClass** (infrastructure), **Gateway** (cluster), **HTTPRoute** (application).
- HTTPRoute rules support **path**, **header**, and **method** matching with explicit precedence.
- **Weighted backendRefs** enable traffic splitting for canary/A/B testing directly in the route.
- Gateway API is more expressive than Ingress and designed for multi-team delegation.
- The CRDs define the API schema; a controller implementation is required for actual traffic routing.

## Reference

- [Gateway API](https://kubernetes.io/docs/concepts/services-networking/gateway/) -- Kubernetes documentation
- [HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/) -- detailed specification

## Additional Resources

- [Gateway API Guides](https://gateway-api.sigs.k8s.io/guides/)
- [HTTP Routing Guide](https://gateway-api.sigs.k8s.io/guides/http-routing/)
- [Implementations](https://gateway-api.sigs.k8s.io/implementations/) -- controllers that support Gateway API
