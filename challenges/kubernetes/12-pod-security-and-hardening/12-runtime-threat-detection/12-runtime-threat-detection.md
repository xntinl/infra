# Exercise 12: Runtime Threat Detection and Response

<!--
difficulty: insane
concepts: [falco, runtime-security, threat-detection, incident-response, ebpf, syscall-monitoring, alerting, automated-response]
tools: [kubectl, helm, falco, kind]
estimated_time: 120m
bloom_level: create
prerequisites: [12-pod-security-and-hardening/05-seccomp-profiles, 12-pod-security-and-hardening/07-apparmor-profiles, 12-pod-security-and-hardening/11-full-cluster-hardening]
-->

## Scenario

Your security team requires runtime threat detection for the Kubernetes cluster. Static policies (PSA, seccomp, AppArmor) prevent many attacks at admission time, but they cannot detect runtime anomalies like a shell being spawned inside a container, a binary being downloaded and executed, or sensitive files being read unexpectedly. You must deploy Falco for runtime syscall monitoring, create custom rules for your workloads, and implement automated incident response that isolates compromised pods.

## Constraints

1. Deploy Falco as a DaemonSet using the eBPF probe (not the kernel module) for compatibility.
2. Create at least 5 custom Falco rules targeting your specific workloads:
   - Detect shell execution in any container (`/bin/sh`, `/bin/bash`, etc.)
   - Detect file writes to `/etc/` in any container
   - Detect outbound connections to unexpected ports (not 80, 443, 53)
   - Detect reading of `/etc/shadow` or `/etc/passwd`
   - Detect process execution of a binary not in the original image
3. Falco alerts must be sent to both stdout (for kubectl logs) and a webhook endpoint.
4. Implement an automated response controller (a simple Pod watching Falco events via the gRPC API) that:
   - Adds a label `security.example.com/compromised=true` to pods that trigger critical alerts
   - Creates a NetworkPolicy that isolates the labeled pod (deny all egress)
   - Creates a Kubernetes Event recording the incident
5. All Falco pods must themselves be hardened: non-root where possible, read-only filesystem, dropped capabilities (except the ones Falco requires).
6. Create a "canary" deployment that intentionally triggers each custom rule to verify detection works.
7. Response latency from detection to pod isolation must be under 30 seconds.

## Success Criteria

1. Falco DaemonSet is running on every node with eBPF probe.
2. Each of the 5 custom rules triggers an alert when the corresponding canary action is performed.
3. Alerts appear in both Falco stdout and the webhook endpoint.
4. The response controller automatically labels and isolates a compromised pod within 30 seconds.
5. The isolated pod cannot make outbound network connections after isolation.
6. An Event is created on the compromised pod with the incident details.
7. Normal workloads (not triggering rules) continue to run without interference.

## Verification Commands

```bash
# Falco is running on all nodes
kubectl get ds falco -n falco-system
kubectl get pods -n falco-system -o wide

# Trigger a shell detection
kubectl exec -it canary-pod -n canary -- /bin/sh -c "echo triggered"

# Check Falco logs for the alert
kubectl logs -n falco-system -l app.kubernetes.io/name=falco --tail=20 | \
  grep "shell"

# Trigger a file write to /etc
kubectl exec canary-pod -n canary -- touch /etc/evil

# Check for automated response
kubectl get pod canary-pod -n canary --show-labels | \
  grep compromised

# Check isolation NetworkPolicy was created
kubectl get networkpolicy -n canary

# Verify pod is isolated (outbound blocked)
kubectl exec canary-pod -n canary -- \
  wget -q --timeout=3 http://example.com 2>&1
# Expected: timeout or connection refused

# Check incident Event
kubectl get events -n canary --field-selector reason=SecurityIncident

# Response controller is running
kubectl get pods -n falco-system -l app=falco-responder

# Verify all 5 rules are loaded
kubectl exec -n falco-system $(kubectl get pod -n falco-system -l app.kubernetes.io/name=falco -o name | head -1) -- \
  cat /etc/falco/falco_rules.local.yaml | grep "rule:"
```

## Cleanup

```bash
kubectl delete namespace canary
helm uninstall falco -n falco-system 2>/dev/null
kubectl delete namespace falco-system
```
