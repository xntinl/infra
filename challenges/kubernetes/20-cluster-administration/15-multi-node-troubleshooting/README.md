# Multi-Node Cluster Troubleshooting Challenge

<!--
difficulty: insane
concepts: [troubleshooting, multi-node, control-plane, worker-nodes, networking, dns, certificates, kubelet, etcd]
tools: [kubectl, kubeadm, systemctl, journalctl, crictl, etcdctl, openssl, ss, iptables]
estimated_time: 60m
bloom_level: create
prerequisites: [10-cluster-component-debugging, 11-kubelet-troubleshooting, 13-cluster-networking-debugging, 06-certificate-expiry-rotation]
-->

## Scenario

You have inherited a 4-node Kubernetes cluster (1 control plane + 3 workers) that has multiple simultaneous issues. Users are reporting:

- Some deployments are stuck with pods in Pending state
- A specific worker node shows NotReady
- DNS resolution is intermittently failing
- New services are not getting endpoints
- One namespace cannot create any new pods (quota-related)

Your task is to identify and fix all issues. Each issue is independent, but some may mask others.

## Constraints

1. You may not rebuild or reset any node -- all fixes must be in-place
2. You may not delete existing workloads to free resources (fix the root causes)
3. You must document each issue found and the fix applied
4. All fixes must survive a node reboot
5. You must complete all fixes within the exercise time

## Success Criteria

1. All 4 nodes show Ready status
2. No pods are stuck in Pending (except those intentionally unschedulable)
3. DNS resolution works from every node (test with a busybox pod on each worker)
4. All services have correct endpoints
5. The previously-blocked namespace can create new pods
6. `kubectl get events -A --field-selector type=Warning` shows no new warnings after fixes
7. All control plane components pass health checks

## Verification Commands

```bash
# All nodes Ready
kubectl get nodes
# Expected: 4 nodes, all Ready

# No stuck pods
kubectl get pods -A --field-selector status.phase=Pending
# Expected: no results

# DNS works from each worker
for node in worker-1 worker-2 worker-3; do
  kubectl run dns-test-$node --image=busybox:1.37 --restart=Never \
    --overrides="{\"spec\":{\"nodeName\":\"$node\"}}" \
    --command -- nslookup kubernetes.default.svc.cluster.local
  sleep 5
  kubectl logs dns-test-$node
  kubectl delete pod dns-test-$node
done

# All services have endpoints
kubectl get endpoints -A | awk '$2 == "<none>" {print "MISSING ENDPOINTS:", $0}'
# Expected: no output (except headless services with no ready pods)

# Control plane health
for endpoint in https://localhost:6443/healthz https://localhost:10259/healthz https://localhost:10257/healthz; do
  echo "$endpoint: $(curl -sk $endpoint)"
done

# No new warnings
kubectl get events -A --field-selector type=Warning --sort-by='.lastTimestamp' | tail -5

# Test workload creation in the previously-blocked namespace
kubectl run final-test --image=nginx:1.27 -n <blocked-namespace>
kubectl wait --for=condition=ready pod/final-test -n <blocked-namespace> --timeout=60s
kubectl delete pod final-test -n <blocked-namespace>
```

## Cleanup

```bash
# Remove any test pods created during verification
kubectl delete pod --all -l run=dns-test --all-namespaces --ignore-not-found
kubectl delete pod final-test --all-namespaces --ignore-not-found
```
