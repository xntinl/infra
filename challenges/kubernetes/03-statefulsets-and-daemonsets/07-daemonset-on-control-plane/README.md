# 7. Running DaemonSets on Control Plane Nodes

<!--
difficulty: advanced
concepts: [control-plane-taints, tolerations, daemonset-scheduling, node-roles, system-pods]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [03-05, 03-06]
-->

## Prerequisites

- A running Kubernetes cluster with identifiable control-plane nodes (kind or kubeadm clusters work best; minikube single-node also works)
- `kubectl` installed and configured
- Completion of exercises [05](../05-daemonsets-with-tolerations/) and [06](../06-daemonset-update-strategies/)
- Understanding of taints and tolerations

## Learning Objectives

- **Analyze** the standard taints applied to control-plane nodes and why they exist
- **Evaluate** which workloads justify running on control-plane nodes
- **Create** DaemonSets with precise tolerations for control-plane scheduling

## Architecture

Control-plane nodes in a kubeadm-bootstrapped cluster carry these taints:

```
node-role.kubernetes.io/control-plane:NoSchedule
```

Older clusters (pre-1.24) may also have:

```
node-role.kubernetes.io/master:NoSchedule
```

These taints prevent user workloads from competing with kube-apiserver, etcd, kube-scheduler, and kube-controller-manager for resources. Only system-critical DaemonSets (monitoring, log collection, security agents) should tolerate these taints.

## The Challenge

Build a comprehensive node monitoring DaemonSet that:

1. Runs on ALL nodes including control-plane nodes
2. Tolerates both `control-plane` and legacy `master` taints for compatibility
3. Collects node-level metrics by mounting `/proc` and `/sys` read-only
4. Uses appropriate resource limits to avoid starving control-plane components
5. Uses `OnDelete` update strategy to prevent automated rollouts on critical nodes

Consider:
- What happens if your DaemonSet has a bug and crashes on the control-plane node?
- How do `priorityClassName` values like `system-node-critical` affect eviction?
- What tolerations do kube-system DaemonSets like kube-proxy use?

<details>
<summary>Hint 1: Inspect existing control-plane tolerations</summary>

```bash
kubectl get ds -n kube-system kube-proxy -o jsonpath='{.spec.template.spec.tolerations}' | jq .
```

This shows you the pattern that Kubernetes itself uses for system DaemonSets.

</details>

<details>
<summary>Hint 2: Tolerating multiple taints</summary>

```yaml
tolerations:
  - key: "node-role.kubernetes.io/control-plane"
    operator: "Exists"
    effect: "NoSchedule"
  - key: "node-role.kubernetes.io/master"
    operator: "Exists"
    effect: "NoSchedule"
```

</details>

<details>
<summary>Hint 3: Mounting proc and sys</summary>

```yaml
volumes:
  - name: proc
    hostPath:
      path: /proc
      type: Directory
  - name: sys
    hostPath:
      path: /sys
      type: Directory
```

Mount both as `readOnly: true` in the container.

</details>

## Verify What You Learned

```bash
# DaemonSet should have pods on control-plane nodes
kubectl get pods -l app=node-monitor -o wide
# Compare NODE column against:
kubectl get nodes --show-labels | grep control-plane

# Verify tolerations include control-plane
kubectl get ds node-monitor -o jsonpath='{.spec.template.spec.tolerations}' | python3 -m json.tool

# Verify /proc is accessible
kubectl exec ds/node-monitor -- ls /host-proc/cpuinfo

# Verify update strategy
kubectl get ds node-monitor -o jsonpath='{.spec.updateStrategy.type}'
# Expected: OnDelete
```

## Cleanup

```bash
kubectl delete daemonset node-monitor
```

## What's Next

StatefulSets rely on headless Services for DNS-based pod discovery. In [exercise 08 (StatefulSet DNS and Headless Service Patterns)](../08-statefulset-headless-dns-patterns/), you will explore DNS resolution patterns in depth.

## Summary

- Control-plane nodes carry `node-role.kubernetes.io/control-plane:NoSchedule` to protect system components
- DaemonSets must explicitly tolerate this taint to schedule on control-plane nodes
- Tolerate both `control-plane` and legacy `master` taints for cross-version compatibility
- Use `OnDelete` strategy for control-plane DaemonSets to prevent automated disruption
- Set resource limits conservatively to avoid starving kube-apiserver and etcd

## Reference

- [Taints and Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)
- [DaemonSet on Control Plane Nodes](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/#running-pods-on-select-nodes)
- [Pod Priority and Preemption](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/)
