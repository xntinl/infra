<!--
difficulty: insane
concepts: [microsegmentation, zero-trust, multi-tier-policies, least-privilege-networking, full-policy-coverage]
tools: [kubectl, network-policy-cni]
estimated_time: 90m
bloom_level: create
prerequisites: [network-policies-zero-trust, namespace-isolation-patterns, egress-network-policies, ipblock-and-cidr-policies]
-->

# 7.11 Full Microsegmentation of a Microservices App

## The Scenario

You are the platform security engineer for an e-commerce company. The application consists of six microservices spread across two namespaces. Your task is to implement full microsegmentation so that every service can only communicate with the exact services it needs, on the exact ports required, in the exact direction required. No lateral movement should be possible.

The architecture:

```
  Namespace: ecommerce-frontend
  +-------------+     +-------------+
  | web-ui      |---->| bff-api     |
  | (port 80)   |     | (port 3000) |
  +-------------+     +------+------+
                              |
  ------------------------------------ namespace boundary
                              |
  Namespace: ecommerce-backend
  +------v------+     +-------------+     +-------------+
  | product-svc |---->| inventory   |     | payment-svc |
  | (port 8080) |     | (port 8081) |     | (port 8443) |
  +------+------+     +-------------+     +------^------+
         |                                       |
         +--------->-------->-------->-----------+
                  (product-svc calls payment-svc)

  +-------------+
  | postgres    |  <---- only product-svc and payment-svc can connect
  | (port 5432) |
  +-------------+
```

Additional requirements:
- The `bff-api` calls `product-svc` and `payment-svc` across the namespace boundary
- `product-svc` calls `inventory` and `payment-svc`
- Only `product-svc` and `payment-svc` can reach `postgres`
- All services need DNS resolution
- `payment-svc` must be able to reach an external payment gateway on HTTPS (port 443) using an ipBlock rule, but no other external addresses

## Constraints

1. Default deny-all (ingress + egress) must be applied in both namespaces
2. Every allowed flow must have both an egress rule on the source and an ingress rule on the destination
3. No wildcard namespace selectors -- use explicit namespace labels
4. Port restrictions are mandatory on every rule -- no open-port policies
5. DNS egress must be scoped to the kube-system namespace (not `namespaceSelector: {}`)
6. The payment gateway ipBlock must use a specific CIDR (e.g., `203.0.113.0/24`) with no RFC 1918 ranges allowed
7. No pod should be able to reach any service it does not explicitly need

## Success Criteria

1. `web-ui` can reach `bff-api` on port 3000 and nothing else
2. `bff-api` can reach `product-svc` on port 8080 and `payment-svc` on port 8443
3. `product-svc` can reach `inventory` on port 8081, `payment-svc` on port 8443, and `postgres` on port 5432
4. `payment-svc` can reach `postgres` on port 5432 and the external payment gateway on port 443
5. `inventory` cannot initiate connections to any other service
6. `postgres` cannot initiate connections to any service
7. No cross-namespace traffic is allowed except the explicitly defined flows
8. A rogue pod deployed in either namespace with no matching labels cannot communicate with any service

## Verification Commands

```bash
# Verify policy count (should be 2 deny-all + 2 DNS + many allow rules)
kubectl get networkpolicy -n ecommerce-frontend --no-headers | wc -l
kubectl get networkpolicy -n ecommerce-backend --no-headers | wc -l

# Positive tests (all should succeed)
kubectl exec -n ecommerce-frontend deploy/web-ui -- wget -qO- --timeout=5 http://bff-api:3000
kubectl exec -n ecommerce-frontend deploy/bff-api -- wget -qO- --timeout=5 http://product-svc.ecommerce-backend:8080
kubectl exec -n ecommerce-backend deploy/product-svc -- sh -c "echo PING | nc -w3 postgres 5432"

# Negative tests (all should fail/timeout)
kubectl exec -n ecommerce-frontend deploy/web-ui -- wget -qO- --timeout=3 http://product-svc.ecommerce-backend:8080 2>&1 || echo "PASS: blocked"
kubectl exec -n ecommerce-backend deploy/inventory -- wget -qO- --timeout=3 http://product-svc:8080 2>&1 || echo "PASS: blocked"
kubectl exec -n ecommerce-backend deploy/postgres -- sh -c "echo | nc -w3 product-svc 8080" 2>&1 || echo "PASS: blocked"

# Rogue pod test
kubectl run rogue --image=busybox:1.37 -n ecommerce-backend -- sh -c "sleep 60"
kubectl exec -n ecommerce-backend rogue -- wget -qO- --timeout=3 http://product-svc:8080 2>&1 || echo "PASS: rogue blocked"
kubectl delete pod rogue -n ecommerce-backend
```

## Cleanup

```bash
kubectl delete namespace ecommerce-frontend ecommerce-backend
```
