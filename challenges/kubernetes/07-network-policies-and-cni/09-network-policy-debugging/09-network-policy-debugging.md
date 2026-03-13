<!--
difficulty: advanced
concepts: [network-policy-debugging, connectivity-testing, policy-ordering, cni-logs, packet-tracing]
tools: [kubectl, cilium-cli, calicoctl, tcpdump]
estimated_time: 40m
bloom_level: analyze
prerequisites: [network-policies-zero-trust, egress-network-policies, ingress-network-policies]
-->

# 7.09 Network Policy Debugging and Troubleshooting

## Architecture

Network policies can silently drop traffic with no obvious error message. Debugging requires a systematic approach: verify the policy exists, confirm selectors match, check the CNI enforcement layer, and trace packets when all else fails.

```
Is the policy created?
   |
   v
Does podSelector match the target pods?
   |
   v
Do ingress/egress selectors match the source/destination?
   |
   v
Is the CNI plugin enforcing policies?
   |
   v
Are there conflicting or overlapping policies?
   |
   v
Packet trace (tcpdump, Cilium monitor, Calico logs)
```

## Suggested Steps

### 1. Verify policies exist and selectors match

```bash
# List all policies in the namespace
kubectl get networkpolicy -n <namespace>

# Describe a specific policy to see resolved selectors
kubectl describe networkpolicy <name> -n <namespace>

# Check which pods the podSelector matches
kubectl get pods -n <namespace> -l <label-key>=<label-value> --show-labels
```

### 2. Confirm labels are correct on pods and namespaces

A common failure: the policy selects `app: backend` but the pod has `app: api-backend`.

```bash
# Show all labels on pods
kubectl get pods -n <namespace> --show-labels

# Show namespace labels (critical for namespaceSelector)
kubectl get namespace <namespace> --show-labels
```

### 3. Check if the CNI is enforcing policies

```bash
# Calico: check policy enforcement status
kubectl get felixconfiguration default -o yaml 2>/dev/null || echo "Not Calico"

# Cilium: check policy enforcement mode
cilium status 2>/dev/null || echo "Not Cilium"
cilium policy get 2>/dev/null

# Flannel: does NOT support NetworkPolicy
# If using Flannel, policies are created but never enforced
```

### 4. Test connectivity systematically

Deploy a debug pod and test each direction:

```yaml
# debug-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: debug
  namespace: <target-namespace>
  labels:
    app: debug
spec:
  containers:
    - name: debug
      image: busybox:1.37
      command: ["sh", "-c", "sleep 3600"]
```

```bash
# Test DNS first
kubectl exec -n <ns> debug -- nslookup kubernetes.default

# Test TCP connectivity
kubectl exec -n <ns> debug -- wget -qO- --timeout=3 http://<service>:<port>

# Test with IP directly (bypasses DNS issues)
kubectl exec -n <ns> debug -- wget -qO- --timeout=3 http://<pod-ip>:<port>
```

### 5. Inspect policy evaluation order

NetworkPolicies are additive. If any policy allows a flow, it is permitted. There is no "deny" override. Check for unexpected allow rules:

```bash
# Get all policies that match a specific pod
kubectl get networkpolicy -n <namespace> -o json | \
  jq '.items[] | select(.spec.podSelector.matchLabels | to_entries[] | .key == "app" and .value == "backend") | .metadata.name'
```

### 6. Packet-level debugging

**Cilium**:
```bash
# Monitor traffic for a specific pod
cilium monitor --related-to <pod-id>

# Policy decision log
cilium monitor --type policy-verdict
```

**Calico**:
```bash
# Enable flow logs
calicoctl patch felixconfiguration default --patch '{"spec":{"flowLogsFlushInterval":"10s"}}'

# Check deny logs
kubectl logs -n calico-system -l k8s-app=calico-node --tail=50
```

**tcpdump on the node** (last resort):
```bash
# Find the node running the pod
kubectl get pod <pod> -n <ns> -o wide

# SSH to that node and capture on the pod's veth interface
crictl ps | grep <pod-name>
nsenter -t <pid> -n tcpdump -i eth0 -nn port 8080
```

### 7. Common debugging scenarios

| Symptom | Likely Cause | Check |
|---------|-------------|-------|
| All traffic blocked after applying policy | Missing DNS egress rule | Add allow-dns policy |
| Policy exists but traffic flows freely | CNI does not support policies (e.g., Flannel) | Switch to Calico/Cilium |
| Cross-namespace traffic blocked | Missing namespace labels | `kubectl get ns --show-labels` |
| Intermittent failures | Policy applied but pods not yet relabeled | Check rollout status |
| Egress works by IP but not by name | DNS egress blocked | Allow UDP/TCP 53 to kube-system |

## Verify

```bash
# Comprehensive policy audit for a namespace
echo "=== Policies ==="
kubectl get networkpolicy -n <namespace>
echo "=== Pod Labels ==="
kubectl get pods -n <namespace> --show-labels
echo "=== Namespace Labels ==="
kubectl get namespace <namespace> --show-labels
echo "=== Policy Details ==="
kubectl get networkpolicy -n <namespace> -o yaml
```

## Cleanup

Remove any debug pods you created:

```bash
kubectl delete pod debug -n <namespace>
```

## What's Next

Continue to [7.10 CNI Plugin Comparison: Calico vs Cilium vs Flannel](../10-cni-comparison/10-cni-comparison.md) to understand the differences between CNI plugins and their NetworkPolicy capabilities.

## Summary

- Always verify pod labels and namespace labels match policy selectors exactly.
- DNS egress is the most commonly forgotten rule in deny-all setups.
- NetworkPolicies are additive -- any matching allow rule permits traffic.
- Use CNI-specific tools (cilium monitor, calicoctl) for deep inspection.
- Flannel does not enforce NetworkPolicies; policies are silently ignored.

## References

- [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Cilium Troubleshooting](https://docs.cilium.io/en/stable/operations/troubleshooting/)
- [Calico Troubleshooting](https://docs.tigera.io/calico/latest/operations/troubleshoot/)
