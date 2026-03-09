# 6. DaemonSet Update Strategies: Rolling vs OnDelete

<!--
difficulty: intermediate
concepts: [daemonset-update-strategy, rolling-update, on-delete, max-unavailable, max-surge]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [03-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (DaemonSets with Tolerations)](../05-daemonsets-with-tolerations/)

## Learning Objectives

- **Understand** the difference between `RollingUpdate` and `OnDelete` update strategies for DaemonSets
- **Apply** `maxUnavailable` and `maxSurge` to control rolling update behavior
- **Analyze** when `OnDelete` is the safer choice for critical system daemons

## Why DaemonSet Update Strategies?

DaemonSets run infrastructure-critical workloads: log collectors, monitoring agents, CNI plugins. Updating these pods has different risk profiles than updating Deployment pods. A broken CNI plugin update could partition your cluster. DaemonSets offer two strategies to manage this risk.

## Step 1: Deploy a DaemonSet with RollingUpdate

```yaml
# ds-rolling.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: monitor-agent
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1        # Update one node at a time
      # maxSurge: 0            # Default: no extra pods during update
  selector:
    matchLabels:
      app: monitor-agent
  template:
    metadata:
      labels:
        app: monitor-agent
        version: v1
    spec:
      containers:
        - name: agent
          image: busybox:1.37
          command: ["sh", "-c", "echo Running v1; sleep infinity"]
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
```

```bash
kubectl apply -f ds-rolling.yaml
kubectl rollout status daemonset/monitor-agent
```

## Step 2: Trigger a Rolling Update

Update the image tag and version label:

```bash
kubectl set env daemonset/monitor-agent VERSION=v2
```

Watch pods update one at a time:

```bash
kubectl rollout status daemonset/monitor-agent
kubectl get pods -l app=monitor-agent -o custom-columns=NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase -w
```

With `maxUnavailable: 1`, Kubernetes terminates one old pod, waits for the new pod on that node to become Ready, then moves to the next node.

## Step 3: Deploy a DaemonSet with OnDelete

```yaml
# ds-ondelete.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cni-plugin
spec:
  updateStrategy:
    type: OnDelete             # Pods only update when manually deleted
  selector:
    matchLabels:
      app: cni-plugin
  template:
    metadata:
      labels:
        app: cni-plugin
        version: v1
    spec:
      containers:
        - name: cni
          image: busybox:1.37
          command: ["sh", "-c", "echo CNI v1; sleep infinity"]
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
```

```bash
kubectl apply -f ds-ondelete.yaml
kubectl rollout status daemonset/cni-plugin
```

## Step 4: Update OnDelete and Observe

Update the template:

```bash
kubectl set env daemonset/cni-plugin VERSION=v2
```

Check the pods — nothing happens:

```bash
kubectl get pods -l app=cni-plugin -o custom-columns=NAME:.metadata.name,ENV:.spec.containers[0].env[0].value
```

Pods still run v1. With `OnDelete`, you must manually delete each pod to trigger the update:

```bash
# Delete one pod to trigger its replacement with v2
kubectl delete pod -l app=cni-plugin --field-selector spec.nodeName=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
```

The replacement pod will use the new template. Other nodes still run the old version until you delete their pods.

## Step 5: Understand maxSurge

Starting with Kubernetes 1.22, DaemonSet `RollingUpdate` supports `maxSurge`. When set to 1, Kubernetes creates the new pod on a node before terminating the old one, providing zero-downtime updates per node:

```yaml
updateStrategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1              # Create new pod before deleting old
```

This is ideal for workloads where brief gaps in coverage are unacceptable.

## Verify What You Learned

```bash
# monitor-agent should be fully updated
kubectl get daemonset monitor-agent -o jsonpath='Strategy: {.spec.updateStrategy.type}'
# Expected: Strategy: RollingUpdate

# cni-plugin should have mixed versions (some v1, some v2)
kubectl get daemonset cni-plugin -o jsonpath='Strategy: {.spec.updateStrategy.type}'
# Expected: Strategy: OnDelete

kubectl get pods -l app=cni-plugin -o custom-columns=NAME:.metadata.name,ENV:.spec.containers[0].env
```

## Cleanup

```bash
kubectl delete daemonset monitor-agent cni-plugin
```

## What's Next

Some DaemonSets need to run on control-plane nodes, which are typically tainted. In [exercise 07 (Running DaemonSets on Control Plane Nodes)](../07-daemonset-on-control-plane/), you will learn the taint keys and toleration patterns needed.

## Summary

- `RollingUpdate` (default) automatically replaces DaemonSet pods node by node
- `maxUnavailable` controls how many nodes can be without a running pod during updates
- `maxSurge` creates the new pod before deleting the old one for zero-downtime per-node updates
- `OnDelete` requires manual pod deletion to trigger updates — useful for critical infrastructure
- Use `OnDelete` for CNI plugins and other components where automated rollouts could be dangerous

## Reference

- [DaemonSet Update Strategy](https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set/)
- [DaemonSet Rolling Update](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/#rolling-update)
