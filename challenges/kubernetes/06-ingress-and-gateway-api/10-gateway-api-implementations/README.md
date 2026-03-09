# 10. Gateway API with Different Implementations

<!--
difficulty: advanced
concepts: [envoy-gateway, cilium, nginx-gateway-fabric, controller-comparison, conformance]
tools: [kubectl, minikube, helm]
estimated_time: 50m
bloom_level: analyze
prerequisites: [06-05, 06-08]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with at least 4GB memory
- `kubectl` and `helm` installed and configured
- Completion of [exercise 05](../05-gateway-api-fundamentals/) and [exercise 08](../08-gateway-api-traffic-splitting/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Deploy** a real Gateway API implementation (Envoy Gateway) and verify end-to-end traffic routing
- **Analyze** the differences between Gateway API implementations
- **Evaluate** conformance levels and implementation-specific features

## Architecture

```
                    Gateway API (standard interface)
                    ┌─────────────────────────────┐
                    │  GatewayClass → Gateway →    │
                    │  HTTPRoute → backendRefs     │
                    └──────────────┬──────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
     Envoy Gateway          Cilium Gateway       nginx Gateway
     (Envoy proxy)          (eBPF dataplane)     Fabric (nginx)
     - Full conformance     - High performance   - Familiar nginx
     - Extension policies   - No sidecar         - Annotation compat
```

## The Challenge

### Step 1: Install Envoy Gateway

Envoy Gateway is a CNCF project that implements Gateway API using Envoy proxy:

```bash
helm install eg oci://docker.io/envoyproxy/gateway-helm \
  --version v1.2.0 \
  --namespace envoy-gateway-system \
  --create-namespace
```

Wait for the controller to be ready:

```bash
kubectl wait --namespace envoy-gateway-system \
  --for=condition=Available deployment/envoy-gateway \
  --timeout=120s
```

Verify the GatewayClass was created automatically:

```bash
kubectl get gatewayclass
```

### Step 2: Deploy Backend Applications

```yaml
# backends.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo-v1
spec:
  replicas: 2
  selector:
    matchLabels:
      app: echo
      version: v1
  template:
    metadata:
      labels:
        app: echo
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"version":"v1","pod":"'$(hostname)'"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: echo-v1-svc
spec:
  selector:
    app: echo
    version: v1
  ports: [{ port: 80, targetPort: 80, name: http }]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo-v2
spec:
  replicas: 2
  selector:
    matchLabels:
      app: echo
      version: v2
  template:
    metadata:
      labels:
        app: echo
        version: v2
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports: [{ containerPort: 80 }]
          command: ["/bin/sh", "-c"]
          args: ['echo ''{"version":"v2","pod":"'$(hostname)'"}'' > /usr/share/nginx/html/index.html; nginx -g "daemon off;"']
---
apiVersion: v1
kind: Service
metadata:
  name: echo-v2-svc
spec:
  selector:
    app: echo
    version: v2
  ports: [{ port: 80, targetPort: 80, name: http }]
```

```bash
kubectl apply -f backends.yaml
```

### Step 3: Create Gateway and HTTPRoute

```yaml
# gateway-route.yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: eg-gateway
spec:
  gatewayClassName: eg                  # Envoy Gateway's GatewayClass
  listeners:
    - name: http
      protocol: HTTP
      port: 8080
      allowedRoutes:
        namespaces:
          from: Same
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: echo-route
spec:
  parentRefs:
    - name: eg-gateway
  rules:
    - matches:
        - headers:
            - name: X-Version
              value: v2
      backendRefs:
        - name: echo-v2-svc
          port: 80
    - backendRefs:
        - name: echo-v1-svc
          port: 80
          weight: 90
        - name: echo-v2-svc
          port: 80
          weight: 10
```

```bash
kubectl apply -f gateway-route.yaml
```

### Step 4: Test End-to-End Traffic

Wait for Envoy Gateway to provision the data plane:

```bash
kubectl get gateway eg-gateway -o jsonpath='{.status.conditions[*].type}{"\n"}{.status.conditions[*].status}'
```

Get the Gateway's service address:

```bash
GATEWAY_URL=$(kubectl get gateway eg-gateway -o jsonpath='{.status.addresses[0].value}')
GATEWAY_PORT=$(kubectl get svc -l gateway.envoyproxy.io/owning-gateway-name=eg-gateway -o jsonpath='{.items[0].spec.ports[0].nodePort}' 2>/dev/null || echo "8080")

# Default traffic (weighted split)
kubectl run test1 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://eg-gateway-envoy-gateway-system:8080/ 2>/dev/null || echo "Use port-forward if direct access unavailable"

# Header-based routing to v2
kubectl run test2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- --header="X-Version: v2" http://eg-gateway-envoy-gateway-system:8080/ 2>/dev/null || echo "Use port-forward if direct access unavailable"
```

Alternative access via port-forward:

```bash
kubectl port-forward svc/$(kubectl get svc -l gateway.envoyproxy.io/owning-gateway-name=eg-gateway -o jsonpath='{.items[0].metadata.name}') 8080:8080 &
curl -s localhost:8080
curl -s -H "X-Version: v2" localhost:8080
kill %1
```

### Step 5: Analyze Implementation Details

Examine what Envoy Gateway created behind the scenes:

```bash
# Envoy proxy pods created by the controller
kubectl get pods -l gateway.envoyproxy.io/owning-gateway-name=eg-gateway

# Service created for the Gateway
kubectl get svc -l gateway.envoyproxy.io/owning-gateway-name=eg-gateway

# Gateway status shows programmed conditions
kubectl get gateway eg-gateway -o yaml | grep -A5 "conditions:"
```

Compare with how the API objects look:

```bash
kubectl describe httproute echo-route
```

The `parentRef` status shows whether the route is accepted by the Gateway.

## Verify What You Learned

```bash
# GatewayClass exists and is accepted
kubectl get gatewayclass eg

# Gateway is programmed
kubectl get gateway eg-gateway

# HTTPRoute is accepted by the Gateway
kubectl get httproute echo-route -o jsonpath='{.status.parents[0].conditions[*].type}'

# Envoy proxy pods are running
kubectl get pods -l gateway.envoyproxy.io/owning-gateway-name=eg-gateway
```

## Cleanup

```bash
kubectl delete httproute echo-route
kubectl delete gateway eg-gateway
kubectl delete deployment echo-v1 echo-v2
kubectl delete svc echo-v1-svc echo-v2-svc
helm uninstall eg -n envoy-gateway-system
kubectl delete namespace envoy-gateway-system
```

## What's Next

The final two exercises are insane-level challenges. In [exercise 11 (Zero-Downtime Ingress to Gateway API Migration)](../11-zero-downtime-ingress-migration/), you will plan and execute a production migration from Ingress to Gateway API with zero downtime.

## Summary

- Gateway API is an **interface specification** -- multiple implementations exist with different proxy backends.
- **Envoy Gateway** uses Envoy proxy and provides full Gateway API conformance.
- Implementations auto-provision data plane proxies when a Gateway resource is created.
- The Gateway `status` field shows whether the controller has programmed the configuration.
- HTTPRoute `status.parents` shows which Gateways have accepted the route.
- Implementation choice depends on your environment: Envoy for features, Cilium for performance, nginx for familiarity.

## Reference

- [Gateway API Implementations](https://gateway-api.sigs.k8s.io/implementations/)
- [Envoy Gateway](https://gateway.envoyproxy.io/)

## Additional Resources

- [Gateway API Conformance](https://gateway-api.sigs.k8s.io/concepts/conformance/)
- [Cilium Gateway API](https://docs.cilium.io/en/stable/network/servicemesh/gateway-api/gateway-api/)
- [nginx Gateway Fabric](https://docs.nginx.com/nginx-gateway-fabric/)
