# 1. Ingress Controller and Routing

<!--
difficulty: basic
concepts: [ingress, ingress-controller, host-routing, path-routing, pathType]
tools: [kubectl, minikube, helm]
estimated_time: 35m
bloom_level: understand
prerequisites: [05-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- `helm` installed (v3+)
- Completion of [05-01 (Services: ClusterIP, NodePort, LoadBalancer)](../../05-services-and-service-discovery/01-services-clusterip-nodeport-loadbalancer/)

Verify your cluster is ready:

```bash
kubectl cluster-info
helm version
```

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** the relationship between Ingress resources and Ingress Controllers
- **Explain** host-based and path-based routing rules
- **Apply** Ingress configuration with `ingressClassName` and `pathType`

## Why Ingress?

Services expose applications, but each NodePort or LoadBalancer consumes a port or a cloud load balancer. In a real cluster with dozens of applications, you would need dozens of load balancers -- expensive and hard to manage. Ingress solves this by providing a single entry point that routes HTTP/HTTPS traffic to different backend Services based on the hostname and URL path of the request.

An Ingress resource is just a set of routing rules stored in the API server. By itself, it does nothing. You need an Ingress Controller -- a pod running a reverse proxy (typically nginx, Traefik, or HAProxy) -- that watches for Ingress resources and configures itself accordingly. Think of the Ingress resource as a config file and the Ingress Controller as the web server that reads it.

Understanding Ingress is essential even as the ecosystem moves toward Gateway API, because Ingress remains widely deployed and is the baseline for HTTP routing in Kubernetes.

## Step 1: Install the nginx Ingress Controller

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.service.type=NodePort
```

Wait for the controller to be ready:

```bash
kubectl wait --namespace ingress-nginx \
  --for=condition=Ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s
```

## Step 2: Deploy Two Backend Applications

```yaml
# app-frontend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: frontend
  template:
    metadata:
      labels:
        app: frontend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command:
            - /bin/sh
            - -c
            - |
              echo "<h1>Frontend App</h1><p>Pod: $(hostname)</p>" > /usr/share/nginx/html/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: frontend-svc
spec:
  type: ClusterIP                    # Ingress routes to ClusterIP Services
  selector:
    app: frontend
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```yaml
# app-backend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-api
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend-api
  template:
    metadata:
      labels:
        app: backend-api
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command:
            - /bin/sh
            - -c
            - |
              mkdir -p /usr/share/nginx/html/api
              echo '{"service":"backend-api","pod":"'$(hostname)'"}' > /usr/share/nginx/html/api/index.html
              echo '{"status":"healthy"}' > /usr/share/nginx/html/api/health
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: backend-api-svc
spec:
  type: ClusterIP
  selector:
    app: backend-api
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```bash
kubectl apply -f app-frontend.yaml
kubectl apply -f app-backend.yaml
```

## Step 3: Create an Ingress Resource

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app-ingress
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /$2   # Strips /api prefix before forwarding
    nginx.ingress.kubernetes.io/ssl-redirect: "false"  # Allow HTTP for local testing
spec:
  ingressClassName: nginx             # Binds to the nginx Ingress Controller
  rules:
    - host: app.example.local         # Host-based routing
      http:
        paths:
          - path: /                    # Catch-all for frontend
            pathType: Prefix           # Matches / and all subpaths
            backend:
              service:
                name: frontend-svc
                port:
                  number: 80
          - path: /api(/|$)(.*)        # Regex capture for rewrite
            pathType: ImplementationSpecific  # Required for regex paths
            backend:
              service:
                name: backend-api-svc
                port:
                  number: 80
    - host: api.example.local          # Separate host for API-only access
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: backend-api-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress.yaml
```

## Common Mistakes

### Mistake 1: No Ingress Controller Installed

Creating an Ingress resource without an Ingress Controller does nothing. The resource is accepted by the API server but no proxy is configured. Always install a controller first.

### Mistake 2: Missing ingressClassName

Without `ingressClassName`, the Ingress may not be picked up by any controller if multiple controllers are installed or if the cluster does not have a default IngressClass.

### Mistake 3: Wrong pathType

There are three path types:
- `Prefix` -- matches the path and all subpaths (e.g., `/api` matches `/api`, `/api/v1`, `/api/users`)
- `Exact` -- matches only the exact path (e.g., `/api` matches `/api` but not `/api/v1`)
- `ImplementationSpecific` -- behavior depends on the controller (nginx supports regex here)

Using `Exact` when you need subpath matching causes 404 errors.

## Verify What You Learned

Get the Ingress Controller's NodePort:

```bash
INGRESS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "Ingress at: http://$NODE_IP:$INGRESS_PORT"
```

Test host-based routing to frontend:

```bash
curl -s -H "Host: app.example.local" http://$NODE_IP:$INGRESS_PORT/
```

Expected output: HTML with "Frontend App".

Test path-based routing to backend:

```bash
curl -s -H "Host: app.example.local" http://$NODE_IP:$INGRESS_PORT/api/
```

Expected output: JSON with "backend-api".

Test host-based routing to API directly:

```bash
curl -s -H "Host: api.example.local" http://$NODE_IP:$INGRESS_PORT/
```

Inspect the Ingress:

```bash
kubectl describe ingress app-ingress
```

## Cleanup

```bash
kubectl delete ingress app-ingress
kubectl delete deployment frontend backend-api
kubectl delete svc frontend-svc backend-api-svc
helm uninstall ingress-nginx -n ingress-nginx
kubectl delete namespace ingress-nginx
```

## What's Next

Now that you understand basic Ingress routing, [exercise 02 (Ingress Host-Based and Path-Based Routing)](../02-ingress-host-and-path-routing/) goes deeper into routing patterns, including multiple hosts, exact vs prefix paths, and default backends.

## Summary

- **Ingress** resources define HTTP routing rules; **Ingress Controllers** implement them.
- `ingressClassName` binds an Ingress to a specific controller.
- **Host-based routing** uses the `Host` header to direct traffic to different backends.
- **Path-based routing** uses URL paths; `pathType` controls matching (Prefix, Exact, ImplementationSpecific).
- Annotations like `rewrite-target` are controller-specific and modify request handling.
- Ingress routes to **ClusterIP Services** -- no NodePort or LoadBalancer needed for backends.

## Reference

- [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) -- concepts and configuration
- [Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/) -- available implementations

## Additional Resources

- [nginx-ingress Annotations](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/)
- [Path Types](https://kubernetes.io/docs/concepts/services-networking/ingress/#path-types)
- [TLS Termination](https://kubernetes.io/docs/concepts/services-networking/ingress/#tls)
