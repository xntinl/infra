# 17.02 Linkerd Installation and Meshing

<!--
difficulty: basic
concepts: [linkerd, service-mesh, proxy-injection, linkerd-viz, control-plane, data-plane]
tools: [kubectl, linkerd]
estimated_time: 25m
bloom_level: understand
prerequisites: [namespaces, deployments, services]
-->

## What You Will Learn

In this exercise you will install Linkerd, a lightweight service mesh focused
on simplicity and performance. You will inject the Linkerd proxy into an
application, verify mTLS is active, and explore the built-in dashboard for
observing traffic between services.

## Why It Matters

Linkerd is an alternative to Istio that prioritizes operational simplicity and
low resource overhead. Its Rust-based micro-proxy (linkerd2-proxy) is
significantly lighter than Envoy. Understanding both Istio and Linkerd lets you
choose the right mesh for your use case -- Linkerd for simplicity, Istio for
feature breadth.

## Step-by-Step

### 1 -- Install the Linkerd CLI

```bash
curl -fsL https://run.linkerd.io/install | sh
export PATH=$HOME/.linkerd2/bin:$PATH
linkerd version
```

### 2 -- Validate the Cluster

```bash
linkerd check --pre
```

### 3 -- Install Linkerd Control Plane

```bash
linkerd install --crds | kubectl apply -f -
linkerd install | kubectl apply -f -
linkerd check
```

### 4 -- Install the Viz Extension (Dashboard)

```bash
linkerd viz install | kubectl apply -f -
linkerd viz check
```

### 5 -- Create a Namespace and Deploy an Application

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: linkerd-demo
  annotations:
    linkerd.io/inject: enabled    # automatic injection for Linkerd
```

```yaml
# app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: linkerd-demo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: linkerd-demo
spec:
  selector:
    app: web
  ports:
    - name: http
      port: 80
      targetPort: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: client
  namespace: linkerd-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: client
  template:
    metadata:
      labels:
        app: client
    spec:
      containers:
        - name: client
          image: curlimages/curl:8.7.1
          command: ["sh", "-c"]
          args:
            - |
              while true; do
                curl -s http://web.linkerd-demo/
                sleep 2
              done
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 50m
              memory: 32Mi
```

### 6 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f app.yaml
```

### 7 -- Verify Injection

```bash
# Pods should show 2/2 (app + linkerd-proxy)
kubectl get pods -n linkerd-demo

# List containers
kubectl get pods -n linkerd-demo -l app=web -o jsonpath='{.items[0].spec.containers[*].name}'
# Output: web linkerd-proxy
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Forgetting `linkerd.io/inject: enabled` annotation | Pods run outside the mesh without proxy injection |
| Skipping `linkerd check` after install | Missing CRDs or webhook issues go unnoticed |
| Not installing the viz extension | No dashboard or traffic metrics available |
| Running `linkerd install` without `--crds` first | CRDs must exist before the control plane is installed |

## Verify

```bash
# 1. Control plane is healthy
linkerd check

# 2. Proxies injected
kubectl get pods -n linkerd-demo

# 3. mTLS is active
linkerd viz -n linkerd-demo edges deployment
# The "SECURED" column should show all connections as secured

# 4. Traffic metrics visible
linkerd viz stat -n linkerd-demo deployment
# Should show success rate, RPS, and latency

# 5. Real-time traffic view
linkerd viz top -n linkerd-demo deployment/web

# 6. Dashboard
linkerd viz dashboard &
```

## Cleanup

```bash
kubectl delete namespace linkerd-demo
linkerd viz uninstall | kubectl delete -f -
linkerd uninstall | kubectl delete -f -
```

## What's Next

Continue to [17.03 Istio Traffic Management](../03-istio-traffic-management/)
to learn about VirtualService and DestinationRule for fine-grained traffic
control, or jump to [17.10 Linkerd Traffic Splitting](../10-linkerd-traffic-splitting/)
for Linkerd-specific traffic management.

## Summary

- Linkerd uses a Rust-based micro-proxy that is lighter than Envoy.
- The `linkerd.io/inject: enabled` annotation triggers proxy injection.
- mTLS is enabled by default with zero configuration.
- The viz extension provides a dashboard, `stat`, `top`, and `edges` commands.
- `linkerd check` validates the entire installation end to end.
- Linkerd follows a two-step install: CRDs first, then the control plane.

## References

- [Linkerd Getting Started](https://linkerd.io/2/getting-started/)
- [Automatic Proxy Injection](https://linkerd.io/2/features/proxy-injection/)

## Additional Resources

- [Linkerd Architecture](https://linkerd.io/2/reference/architecture/)
- [Linkerd vs Istio](https://linkerd.io/2/faq/#whats-the-difference-between-linkerd-and-istio)
- [Linkerd CLI Reference](https://linkerd.io/2/reference/cli/)
