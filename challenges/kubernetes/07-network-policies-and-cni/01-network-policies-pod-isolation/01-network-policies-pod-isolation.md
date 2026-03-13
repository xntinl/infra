<!--
difficulty: basic
concepts: [network-policies, pod-selector, ingress-rules, egress-rules, cni-requirement]
tools: [kubectl, network-policy-cni]
estimated_time: 30m
bloom_level: understand
prerequisites: [pods, services, namespaces, labels-and-selectors]
-->

# 7.01 Network Policies: Pod Isolation

## What You Will Learn

- How Kubernetes NetworkPolicy resources control traffic flow between pods
- The difference between ingress and egress policy rules
- How `podSelector` targets specific pods by label
- Why a compatible CNI plugin (Calico, Cilium) is required for enforcement

## Why This Matters

By default, every pod in a Kubernetes cluster can communicate with every other pod. In production, unrestricted communication is a security risk -- a compromised pod could reach any service. NetworkPolicies let you declare exactly which traffic flows are permitted, isolating workloads from each other.

## Steps

### 1. Create a namespace with labels

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: netpol-demo
  labels:
    purpose: network-policy-demo   # label used by namespace selectors later
```

### 2. Apply a default deny-all policy

This policy selects every pod in the namespace (empty `podSelector`) and lists both Ingress and Egress in `policyTypes` without defining any allow rules -- effectively blocking all traffic.

```yaml
# default-deny.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: netpol-demo
spec:
  podSelector: {}          # matches every pod in the namespace
  policyTypes:
    - Ingress              # blocks all incoming traffic
    - Egress               # blocks all outgoing traffic
```

### 3. Deploy a backend service

```yaml
# backend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: netpol-demo
  labels:
    app: backend
    tier: backend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend
      tier: backend
  template:
    metadata:
      labels:
        app: backend
        tier: backend
    spec:
      containers:
        - name: backend
          image: nginx:1.27
          ports:
            - containerPort: 8080
          command:
            - /bin/sh
            - -c
            - |
              cat > /etc/nginx/conf.d/default.conf << 'EOF'
              server {
                  listen 8080;
                  location / {
                      return 200 '{"service":"backend","hostname":"HOSTNAME"}\n';
                      add_header Content-Type application/json;
                  }
                  location /health {
                      return 200 '{"status":"ok"}\n';
                      add_header Content-Type application/json;
                  }
              }
              EOF
              sed -i "s/HOSTNAME/$(hostname)/" /etc/nginx/conf.d/default.conf
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: backend-svc
  namespace: netpol-demo
spec:
  type: ClusterIP
  selector:
    app: backend
    tier: backend
  ports:
    - port: 8080
      targetPort: 8080
      name: http
```

### 4. Deploy a frontend pod

```yaml
# frontend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: netpol-demo
  labels:
    app: frontend
    tier: frontend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frontend
      tier: frontend
  template:
    metadata:
      labels:
        app: frontend
        tier: frontend
    spec:
      containers:
        - name: frontend
          image: busybox:1.37
          command: ["sh", "-c", "sleep 3600"]
```

### 5. Deploy an unauthorized pod

```yaml
# unauthorized.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: unauthorized
  namespace: netpol-demo
  labels:
    app: unauthorized
    tier: external
spec:
  replicas: 1
  selector:
    matchLabels:
      app: unauthorized
      tier: external
  template:
    metadata:
      labels:
        app: unauthorized
        tier: external
    spec:
      containers:
        - name: attacker
          image: busybox:1.37
          command: ["sh", "-c", "sleep 3600"]
```

### 6. Allow frontend-to-backend ingress on port 8080

```yaml
# allow-frontend-to-backend.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-frontend-to-backend
  namespace: netpol-demo
spec:
  podSelector:
    matchLabels:
      app: backend              # this policy applies to backend pods
      tier: backend
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: frontend     # only frontend pods may connect
              tier: frontend
      ports:
        - protocol: TCP
          port: 8080            # only on port 8080
```

### 7. Allow frontend egress to backend and DNS

```yaml
# allow-frontend-egress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-frontend-egress
  namespace: netpol-demo
spec:
  podSelector:
    matchLabels:
      app: frontend
      tier: frontend
  policyTypes:
    - Egress
  egress:
    - to:                        # rule 1: allow traffic to backend pods
        - podSelector:
            matchLabels:
              app: backend
              tier: backend
      ports:
        - protocol: TCP
          port: 8080
    - to:                        # rule 2: allow DNS resolution
        - namespaceSelector: {}  # any namespace (kube-system hosts CoreDNS)
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

### 8. Apply all manifests

```bash
kubectl apply -f namespace.yaml
kubectl apply -f default-deny.yaml
kubectl apply -f backend.yaml
kubectl apply -f frontend.yaml
kubectl apply -f unauthorized.yaml
kubectl apply -f allow-frontend-to-backend.yaml
kubectl apply -f allow-frontend-egress.yaml
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Creating NetworkPolicies without a compatible CNI | Resources are accepted by the API but have zero effect | Install Calico or Cilium before relying on policies |
| Forgetting egress DNS rule | Pods cannot resolve service names, connections time out | Always add an egress rule for UDP/TCP 53 |
| Using `podSelector: {}` without specifying `policyTypes` | Policy matches all pods but does not block anything because no types are listed | Explicitly list `Ingress`, `Egress`, or both in `policyTypes` |
| Mixing AND vs OR semantics in `from` | Multiple entries under `from` are OR'd; entries within a single `from` item are AND'd | Double-check the YAML list structure |

## Verify

1. Confirm all policies exist:

```bash
kubectl get networkpolicy -n netpol-demo
```

Expected: `default-deny-all`, `allow-frontend-to-backend`, `allow-frontend-egress`.

2. Confirm pods are running:

```bash
kubectl get pods -n netpol-demo -o wide
```

3. Frontend CAN reach backend (allowed):

```bash
kubectl exec -n netpol-demo deploy/frontend -- wget -qO- --timeout=5 http://backend-svc:8080/
```

Expected: JSON response from backend.

4. Unauthorized pod CANNOT reach backend (blocked):

```bash
kubectl exec -n netpol-demo deploy/unauthorized -- wget -qO- --timeout=5 http://backend-svc:8080/ 2>&1 || echo "Connection blocked"
```

Expected: timeout or connection refused.

5. Inspect a policy:

```bash
kubectl describe networkpolicy allow-frontend-to-backend -n netpol-demo
```

## Cleanup

```bash
kubectl delete namespace netpol-demo
```

## What's Next

Move on to [7.02 Default Deny All Traffic](../02-default-deny-policies/02-default-deny-policies.md) to learn how to build a deny-by-default foundation for every namespace.

## Summary

- NetworkPolicy resources declare which pods can send or receive traffic.
- An empty `podSelector` matches every pod in the namespace.
- Ingress rules control inbound traffic; egress rules control outbound traffic.
- DNS egress (port 53) must be explicitly allowed when using default-deny.
- A CNI plugin that supports NetworkPolicy (Calico, Cilium) is required for enforcement.
- Unauthorized pods are effectively isolated when no matching allow rule exists.

## References

- [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Declare Network Policy](https://kubernetes.io/docs/tasks/administer-cluster/declare-network-policy/)

## Additional Resources

- [Network Policy Recipes](https://github.com/ahmetb/kubernetes-network-policy-recipes)
- [Calico Network Policies](https://docs.tigera.io/calico/latest/network-policy/)
- [Network Policy Editor (Cilium)](https://editor.networkpolicy.io/)
