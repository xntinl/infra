<!--
difficulty: advanced
concepts: [cni, calico, cilium, flannel, ebpf, network-plugin, overlay-network, vxlan, wireguard]
tools: [kubectl, cilium-cli, calicoctl]
estimated_time: 45m
bloom_level: analyze
prerequisites: [network-policies-pod-isolation, network-policy-debugging]
-->

# 7.10 CNI Plugin Comparison: Calico vs Cilium vs Flannel

## Architecture

The Container Network Interface (CNI) plugin is responsible for assigning IP addresses to pods, setting up routes between nodes, and optionally enforcing NetworkPolicy. Choosing the right CNI is one of the most impactful infrastructure decisions for a Kubernetes cluster.

```
                    +-------------------+
                    |   Kubernetes API  |
                    +-------------------+
                            |
              +-------------+-------------+
              |             |             |
         +---------+  +---------+  +---------+
         | Flannel |  | Calico  |  | Cilium  |
         +---------+  +---------+  +---------+
         | VXLAN   |  | BGP/VXLAN| | eBPF    |
         | L3 only |  | L3/L4   |  | L3-L7   |
         | No NP   |  | Full NP |  | Full NP |
         +---------+  +---------+  +---------+
```

## Suggested Steps

### 1. Understand the comparison matrix

| Feature | Flannel | Calico | Cilium |
|---------|---------|--------|--------|
| NetworkPolicy support | No | Yes (L3/L4) | Yes (L3/L4/L7) |
| Data plane | VXLAN/host-gw | iptables/eBPF | eBPF |
| Encryption | No | WireGuard | WireGuard/IPsec |
| Observability | Minimal | Flow logs | Hubble (full visibility) |
| Performance | Good | Good (eBPF mode) | Excellent (kube-proxy replacement) |
| Complexity | Low | Medium | Medium-High |
| FQDN-based policies | No | Enterprise only | Yes (open source) |
| Multi-cluster | No | Yes (Typha) | Yes (ClusterMesh) |

### 2. Install and compare (pick one or more)

**Flannel** (simplest, no NetworkPolicy):
```bash
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
```

**Calico** (production standard, full L3/L4 NetworkPolicy):
```bash
kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml
```

**Cilium** (eBPF-based, L7 policies, observability):
```bash
cilium install
cilium status --wait
```

### 3. Test NetworkPolicy enforcement

Deploy the same deny-all + allow policy set from exercise 7.01 and verify:

- **Flannel**: policies are accepted by the API but traffic flows freely (no enforcement)
- **Calico**: policies are enforced at L3/L4; unauthorized traffic is dropped
- **Cilium**: policies are enforced at L3/L4/L7; HTTP-level filtering works

### 4. Compare observability

**Calico flow logs**:
```bash
calicoctl node status
kubectl logs -n calico-system -l k8s-app=calico-node --tail=20
```

**Cilium Hubble**:
```bash
cilium hubble port-forward &
hubble observe --namespace netpol-demo
hubble observe --verdict DROPPED
```

### 5. Evaluate performance characteristics

- **Flannel + kube-proxy**: iptables-based service routing; linear rule scanning as service count grows
- **Calico eBPF mode**: replaces kube-proxy; hash-based lookups for services
- **Cilium**: native eBPF data plane; replaces kube-proxy; constant-time service lookups

### 6. Analyze when to use each

| Use Case | Recommended CNI |
|----------|----------------|
| Development/testing, no policy needed | Flannel |
| Production with L3/L4 NetworkPolicy | Calico |
| Zero-trust with L7 policies | Cilium |
| Compliance requiring encryption | Calico or Cilium (WireGuard) |
| Multi-cluster networking | Cilium (ClusterMesh) or Calico (federation) |
| Maximum observability | Cilium (Hubble) |

## Verify

```bash
# Check which CNI is installed
ls /etc/cni/net.d/

# Verify pod networking
kubectl run test --image=busybox:1.37 --rm -it -- ip addr

# Check CNI-specific components
kubectl get pods -n kube-system | grep -E "calico|cilium|flannel"

# Test a basic NetworkPolicy to confirm enforcement
kubectl create namespace cni-test
kubectl run server --image=nginx:1.27 -n cni-test --port=80
kubectl expose pod server --port=80 -n cni-test
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: deny-all
  namespace: cni-test
spec:
  podSelector: {}
  policyTypes: [Ingress]
EOF
kubectl run client --image=busybox:1.37 -n cni-test --rm -it -- wget -qO- --timeout=3 http://server 2>&1 || echo "Policy enforced"
```

## Cleanup

```bash
kubectl delete namespace cni-test
```

## What's Next

Continue to [7.11 Full Microsegmentation of a Microservices App](../11-microsegmentation-challenge/11-microsegmentation-challenge.md) to apply everything you have learned in a full-scale challenge.

## Summary

- Flannel is the simplest CNI but does not enforce NetworkPolicy.
- Calico provides robust L3/L4 NetworkPolicy with optional eBPF data plane.
- Cilium offers L7 policies, eBPF performance, and Hubble observability.
- CNI choice determines whether your NetworkPolicies are enforced or silently ignored.

## References

- [Kubernetes Network Plugins](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/)
- [Calico Documentation](https://docs.tigera.io/calico/latest/about/)
- [Cilium Documentation](https://docs.cilium.io/en/stable/)
- [Flannel Documentation](https://github.com/flannel-io/flannel)
