# 6. Gateway API: HTTPRoute Advanced Matching

<!--
difficulty: intermediate
concepts: [httproute, method-matching, query-params, request-filters, response-headers]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: apply
prerequisites: [06-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (Gateway API Fundamentals)](../05-gateway-api-fundamentals/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** HTTPRoute rules with method and query parameter matching
- **Apply** request and response header modification filters
- **Implement** URL redirect and path rewrite filters

## The Challenge

Build a comprehensive HTTPRoute configuration that demonstrates Gateway API's full matching and filtering capabilities. Route traffic based on HTTP methods and query parameters, add custom headers to requests and responses, and configure URL redirects.

### Step 1: Install CRDs and Deploy Backends

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

```yaml
# backends.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: read-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: read-api
  template:
    metadata:
      labels:
        app: read-api
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"handler":"read-api"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: read-api-svc
spec:
  selector:
    app: read-api
  ports: [{ port: 80, targetPort: 80, name: http }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: write-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: write-api
  template:
    metadata:
      labels:
        app: write-api
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"handler":"write-api"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: write-api-svc
spec:
  selector:
    app: write-api
  ports: [{ port: 80, targetPort: 80, name: http }]
```

```bash
kubectl apply -f backends.yaml
```

### Step 2: Create Gateway

```yaml
# gateway.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: advanced-gc
spec:
  controllerName: example.com/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: advanced-gw
spec:
  gatewayClassName: advanced-gc
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: Same
```

```bash
kubectl apply -f gateway.yaml
```

### Step 3: Create HTTPRoute with Method and Query Parameter Matching

```yaml
# httproute-advanced.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: method-route
spec:
  parentRefs:
    - name: advanced-gw
  rules:
    # GET requests go to read-api
    - matches:
        - method: GET
          path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: read-api-svc
          port: 80
    # POST/PUT/DELETE requests go to write-api
    - matches:
        - method: POST
          path:
            type: PathPrefix
            value: /api
        - method: PUT
          path:
            type: PathPrefix
            value: /api
        - method: DELETE
          path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: write-api-svc
          port: 80
```

### Step 4: Create HTTPRoute with Header Filters

```yaml
# httproute-filters.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: filtered-route
spec:
  parentRefs:
    - name: advanced-gw
  hostnames:
    - "filtered.example.local"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      filters:
        - type: RequestHeaderModifier
          requestHeaderModifier:
            add:
              - name: X-Request-ID
                value: "gateway-injected"
            set:
              - name: X-Forwarded-Proto
                value: "https"
        - type: ResponseHeaderModifier
          responseHeaderModifier:
            add:
              - name: X-Served-By
                value: "gateway-api"
            remove:
              - "Server"              # Strip the Server header
      backendRefs:
        - name: read-api-svc
          port: 80
```

### Step 5: Create HTTPRoute with Redirect and Rewrite

```yaml
# httproute-redirect.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: redirect-route
spec:
  parentRefs:
    - name: advanced-gw
  hostnames:
    - "old.example.local"
  rules:
    # Redirect /legacy to /api on new host
    - matches:
        - path:
            type: PathPrefix
            value: /legacy
      filters:
        - type: RequestRedirect
          requestRedirect:
            hostname: new.example.local
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /api
            statusCode: 301
    # URL rewrite -- client sees /v2/*, backend sees /*
    - matches:
        - path:
            type: PathPrefix
            value: /v2
      filters:
        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /
      backendRefs:
        - name: read-api-svc
          port: 80
```

```bash
kubectl apply -f httproute-advanced.yaml
kubectl apply -f httproute-filters.yaml
kubectl apply -f httproute-redirect.yaml
```

### Step 6: Inspect All Routes

```bash
kubectl get httproute
kubectl describe httproute method-route
kubectl describe httproute filtered-route
kubectl describe httproute redirect-route
```

<details>
<summary>What is the difference between URLRewrite and RequestRedirect?</summary>

**URLRewrite** modifies the request before forwarding to the backend -- the client does not see the change. The URL in the browser stays the same.

**RequestRedirect** sends an HTTP redirect (301/302) back to the client, telling it to make a new request to a different URL. The client's URL changes.

</details>

## Verify What You Learned

```bash
# All HTTPRoutes are accepted
kubectl get httproute

# Method-based route has correct matches
kubectl get httproute method-route -o yaml | grep -A3 "method:"

# Filter route has request and response header modifiers
kubectl describe httproute filtered-route

# Redirect route shows 301 redirect configuration
kubectl describe httproute redirect-route
```

## Cleanup

```bash
kubectl delete httproute method-route filtered-route redirect-route
kubectl delete gateway advanced-gw
kubectl delete gatewayclass advanced-gc
kubectl delete deployment read-api write-api
kubectl delete svc read-api-svc write-api-svc
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/standard-install.yaml
```

## What's Next

Different controllers can coexist in the same cluster. In [exercise 07 (IngressClass and Multiple Controllers)](../07-ingress-class-multiple-controllers/), you will configure multiple Ingress Controllers and learn how IngressClass selects which controller handles each Ingress resource.

## Summary

- HTTPRoute supports matching on **HTTP method** (GET, POST, PUT, DELETE) for read/write separation.
- **RequestHeaderModifier** and **ResponseHeaderModifier** filters add, set, or remove headers.
- **RequestRedirect** sends the client to a different URL with a configurable status code (301/302).
- **URLRewrite** modifies the path before forwarding, invisible to the client.
- `ReplacePrefixMatch` rewrites only the matched prefix, preserving the rest of the path.

## Reference

- [HTTPRoute Matching](https://gateway-api.sigs.k8s.io/api-types/httproute/#matches)
- [HTTPRoute Filters](https://gateway-api.sigs.k8s.io/api-types/httproute/#filters)

## Additional Resources

- [HTTP Header Modifier](https://gateway-api.sigs.k8s.io/guides/http-header-modifier/)
- [HTTP Redirects and Rewrites](https://gateway-api.sigs.k8s.io/guides/http-redirect-rewrite/)
