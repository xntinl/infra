# 11. Zero-Downtime Ingress to Gateway API Migration

<!--
difficulty: insane
concepts: [migration-strategy, dual-stack, traffic-shifting, ingress-to-gateway, compatibility]
tools: [kubectl, minikube, helm]
estimated_time: 90m
bloom_level: create
prerequisites: [06-04, 06-08, 06-10]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with at least 4GB memory
- `kubectl` and `helm` installed and configured
- Completion of exercises 04, 08, and 10

## The Scenario

Your production cluster runs 6 Ingress resources managed by the nginx Ingress Controller. The platform team has decided to migrate to Gateway API using Envoy Gateway. The migration must be zero-downtime: at no point should any of the 6 applications become unreachable. You must design and execute a phased migration plan that runs both systems in parallel, shifts traffic incrementally, and decommissions the old Ingress resources only after full validation.

## Constraints

1. **6 existing Ingress resources** covering 3 hosts (`app.example.com`, `api.example.com`, `admin.example.com`) with path-based routing, TLS termination, rate limiting annotations, and CORS headers. Create these Ingress resources first as your starting state.
2. **Parallel operation**: the nginx Ingress Controller and Envoy Gateway must run simultaneously during migration. Both must be capable of serving the same hostnames.
3. **Phased migration**: migrate one host at a time, not all at once. For each host:
   - Create the equivalent Gateway API resources (Gateway + HTTPRoute)
   - Verify the Gateway API route serves identical responses
   - Shift traffic from Ingress to Gateway API (using DNS weight, external LB, or Service selector switching)
   - Validate under production-like load (simulate with a loop of requests)
   - Delete the Ingress resource only after 5 minutes of clean Gateway API serving
4. **Feature parity**: every Ingress annotation behavior (rate limiting, CORS, rewrites) must be replicated in Gateway API using HTTPRoute filters or implementation-specific policies.
5. **Rollback capability**: at any point during migration, you must be able to revert a host back to the Ingress-based routing within 30 seconds.
6. **No application changes**: backend Deployments and Services must not be modified during migration.
7. **Audit trail**: maintain a migration log showing timestamps, which host was migrated, and verification results.

## Success Criteria

1. Starting state: 6 Ingress resources, all 3 hosts reachable via the nginx Ingress Controller.
2. Envoy Gateway is installed and running alongside nginx Ingress Controller.
3. Host `app.example.com` is migrated first: Gateway API resources created, traffic shifted, Ingress resource deleted, and the host continues serving without errors.
4. Hosts `api.example.com` and `admin.example.com` are migrated subsequently.
5. After full migration: 0 Ingress resources remain, 3 HTTPRoutes serve all traffic, nginx Ingress Controller is uninstalled.
6. A continuous request loop running during the entire migration shows zero connection errors.
7. Rate limiting, CORS, and rewrite behaviors work identically under Gateway API.

## Verification Commands

```bash
# Starting state -- all Ingress resources exist
kubectl get ingress

# Both controllers running
kubectl get pods -n ingress-nginx
kubectl get pods -n envoy-gateway-system

# Continuous traffic test (run in background during migration)
while true; do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: app.example.com" http://<endpoint>/)
  echo "$(date +%H:%M:%S) app.example.com: $STATUS"
  sleep 1
done

# Post-migration -- no Ingress resources remain
kubectl get ingress
# Expected: No resources found

# All traffic via Gateway API
kubectl get httproute
kubectl get gateway

# nginx Ingress Controller removed
kubectl get pods -n ingress-nginx
# Expected: No resources found
```

## Cleanup

```bash
kubectl delete httproute --all
kubectl delete gateway --all
kubectl delete gatewayclass --all
kubectl delete deployment --all
kubectl delete svc --all
helm uninstall eg -n envoy-gateway-system 2>/dev/null
helm uninstall ingress-nginx -n ingress-nginx 2>/dev/null
kubectl delete namespace envoy-gateway-system ingress-nginx 2>/dev/null
```
