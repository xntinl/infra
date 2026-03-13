# Exercise 12: Zero-Trust API Access Architecture

<!--
difficulty: insane
concepts: [zero-trust, oidc, service-mesh, network-policies, audit-logging, token-projection, mtls, admission-control]
tools: [kubectl, kind]
estimated_time: 90m
bloom_level: create
prerequisites: [11-rbac-and-authentication/07-service-account-token-projection, 11-rbac-and-authentication/09-oidc-authentication, 11-rbac-and-authentication/10-impersonation-and-audit]
-->

## Scenario

Your organization is adopting a zero-trust security model for Kubernetes API access. The principle is: **never trust, always verify** -- every API request must be authenticated, authorized, and audited regardless of its network origin. You must design and implement a comprehensive API access architecture that eliminates implicit trust.

## Constraints

1. No pod may use the `default` ServiceAccount. Every workload must have a dedicated ServiceAccount with `automountServiceAccountToken: false`.
2. Pods that need API access must use projected tokens with audience-scoped, time-limited JWTs (maximum 1-hour expiration).
3. Human users must authenticate via OIDC (simulated with impersonation). No static tokens or client certificates for humans.
4. All API access must be logged via an audit policy with at least `Metadata` level for all resources and `RequestResponse` level for Secrets, RBAC objects, and authentication resources.
5. A ValidatingWebhookConfiguration must reject any pod that mounts the default ServiceAccount token path (`/var/run/secrets/kubernetes.io/serviceaccount`).
6. NetworkPolicies must restrict which pods can reach the API server (only pods with label `api-access: "true"`).
7. RBAC must follow deny-by-default: no ClusterRoleBindings to broad groups (e.g., `system:authenticated`), and every binding must reference a named ServiceAccount, user, or specific group.
8. Implement token review: a sidecar container in sensitive workloads that validates peer tokens before allowing requests.

## Success Criteria

1. A pod with the `default` ServiceAccount and automount enabled is rejected by the webhook.
2. A pod with a dedicated ServiceAccount and projected token (audience-scoped, 1-hour TTL) is accepted.
3. Human access via impersonation only works for OIDC-prefixed users with matching ClusterRoleBindings.
4. Audit logs capture every Secret read, RBAC modification, and authentication event with full request/response bodies.
5. Pods without the `api-access: "true"` label cannot reach the API server (NetworkPolicy enforced).
6. TokenReview validates projected tokens and rejects expired or wrong-audience tokens.
7. No pod in any namespace uses a legacy Secret-based ServiceAccount token.

## Verification Commands

```bash
# Pod with default SA is rejected
kubectl run test-default --image=nginx:1.27 -n zero-trust 2>&1 | grep -i "denied\|rejected"

# Pod with dedicated SA and projected token is accepted
kubectl get pod secure-workload -n zero-trust -o jsonpath='{.status.phase}'

# Audit log captures Secret access
docker exec -it kind-control-plane \
  grep '"secrets"' /var/log/kubernetes/audit.log | \
  grep RequestResponse | tail -3

# NetworkPolicy blocks API access from unlabeled pods
kubectl exec -n zero-trust no-api-pod -- \
  curl -sk --max-time 3 https://kubernetes.default.svc/api 2>&1

# TokenReview validates tokens
TOKEN=$(kubectl create token secure-sa -n zero-trust --audience=app --duration=600s)
kubectl create -f - <<EOF
apiVersion: authentication.k8s.io/v1
kind: TokenReview
spec:
  token: "$TOKEN"
  audiences: ["app"]
EOF

# Check no legacy tokens exist
kubectl get secrets --all-namespaces -o json | \
  jq '.items[] | select(.type=="kubernetes.io/service-account-token") | .metadata.name'

# Verify OIDC-prefixed user access
kubectl auth can-i list pods \
  --as="oidc:admin@example.com" \
  --as-group="oidc:cluster-admins" \
  -n zero-trust
```

## Cleanup

```bash
kubectl delete namespace zero-trust 2>/dev/null
kubectl delete validatingwebhookconfiguration default-sa-blocker 2>/dev/null
kubectl delete clusterrolebinding oidc-admins-binding 2>/dev/null
kubectl delete clusterrole oidc-admins 2>/dev/null
# If using kind: kind delete cluster
```
