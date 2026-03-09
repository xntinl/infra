<!--
difficulty: insane
concepts: [multi-tenancy, tenant-isolation, namespace-per-tenant, shared-services, admission-control, network-policy-automation]
tools: [kubectl, kustomize, network-policy-cni]
estimated_time: 120m
bloom_level: create
prerequisites: [namespace-isolation-patterns, network-policies-zero-trust, microsegmentation-challenge]
-->

# 7.12 Multi-Tenant Network Isolation Platform

## The Scenario

You are building a multi-tenant SaaS platform on Kubernetes. Each tenant gets their own namespace, and tenants must be completely isolated from each other at the network level. However, all tenants share a set of platform services (logging, monitoring, ingress) that live in dedicated platform namespaces. Your job is to design and implement the full network isolation architecture.

The platform has:
- 3 tenant namespaces: `tenant-alpha`, `tenant-bravo`, `tenant-charlie`
- 3 platform namespaces: `platform-ingress`, `platform-monitoring`, `platform-logging`
- Each tenant runs a web app (port 80) and an API (port 8080)
- Each tenant has a dedicated database (port 5432)

```
  platform-ingress          platform-monitoring       platform-logging
  +--------------+          +------------------+      +--------------+
  | nginx-ingress|--------->| prometheus       |      | fluentd      |
  | controller   |          | (scrapes /metrics|      | (collects    |
  +--------------+          |  from all tenant |      |  logs from   |
        |                   |  pods on 9090)   |      |  all pods)   |
        v                   +------------------+      +--------------+
  +-----+------+-----+                                      ^
  |            |      |                                      |
  v            v      v                                      |
tenant-alpha  tenant-bravo  tenant-charlie                   |
+----------+  +----------+  +----------+                     |
| web (80) |  | web (80) |  | web (80) |---------------------+
| api(8080)|  | api(8080)|  | api(8080)|   (all pods emit logs)
| db (5432)|  | db (5432)|  | db (5432)|
+----------+  +----------+  +----------+
     X              X              X
     |-- blocked ---|-- blocked ---|
```

## Constraints

1. Every tenant namespace must have a default deny-all policy (ingress + egress)
2. Tenants must not be able to communicate with each other -- zero cross-tenant traffic
3. The ingress controller must reach tenant web apps on port 80 only
4. Prometheus must reach all tenant pods on port 9090 (metrics) only
5. Fluentd must be reachable from all tenant pods on port 24224 (log forwarding) only
6. Tenant pods need DNS resolution (scoped to kube-system)
7. Within each tenant namespace, only the web app can reach the API, and only the API can reach the database
8. Policies must be templatable -- adding a new tenant should require only changing the tenant name, not rewriting policies
9. A rogue pod in any tenant namespace must not be able to reach any service (in its own or other namespaces)
10. Platform namespaces must also have deny-all policies with explicit exceptions

## Success Criteria

1. Total inter-tenant isolation verified: pods in `tenant-alpha` cannot reach any pod in `tenant-bravo` or `tenant-charlie`
2. Ingress controller can reach all tenant web apps on port 80
3. Prometheus can scrape metrics from all tenant pods on port 9090
4. All tenant pods can forward logs to Fluentd on port 24224
5. Intra-tenant traffic follows the web -> api -> db chain; no reverse flow
6. A new tenant can be onboarded by duplicating and parameterizing the policy set
7. At least 20 NetworkPolicy resources across all namespaces
8. No policy uses an empty `namespaceSelector: {}` -- all selectors must be explicit

## Verification Commands

```bash
# Count total policies
kubectl get networkpolicy -A --no-headers | wc -l

# Cross-tenant isolation
kubectl exec -n tenant-alpha deploy/web -- wget -qO- --timeout=3 http://web.tenant-bravo 2>&1 || echo "PASS: cross-tenant blocked"
kubectl exec -n tenant-bravo deploy/api -- wget -qO- --timeout=3 http://api.tenant-charlie:8080 2>&1 || echo "PASS: cross-tenant blocked"

# Ingress to tenant web (should succeed)
kubectl exec -n platform-ingress deploy/ingress-controller -- wget -qO- --timeout=5 http://web.tenant-alpha

# Prometheus scrape (should succeed on 9090)
kubectl exec -n platform-monitoring deploy/prometheus -- wget -qO- --timeout=5 http://web.tenant-alpha:9090/metrics 2>&1

# Prometheus cannot reach tenant API port (should fail)
kubectl exec -n platform-monitoring deploy/prometheus -- wget -qO- --timeout=3 http://api.tenant-alpha:8080 2>&1 || echo "PASS: wrong port blocked"

# Intra-tenant chain
kubectl exec -n tenant-alpha deploy/web -- wget -qO- --timeout=5 http://api:8080
kubectl exec -n tenant-alpha deploy/api -- sh -c "echo SELECT 1 | nc -w3 db 5432"

# Reverse flow blocked
kubectl exec -n tenant-alpha deploy/db -- sh -c "echo | nc -w3 api 8080" 2>&1 || echo "PASS: reverse blocked"
kubectl exec -n tenant-alpha deploy/api -- wget -qO- --timeout=3 http://web 2>&1 || echo "PASS: reverse blocked"

# Rogue pod
kubectl run rogue --image=busybox:1.37 -n tenant-alpha -- sh -c "sleep 60"
kubectl exec -n tenant-alpha rogue -- wget -qO- --timeout=3 http://api:8080 2>&1 || echo "PASS: rogue blocked"
kubectl delete pod rogue -n tenant-alpha
```

## Cleanup

```bash
kubectl delete namespace tenant-alpha tenant-bravo tenant-charlie platform-ingress platform-monitoring platform-logging
```
