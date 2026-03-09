# 10. A/B Testing with Header-Based Routing

<!--
difficulty: advanced
concepts: [ab-testing, ingress-routing, header-based-routing, nginx-ingress, traffic-management]
tools: [kubectl, minikube, nginx-ingress-controller]
estimated_time: 45m
bloom_level: analyze
prerequisites: [02-09]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- nginx Ingress Controller installed (e.g., `minikube addons enable ingress`)
- Completion of [exercise 09 (Canary Deployments)](../09-canary-deployments/)

Verify Ingress Controller is running:

```bash
kubectl get pods -n ingress-nginx
# Or for minikube: minikube addons list | grep ingress
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** Ingress annotations for header-based and cookie-based routing
- **Analyze** how A/B testing differs from canary deployments in routing precision
- **Evaluate** when to use header-based routing vs. replica-ratio-based canary

## Architecture

Unlike canary deployments (which split traffic based on Pod count), A/B testing routes specific users to a specific version based on request properties. The nginx Ingress Controller supports this via canary annotations:

```
Client (Header: X-Version: canary)  -->  Ingress (canary annotation)  -->  Service-v2  -->  Pods v2
Client (no header)                  -->  Ingress (primary)             -->  Service-v1  -->  Pods v1
```

This gives precise control: only users who send the matching header see the new version.

## Steps

### 1. Deploy Two Versions with Separate Services

```yaml
# ab-versions.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v1
spec:
  replicas: 2
  selector:
    matchLabels:
      app: ab-app
      version: v1
  template:
    metadata:
      labels:
        app: ab-app
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html
      initContainers:
        - name: setup
          image: busybox:1.37
          command: ["sh", "-c", "echo 'Response from version A (v1)' > /html/index.html"]
          volumeMounts:
            - name: html
              mountPath: /html
      volumes:
        - name: html
          emptyDir: {}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-v2
spec:
  replicas: 2
  selector:
    matchLabels:
      app: ab-app
      version: v2
  template:
    metadata:
      labels:
        app: ab-app
        version: v2
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html
      initContainers:
        - name: setup
          image: busybox:1.37
          command: ["sh", "-c", "echo 'Response from version B (v2)' > /html/index.html"]
          volumeMounts:
            - name: html
              mountPath: /html
      volumes:
        - name: html
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: app-v1
spec:
  selector:
    app: ab-app
    version: v1
  ports:
    - port: 80
---
apiVersion: v1
kind: Service
metadata:
  name: app-v2
spec:
  selector:
    app: ab-app
    version: v2
  ports:
    - port: 80
```

```bash
kubectl apply -f ab-versions.yaml
```

### 2. Create the Primary Ingress

```yaml
# ingress-primary.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ab-primary
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx
  rules:
    - host: ab-test.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: app-v1
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-primary.yaml
```

### 3. Create the Canary Ingress with Header Routing

```yaml
# ingress-canary.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ab-canary
  annotations:
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-by-header: "X-Version"
    nginx.ingress.kubernetes.io/canary-by-header-value: "canary"
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx
  rules:
    - host: ab-test.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: app-v2
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-canary.yaml
```

### 4. Test Header-Based Routing

Get the Ingress IP:

```bash
INGRESS_IP=$(kubectl get ingress ab-primary -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
# For minikube: INGRESS_IP=$(minikube ip)
```

Test without the header (routes to v1):

```bash
curl -s -H "Host: ab-test.local" http://$INGRESS_IP/
# Expected: Response from version A (v1)
```

Test with the header (routes to v2):

```bash
curl -s -H "Host: ab-test.local" -H "X-Version: canary" http://$INGRESS_IP/
# Expected: Response from version B (v2)
```

### 5. Cookie-Based Routing (Alternative)

You can also route by cookie. Update the canary Ingress annotations:

```yaml
annotations:
  nginx.ingress.kubernetes.io/canary: "true"
  nginx.ingress.kubernetes.io/canary-by-cookie: "version"
```

With this, setting the cookie `version=always` routes to v2, and `version=never` always routes to v1.

## Verify What You Learned

```bash
# Default traffic goes to v1
curl -s -H "Host: ab-test.local" http://$INGRESS_IP/
# Expected: Response from version A (v1)

# Header-targeted traffic goes to v2
curl -s -H "Host: ab-test.local" -H "X-Version: canary" http://$INGRESS_IP/
# Expected: Response from version B (v2)

# Both deployments running
kubectl get deployments -l app=ab-app
```

## Cleanup

```bash
kubectl delete ingress ab-primary ab-canary
kubectl delete deployment app-v1 app-v2
kubectl delete svc app-v1 app-v2
```

## Summary

- A/B testing with header routing gives **precise control** over which users see which version
- The nginx Ingress Controller supports `canary-by-header`, `canary-by-header-value`, and `canary-by-cookie`
- Two separate Services and Ingress resources are needed (primary + canary)
- Unlike replica-ratio canary, header routing is **deterministic** -- the same user always gets the same version
- Useful for feature flags, internal testing, and partner-specific rollouts

## Reference

- [NGINX Ingress Canary Annotations](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary) — annotation reference
- [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/) — official concept documentation
- [IngressClass](https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-class) — selecting the Ingress controller
