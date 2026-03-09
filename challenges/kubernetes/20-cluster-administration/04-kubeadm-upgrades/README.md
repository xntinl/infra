# kubeadm Minor Version Upgrades

<!--
difficulty: intermediate
concepts: [kubeadm-upgrade, control-plane-upgrade, worker-node-upgrade, version-skew, drain]
tools: [kubeadm, kubectl, apt, systemctl]
estimated_time: 40m
bloom_level: apply
prerequisites: [01-kubeadm-cluster-setup]
-->

## Overview

Kubernetes releases minor versions roughly every four months. kubeadm provides a structured upgrade path: upgrade the control plane first, then worker nodes one at a time. This exercise walks through upgrading a cluster from v1.30 to v1.31.

## Why This Matters

Running outdated Kubernetes versions means missing security patches and features. The kubeadm upgrade process is the standard procedure tested by the Kubernetes release team and is required knowledge for CKA certification.

## Step-by-Step Instructions

### Step 1 -- Determine Current Version and Plan

```bash
# Check current cluster version
kubectl get nodes
kubeadm version
kubectl version --short

# View available upgrade versions
sudo apt-cache madison kubeadm | head -10
```

### Step 2 -- Upgrade the Control Plane Node

Upgrade kubeadm first, then use it to upgrade the control plane components.

```bash
# Unhold and upgrade kubeadm
sudo apt-mark unhold kubeadm
sudo apt-get update
sudo apt-get install -y kubeadm=1.31.0-1.1
sudo apt-mark hold kubeadm

# Verify kubeadm version
kubeadm version

# Check the upgrade plan (shows what will change)
sudo kubeadm upgrade plan

# Apply the upgrade
sudo kubeadm upgrade apply v1.31.0
```

### Step 3 -- Drain the Control Plane Node

```bash
# Drain the node to evict workloads (control plane pods are static, unaffected)
kubectl drain $(hostname) --ignore-daemonsets --delete-emptydir-data
```

### Step 4 -- Upgrade kubelet and kubectl on Control Plane

```bash
sudo apt-mark unhold kubelet kubectl
sudo apt-get install -y kubelet=1.31.0-1.1 kubectl=1.31.0-1.1
sudo apt-mark hold kubelet kubectl

# Restart kubelet
sudo systemctl daemon-reload
sudo systemctl restart kubelet

# Uncordon the node
kubectl uncordon $(hostname)
```

### Step 5 -- Upgrade Each Worker Node

Repeat for every worker node in the cluster.

```bash
# From the control plane: drain the worker
kubectl drain <worker-node> --ignore-daemonsets --delete-emptydir-data
```

On the worker node:

```bash
# Upgrade kubeadm
sudo apt-mark unhold kubeadm
sudo apt-get update
sudo apt-get install -y kubeadm=1.31.0-1.1
sudo apt-mark hold kubeadm

# Upgrade the node configuration
sudo kubeadm upgrade node

# Upgrade kubelet and kubectl
sudo apt-mark unhold kubelet kubectl
sudo apt-get install -y kubelet=1.31.0-1.1 kubectl=1.31.0-1.1
sudo apt-mark hold kubelet kubectl

sudo systemctl daemon-reload
sudo systemctl restart kubelet
```

From the control plane, uncordon the worker:

```bash
kubectl uncordon <worker-node>
```

### Step 6 -- Verify the Upgrade

```bash
# All nodes should show v1.31.0
kubectl get nodes

# System pods should be running
kubectl get pods -n kube-system

# Verify API server version
kubectl version
```

## Spot the Bug

The following upgrade sequence has an error. Can you find it?

```bash
sudo apt-get install -y kubelet=1.31.0-1.1
sudo systemctl restart kubelet
sudo kubeadm upgrade apply v1.31.0
```

<details>
<summary>Answer</summary>

The order is wrong. You must upgrade kubeadm and run `kubeadm upgrade apply` **before** upgrading kubelet. Upgrading kubelet first can cause it to become incompatible with the still-old control plane components.

Correct order: kubeadm -> kubeadm upgrade apply -> kubelet + kubectl -> restart kubelet.
</details>

## Verify

```bash
# All nodes report v1.31.0
kubectl get nodes -o custom-columns='NAME:.metadata.name,VERSION:.status.nodeInfo.kubeletVersion'

# kube-apiserver image is updated
kubectl get pod -n kube-system -l component=kube-apiserver \
  -o jsonpath='{.items[0].spec.containers[0].image}'

# Workloads are running normally
kubectl get pods -A --field-selector status.phase!=Running,status.phase!=Succeeded
```

## Cleanup

No cleanup required -- the cluster is now running the upgraded version. To practice again, set up a fresh v1.30 cluster using Exercise 01.

## Reference

- [Upgrading kubeadm clusters](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-upgrade/)
- [Version Skew Policy](https://kubernetes.io/docs/setup/release/version-skew-policy/)
