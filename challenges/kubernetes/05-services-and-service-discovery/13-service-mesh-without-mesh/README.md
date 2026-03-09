# 13. Building Service Mesh Patterns Without a Mesh

<!--
difficulty: insane
concepts: [retry-logic, circuit-breaker, sidecar-proxy, mutual-tls, traffic-mirroring]
tools: [kubectl, minikube]
estimated_time: 90m
bloom_level: create
prerequisites: [05-06, 05-10, 05-12]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running v1.29+
- `kubectl` installed and configured
- Completion of exercises 06, 10, and 12

## The Scenario

Your organization cannot adopt a service mesh due to operational complexity constraints, but you need retry logic, health-aware routing, and traffic observability between three microservices: `gateway`, `orders`, and `inventory`. Build these capabilities using only native Kubernetes primitives -- Services, sidecar containers, Headless Services, and liveness/readiness probes. No Istio, no Linkerd, no Envoy, no custom controllers.

## Constraints

1. **Three microservices** (`gateway`, `orders`, `inventory`) deployed as Deployments with 2 replicas each, communicating via ClusterIP Services.
2. **Sidecar proxy**: each pod must include a native sidecar container (defined in `initContainers` with `restartPolicy: Always`) running `nginx:1.27` configured as a reverse proxy. The main application connects to `localhost` on the sidecar port; the sidecar forwards to the upstream Service. This decouples the application from service discovery.
3. **Health-aware routing**: the `orders` Service must use readiness probes that check a `/health` endpoint. Simulate a failure in one pod (return 503 from `/health`) and verify the Service stops routing to it within 15 seconds.
4. **Retry-on-failure**: the sidecar nginx proxy for the `gateway` must be configured with `proxy_next_upstream error timeout http_503` so that if the first upstream pod returns 503, nginx automatically retries on another pod.
5. **Request logging**: each sidecar must write access logs to a shared `emptyDir` volume at `/var/log/mesh/access.log` in a structured format, providing a centralized view of inter-service traffic.
6. **No external tools**: no service mesh CRDs, no Envoy, no custom operators. Only built-in Kubernetes resources and nginx configuration.

## Success Criteria

1. All three microservices are running with 2 replicas and a sidecar proxy in each pod (3 containers per pod counting the native sidecar).
2. The `gateway` connects to `orders` through its local sidecar proxy on `localhost:8081`, not directly to the orders ClusterIP.
3. When one `orders` pod fails its readiness probe, the Endpoints list drops to 1 within 15 seconds, and the `gateway` sidecar transparently retries on the healthy pod with no client-visible errors.
4. Access logs from all sidecars are queryable via `kubectl exec` and show request paths, response codes, and upstream addresses.
5. Restoring the failed pod's health brings it back into the Endpoints, and traffic distributes to both pods again.

## Verification Commands

```bash
# All pods running with sidecar
kubectl get pods -l mesh=enabled -o wide
kubectl get pods -l mesh=enabled -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.name}{" "}{end}{"\n"}{end}'

# Endpoints update within 15s of readiness failure
kubectl get endpoints orders-svc --watch

# Gateway request succeeds even with one orders pod unhealthy
kubectl exec deploy/gateway -c main -- wget -qO- http://localhost:8081/orders

# Sidecar access logs show upstream routing
kubectl exec deploy/gateway -c proxy -- cat /var/log/mesh/access.log

# After restoring health, both pods appear in endpoints
kubectl get endpoints orders-svc
```

## Cleanup

```bash
kubectl delete deployment gateway orders inventory
kubectl delete svc gateway-svc orders-svc inventory-svc
```
