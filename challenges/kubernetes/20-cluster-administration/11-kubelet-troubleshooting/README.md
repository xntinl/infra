# Kubelet Troubleshooting and Configuration

<!--
difficulty: advanced
concepts: [kubelet, node-status, container-runtime, cgroup-driver, kubelet-config, node-conditions]
tools: [systemctl, journalctl, kubectl, crictl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [01-kubeadm-cluster-setup]
-->

## Overview

The kubelet is the primary node agent that manages pod lifecycle on each node. When the kubelet fails, the node becomes NotReady and all pods on it are affected. This exercise covers kubelet configuration, common failure modes, and systematic troubleshooting techniques.

## Architecture

```
┌────────────────────────────────────────────────┐
│                  Node                           │
│                                                  │
│  ┌──────────┐     ┌────────────────┐            │
│  │  kubelet  │────▶│   containerd    │            │
│  │          │     │  (CRI runtime)  │            │
│  │ :10250   │     └────────────────┘            │
│  └────┬─────┘                                    │
│       │                                          │
│       ├── Watches /etc/kubernetes/manifests/     │
│       │   (static pods)                          │
│       ├── Reports node status to API server      │
│       ├── Manages pod sandbox and containers     │
│       └── Handles volume mounts and networking   │
└────────────────────────────────────────────────┘
```

The kubelet reads its configuration from:
- `/var/lib/kubelet/config.yaml` -- KubeletConfiguration object
- `/etc/default/kubelet` or `/etc/sysconfig/kubelet` -- environment variables
- `/etc/kubernetes/kubelet.conf` -- kubeconfig for API server authentication

## Suggested Steps

### 1. Check Kubelet Status and Logs

```bash
# Service status
sudo systemctl status kubelet

# Recent logs
sudo journalctl -u kubelet --no-pager -n 100

# Follow logs in real time
sudo journalctl -u kubelet -f

# Check for specific error patterns
sudo journalctl -u kubelet --no-pager | grep -i "error\|failed\|unable" | tail -20
```

### 2. Inspect Node Conditions

```bash
# Node conditions tell you why a node is NotReady
kubectl describe node <node-name> | grep -A 20 "Conditions:"

# Key conditions:
# Ready=False          -- kubelet is not reporting
# MemoryPressure=True  -- node is running out of memory
# DiskPressure=True    -- node disk is running out of space
# PIDPressure=True     -- too many processes
# NetworkUnavailable   -- CNI plugin not configured

# Check node allocatable resources
kubectl describe node <node-name> | grep -A 10 "Allocatable:"
```

### 3. Common Kubelet Failures

**Container runtime not running:**
```bash
sudo systemctl status containerd
sudo crictl info
# Fix: sudo systemctl start containerd
```

**Wrong cgroup driver:**
```bash
# Check kubelet's cgroup driver
sudo cat /var/lib/kubelet/config.yaml | grep cgroupDriver

# Check containerd's cgroup driver
sudo containerd config dump | grep SystemdCgroup

# They MUST match. If kubelet uses systemd, containerd must too.
```

**Certificate issues:**
```bash
# Check kubelet's client certificate
sudo openssl x509 -in /var/lib/kubelet/pki/kubelet-client-current.pem -noout -dates

# Check kubelet kubeconfig
sudo cat /etc/kubernetes/kubelet.conf | grep server
# Verify the API server endpoint is correct
```

**DNS resolution failures:**
```bash
# kubelet needs to resolve the API server hostname
nslookup <api-server-hostname>
# Check /etc/resolv.conf
```

### 4. Kubelet Configuration

```bash
# View the active kubelet configuration
sudo cat /var/lib/kubelet/config.yaml

# Key configuration fields:
# - clusterDNS: CoreDNS service IP
# - clusterDomain: cluster.local
# - cgroupDriver: systemd
# - staticPodPath: /etc/kubernetes/manifests
# - authentication/authorization: webhook settings
# - evictionHard: memory/disk thresholds for pod eviction

# Modify kubelet configuration
sudo vi /var/lib/kubelet/config.yaml
sudo systemctl restart kubelet
```

### 5. kubelet Resource Reservations

```bash
# Check system and kube reserved resources
sudo cat /var/lib/kubelet/config.yaml | grep -A 5 "systemReserved\|kubeReserved"

# Example configuration in config.yaml:
# systemReserved:
#   cpu: 100m
#   memory: 256Mi
# kubeReserved:
#   cpu: 100m
#   memory: 256Mi
# evictionHard:
#   memory.available: 100Mi
#   nodefs.available: 10%
```

## Verify

```bash
# kubelet is active and running
sudo systemctl is-active kubelet

# Node is Ready
kubectl get node <node-name>

# Pods are running on the node
kubectl get pods --field-selector spec.nodeName=<node-name> -A

# kubelet healthz endpoint responds
curl -k https://localhost:10250/healthz
```

## Cleanup

Restore any modified kubelet configuration files and restart kubelet:

```bash
sudo systemctl restart kubelet
```

## Reference

- [Kubelet](https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet/)
- [KubeletConfiguration](https://kubernetes.io/docs/reference/config-api/kubelet-config.v1beta1/)
- [Node Status Conditions](https://kubernetes.io/docs/concepts/architecture/nodes/#condition)
