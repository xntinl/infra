# etcd Backup and Restore

<!--
difficulty: intermediate
concepts: [etcd, backup, restore, snapshot, cluster-state, etcdctl]
tools: [etcdctl, kubectl, kubeadm]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-kubeadm-cluster-setup]
-->

## Overview

etcd is the key-value store that holds all Kubernetes cluster state. Losing etcd data means losing all resource definitions, secrets, and configuration. This exercise covers taking etcd snapshots and restoring from them -- a critical disaster recovery skill.

## Why This Matters

Without a valid etcd backup, a cluster failure can mean total data loss. Regular etcd snapshots are the foundation of any Kubernetes disaster recovery plan. The CKA exam tests this skill directly.

## Step-by-Step Instructions

### Step 1 -- Identify etcd Configuration

```bash
# Find the etcd pod and extract connection parameters
kubectl get pods -n kube-system -l component=etcd

# Get the etcd command-line arguments
kubectl describe pod etcd-$(hostname) -n kube-system | grep -A 20 "Command:"

# Key parameters you need:
# --listen-client-urls (endpoint)
# --cert-file (server cert)
# --key-file (server key)
# --trusted-ca-file (CA cert)
```

Typical paths on a kubeadm cluster:

| Parameter | Path |
|-----------|------|
| Endpoint | `https://127.0.0.1:2379` |
| CA cert | `/etc/kubernetes/pki/etcd/ca.crt` |
| Server cert | `/etc/kubernetes/pki/etcd/server.crt` |
| Server key | `/etc/kubernetes/pki/etcd/server.key` |

### Step 2 -- Install etcdctl (if needed)

```bash
# etcdctl may already be available via the etcd container
# To use it from the host, install the etcd client:
ETCD_VER=v3.5.12
curl -L https://github.com/etcd-io/etcd/releases/download/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz \
  | tar xz --strip-components=1 -C /usr/local/bin/ etcd-${ETCD_VER}-linux-amd64/etcdctl

etcdctl version
```

### Step 3 -- Create Test Resources (Before Backup)

```bash
# Create resources to verify backup/restore
kubectl create namespace etcd-test
kubectl create configmap test-data -n etcd-test --from-literal=backup=before
kubectl create deployment pre-backup --image=nginx:1.27 -n etcd-test
kubectl rollout status deployment/pre-backup -n etcd-test
```

### Step 4 -- Take an etcd Snapshot

```bash
ETCDCTL_API=3 etcdctl snapshot save /tmp/etcd-backup.db \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key

# Verify the snapshot
ETCDCTL_API=3 etcdctl snapshot status /tmp/etcd-backup.db --write-table
```

### Step 5 -- Create Resources After Backup

```bash
# These resources will be lost when we restore
kubectl create deployment post-backup --image=nginx:1.27 -n etcd-test
kubectl create configmap post-data -n etcd-test --from-literal=backup=after
```

### Step 6 -- Restore from Snapshot

```bash
# Stop the API server and etcd (move their manifests)
sudo mv /etc/kubernetes/manifests/kube-apiserver.yaml /tmp/
sudo mv /etc/kubernetes/manifests/etcd.yaml /tmp/

# Wait for them to stop
sleep 20
sudo crictl ps | grep -E "etcd|apiserver"  # should show nothing

# Remove old etcd data
sudo mv /var/lib/etcd /var/lib/etcd.bak

# Restore the snapshot to a new data directory
ETCDCTL_API=3 etcdctl snapshot restore /tmp/etcd-backup.db \
  --data-dir=/var/lib/etcd

# Restore the static pod manifests
sudo mv /tmp/etcd.yaml /etc/kubernetes/manifests/
sudo mv /tmp/kube-apiserver.yaml /etc/kubernetes/manifests/

# Wait for the control plane to come back
sleep 30
kubectl get nodes
```

### Step 7 -- Verify the Restore

```bash
# pre-backup resources should exist
kubectl get deployment pre-backup -n etcd-test
kubectl get configmap test-data -n etcd-test

# post-backup resources should be GONE (they were created after the snapshot)
kubectl get deployment post-backup -n etcd-test   # should return NotFound
kubectl get configmap post-data -n etcd-test       # should return NotFound
```

## Verify

```bash
# Cluster is healthy
kubectl get nodes
kubectl get pods -n kube-system

# Pre-backup data is intact
kubectl get configmap test-data -n etcd-test \
  -o jsonpath='{.data.backup}'
# Expected: "before"

# Post-backup data is gone
kubectl get configmap post-data -n etcd-test 2>&1 | grep -q "NotFound" && echo "PASS"
```

## Cleanup

```bash
kubectl delete namespace etcd-test
rm -f /tmp/etcd-backup.db
sudo rm -rf /var/lib/etcd.bak
```

## Reference

- [Backing up an etcd cluster](https://kubernetes.io/docs/tasks/administer-cluster/configure-upgrade-etcd/#backing-up-an-etcd-cluster)
- [etcd Disaster Recovery](https://etcd.io/docs/v3.5/op-guide/recovery/)
