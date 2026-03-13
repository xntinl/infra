# 4. Ingress Annotations: Rate Limiting, CORS, Rewrites

<!--
difficulty: intermediate
concepts: [ingress-annotations, rate-limiting, cors, url-rewrite, proxy-settings]
tools: [kubectl, minikube, helm]
estimated_time: 30m
bloom_level: apply
prerequisites: [06-01, 06-03]
-->

## Prerequisites

- A running Kubernetes cluster with the nginx Ingress Controller installed
- `kubectl` installed and configured
- Completion of [exercise 03 (Ingress TLS Termination)](../03-ingress-tls-termination/03-ingress-tls-termination.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** rate limiting annotations to protect backend Services from abuse
- **Apply** CORS annotations for cross-origin browser requests
- **Implement** URL rewrite rules to decouple external paths from backend routes

## The Challenge

Configure three advanced Ingress behaviors using annotations: rate limiting to protect an API from abuse, CORS headers to allow cross-origin requests from a frontend, and URL rewrites to serve a versioned API under a clean path structure.

### Step 1: Deploy Backend Services

```yaml
# backends.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-v1
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api
      version: v1
  template:
    metadata:
      labels:
        app: api
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo '{"version":"v1","pod":"'$(hostname)'"}' > /usr/share/nginx/html/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: api-v1-svc
spec:
  selector:
    app: api
    version: v1
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```bash
kubectl apply -f backends.yaml
```

### Step 2: Create an Ingress with Rate Limiting

```yaml
# ingress-rate-limit.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: rate-limited-api
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
    nginx.ingress.kubernetes.io/limit-rps: "5"              # 5 requests per second
    nginx.ingress.kubernetes.io/limit-burst-multiplier: "3"  # Burst up to 15
    nginx.ingress.kubernetes.io/limit-connections: "10"      # Max 10 concurrent
spec:
  ingressClassName: nginx
  rules:
    - host: api.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api-v1-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-rate-limit.yaml
```

### Step 3: Create an Ingress with CORS

```yaml
# ingress-cors.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cors-api
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
    nginx.ingress.kubernetes.io/enable-cors: "true"
    nginx.ingress.kubernetes.io/cors-allow-origin: "https://frontend.example.com"
    nginx.ingress.kubernetes.io/cors-allow-methods: "GET, POST, PUT, DELETE, OPTIONS"
    nginx.ingress.kubernetes.io/cors-allow-headers: "Content-Type, Authorization"
    nginx.ingress.kubernetes.io/cors-max-age: "3600"
spec:
  ingressClassName: nginx
  rules:
    - host: cors-api.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api-v1-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-cors.yaml
```

### Step 4: Create an Ingress with URL Rewrite

Strip `/v1/` prefix so the backend receives requests at `/`:

```yaml
# ingress-rewrite.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: rewrite-api
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
    nginx.ingress.kubernetes.io/rewrite-target: /$2          # Captures path after /v1/
    nginx.ingress.kubernetes.io/use-regex: "true"
spec:
  ingressClassName: nginx
  rules:
    - host: versioned.example.local
      http:
        paths:
          - path: /v1(/|$)(.*)
            pathType: ImplementationSpecific
            backend:
              service:
                name: api-v1-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-rewrite.yaml
```

### Step 5: Test All Three Patterns

```bash
INGRESS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Rate limiting -- rapid requests should eventually get 503
for i in $(seq 1 20); do
  echo -n "Request $i: "
  curl -s -o /dev/null -w "%{http_code}" -H "Host: api.example.local" http://$NODE_IP:$INGRESS_PORT/
  echo
done

# CORS -- preflight response should include CORS headers
curl -s -I -X OPTIONS \
  -H "Host: cors-api.example.local" \
  -H "Origin: https://frontend.example.com" \
  -H "Access-Control-Request-Method: POST" \
  http://$NODE_IP:$INGRESS_PORT/

# Rewrite -- /v1/ maps to backend /
curl -s -H "Host: versioned.example.local" http://$NODE_IP:$INGRESS_PORT/v1/
```

<details>
<summary>What should the CORS preflight response look like?</summary>

The response headers should include:
- `Access-Control-Allow-Origin: https://frontend.example.com`
- `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization`
- `Access-Control-Max-Age: 3600`

</details>

## Verify What You Learned

```bash
# Rate limiting annotations are set
kubectl describe ingress rate-limited-api

# CORS annotations are set
kubectl describe ingress cors-api

# Rewrite works -- /v1/ serves the same content as /
curl -s -H "Host: versioned.example.local" http://$NODE_IP:$INGRESS_PORT/v1/
```

## Cleanup

```bash
kubectl delete ingress rate-limited-api cors-api rewrite-api
kubectl delete deployment api-v1
kubectl delete svc api-v1-svc
```

## What's Next

Ingress has served HTTP routing well, but Gateway API offers a richer and more standardized model. In [exercise 05 (Gateway API Fundamentals)](../05-gateway-api-fundamentals/05-gateway-api-fundamentals.md), you will learn the core Gateway API concepts: GatewayClass, Gateway, and HTTPRoute.

## Summary

- Annotations are **controller-specific** -- nginx-ingress annotations do not work with Traefik or HAProxy controllers.
- **Rate limiting** protects backends with `limit-rps`, `limit-burst-multiplier`, and `limit-connections`.
- **CORS** annotations handle cross-origin preflight requests without backend changes.
- **URL rewrite** with `rewrite-target` decouples external URL structure from backend paths.
- Always test annotations in a non-production environment first -- incorrect regex in rewrites can break routing.

## Reference

- [nginx-ingress Annotations](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/)
- [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/)

## Additional Resources

- [Rate Limiting](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#rate-limiting)
- [CORS](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#enable-cors)
- [Rewrite](https://kubernetes.github.io/ingress-nginx/examples/rewrite/)
