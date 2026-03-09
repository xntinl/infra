# 7. IngressClass and Multiple Controllers

<!--
difficulty: intermediate
concepts: [ingress-class, multiple-controllers, default-ingress-class, controller-selection]
tools: [kubectl, minikube, helm]
estimated_time: 35m
bloom_level: apply
prerequisites: [06-01, 06-04]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` and `helm` installed and configured
- Completion of [exercise 04 (Ingress Annotations)](../04-ingress-annotations/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** multiple Ingress Controllers in the same cluster
- **Apply** IngressClass resources to route Ingress resources to specific controllers
- **Set** a default IngressClass for Ingress resources that omit `ingressClassName`

## The Challenge

Deploy two Ingress Controllers (nginx with different configurations) and use IngressClass to control which controller handles each Ingress resource. Configure one as the default IngressClass and verify that unclassified Ingress resources are picked up by the correct controller.

### Step 1: Install Two Ingress Controllers

Install a primary controller with `ingressClass: nginx-primary`:

```bash
helm install primary ingress-nginx/ingress-nginx \
  --namespace ingress-primary \
  --create-namespace \
  --set controller.ingressClassResource.name=nginx-primary \
  --set controller.ingressClassResource.controllerValue=k8s.io/ingress-nginx-primary \
  --set controller.ingressClassResource.default=true \
  --set controller.service.type=NodePort \
  --set controller.service.nodePorts.http=30080
```

Install a secondary controller with `ingressClass: nginx-internal`:

```bash
helm install internal ingress-nginx/ingress-nginx \
  --namespace ingress-internal \
  --create-namespace \
  --set controller.ingressClassResource.name=nginx-internal \
  --set controller.ingressClassResource.controllerValue=k8s.io/ingress-nginx-internal \
  --set controller.service.type=NodePort \
  --set controller.service.nodePorts.http=30081 \
  --set controller.electionID=ingress-internal-leader
```

### Step 2: Verify IngressClasses

```bash
kubectl get ingressclass
```

You should see two IngressClasses. One should have `(default)` marking.

Inspect the default IngressClass:

```bash
kubectl get ingressclass nginx-primary -o yaml
```

The annotation `ingressclass.kubernetes.io/is-default-class: "true"` marks it as default.

### Step 3: Deploy Backend Services

```yaml
# backends.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: public-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: public-app
  template:
    metadata:
      labels:
        app: public-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c", "echo 'public-app' > /usr/share/nginx/html/index.html; nginx -g 'daemon off;'"]
---
apiVersion: v1
kind: Service
metadata:
  name: public-app-svc
spec:
  selector:
    app: public-app
  ports: [{ port: 80, targetPort: 80, name: http }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: internal-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: internal-app
  template:
    metadata:
      labels:
        app: internal-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c", "echo 'internal-app' > /usr/share/nginx/html/index.html; nginx -g 'daemon off;'"]
---
apiVersion: v1
kind: Service
metadata:
  name: internal-app-svc
spec:
  selector:
    app: internal-app
  ports: [{ port: 80, targetPort: 80, name: http }]
```

```bash
kubectl apply -f backends.yaml
```

### Step 4: Create Ingress Resources Targeting Different Controllers

```yaml
# ingress-public.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: public-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
spec:
  ingressClassName: nginx-primary       # Handled by primary controller
  rules:
    - host: public.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: public-app-svc
                port:
                  number: 80
---
# ingress-internal.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: internal-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
spec:
  ingressClassName: nginx-internal      # Handled by internal controller
  rules:
    - host: internal.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: internal-app-svc
                port:
                  number: 80
---
# ingress-default.yaml -- no ingressClassName, uses default
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: default-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
spec:
  # No ingressClassName -- picked up by the default IngressClass
  rules:
    - host: default.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: public-app-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-public.yaml
```

### Step 5: Test Controller Selection

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Public ingress via primary controller (port 30080)
curl -s -H "Host: public.example.local" http://$NODE_IP:30080

# Internal ingress via internal controller (port 30081)
curl -s -H "Host: internal.example.local" http://$NODE_IP:30081

# Default ingress via primary controller (port 30080) -- no ingressClassName specified
curl -s -H "Host: default.example.local" http://$NODE_IP:30080
```

<details>
<summary>What happens if no default IngressClass exists and ingressClassName is omitted?</summary>

The Ingress resource is created but no controller picks it up. It sits idle with no routing configured. Always set `ingressClassName` explicitly or ensure one IngressClass is marked as default.

</details>

## Verify What You Learned

```bash
# Two IngressClasses exist
kubectl get ingressclass

# Each Ingress shows its class
kubectl get ingress -o custom-columns=NAME:.metadata.name,CLASS:.spec.ingressClassName

# Primary controller handles public and default
kubectl describe ingress public-ingress | grep "Controller"
kubectl describe ingress default-ingress | grep "Controller"
```

## Cleanup

```bash
kubectl delete ingress public-ingress internal-ingress default-ingress
kubectl delete deployment public-app internal-app
kubectl delete svc public-app-svc internal-app-svc
helm uninstall primary -n ingress-primary
helm uninstall internal -n ingress-internal
kubectl delete namespace ingress-primary ingress-internal
```

## What's Next

Gateway API supports traffic splitting natively. In [exercise 08 (Gateway API Traffic Splitting and Canary)](../08-gateway-api-traffic-splitting/), you will configure weighted backends for gradual rollouts and canary deployments.

## Summary

- **IngressClass** maps Ingress resources to specific controllers via `ingressClassName`.
- Multiple controllers can run in the same cluster, each watching for their own IngressClass.
- The `ingressclass.kubernetes.io/is-default-class: "true"` annotation marks a default class for unclassified Ingress resources.
- Always specify `ingressClassName` explicitly in production to avoid ambiguity.
- Each controller instance needs a unique `electionID` to avoid leader election conflicts.

## Reference

- [IngressClass](https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-class)
- [Multiple Ingress Controllers](https://kubernetes.github.io/ingress-nginx/user-guide/multiple-ingress/)

## Additional Resources

- [Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/)
- [nginx-ingress IngressClass](https://kubernetes.github.io/ingress-nginx/user-guide/basic-usage/)
