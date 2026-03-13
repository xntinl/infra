<!--
difficulty: intermediate
concepts: [ingress-network-policy, pod-selector, namespace-selector, combined-selectors, port-filtering]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [network-policies-pod-isolation, default-deny-policies]
-->

# 7.03 Ingress Network Policies with Pod and Namespace Selectors

## What You Will Learn

- How to write ingress rules using `podSelector`, `namespaceSelector`, and their combination
- The critical difference between AND and OR semantics in the `from` array
- How to restrict ingress to specific ports and protocols

## Steps

### 1. Create namespaces

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: app-ns
  labels:
    team: app
---
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring-ns
  labels:
    team: monitoring
---
apiVersion: v1
kind: Namespace
metadata:
  name: untrusted-ns
  labels:
    team: untrusted
```

### 2. Deploy a target service

```yaml
# api-server.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  namespace: app-ns
spec:
  replicas: 1
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
        tier: backend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
              name: http
            - containerPort: 9090
              name: metrics
---
apiVersion: v1
kind: Service
metadata:
  name: api-server
  namespace: app-ns
spec:
  selector:
    app: api-server
  ports:
    - port: 80
      targetPort: 80
      name: http
    - port: 9090
      targetPort: 9090
      name: metrics
```

### 3. Allow ingress from specific pods in the same namespace (AND)

This policy allows ingress to the api-server only from pods labeled `role: frontend` within the same namespace. Both `podSelector` conditions in a single `from` item are AND'd.

```yaml
# allow-same-ns-frontend.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-same-ns-frontend
  namespace: app-ns
spec:
  podSelector:
    matchLabels:
      app: api-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:                  # same-namespace pods with role=frontend
            matchLabels:
              role: frontend
      ports:
        - protocol: TCP
          port: 80
```

### 4. Allow ingress from a different namespace (namespaceSelector)

Allow the monitoring namespace to scrape the metrics port.

```yaml
# allow-monitoring-ns.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-monitoring-scrape
  namespace: app-ns
spec:
  podSelector:
    matchLabels:
      app: api-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:            # any pod in monitoring-ns
            matchLabels:
              team: monitoring
      ports:
        - protocol: TCP
          port: 9090                    # metrics port only
```

### 5. Combined selector: namespace AND pod (single `from` item)

Allow only pods labeled `role: prometheus` in the `monitoring-ns` namespace. When `namespaceSelector` and `podSelector` appear in the same `from` item, they are AND'd.

```yaml
# allow-prometheus-only.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-prometheus-only
  namespace: app-ns
spec:
  podSelector:
    matchLabels:
      app: api-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              team: monitoring
          podSelector:                  # AND -- must match both
            matchLabels:
              role: prometheus
      ports:
        - protocol: TCP
          port: 9090
```

### Spot the Bug

The following policy is intended to allow ingress only from pods labeled `role: prometheus` in the monitoring namespace. What is wrong?

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: buggy-policy
  namespace: app-ns
spec:
  podSelector:
    matchLabels:
      app: api-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              team: monitoring
        - podSelector:
            matchLabels:
              role: prometheus
      ports:
        - protocol: TCP
          port: 9090
```

<details>
<summary>Answer</summary>

The two selectors are separate items in the `from` array (note the two `-` dashes), so they are OR'd: traffic is allowed from **any pod in monitoring-ns** OR **any pod labeled role=prometheus in app-ns**. To AND them, both selectors must be in a single `from` item (no second dash).

</details>

### 6. Deploy test pods

```bash
# Frontend in app-ns
kubectl run frontend --image=busybox:1.37 -n app-ns -l role=frontend -- sh -c "sleep 3600"

# Random pod in app-ns (no matching label)
kubectl run random --image=busybox:1.37 -n app-ns -l role=random -- sh -c "sleep 3600"

# Prometheus in monitoring-ns
kubectl run prometheus --image=busybox:1.37 -n monitoring-ns -l role=prometheus -- sh -c "sleep 3600"

# Attacker in untrusted-ns
kubectl run attacker --image=busybox:1.37 -n untrusted-ns -l role=attacker -- sh -c "sleep 3600"
```

## Verify

```bash
# frontend in app-ns -> api-server port 80 (should succeed)
kubectl exec -n app-ns frontend -- wget -qO- --timeout=5 http://api-server

# random pod in app-ns -> api-server port 80 (should fail)
kubectl exec -n app-ns random -- wget -qO- --timeout=3 http://api-server 2>&1 || echo "Blocked"

# prometheus in monitoring-ns -> api-server port 9090 (should succeed)
kubectl exec -n monitoring-ns prometheus -- wget -qO- --timeout=5 http://api-server.app-ns:9090 2>&1 || echo "Check result"

# attacker in untrusted-ns -> api-server (should fail)
kubectl exec -n untrusted-ns attacker -- wget -qO- --timeout=3 http://api-server.app-ns 2>&1 || echo "Blocked"
```

## Cleanup

```bash
kubectl delete namespace app-ns monitoring-ns untrusted-ns
```

## What's Next

Continue to [7.04 Egress Network Policies and DNS Access](../04-egress-network-policies/04-egress-network-policies.md) to learn how to control outbound traffic from your pods.

## Summary

- `podSelector` in a `from` rule matches pods in the same namespace.
- `namespaceSelector` matches pods from other namespaces by label.
- Two selectors in a single `from` item are AND'd; separate `from` items are OR'd.
- Always pair ingress policies with port restrictions to limit the attack surface.

## References

- [Network Policies - Behavior of `to` and `from` selectors](https://kubernetes.io/docs/concepts/services-networking/network-policies/#behavior-of-to-and-from-selectors)
- [Network Policy Recipes](https://github.com/ahmetb/kubernetes-network-policy-recipes)
