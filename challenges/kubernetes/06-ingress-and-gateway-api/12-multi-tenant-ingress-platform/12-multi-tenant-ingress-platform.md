# 12. Multi-Tenant Ingress Platform Design

<!--
difficulty: insane
concepts: [multi-tenancy, rbac, namespace-isolation, gateway-delegation, resource-quotas]
tools: [kubectl, minikube, helm]
estimated_time: 120m
bloom_level: create
prerequisites: [06-07, 06-08, 06-10]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with at least 4GB memory
- `kubectl` and `helm` installed and configured
- Completion of exercises 07, 08, and 10

## The Scenario

You are the platform engineer for a company hosting 4 product teams on a shared Kubernetes cluster. Each team deploys their own applications in dedicated namespaces. You must design and implement a multi-tenant ingress platform where:

- The platform team controls the shared Gateway infrastructure (GatewayClass, Gateway, TLS certificates).
- Product teams can create their own HTTPRoutes in their namespaces without platform team involvement.
- Teams cannot interfere with each other's routing (no hostname hijacking, no cross-tenant traffic).
- Resource consumption is bounded per tenant (rate limiting, connection limits).

## Constraints

1. **4 tenant namespaces**: `team-alpha`, `team-beta`, `team-gamma`, `team-delta`. Each namespace has a Deployment with 2 replicas and a ClusterIP Service.
2. **Shared Gateway**: a single Gateway in namespace `platform-ingress` with listeners for `*.alpha.example.com`, `*.beta.example.com`, `*.gamma.example.com`, `*.delta.example.com`. Each listener uses `allowedRoutes.namespaces.from: Selector` with a label selector matching only the corresponding team namespace.
3. **Namespace isolation via RBAC**: each team has a ServiceAccount that can only create/modify HTTPRoute resources in their own namespace. They cannot modify the Gateway, GatewayClass, or other teams' namespaces. Create Roles and RoleBindings to enforce this.
4. **Hostname ownership**: team-alpha can only create HTTPRoutes for `*.alpha.example.com`. If team-alpha tries to attach an HTTPRoute for `*.beta.example.com`, the Gateway must reject it.
5. **Per-tenant rate limiting**: each tenant's listener or route must enforce a maximum of 100 requests per second per source IP (use implementation-specific policies or annotations).
6. **TLS certificates**: the platform team provisions wildcard certificates for each subdomain and references them in the Gateway listeners. Teams do not handle TLS.
7. **Resource quotas**: each tenant namespace has a ResourceQuota limiting the number of HTTPRoute objects to 10.

## Success Criteria

1. Four tenant namespaces exist, each with a labeled Deployment, Service, and team-specific RBAC.
2. A shared Gateway in `platform-ingress` has 4 listeners, each restricted to one tenant namespace.
3. Team-alpha can create an HTTPRoute for `app.alpha.example.com` and traffic reaches their backend.
4. Team-alpha cannot create an HTTPRoute for `app.beta.example.com` -- the Gateway rejects it (route not accepted).
5. Team-alpha's ServiceAccount cannot modify resources in `team-beta` or `platform-ingress` namespaces.
6. TLS is terminated at the Gateway; backends receive plain HTTP.
7. ResourceQuota prevents any team from creating more than 10 HTTPRoutes.
8. Each tenant has independent rate limiting that does not affect other tenants.

## Verification Commands

```bash
# All tenant namespaces exist with correct labels
kubectl get namespaces -l tier=tenant

# Gateway has 4 listeners with namespace restrictions
kubectl get gateway shared-gw -n platform-ingress -o yaml | grep -A5 "allowedRoutes"

# Team-alpha HTTPRoute is accepted
kubectl get httproute -n team-alpha -o jsonpath='{.items[0].status.parents[0].conditions[*].type}'

# Team-alpha cannot create route for beta domain
kubectl --as=system:serviceaccount:team-alpha:team-alpha-sa apply -f bad-route.yaml
# Expected: rejected by Gateway (ParentRef not accepted)

# RBAC prevents cross-namespace access
kubectl auth can-i create httproute --as=system:serviceaccount:team-alpha:team-alpha-sa -n team-beta
# Expected: no

kubectl auth can-i get gateway --as=system:serviceaccount:team-alpha:team-alpha-sa -n platform-ingress
# Expected: no

# ResourceQuota limits HTTPRoute count
kubectl get resourcequota -n team-alpha

# Traffic reaches correct backend
curl -sk -H "Host: app.alpha.example.com" https://<gateway-endpoint>/
```

## Cleanup

```bash
kubectl delete namespace team-alpha team-beta team-gamma team-delta platform-ingress
kubectl delete gatewayclass shared-gc
```
