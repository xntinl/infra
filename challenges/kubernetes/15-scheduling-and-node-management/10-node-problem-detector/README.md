<!--
difficulty: advanced
concepts: [node-problem-detector, node-conditions, kernel-monitor, remediation, draino, node-lifecycle]
tools: [kubectl, node-problem-detector]
estimated_time: 35m
bloom_level: analyze
prerequisites: [node-maintenance-cordon-drain, daemonsets, kubelet-configuration]
-->

# 15.10 - Node Problem Detector and Remediation

## Architecture

```
  +-------------------+     +-------------------+     +-------------------+
  |     Node A        |     |     Node B        |     |     Node C        |
  |  +-------------+  |     |  +-------------+  |     |  +-------------+  |
  |  |    NPD      |  |     |  |    NPD      |  |     |  |    NPD      |  |
  |  | (DaemonSet) |  |     |  | (DaemonSet) |  |     |  | (DaemonSet) |  |
  |  +------+------+  |     |  +------+------+  |     |  +------+------+  |
  |         |         |     |         |         |     |         |         |
  |  monitors:        |     |  monitors:        |     |  monitors:        |
  |  - kernel logs    |     |  - kernel logs    |     |  - kernel logs    |
  |  - filesystem     |     |  - filesystem     |     |  - filesystem     |
  |  - docker/crio    |     |  - docker/crio    |     |  - docker/crio    |
  +--------+----------+     +--------+----------+     +--------+----------+
           |                          |                         |
           +------------+-------------+-------------+-----------+
                        |                           |
                +-------v-------+          +--------v--------+
                | Node Condition|          |   Remediation   |
                | (via API      |          | (Draino, custom |
                |  Server)      |          |  controller)    |
                +---------------+          +-----------------+
```

Kubernetes can tell you when a node is `NotReady`, but it cannot tell you *why*. The **Node Problem Detector** (NPD) runs as a DaemonSet, monitors system-level issues (kernel panics, filesystem corruption, container runtime failures), and reports them as node conditions or events. Combined with automated remediation tools, NPD enables self-healing infrastructure.

## What You Will Learn

- How NPD monitors kernel logs, filesystem health, and container runtime status
- How NPD reports problems as node conditions and Kubernetes events
- How to configure custom problem detectors via ConfigMap
- How to set up automated remediation with tools like Draino or custom controllers

## Suggested Steps

1. Deploy Node Problem Detector as a DaemonSet
2. Check the default problem detectors (kernel log, abrt, systemd)
3. Verify that node conditions appear in `kubectl describe node`
4. Add a custom problem detector that checks for specific log patterns
5. Simulate a problem and observe NPD reporting it
6. Set up Draino to automatically cordon+drain nodes with problems

### NPD DaemonSet Deployment

```yaml
# npd-daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-problem-detector
  namespace: kube-system
  labels:
    app: node-problem-detector
spec:
  selector:
    matchLabels:
      app: node-problem-detector
  template:
    metadata:
      labels:
        app: node-problem-detector
    spec:
      serviceAccountName: node-problem-detector
      hostNetwork: true
      hostPID: true
      containers:
        - name: node-problem-detector
          image: registry.k8s.io/node-problem-detector/node-problem-detector:v0.8.19
          command:
            - /node-problem-detector
            - --logtostderr
            - --config.system-log-monitor=/config/kernel-monitor.json,/config/docker-monitor.json
            - --config.custom-plugin-monitor=/config/custom-plugin-monitor.json
          securityContext:
            privileged: true
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          volumeMounts:
            - name: log
              mountPath: /var/log
              readOnly: true
            - name: kmsg
              mountPath: /dev/kmsg
              readOnly: true
            - name: config
              mountPath: /config
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
      volumes:
        - name: log
          hostPath:
            path: /var/log
        - name: kmsg
          hostPath:
            path: /dev/kmsg
        - name: config
          configMap:
            name: npd-config
```

### Custom Problem Detector Configuration

```yaml
# npd-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: npd-config
  namespace: kube-system
data:
  kernel-monitor.json: |
    {
      "plugin": "kmsg",
      "logPath": "/dev/kmsg",
      "lookback": "5m",
      "bufferSize": 10,
      "source": "kernel-monitor",
      "conditions": [
        {
          "type": "KernelDeadlock",
          "reason": "KernelHasNoDeadlock",
          "message": "kernel has no deadlock"
        }
      ],
      "rules": [
        {
          "type": "temporary",
          "reason": "OOMKilling",
          "pattern": "Killed process \\d+ .+ total-vm:\\d+kB, anon-rss:\\d+kB"
        },
        {
          "type": "permanent",
          "condition": "KernelDeadlock",
          "reason": "AUFSUmountHung",
          "pattern": "task aufs_destroy:.*blocked for more than 120 seconds"
        }
      ]
    }
  custom-plugin-monitor.json: |
    {
      "plugin": "custom",
      "pluginConfig": {
        "invoke_interval": "30s",
        "timeout": "5s",
        "max_output_length": 80
      },
      "source": "custom-monitor",
      "conditions": [
        {
          "type": "DiskProblem",
          "reason": "DiskIsOK",
          "message": "disk is functioning correctly"
        }
      ],
      "rules": [
        {
          "type": "permanent",
          "condition": "DiskProblem",
          "reason": "DiskReadOnly",
          "path": "/bin/sh",
          "args": ["-c", "test -w /host-root/tmp || echo 'Filesystem is read-only'"],
          "timeout": "5s"
        }
      ]
    }
```

### Draino for Automated Remediation

```yaml
# draino-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: draino
  namespace: kube-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: draino
  template:
    metadata:
      labels:
        app: draino
    spec:
      serviceAccountName: draino
      containers:
        - name: draino
          image: planetlabs/draino:latest
          command:
            - /draino
            - --node-label-expr=node-role.kubernetes.io/worker
            - --eviction-headroom=30s
            - --drain-buffer=10m
            - KernelDeadlock                  # drain nodes with this condition
            - DiskProblem                     # drain nodes with this condition
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
```

## Verify

```bash
# 1. Check NPD pods are running on all nodes
kubectl get pods -n kube-system -l app=node-problem-detector -o wide

# 2. Check node conditions added by NPD
kubectl describe node <node-name> | grep -A20 Conditions

# 3. Check for NPD-generated events
kubectl get events --field-selector source=node-problem-detector

# 4. Look for specific conditions
kubectl get nodes -o custom-columns=\
'NAME:.metadata.name,'\
'KERNEL_DEADLOCK:.status.conditions[?(@.type=="KernelDeadlock")].status,'\
'DISK_PROBLEM:.status.conditions[?(@.type=="DiskProblem")].status'

# 5. Check NPD logs
kubectl logs -n kube-system -l app=node-problem-detector --tail=50

# 6. If using Draino, check for automated remediation
kubectl get events --field-selector reason=CordonStarting
kubectl logs -n kube-system -l app=draino --tail=50
```

## Cleanup

```bash
kubectl delete daemonset node-problem-detector -n kube-system
kubectl delete configmap npd-config -n kube-system
kubectl delete deployment draino -n kube-system
```

## What's Next

You now have tools to detect and remediate node problems. The final exercises combine everything into complex scheduling challenges: [15.11 - Advanced Multi-Constraint Scheduling Optimization](../11-advanced-scheduling-optimization/).

## Summary

- Node Problem Detector runs as a DaemonSet monitoring kernel logs, filesystem, and container runtime
- Problems are reported as node conditions (persistent) or events (transient)
- Custom problem detectors can run arbitrary scripts and report results
- Draino automates cordon+drain for nodes with specific problem conditions
- Combining NPD + Draino + Cluster Autoscaler creates self-healing infrastructure
