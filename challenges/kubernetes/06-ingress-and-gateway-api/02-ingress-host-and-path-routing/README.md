# 2. Ingress Host-Based and Path-Based Routing

<!--
difficulty: basic
concepts: [host-routing, path-routing, default-backend, pathType, multi-host-ingress]
tools: [kubectl, minikube, helm]
estimated_time: 30m
bloom_level: understand
prerequisites: [06-01]
-->

## Prerequisites

- A running Kubernetes cluster with the nginx Ingress Controller installed (see [exercise 01](../01-ingress-controller-routing/))
- `kubectl` installed and configured
- Completion of [exercise 01 (Ingress Controller and Routing)](../01-ingress-controller-routing/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** how Ingress evaluates host and path rules in order of specificity
- **Differentiate** between Prefix and Exact pathType matching behaviors
- **Configure** a default backend for unmatched requests

## Why Host and Path Routing?

Most web architectures serve multiple services behind a single domain. A marketing site might live at `www.example.com`, the API at `api.example.com`, and internal dashboards at `dashboard.example.com`. Within a single host, different URL paths might route to different microservices: `/users` to the users service, `/orders` to the orders service. Ingress handles both dimensions -- host and path -- in a single resource, eliminating the need for separate load balancers.

Understanding rule evaluation order is important: Ingress Controllers evaluate the most specific path first, then fall back to less specific ones. `Exact` matches take priority over `Prefix` matches. Knowing this prevents routing surprises.

## Step 1: Install the Ingress Controller (If Not Already Running)

```bash
# Skip if already installed from exercise 01
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.service.type=NodePort
```

## Step 2: Deploy Three Microservices

```yaml
# microservices.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: users-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: users-api
  template:
    metadata:
      labels:
        app: users-api
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              mkdir -p /usr/share/nginx/html/users
              echo '{"service":"users-api","pod":"'$(hostname)'"}' > /usr/share/nginx/html/users/index.html
              echo '{"service":"users-api","user":"john"}' > /usr/share/nginx/html/users/1
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: users-api-svc
spec:
  selector:
    app: users-api
  ports:
    - port: 80
      targetPort: 80
      name: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: orders-api
  template:
    metadata:
      labels:
        app: orders-api
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              mkdir -p /usr/share/nginx/html/orders
              echo '{"service":"orders-api","pod":"'$(hostname)'"}' > /usr/share/nginx/html/orders/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: orders-api-svc
spec:
  selector:
    app: orders-api
  ports:
    - port: 80
      targetPort: 80
      name: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: default-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: default-app
  template:
    metadata:
      labels:
        app: default-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo '{"service":"default","message":"no matching route"}' > /usr/share/nginx/html/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: default-app-svc
spec:
  selector:
    app: default-app
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```bash
kubectl apply -f microservices.yaml
```

## Step 3: Create an Ingress with Multiple Hosts, Paths, and a Default Backend

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: multi-route-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
spec:
  ingressClassName: nginx
  defaultBackend:                          # Handles requests that match no rule
    service:
      name: default-app-svc
      port:
        number: 80
  rules:
    - host: shop.example.local
      http:
        paths:
          - path: /users                   # Prefix match -- /users, /users/1, /users/search
            pathType: Prefix
            backend:
              service:
                name: users-api-svc
                port:
                  number: 80
          - path: /orders                  # Prefix match
            pathType: Prefix
            backend:
              service:
                name: orders-api-svc
                port:
                  number: 80
    - host: admin.example.local
      http:
        paths:
          - path: /                        # Catch-all for admin host
            pathType: Prefix
            backend:
              service:
                name: default-app-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress.yaml
```

## Step 4: Test the Routing

```bash
INGRESS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Users API via path routing
curl -s -H "Host: shop.example.local" http://$NODE_IP:$INGRESS_PORT/users/

# Orders API via path routing
curl -s -H "Host: shop.example.local" http://$NODE_IP:$INGRESS_PORT/orders/

# Admin host routes to default app
curl -s -H "Host: admin.example.local" http://$NODE_IP:$INGRESS_PORT/

# Unknown host falls back to defaultBackend
curl -s -H "Host: unknown.example.local" http://$NODE_IP:$INGRESS_PORT/
```

## Common Mistakes

### Mistake 1: Path Prefix Does Not Include Trailing Content

`pathType: Prefix` with `/users` matches `/users`, `/users/`, and `/users/123`, but NOT `/userslists`. The prefix match is on complete path segments separated by `/`.

### Mistake 2: Forgetting the Default Backend

Without a `defaultBackend`, requests to unmatched hosts or paths return the controller's default 404 page. Setting a custom default backend provides a better user experience.

### Mistake 3: Multiple Ingress Resources Conflicting

Two Ingress resources with the same host and overlapping paths produce undefined behavior. Consolidate rules under a single Ingress per host, or use explicit priority annotations if the controller supports them.

## Verify What You Learned

```bash
INGRESS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Path-based routing works
curl -s -H "Host: shop.example.local" http://$NODE_IP:$INGRESS_PORT/users/
curl -s -H "Host: shop.example.local" http://$NODE_IP:$INGRESS_PORT/orders/

# Host-based routing works
curl -s -H "Host: admin.example.local" http://$NODE_IP:$INGRESS_PORT/

# Default backend catches unmatched
curl -s -H "Host: nowhere.example.local" http://$NODE_IP:$INGRESS_PORT/

# Ingress shows all rules
kubectl describe ingress multi-route-ingress
```

## Cleanup

```bash
kubectl delete ingress multi-route-ingress
kubectl delete deployment users-api orders-api default-app
kubectl delete svc users-api-svc orders-api-svc default-app-svc
```

## What's Next

Ingress can terminate TLS connections so your backend Services do not need to handle HTTPS. In [exercise 03 (Ingress TLS Termination)](../03-ingress-tls-termination/), you will create TLS certificates, store them as Kubernetes Secrets, and configure Ingress to serve HTTPS.

## Summary

- **Host-based routing** directs traffic based on the HTTP `Host` header to different backend Services.
- **Path-based routing** uses URL path prefixes or exact matches to route within a single host.
- `pathType: Prefix` matches complete path segments; `pathType: Exact` matches only the literal path.
- **defaultBackend** handles requests that match no host or path rule.
- More specific paths take priority over less specific ones during rule evaluation.
- Consolidate rules under a single Ingress per host to avoid conflicts.

## Reference

- [Ingress Rules](https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-rules) -- host and path configuration
- [Path Types](https://kubernetes.io/docs/concepts/services-networking/ingress/#path-types) -- Prefix, Exact, ImplementationSpecific

## Additional Resources

- [Default Backend](https://kubernetes.io/docs/concepts/services-networking/ingress/#default-backend)
- [Name-based Virtual Hosting](https://kubernetes.io/docs/concepts/services-networking/ingress/#name-based-virtual-hosting)
