<!--
difficulty: advanced
concepts: [kubelet-config, system-reserved, kube-reserved, eviction-thresholds, allocatable-resources, topology-manager]
tools: [kubectl, kubelet]
estimated_time: 40m
bloom_level: analyze
prerequisites: [resource-requests-and-limits, node-management]
-->

# 15.09 - Kubelet Configuration and Resource Reservations

## Architecture

```
  +---------------------------------------------------+
  |                    Node Total                      |
  |  +-----------+  +-----------+  +----------------+ |
  |  |  kube-    |  |  system-  |  |  allocatable   | |
  |  |  reserved |  |  reserved |  |  (for pods)    | |
  |  |  (kubelet,|  |  (sshd,   |  |                | |
  |  |  kube-    |  |  systemd, |  | [pod] [pod]    | |
  |  |  proxy)   |  |  journald)|  | [pod] [pod]    | |
  |  +-----------+  +-----------+  +----------------+ |
  |                                                    |
  |  +----------------------------------------------+ |
  |  |  eviction threshold (hard/soft)               | |
  |  +----------------------------------------------+ |
  +---------------------------------------------------+

  allocatable = total - kube-reserved - system-reserved - eviction-threshold
```

The kubelet is the agent on every node responsible for pod lifecycle. Misconfigured kubelets lead to node instability: pods consuming all memory and triggering OOM kills, or the system daemons starving for CPU. Resource reservations carve out protected capacity for Kubernetes components and the OS, while eviction thresholds trigger pod eviction before the node becomes unresponsive.

## What You Will Learn

- How `kubeReserved` and `systemReserved` protect node stability
- How `allocatable` resources are calculated and how they appear in `kubectl describe node`
- How hard and soft eviction thresholds work
- How the Topology Manager optimizes NUMA-aware pod placement

## Suggested Steps

1. Examine the current kubelet configuration on a node
2. Understand the allocatable resource calculation
3. Configure `kubeReserved` and `systemReserved`
4. Set up hard and soft eviction thresholds
5. Verify the effective allocatable resources
6. Explore Topology Manager policies for NUMA-aware workloads

### KubeletConfiguration

```yaml
# kubelet-config.yaml
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
kubeReserved:
  cpu: 100m                         # reserved for kubelet, kube-proxy
  memory: 256Mi
  ephemeral-storage: 1Gi
systemReserved:
  cpu: 100m                         # reserved for OS daemons (sshd, systemd)
  memory: 256Mi
  ephemeral-storage: 1Gi
evictionHard:
  memory.available: 100Mi           # evict pods when free memory drops below 100Mi
  nodefs.available: 10%             # evict when disk drops below 10%
  imagefs.available: 15%            # evict when image filesystem drops below 15%
  nodefs.inodesFree: 5%
evictionSoft:
  memory.available: 200Mi           # soft threshold (triggers graceful eviction)
  nodefs.available: 15%
evictionSoftGracePeriod:
  memory.available: 1m30s           # wait 90 seconds before acting on soft threshold
  nodefs.available: 1m30s
evictionMaxPodGracePeriod: 60       # max grace period for soft-evicted pods
enforceNodeAllocatable:
  - pods                            # enforce allocatable limits on pods
  - kube-reserved
  - system-reserved
topologyManagerPolicy: best-effort  # NUMA-aware placement (none, best-effort, restricted, single-numa-node)
```

### Viewing Current Kubelet Config

```bash
# Check the effective kubelet config on a node
kubectl get --raw "/api/v1/nodes/<node-name>/proxy/configz" | python3 -m json.tool

# Check allocatable vs capacity
kubectl describe node <node-name> | grep -A10 "Allocatable:"
kubectl describe node <node-name> | grep -A10 "Capacity:"
```

### Node Resource Inspection Script

```bash
# Compare capacity vs allocatable across all nodes
kubectl get nodes -o custom-columns=\
'NAME:.metadata.name,'\
'CPU_CAP:.status.capacity.cpu,'\
'CPU_ALLOC:.status.allocatable.cpu,'\
'MEM_CAP:.status.capacity.memory,'\
'MEM_ALLOC:.status.allocatable.memory'
```

### Testing Eviction Thresholds

```yaml
# memory-hog.yaml
apiVersion: v1
kind: Pod
metadata:
  name: memory-hog
spec:
  containers:
    - name: stress
      image: busybox:1.37
      command:
        - /bin/sh
        - -c
        - |
          # Allocate memory in 10Mi chunks
          i=0
          while true; do
            dd if=/dev/zero bs=1M count=10 | tr '\0' 'x' > /dev/null &
            i=$((i+1))
            echo "Allocated $((i*10))Mi"
            sleep 1
          done
      resources:
        requests:
          memory: 64Mi
        limits:
          memory: 2Gi
```

## Verify

```bash
# 1. Check node capacity vs allocatable
kubectl describe node <node-name> | grep -A20 "Allocated resources"

# 2. Calculate expected allocatable
# allocatable = capacity - kubeReserved - systemReserved - evictionHard
# Example: 4Gi total - 256Mi kube - 256Mi system - 100Mi eviction = ~3.4Gi

# 3. Verify eviction thresholds in kubelet config
kubectl get --raw "/api/v1/nodes/<node-name>/proxy/configz" | python3 -c "
import json, sys
config = json.load(sys.stdin)['kubeletconfig']
print('Eviction Hard:', config.get('evictionHard', 'not set'))
print('Eviction Soft:', config.get('evictionSoft', 'not set'))
print('Kube Reserved:', config.get('kubeReserved', 'not set'))
print('System Reserved:', config.get('systemReserved', 'not set'))
"

# 4. Check node conditions (MemoryPressure, DiskPressure)
kubectl describe node <node-name> | grep -A5 Conditions

# 5. Watch for eviction events
kubectl get events --field-selector reason=Evicted --sort-by='.lastTimestamp'

# 6. Verify topology manager policy
kubectl get --raw "/api/v1/nodes/<node-name>/proxy/configz" | python3 -c "
import json, sys
config = json.load(sys.stdin)['kubeletconfig']
print('Topology Manager Policy:', config.get('topologyManagerPolicy', 'none'))
"
```

## Cleanup

```bash
kubectl delete pod memory-hog --ignore-not-found
```

## What's Next

The kubelet manages individual nodes, but detecting and responding to node problems requires a separate mechanism. The next exercise covers the Node Problem Detector and automated remediation: [15.10 - Node Problem Detector and Remediation](../10-node-problem-detector/).

## Summary

- `kubeReserved` protects CPU and memory for kubelet and kube-proxy
- `systemReserved` protects resources for OS-level daemons
- `allocatable = capacity - kubeReserved - systemReserved - evictionThreshold`
- Hard eviction thresholds trigger immediate pod eviction; soft thresholds allow a grace period
- `enforceNodeAllocatable` controls whether reservations are enforced via cgroups
- Topology Manager optimizes NUMA locality for latency-sensitive workloads
