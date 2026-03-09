<!--
difficulty: basic
concepts: [default-deny, network-policy, ingress-deny, egress-deny, namespace-security-baseline]
tools: [kubectl]
estimated_time: 20m
bloom_level: understand
prerequisites: [pods, namespaces, network-policies-pod-isolation]
-->

# 7.02 Default Deny All Traffic

## What You Will Learn

- How to create default-deny policies that block all ingress, all egress, or both
- Why default-deny is the foundation of every secure namespace
- The difference between ingress-only, egress-only, and combined deny policies
- How pods behave when no traffic is permitted

## Why This Matters

Without a default-deny policy, any new pod deployed into a namespace can immediately communicate with every other pod in the cluster. A default-deny policy inverts this model: nothing is allowed until an explicit rule says otherwise. This is the cornerstone of zero-trust networking in Kubernetes.

## Steps

### 1. Create a namespace for testing

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: deny-demo
  labels:
    env: demo
```

### 2. Default deny all ingress traffic

This policy blocks every incoming connection to every pod in the namespace. Pods can still initiate outbound connections.

```yaml
# deny-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all-ingress
  namespace: deny-demo
spec:
  podSelector: {}        # applies to all pods
  policyTypes:
    - Ingress            # no ingress rules = deny all inbound
```

### 3. Default deny all egress traffic

This policy blocks every outgoing connection from every pod. Pods can still receive connections (unless also blocked by an ingress policy).

```yaml
# deny-egress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all-egress
  namespace: deny-demo
spec:
  podSelector: {}        # applies to all pods
  policyTypes:
    - Egress             # no egress rules = deny all outbound
```

### 4. Default deny all traffic (both directions)

The most restrictive baseline -- blocks both inbound and outbound traffic.

```yaml
# deny-all.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all-traffic
  namespace: deny-demo
spec:
  podSelector: {}        # applies to all pods
  policyTypes:
    - Ingress            # deny all inbound
    - Egress             # deny all outbound
```

### 5. Deploy test pods

```yaml
# test-pods.yaml
apiVersion: v1
kind: Pod
metadata:
  name: server
  namespace: deny-demo
  labels:
    role: server
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
  name: server
  namespace: deny-demo
spec:
  selector:
    role: server
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: Pod
metadata:
  name: client
  namespace: deny-demo
  labels:
    role: client
spec:
  containers:
    - name: busybox
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
```

### 6. Apply and observe

```bash
# Create namespace and pods first
kubectl apply -f namespace.yaml
kubectl apply -f test-pods.yaml

# Wait for pods
kubectl wait --for=condition=ready pod --all -n deny-demo --timeout=60s

# Test connectivity BEFORE deny (should work)
kubectl exec -n deny-demo client -- wget -qO- --timeout=5 http://server

# Apply the combined deny-all policy
kubectl apply -f deny-all.yaml

# Test connectivity AFTER deny (should fail)
kubectl exec -n deny-demo client -- wget -qO- --timeout=5 http://server 2>&1 || echo "Blocked"
```

## Common Mistakes

| Mistake | Why It Fails | Fix |
|---------|-------------|-----|
| Omitting `policyTypes` entirely | Without `policyTypes`, Kubernetes infers only Ingress if `ingress` is absent, egress is not affected | Always state `policyTypes` explicitly |
| Applying deny-all without a DNS egress exception | All pods lose DNS resolution; services become unreachable by name | Pair deny-all with a DNS egress allow policy |
| Assuming deny-all applies cluster-wide | NetworkPolicies are namespaced; each namespace needs its own | Apply deny policies to every namespace that needs them |
| Creating deny-all in `kube-system` without exceptions | Breaks CoreDNS and other critical services | Be very careful with system namespaces |

## Verify

1. List policies:

```bash
kubectl get networkpolicy -n deny-demo
```

2. Confirm client cannot reach server:

```bash
kubectl exec -n deny-demo client -- wget -qO- --timeout=3 http://server 2>&1 || echo "Blocked as expected"
```

3. Confirm client cannot reach external addresses:

```bash
kubectl exec -n deny-demo client -- wget -qO- --timeout=3 http://1.1.1.1 2>&1 || echo "Egress blocked"
```

4. Describe the policy to verify its scope:

```bash
kubectl describe networkpolicy deny-all-traffic -n deny-demo
```

## Cleanup

```bash
kubectl delete namespace deny-demo
```

## What's Next

Continue to [7.03 Ingress Network Policies with Pod and Namespace Selectors](../03-ingress-network-policies/) to learn how to write granular ingress rules that whitelist specific sources.

## Summary

- A default-deny policy with empty `podSelector` and explicit `policyTypes` blocks all matching traffic.
- You can deny ingress only, egress only, or both directions independently.
- Default-deny is namespace-scoped -- you must apply it to every namespace that needs protection.
- Always pair a full deny-all with a DNS egress exception so pods can resolve service names.
- This pattern is the foundation of zero-trust networking in Kubernetes.

## References

- [Network Policies - Default Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/#default-policies)
- [Declare Network Policy](https://kubernetes.io/docs/tasks/administer-cluster/declare-network-policy/)

## Additional Resources

- [Network Policy Recipes](https://github.com/ahmetb/kubernetes-network-policy-recipes)
- [Network Policy Editor (Cilium)](https://editor.networkpolicy.io/)
