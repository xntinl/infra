# kubeadm Cluster Bootstrap

<!--
difficulty: basic
concepts: [kubeadm, cluster-initialization, control-plane, worker-nodes, cni, containerd]
tools: [kubeadm, kubectl, systemctl, containerd]
estimated_time: 45m
bloom_level: understand
prerequisites: [linux-administration, networking-basics]
-->

## Overview

kubeadm is the official Kubernetes tool for bootstrapping production-grade clusters. In this exercise you will initialize a control plane node and join a worker node, producing a two-node cluster with a working CNI plugin.

Understanding the cluster bootstrap process is essential for anyone managing on-premises or bare-metal Kubernetes installations.

## Why This Matters

Managed Kubernetes services abstract away the control plane, but many organizations run self-managed clusters. Knowing how kubeadm works gives you the ability to build, upgrade, and recover clusters from scratch. Even if you primarily use managed services, the concepts here (certificates, etcd, static pods) underpin every Kubernetes cluster.

## Prerequisites

Two Ubuntu 22.04 machines (VMs or bare-metal) with:
- 2 CPUs and 2 GB RAM minimum per node
- Full network connectivity between nodes
- Swap disabled
- Unique hostnames, MAC addresses, and product UUIDs

## Step-by-Step Instructions

### Step 1 -- Install Container Runtime (Both Nodes)

Kubernetes requires a CRI-compatible container runtime. Install containerd on both nodes.

```bash
# Load required kernel modules
cat <<EOF | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

# Set required sysctl parameters
cat <<EOF | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF

sudo sysctl --system

# Install containerd
sudo apt-get update
sudo apt-get install -y containerd

# Configure containerd to use systemd cgroup driver
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml

sudo systemctl restart containerd
sudo systemctl enable containerd
```

### Step 2 -- Install kubeadm, kubelet, and kubectl (Both Nodes)

```bash
# Install prerequisites
sudo apt-get install -y apt-transport-https ca-certificates curl gpg

# Add the Kubernetes apt repository (v1.30)
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.30/deb/Release.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg

echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.30/deb/ /' \
  | sudo tee /etc/apt/sources.list.d/kubernetes.list

# Install and pin versions
sudo apt-get update
sudo apt-get install -y kubelet=1.30.4-1.1 kubeadm=1.30.4-1.1 kubectl=1.30.4-1.1
sudo apt-mark hold kubelet kubeadm kubectl
```

### Step 3 -- Disable Swap (Both Nodes)

```bash
# Disable swap immediately
sudo swapoff -a

# Prevent swap from being re-enabled on reboot
sudo sed -i '/ swap / s/^/#/' /etc/fstab
```

### Step 4 -- Initialize the Control Plane (Control Plane Node Only)

```bash
# Initialize the cluster with a pod network CIDR for Calico
sudo kubeadm init \
  --pod-network-cidr=192.168.0.0/16 \
  --kubernetes-version=1.30.4

# Set up kubeconfig for the current user
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config
```

Save the `kubeadm join` command printed at the end -- you will need it for the worker node.

### Step 5 -- Install a CNI Plugin (Control Plane Node)

Without a CNI plugin, pods cannot communicate across nodes and CoreDNS pods stay in `Pending`.

```bash
# Install Calico CNI
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml
```

### Step 6 -- Join the Worker Node (Worker Node Only)

Run the join command saved from Step 4 on the worker node.

```bash
# Example (your token and hash will differ)
sudo kubeadm join <control-plane-ip>:6443 \
  --token <token> \
  --discovery-token-ca-cert-hash sha256:<hash>
```

If the token expired, generate a new one from the control plane:

```bash
kubeadm token create --print-join-command
```

### Step 7 -- Verify the Cluster

```bash
# Check node status (both should be Ready)
kubectl get nodes

# Verify system pods are running
kubectl get pods -n kube-system

# Test DNS resolution
kubectl run dns-test --image=busybox:1.37 --restart=Never \
  -- nslookup kubernetes.default
kubectl logs dns-test
kubectl delete pod dns-test
```

## Common Mistakes

1. **Forgetting to disable swap** -- kubelet will not start if swap is enabled. Always verify with `free -h`.
2. **Mismatched cgroup drivers** -- containerd and kubelet must both use the same cgroup driver (systemd recommended). Check with `containerd config dump | grep SystemdCgroup`.
3. **Missing br_netfilter module** -- without this module, iptables cannot see bridged traffic and pod networking fails.
4. **Firewall blocking ports** -- the API server (6443), etcd (2379-2380), and kubelet (10250) must be reachable between nodes.
5. **Expired join token** -- tokens expire after 24 hours by default. Use `kubeadm token create --print-join-command` to generate a new one.

## Verify

```bash
# Both nodes should show STATUS = Ready
kubectl get nodes -o wide

# All system pods should be Running or Completed
kubectl get pods -n kube-system

# The cluster-info should show control plane and CoreDNS endpoints
kubectl cluster-info

# Deploy a test workload
kubectl create deployment verify --image=nginx:1.27 --replicas=2
kubectl rollout status deployment/verify
kubectl get pods -o wide  # pods should be scheduled across both nodes
```

## Cleanup

```bash
# Delete the test deployment
kubectl delete deployment verify

# To fully tear down the cluster:
# On the worker node:
sudo kubeadm reset -f

# On the control plane node:
sudo kubeadm reset -f
sudo rm -rf $HOME/.kube
```

## What's Next

- **Exercise 02** -- Learn advanced kubectl commands and output formatting
- **Exercise 04** -- Upgrade your kubeadm cluster to a newer Kubernetes version
- **Exercise 05** -- Back up and restore etcd for disaster recovery

## Summary

- kubeadm automates the control plane bootstrap process including certificate generation, etcd setup, and static pod manifests
- A CRI-compatible container runtime (containerd) must be installed and configured before kubeadm init
- The `--pod-network-cidr` flag must match the CNI plugin's expected range
- Worker nodes join using a token and CA certificate hash for secure discovery
- CoreDNS pods remain Pending until a CNI plugin is installed
- The cluster is ready when all nodes show Ready and kube-system pods are Running

## Reference

- [kubeadm init](https://kubernetes.io/docs/reference/setup-tools/kubeadm/kubeadm-init/)
- [kubeadm join](https://kubernetes.io/docs/reference/setup-tools/kubeadm/kubeadm-join/)
- [Container Runtimes](https://kubernetes.io/docs/setup/production-environment/container-runtimes/)

## Additional Resources

- [Creating a cluster with kubeadm](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/create-cluster-kubeadm/)
- [Calico Getting Started](https://docs.tigera.io/calico/latest/getting-started/kubernetes/quickstart)
- [kubeadm Configuration (v1beta3)](https://kubernetes.io/docs/reference/config-api/kubeadm-config.v1beta3/)
