# 17.01 Istio Installation and Sidecar Injection

<!--
difficulty: basic
concepts: [istio, sidecar-injection, envoy-proxy, istio-system, istioctl, control-plane]
tools: [kubectl, istioctl]
estimated_time: 25m
bloom_level: understand
prerequisites: [namespaces, deployments, services]
-->

## What You Will Learn

In this exercise you will install Istio on a Kubernetes cluster, enable
automatic sidecar injection for a namespace, deploy an application, and verify
that the Envoy sidecar proxy is injected alongside your application container.
You will understand the relationship between the Istio control plane (istiod)
and the data plane (Envoy sidecars).

## Why It Matters

A service mesh provides traffic management, security (mTLS), and observability
without changing application code. Istio is the most widely adopted mesh. Before
you can use any mesh feature, you need a working installation and sidecar
injection. Understanding this foundation is critical for everything that follows
in the service mesh domain.

## Step-by-Step

### 1 -- Install Istio

Download and install `istioctl`:

```bash
curl -L https://istio.io/downloadIstio | sh -
export PATH=$PWD/istio-*/bin:$PATH
```

Install Istio with the demo profile (includes all components for learning):

```bash
istioctl install --set profile=demo -y
```

Verify the installation:

```bash
kubectl get pods -n istio-system
kubectl get svc -n istio-system
```

### 2 -- Enable Automatic Sidecar Injection

Label a namespace so that Istio automatically injects the Envoy sidecar into
every Pod created in that namespace.

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: mesh-demo
  labels:
    istio-injection: enabled    # this label triggers automatic injection
```

### 3 -- Deploy a Sample Application

```yaml
# app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
  namespace: mesh-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
  template:
    metadata:
      labels:
        app: httpbin
        version: v1
    spec:
      containers:
        - name: httpbin
          image: kennethreitz/httpbin:latest
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: mesh-demo
spec:
  selector:
    app: httpbin
  ports:
    - name: http
      port: 80
      targetPort: 80
```

### 4 -- Deploy a Sleep Client (for Testing)

```yaml
# sleep.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep
  namespace: mesh-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep
  template:
    metadata:
      labels:
        app: sleep
    spec:
      containers:
        - name: sleep
          image: curlimages/curl:8.7.1
          command: ["sleep", "3600"]
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 50m
              memory: 32Mi
```

### 5 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f app.yaml
kubectl apply -f sleep.yaml
```

### 6 -- Understand the Sidecar

After injection, each Pod has 2 containers: your app + `istio-proxy` (Envoy).

```bash
# Check container count -- should be 2/2
kubectl get pods -n mesh-demo

# List containers in a Pod
kubectl get pods -n mesh-demo -l app=httpbin -o jsonpath='{.items[0].spec.containers[*].name}'
# Output: httpbin istio-proxy
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Forgetting the `istio-injection: enabled` label | No sidecar injected -- Pods run without the mesh |
| Adding the label after Pods exist | Existing Pods are not re-injected; you must restart them |
| Not naming Service ports (`http`, `grpc`, `tcp`) | Istio cannot determine the protocol and defaults to opaque TCP |
| Using `hostNetwork: true` | Sidecars cannot intercept traffic on the host network |

## Verify

```bash
# 1. Istio control plane is healthy
istioctl version
kubectl get pods -n istio-system

# 2. Sidecar is injected (2/2 containers)
kubectl get pods -n mesh-demo

# 3. Envoy proxy is running
kubectl logs -n mesh-demo -l app=httpbin -c istio-proxy --tail=5

# 4. Service-to-service communication works through the mesh
kubectl exec -n mesh-demo deploy/sleep -c sleep -- curl -s http://httpbin.mesh-demo/get | head -5

# 5. Verify proxy configuration
istioctl proxy-status
istioctl proxy-config cluster deploy/httpbin -n mesh-demo | head -10

# 6. Check mutual TLS is active
istioctl authn tls-check httpbin.mesh-demo.svc.cluster.local -n mesh-demo
```

## Cleanup

```bash
kubectl delete namespace mesh-demo
istioctl uninstall --purge -y
kubectl delete namespace istio-system
```

## What's Next

Continue to [17.02 Linkerd Installation and Meshing](../02-linkerd-basics/) to
see a lighter-weight alternative to Istio, or jump to
[17.03 Istio Traffic Management](../03-istio-traffic-management/) to start
using VirtualService and DestinationRule.

## Summary

- Istio consists of a control plane (istiod) and a data plane (Envoy sidecars).
- The `istio-injection: enabled` namespace label triggers automatic sidecar injection.
- After injection, each Pod gets an `istio-proxy` container alongside the app container.
- Service ports must be named with the protocol prefix (`http-`, `grpc-`, `tcp-`).
- `istioctl proxy-status` shows which Pods are connected to the mesh.
- The demo profile is suitable for learning; production uses `default` or `minimal`.

## References

- [Istio Installation Guide](https://istio.io/latest/docs/setup/getting-started/)
- [Sidecar Injection](https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/)

## Additional Resources

- [Istio Architecture](https://istio.io/latest/docs/ops/deployment/architecture/)
- [istioctl Reference](https://istio.io/latest/docs/reference/commands/istioctl/)
- [Installation Profiles](https://istio.io/latest/docs/setup/additional-setup/config-profiles/)
