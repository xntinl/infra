<!--
difficulty: advanced
concepts: [namespace-isolation, cross-namespace-policies, namespace-labels, multi-namespace-architecture, ingress-controller-exception]
tools: [kubectl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [network-policies-zero-trust, ingress-network-policies, egress-network-policies]
-->

# 7.07 Namespace Isolation Patterns

## Architecture

In multi-team clusters, each team owns one or more namespaces. Namespace isolation ensures that workloads from one team cannot reach workloads from another unless explicitly permitted. This exercise covers three patterns:

1. **Full namespace isolation** -- deny all cross-namespace traffic by default
2. **Selective cross-namespace access** -- allow specific namespaces to reach shared services
3. **Ingress controller exception** -- allow the ingress controller namespace to forward traffic into application namespaces

```
 ingress-ns          team-a-ns          team-b-ns          shared-ns
 +----------+       +----------+       +----------+       +----------+
 | ingress  |------>| app-a    |       | app-b    |------>| redis    |
 | controller|----->|          |       |          |       | (shared) |
 +----------+       +----------+       +----------+       +----------+
                         X                   X
                         |--- blocked -------|
```

## Suggested Steps

### 1. Create four namespaces with descriptive labels

Label each namespace with `team` and `purpose` labels. These labels will be used by `namespaceSelector` in policies.

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-a-ns
  labels:
    team: alpha
    purpose: application
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-b-ns
  labels:
    team: bravo
    purpose: application
---
apiVersion: v1
kind: Namespace
metadata:
  name: shared-ns
  labels:
    team: platform
    purpose: shared-services
---
apiVersion: v1
kind: Namespace
metadata:
  name: ingress-ns
  labels:
    team: platform
    purpose: ingress
```

### 2. Apply default-deny in each application namespace

Create a deny-all policy in `team-a-ns`, `team-b-ns`, and `shared-ns`. Consider whether `ingress-ns` also needs one (it usually does, but with exceptions for the controller).

### 3. Deploy services

- A simple nginx app in `team-a-ns` (label: `app: app-a`)
- A simple nginx app in `team-b-ns` (label: `app: app-b`)
- A Redis instance in `shared-ns` (label: `app: redis-shared`)

### 4. Create namespace-scoped ingress policies

Allow the ingress controller namespace (`purpose: ingress`) to reach application pods on port 80 in both team namespaces:

```yaml
# allow-ingress-controller.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-from-ingress
  namespace: team-a-ns
spec:
  podSelector:
    matchLabels:
      app: app-a
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              purpose: ingress
      ports:
        - protocol: TCP
          port: 80
```

Repeat for `team-b-ns`.

### 5. Allow team-b to access the shared Redis

Create an egress policy in `team-b-ns` and a matching ingress policy in `shared-ns`:

```yaml
# allow-team-b-to-shared-redis.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-egress-to-shared-redis
  namespace: team-b-ns
spec:
  podSelector:
    matchLabels:
      app: app-b
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              purpose: shared-services
          podSelector:
            matchLabels:
              app: redis-shared
      ports:
        - protocol: TCP
          port: 6379
```

### 6. Verify cross-namespace isolation

- `team-a-ns` pods cannot reach `team-b-ns` pods
- `team-b-ns` pods can reach `shared-ns` Redis on port 6379
- `team-a-ns` pods cannot reach `shared-ns` Redis (no allow rule)
- The ingress controller namespace can reach both team namespaces on port 80

### 7. Consider DNS egress

Remember to allow DNS egress in each namespace that has a default-deny-all policy.

## Verify

```bash
# List policies across namespaces
kubectl get networkpolicy -A

# team-a cannot reach team-b (should fail)
kubectl exec -n team-a-ns deploy/app-a -- wget -qO- --timeout=3 http://app-b.team-b-ns 2>&1 || echo "Cross-namespace blocked"

# team-b can reach shared redis (should succeed)
kubectl exec -n team-b-ns deploy/app-b -- sh -c "echo PING | nc -w3 redis-shared.shared-ns 6379"

# team-a cannot reach shared redis (should fail)
kubectl exec -n team-a-ns deploy/app-a -- sh -c "echo PING | nc -w3 redis-shared.shared-ns 6379" 2>&1 || echo "Blocked"
```

## Cleanup

```bash
kubectl delete namespace team-a-ns team-b-ns shared-ns ingress-ns
```

## What's Next

Continue to [7.08 Cilium L7 Network Policies](../08-cilium-l7-policies/) to explore application-layer (HTTP, gRPC) policy enforcement.

## Summary

- Namespace labels are the key to cross-namespace policy selectors.
- Default-deny per namespace prevents accidental cross-team communication.
- The ingress controller namespace typically needs a blanket exception to forward external traffic.
- Shared services require explicit ingress policies listing which consumer namespaces are allowed.

## References

- [Network Policies - namespaceSelector](https://kubernetes.io/docs/concepts/services-networking/network-policies/#behavior-of-to-and-from-selectors)
- [Namespace Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#automatic-labelling)
