# Exercise 11: Full CIS Benchmark Cluster Hardening

<!--
difficulty: insane
concepts: [cis-benchmark, cluster-hardening, kube-bench, api-server-flags, etcd-security, kubelet-config, audit-logging, encryption-at-rest]
tools: [kubectl, kube-bench, kind]
estimated_time: 120m
bloom_level: create
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security, 12-pod-security-and-hardening/05-seccomp-profiles, 12-pod-security-and-hardening/06-secrets-encryption-at-rest, 12-pod-security-and-hardening/07-apparmor-profiles]
-->

## Scenario

You have been given a freshly provisioned Kubernetes cluster (kind or kubeadm) with default settings. Your task is to harden it to pass the CIS Kubernetes Benchmark v1.8+ with zero failures at Level 1 and no more than 3 failures at Level 2. You must address API server configuration, etcd encryption, kubelet security, audit logging, pod security, and network policies.

## Constraints

1. Use `kube-bench` to audit the cluster before and after hardening. The initial scan will show many failures.
2. The API server must be configured with: `--anonymous-auth=false`, `--audit-log-path`, `--audit-log-maxage=30`, `--audit-log-maxbackup=10`, `--encryption-provider-config`, `--kubelet-certificate-authority`, `--profiling=false`, and `--request-timeout=300s`.
3. etcd must use client certificate authentication and have peer TLS configured.
4. The kubelet must have: `--anonymous-auth=false`, `--authorization-mode=Webhook`, `--read-only-port=0`, `--protect-kernel-defaults=true`, and `--streaming-connection-idle-timeout` set to a non-zero value.
5. All namespaces (except kube-system and kube-public) must have Pod Security Admission set to at least `baseline` enforcement.
6. A default NetworkPolicy must deny all ingress in every application namespace.
7. Secrets encryption at rest must be enabled with `aescbc` or `secretbox`.
8. An audit policy must be in place that records at least `Metadata` for all resources and `RequestResponse` for Secrets and RBAC objects.

## Success Criteria

1. `kube-bench run --targets master` passes all Level 1 checks.
2. `kube-bench run --targets node` passes all Level 1 checks.
3. No more than 3 Level 2 check failures across master and node.
4. `kubectl get ns --show-labels` shows PSA labels on all application namespaces.
5. A Secret created after hardening is encrypted in etcd (verified with etcdctl).
6. Audit logs are being written to the configured path.
7. The kubelet's read-only port (10255) is not accessible.
8. Default deny NetworkPolicies exist in all application namespaces.

## Verification Commands

```bash
# Run CIS benchmark
kube-bench run --targets master 2>&1 | tail -30
kube-bench run --targets node 2>&1 | tail -30

# Check API server flags
kubectl get pod -n kube-system -l component=kube-apiserver -o yaml | \
  grep -E "anonymous-auth|profiling|audit-log|encryption-provider"

# Check kubelet configuration
kubectl proxy &
curl -s http://localhost:8001/api/v1/nodes/$(kubectl get nodes -o name | head -1 | cut -d/ -f2)/proxy/configz | \
  python3 -m json.tool | grep -E "anonymousAuth|readOnlyPort|authorizationMode"

# Verify encryption at rest
kubectl create secret generic cis-test-secret \
  --from-literal=data=sensitive-value
sudo ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/cis-test-secret | hexdump -C | head -5
# Expected: encrypted (k8s:enc: prefix)

# Verify audit logs
ls -la /var/log/kubernetes/audit.log

# Check PSA labels
kubectl get ns --show-labels | grep pod-security

# Check NetworkPolicies
kubectl get networkpolicies -A

# Verify kubelet read-only port is disabled
curl -s http://localhost:10255/healthz 2>&1
# Expected: connection refused
```

## Cleanup

```bash
kubectl delete secret cis-test-secret 2>/dev/null
# If using kind: kind delete cluster
```
