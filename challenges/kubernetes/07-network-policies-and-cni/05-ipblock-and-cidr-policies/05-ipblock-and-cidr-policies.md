<!--
difficulty: intermediate
concepts: [ipblock, cidr, except-ranges, external-traffic-control, network-policy]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [egress-network-policies, ingress-network-policies]
-->

# 7.05 IPBlock and CIDR-Based Network Policies

## What You Will Learn

- How to use `ipBlock` with CIDR notation to allow or block traffic by IP range
- How to use the `except` field to carve out exclusions from a CIDR range
- When to use `ipBlock` vs `podSelector` and `namespaceSelector`
- How to control access to and from external (non-cluster) endpoints

## Steps

### 1. Create namespace with default deny

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ipblock-demo
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: ipblock-demo
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
```

### 2. Allow DNS egress

```yaml
# allow-dns.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: ipblock-demo
spec:
  podSelector: {}
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

### 3. Deploy a web-facing service

```yaml
# web-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: ipblock-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: web-app
  namespace: ipblock-demo
spec:
  selector:
    app: web-app
  ports:
    - port: 80
```

### 4. Allow ingress from a specific CIDR range

Allow traffic only from the 10.0.0.0/16 corporate network, excluding the 10.0.99.0/24 guest subnet.

```yaml
# allow-corporate-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-corporate-ingress
  namespace: ipblock-demo
spec:
  podSelector:
    matchLabels:
      app: web-app
  policyTypes:
    - Ingress
  ingress:
    - from:
        - ipBlock:
            cidr: 10.0.0.0/16          # corporate network
            except:
              - 10.0.99.0/24           # exclude guest WiFi subnet
      ports:
        - protocol: TCP
          port: 80
```

### 5. Allow egress to external HTTPS only (no internal)

Allow the web-app to call external APIs over HTTPS while blocking connections to internal cluster IPs.

```yaml
# allow-external-https.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-external-https
  namespace: ipblock-demo
spec:
  podSelector:
    matchLabels:
      app: web-app
  policyTypes:
    - Egress
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0            # all IPv4 addresses
            except:
              - 10.0.0.0/8             # exclude RFC 1918 ranges
              - 172.16.0.0/12
              - 192.168.0.0/16
      ports:
        - protocol: TCP
          port: 443
```

### 6. Combine ipBlock with podSelector (OR logic)

Allow ingress from either internal frontend pods OR the corporate CIDR.

```yaml
# allow-combined-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-combined-ingress
  namespace: ipblock-demo
spec:
  podSelector:
    matchLabels:
      app: web-app
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:                  # OR - internal frontend pods
            matchLabels:
              role: frontend
        - ipBlock:                      # OR - corporate network
            cidr: 10.0.0.0/16
      ports:
        - protocol: TCP
          port: 80
```

### Spot the Bug

This policy intends to block all RFC 1918 addresses while allowing everything else. What is wrong?

```yaml
egress:
  - to:
      - ipBlock:
          cidr: 10.0.0.0/8
      - ipBlock:
          cidr: 172.16.0.0/12
      - ipBlock:
          cidr: 192.168.0.0/16
```

<details>
<summary>Answer</summary>

This policy **allows** egress to those three CIDR ranges -- it does not block them. `ipBlock` in the `to` array specifies allowed destinations. To block internal ranges, use the `except` pattern: set `cidr: 0.0.0.0/0` and list internal ranges in `except`.

</details>

## Verify

```bash
# Check policies
kubectl get networkpolicy -n ipblock-demo

# Test egress to external HTTPS (depends on cluster internet access)
kubectl exec -n ipblock-demo deploy/web-app -- sh -c "wget -qO- --timeout=5 https://httpbin.org/ip" 2>&1 || echo "Check connectivity"

# Test egress to internal cluster IP is blocked
kubectl exec -n ipblock-demo deploy/web-app -- wget -qO- --timeout=3 http://kubernetes.default.svc 2>&1 || echo "Internal blocked"

# Describe a policy to verify CIDR rules
kubectl describe networkpolicy allow-corporate-ingress -n ipblock-demo
```

## Cleanup

```bash
kubectl delete namespace ipblock-demo
```

## What's Next

Continue to [7.06 Network Policies: Zero Trust Architecture](../06-network-policies-zero-trust/06-network-policies-zero-trust.md) to apply these concepts in a full zero-trust deployment.

## Summary

- `ipBlock.cidr` selects traffic by IP address range using CIDR notation.
- `ipBlock.except` carves out sub-ranges to exclude from the allowed CIDR.
- Use `0.0.0.0/0` with `except` for RFC 1918 ranges to allow only external traffic.
- `ipBlock` does not match cluster-internal pod IPs on some CNIs -- prefer `podSelector` for in-cluster targets.

## References

- [Network Policies - ipBlock](https://kubernetes.io/docs/concepts/services-networking/network-policies/#behavior-of-to-and-from-selectors)
- [CIDR Notation](https://en.wikipedia.org/wiki/Classless_Inter-Domain_Routing)
