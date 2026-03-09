# 5. DaemonSets with Tolerations and Node Selection

<!--
difficulty: advanced
concepts: [daemonset, tolerations, taints, node-selector, hostPath]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: evaluate
prerequisites: [03-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with at least one node
- `kubectl` installed and configured
- Completion of [exercise 01 (StatefulSets and Persistent Storage)](../01-statefulsets-and-persistent-storage/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how DaemonSets differ from Deployments in scheduling behavior
- **Create** a DaemonSet that runs on every node including tainted control-plane nodes
- **Evaluate** the security implications of wildcard tolerations vs specific tolerations

## Why DaemonSets?

Some workloads need to run on every node — log collectors, monitoring agents, network plugins. A Deployment with a high replica count does not guarantee one-pod-per-node coverage. DaemonSets fill this gap by automatically placing exactly one pod per matching node, adjusting as nodes join or leave the cluster.

## The Challenge

Create a log-collector DaemonSet called `log-collector` that:

1. Runs exactly one pod on **every** node, including control-plane nodes with the `node-role.kubernetes.io/control-plane:NoSchedule` taint
2. Mounts `/var/log` from the host into the container at `/host-logs` via a `hostPath` volume (type `Directory`)
3. Uses `busybox:1.37` with command: `["sh", "-c", "tail -F /host-logs/*.log 2>/dev/null || sleep infinity"]`
4. Has a `nodeSelector` of `monitoring: enabled` (label your nodes before applying)
5. Requests `50m` CPU and `64Mi` memory

<details>
<summary>Hint 1: DaemonSet basics</summary>

A DaemonSet uses `kind: DaemonSet` with no `replicas` field. The scheduler places one pod per matching node automatically.

</details>

<details>
<summary>Hint 2: Toleration for control-plane</summary>

```yaml
tolerations:
  - key: "node-role.kubernetes.io/control-plane"
    operator: "Exists"
    effect: "NoSchedule"
```

</details>

<details>
<summary>Hint 3: hostPath volume</summary>

```yaml
volumes:
  - name: host-logs
    hostPath:
      path: /var/log
      type: Directory
```

</details>

<details>
<summary>Hint 4: Labeling nodes for nodeSelector</summary>

```bash
kubectl label nodes --all monitoring=enabled
```

Place `nodeSelector` at the pod spec level alongside `containers`.

</details>

<details>
<summary>Hint 5: Verifying placement</summary>

```bash
kubectl get pods -l app=log-collector -o wide
```

The NODE column shows where each pod landed. Compare against `kubectl get nodes`.

</details>

## Spot the Bug

A teammate submits this toleration claiming it ensures the collector runs everywhere:

```yaml
tolerations:
  - operator: "Exists"
```

**What is dangerous about this?**

<details>
<summary>Explanation</summary>

A bare `operator: "Exists"` with no `key` is a **wildcard** -- it tolerates every taint including `not-ready`, `unreachable`, `disk-pressure`, and `unschedulable`. Pods schedule onto nodes being drained or failing. During maintenance, drained nodes immediately get new DaemonSet pods, fighting the drain. Fix: tolerate only the specific key you need.

</details>

## Verify What You Learned

```bash
kubectl get daemonset log-collector
# DESIRED/CURRENT/READY should all equal <your-node-count>, NODE SELECTOR = monitoring=enabled

kubectl get pods -l app=log-collector -o wide
# Every node should have exactly one pod in Running state

kubectl exec ds/log-collector -- ls /host-logs/
# Should list host log files

kubectl get daemonset log-collector -o jsonpath='{.spec.template.spec.tolerations}'
# Should show a toleration with a specific key, not a bare wildcard
```

## Cleanup

```bash
kubectl delete daemonset log-collector
kubectl label nodes --all monitoring-
```

## What's Next

DaemonSets support two update strategies with very different behaviors. In [exercise 06 (DaemonSet Update Strategies)](../06-daemonset-update-strategies/), you will compare RollingUpdate and OnDelete to understand when each is appropriate.

## Summary

- DaemonSets ensure exactly one pod per matching node, unlike Deployments which target a replica count
- Tolerations allow pods to schedule on tainted nodes — use specific keys rather than wildcard `operator: "Exists"`
- `hostPath` volumes give DaemonSet pods access to node-level filesystems for log collection and monitoring
- `nodeSelector` restricts which nodes the DaemonSet targets

## Reference

- [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
- [Taints and Tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/)
- [Volumes - hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath)

## Additional Resources

- [Assign Pods to Nodes using Node Affinity](https://kubernetes.io/docs/tasks/configure-pod-container/assign-pods-nodes-using-node-affinity/)
- [Kubernetes DaemonSet Guide](https://kubernetes.io/docs/tasks/manage-daemon/update-daemon-set/)

---

<details>
<summary>Solution</summary>

```bash
kubectl label nodes --all monitoring=enabled
```

```yaml
# daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: log-collector
spec:
  selector:
    matchLabels:
      app: log-collector
  template:
    metadata:
      labels:
        app: log-collector
    spec:
      nodeSelector:
        monitoring: enabled
      tolerations:
        - key: "node-role.kubernetes.io/control-plane"
          operator: "Exists"
          effect: "NoSchedule"
      containers:
        - name: collector
          image: busybox:1.37
          command: ["sh", "-c", "tail -F /host-logs/*.log 2>/dev/null || sleep infinity"]
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
          volumeMounts:
            - name: host-logs
              mountPath: /host-logs
              readOnly: true
      volumes:
        - name: host-logs
          hostPath:
            path: /var/log
            type: Directory
```

```bash
kubectl apply -f daemonset.yaml
kubectl rollout status daemonset/log-collector
kubectl get pods -l app=log-collector -o wide
```

</details>
