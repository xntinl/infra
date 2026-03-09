# 14. Multi-Cluster Service Discovery

<!--
difficulty: insane
concepts: [multi-cluster, service-export, service-import, dns-forwarding, federation]
tools: [kubectl, minikube, kind]
estimated_time: 120m
bloom_level: create
prerequisites: [05-05, 05-08, 05-12]
-->

## Prerequisites

- Two running Kubernetes clusters (use `kind` to create two local clusters)
- `kubectl` installed with contexts configured for both clusters
- Completion of exercises 05, 08, and 12

## The Scenario

Your company operates two Kubernetes clusters: `cluster-west` runs the user-facing API, and `cluster-east` runs the analytics backend. The API in `cluster-west` needs to discover and call the analytics service in `cluster-east` using standard Kubernetes DNS names, without hardcoding IPs or using VPN tunnels. Design and implement a multi-cluster service discovery solution using only native Kubernetes primitives and CoreDNS configuration.

## Constraints

1. **Two clusters**: create two `kind` clusters named `cluster-west` and `cluster-east`. Each cluster must have its own Service CIDR and Pod CIDR (non-overlapping).
2. **Service in cluster-east**: deploy an `analytics-svc` ClusterIP Service backed by a 2-replica Deployment serving JSON at `/api/report`.
3. **Discovery from cluster-west**: pods in `cluster-west` must be able to resolve `analytics.external.svc.cluster.local` and have it return the correct endpoint(s) from `cluster-east`.
4. **CoreDNS forwarding**: configure CoreDNS in `cluster-west` with a custom zone (`external.svc.cluster.local`) that conditionally forwards to `cluster-east`'s CoreDNS. You must patch the CoreDNS ConfigMap directly.
5. **ExternalName fallback**: as a fallback pattern, create an ExternalName Service in `cluster-west` pointing to `analytics-svc.default.svc.cluster.local` of `cluster-east`, and demonstrate that applications can consume it transparently.
6. **No third-party tools**: no Submariner, no Cilium ClusterMesh, no MCS API controllers. Only CoreDNS configuration, Services, and standard kubectl commands.
7. **Connectivity**: since both `kind` clusters share the host Docker network, pod-to-pod connectivity works natively. In production you would need network peering -- document this assumption.

## Success Criteria

1. Two `kind` clusters are running with non-overlapping CIDRs.
2. `analytics-svc` is accessible from within `cluster-east` and returns valid JSON.
3. CoreDNS in `cluster-west` has a custom forward zone configured for `external.svc.cluster.local`.
4. A pod in `cluster-west` can resolve `analytics.external.svc.cluster.local` and reach the analytics service.
5. The ExternalName fallback Service resolves correctly from `cluster-west`.
6. Documentation of the network connectivity assumption and what would change in a real multi-cloud environment.

## Verification Commands

```bash
# Switch to cluster-east context
kubectl --context kind-cluster-east get svc analytics-svc
kubectl --context kind-cluster-east run test --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://analytics-svc/api/report

# Switch to cluster-west context
kubectl --context kind-cluster-west get configmap coredns -n kube-system -o yaml

# DNS resolution from cluster-west
kubectl --context kind-cluster-west run dns-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup analytics.external.svc.cluster.local

# End-to-end connectivity from cluster-west to cluster-east
kubectl --context kind-cluster-west run e2e-test --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://analytics.external.svc.cluster.local/api/report

# ExternalName fallback
kubectl --context kind-cluster-west get svc analytics-fallback
kubectl --context kind-cluster-west run fallback-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup analytics-fallback
```

## Cleanup

```bash
kind delete cluster --name cluster-west
kind delete cluster --name cluster-east
```
