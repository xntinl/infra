# Exercise 11: Multi-Tenant RBAC Platform Design

<!--
difficulty: insane
concepts: [multi-tenancy, rbac, namespace-isolation, hierarchical-namespaces, resource-quotas, network-policies, tenant-onboarding]
tools: [kubectl, kind]
estimated_time: 90m
bloom_level: create
prerequisites: [11-rbac-and-authentication/04-namespace-scoped-rbac, 11-rbac-and-authentication/05-clusterroles-and-aggregation, 11-rbac-and-authentication/08-rbac-for-cicd-pipelines]
-->

## Scenario

You are the platform engineer for a company running a shared Kubernetes cluster with three tenant teams: **alpha**, **bravo**, and **charlie**. Each tenant needs:

- Isolated namespaces for `dev` and `prod` environments
- Three roles: `admin` (full namespace control), `developer` (deploy workloads), `viewer` (read-only)
- A CI/CD service account that can deploy to both dev and prod
- No tenant should be able to see or modify resources belonging to another tenant
- A platform-admin role that can manage all tenants
- ResourceQuotas to prevent any single tenant from consuming excessive resources

## Constraints

1. Each tenant gets exactly two namespaces: `<tenant>-dev` and `<tenant>-prod` (6 namespaces total).
2. Use aggregated ClusterRoles so new resource types are automatically included in the correct role tier.
3. Tenant CI/CD ServiceAccounts live in a shared `cicd-system` namespace and deploy across both dev and prod namespaces of their tenant only.
4. No tenant ServiceAccount or user may access `kube-system` or any other tenant's namespaces.
5. Platform admins use a ClusterRoleBinding to a custom `platform-admin` ClusterRole -- not `cluster-admin`.
6. Every namespace must have a ResourceQuota limiting pods (20), services (10), and configmaps (30).
7. The tenant admin role must not grant RBAC modification permissions (`roles`, `rolebindings`, `clusterroles`, `clusterrolebindings`).
8. All RBAC objects must use consistent labeling: `platform.example.com/tenant: <name>` and `platform.example.com/role-tier: <admin|developer|viewer>`.

## Success Criteria

1. `kubectl auth can-i --list` for each role tier in each tenant namespace shows exactly the expected permissions.
2. Cross-tenant access is denied for all ServiceAccounts and simulated users.
3. CI/CD ServiceAccount for tenant alpha can deploy to `alpha-dev` and `alpha-prod` but not `bravo-dev`.
4. Platform admin can list pods in all 6 tenant namespaces.
5. ResourceQuotas exist in all 6 namespaces with the specified limits.
6. Adding a new ClusterRole with the correct aggregation label automatically extends the tenant viewer role.

## Verification Commands

```bash
# Cross-tenant isolation (expect "no")
kubectl auth can-i list pods \
  --as=system:serviceaccount:alpha-dev:alpha-admin-sa \
  -n bravo-dev

# CI/CD cross-tenant isolation (expect "no")
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:cicd-system:alpha-cicd \
  -n bravo-prod

# CI/CD own-tenant access (expect "yes")
kubectl auth can-i create deployments.apps \
  --as=system:serviceaccount:cicd-system:alpha-cicd \
  -n alpha-prod

# Platform admin access (expect "yes")
kubectl auth can-i list pods \
  --as=platform-admin@example.com \
  --as-group=platform-admins \
  -n charlie-prod

# Tenant admin cannot modify RBAC (expect "no")
kubectl auth can-i create roles \
  --as=system:serviceaccount:alpha-dev:alpha-admin-sa \
  -n alpha-dev

# ResourceQuotas exist
kubectl get resourcequota -n alpha-dev
kubectl get resourcequota -n bravo-prod

# Aggregation works: add a new ClusterRole and verify it appears
kubectl get clusterrole tenant-viewer -o yaml | grep -A20 rules
```

## Cleanup

```bash
for t in alpha bravo charlie; do
  kubectl delete namespace ${t}-dev ${t}-prod 2>/dev/null
done
kubectl delete namespace cicd-system 2>/dev/null
kubectl delete clusterrole tenant-admin tenant-developer tenant-viewer \
  platform-admin 2>/dev/null
kubectl delete clusterrolebinding platform-admin-binding 2>/dev/null
```
