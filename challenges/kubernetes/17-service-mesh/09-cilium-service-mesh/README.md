# 17.09 Cilium Sidecar-Less Service Mesh

<!--
difficulty: advanced
concepts: [cilium, ebpf, sidecar-less-mesh, cilium-envoy, l7-policy, service-mesh-without-sidecars]
tools: [kubectl, cilium-cli, helm]
estimated_time: 35m
bloom_level: analyze
prerequisites: [services, network-policies, ingress]
-->

## Architecture

```
+---------------------------------------------------+
|  Kubernetes Node                                  |
|                                                   |
|  +-----------+    +-----------+    +-----------+  |
|  | Pod A     |    | Pod B     |    | Pod C     |  |
|  | (no       |    | (no       |    | (no       |  |
|  |  sidecar) |    |  sidecar) |    |  sidecar) |  |
|  +-----+-----+    +-----+-----+    +-----+-----+  |
|        |                |                |        |
|  ======+================+================+=====   |
|                    eBPF datapath                   |
|              (kernel-level routing)                |
|                        |                          |
|              +---------+---------+                |
|              | Cilium Envoy      |                |
|              | (per-node, not    |                |
|              |  per-pod)         |                |
|              +-------------------+                |
+---------------------------------------------------+
```

Cilium provides service mesh capabilities without sidecars by using eBPF for
L3/L4 networking and a per-node Envoy instance for L7 processing. This
eliminates the per-pod overhead of traditional sidecar meshes.

## What You Will Build

- Cilium installed as the CNI with service mesh features enabled
- L7 traffic policies using CiliumNetworkPolicy
- Traffic management using CiliumEnvoyConfig
- Comparison of resource overhead vs sidecar-based meshes

## Suggested Steps

1. Install Cilium with service mesh enabled:

```bash
cilium install --version 1.16.0 \
  --set kubeProxyReplacement=true \
  --set envoyConfig.enabled=true \
  --set loadBalancer.l7.backend=envoy

cilium status --wait
```

2. Create a namespace and deploy services:

```yaml
# services.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: cilium-mesh-lab
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: cilium-mesh-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend
  template:
    metadata:
      labels:
        app: backend
    spec:
      containers:
        - name: backend
          image: kennethreitz/httpbin:latest
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: backend
  namespace: cilium-mesh-lab
spec:
  selector:
    app: backend
  ports:
    - name: http
      port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: client
  namespace: cilium-mesh-lab
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
          command: ["sleep", "3600"]
```

3. Create an L7 CiliumNetworkPolicy:

```yaml
# l7-policy.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: backend-l7
  namespace: cilium-mesh-lab
spec:
  endpointSelector:
    matchLabels:
      app: backend
  ingress:
    - fromEndpoints:
        - matchLabels:
            app: client
      toPorts:
        - ports:
            - port: "80"
              protocol: TCP
          rules:
            http:
              - method: GET
                path: "/get"
              - method: GET
                path: "/headers"
```

4. Create traffic management with CiliumEnvoyConfig for header-based routing,
   retries, or timeouts.

5. Verify L7 policy enforcement and observe that Pods have no sidecar containers.

## Verify

```bash
# Cilium status
cilium status

# Pods have only 1 container (no sidecar)
kubectl get pods -n cilium-mesh-lab -o custom-columns='NAME:.metadata.name,CONTAINERS:.spec.containers[*].name'

# Allowed request succeeds
kubectl exec -n cilium-mesh-lab deploy/client -- curl -s http://backend/get
# Expect: 200

# Blocked request is denied
kubectl exec -n cilium-mesh-lab deploy/client -- curl -s -o /dev/null -w "%{http_code}" -X POST http://backend/post
# Expect: 403

# L7 visibility
cilium hubble observe -n cilium-mesh-lab --protocol http -f

# Resource comparison: no istio-proxy containers
kubectl top pods -n cilium-mesh-lab
```

## Cleanup

```bash
kubectl delete namespace cilium-mesh-lab
```

## References

- [Cilium Service Mesh](https://docs.cilium.io/en/latest/network/servicemesh/)
- [CiliumNetworkPolicy L7 Rules](https://docs.cilium.io/en/latest/security/policy/language/#layer-7-examples)
- [CiliumEnvoyConfig](https://docs.cilium.io/en/latest/network/servicemesh/envoy-config/)
- [Cilium vs Istio](https://isovalent.com/blog/post/cilium-service-mesh/)
