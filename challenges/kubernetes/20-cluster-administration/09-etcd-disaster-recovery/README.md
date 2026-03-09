# etcd Disaster Recovery: Multi-Node

<!--
difficulty: advanced
concepts: [etcd-cluster, quorum, disaster-recovery, snapshot-restore, raft-consensus, member-management]
tools: [etcdctl, kubeadm, kubectl, systemctl]
estimated_time: 50m
bloom_level: analyze
prerequisites: [05-etcd-backup-restore, 01-kubeadm-cluster-setup]
-->

## Overview

In a multi-node etcd cluster, disaster recovery goes beyond simple snapshot restore. You may need to recover from quorum loss, replace failed members, or rebuild the cluster from a single surviving member. This exercise covers multi-node etcd architecture, member management, and recovery from partial and total cluster failure.

## Architecture

A production etcd cluster typically runs 3 or 5 members for fault tolerance based on Raft consensus:

```
┌──────────────────────────────────────────────────┐
│              etcd Raft Cluster (3 nodes)          │
│                                                    │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐      │
│  │  etcd-1   │──│  etcd-2   │──│  etcd-3   │      │
│  │ (Leader)  │   │(Follower) │   │(Follower) │      │
│  └──────────┘   └──────────┘   └──────────┘      │
│                                                    │
│  Quorum = 2 (majority of 3)                       │
│  Tolerates 1 node failure                         │
│  Loses quorum if 2 nodes fail                     │
└──────────────────────────────────────────────────┘
```

| Cluster Size | Quorum | Tolerated Failures |
|-------------|--------|-------------------|
| 1 | 1 | 0 |
| 3 | 2 | 1 |
| 5 | 3 | 2 |
| 7 | 4 | 3 |

## Suggested Steps

### 1. Inspect the etcd Cluster

```bash
# Set environment variables for etcdctl
export ETCDCTL_API=3
export ETCDCTL_ENDPOINTS=https://127.0.0.1:2379
export ETCDCTL_CACERT=/etc/kubernetes/pki/etcd/ca.crt
export ETCDCTL_CERT=/etc/kubernetes/pki/etcd/server.crt
export ETCDCTL_KEY=/etc/kubernetes/pki/etcd/server.key

# List cluster members
etcdctl member list -w table

# Check endpoint health
etcdctl endpoint health --cluster -w table

# Check endpoint status (shows leader, DB size, raft index)
etcdctl endpoint status --cluster -w table
```

### 2. Simulate a Single Member Failure

With a 3-node cluster, losing 1 member preserves quorum. The cluster remains operational.

```bash
# On a non-leader node, stop etcd
# (if using stacked etcd with kubeadm, move the static pod manifest)
sudo mv /etc/kubernetes/manifests/etcd.yaml /tmp/etcd.yaml.disabled

# From a healthy member, verify the cluster is still operational
etcdctl endpoint health --cluster
etcdctl member list -w table
# The failed member shows as "unstarted" or unhealthy

# The Kubernetes API server still works
kubectl get nodes
```

### 3. Replace the Failed Member

```bash
# Remove the failed member from the cluster
MEMBER_ID=$(etcdctl member list | grep <failed-node-name> | cut -d',' -f1)
etcdctl member remove $MEMBER_ID

# On the failed node, clear old data
sudo rm -rf /var/lib/etcd

# Add the node back as a new member
etcdctl member add <node-name> --peer-urls=https://<node-ip>:2380

# On the recovered node, update the etcd manifest with:
#   --initial-cluster-state=existing
#   --initial-cluster=<full-cluster-list>
# Then restore the manifest
sudo mv /tmp/etcd.yaml.disabled /etc/kubernetes/manifests/etcd.yaml
# Edit the manifest to set initial-cluster-state=existing

# Verify recovery
etcdctl member list -w table
etcdctl endpoint health --cluster
```

### 4. Recover from Total Quorum Loss

When a majority of etcd members fail, the cluster is read-only (no writes). Recovery requires restoring from a snapshot.

```bash
# Take a snapshot from the surviving member (if any)
etcdctl snapshot save /tmp/etcd-disaster.db

# On ALL nodes, stop etcd and clear data
sudo mv /etc/kubernetes/manifests/etcd.yaml /tmp/
sudo rm -rf /var/lib/etcd

# Restore snapshot on each node with unique member configuration
# Node 1:
etcdctl snapshot restore /tmp/etcd-disaster.db \
  --name=etcd-1 \
  --data-dir=/var/lib/etcd \
  --initial-cluster=etcd-1=https://10.0.0.1:2380,etcd-2=https://10.0.0.2:2380,etcd-3=https://10.0.0.3:2380 \
  --initial-advertise-peer-urls=https://10.0.0.1:2380

# Node 2:
etcdctl snapshot restore /tmp/etcd-disaster.db \
  --name=etcd-2 \
  --data-dir=/var/lib/etcd \
  --initial-cluster=etcd-1=https://10.0.0.1:2380,etcd-2=https://10.0.0.2:2380,etcd-3=https://10.0.0.3:2380 \
  --initial-advertise-peer-urls=https://10.0.0.2:2380

# Node 3:
etcdctl snapshot restore /tmp/etcd-disaster.db \
  --name=etcd-3 \
  --data-dir=/var/lib/etcd \
  --initial-cluster=etcd-1=https://10.0.0.1:2380,etcd-2=https://10.0.0.2:2380,etcd-3=https://10.0.0.3:2380 \
  --initial-advertise-peer-urls=https://10.0.0.3:2380

# Restore etcd manifests on all nodes (with initial-cluster-state=new)
sudo mv /tmp/etcd.yaml /etc/kubernetes/manifests/
```

### 5. Force New Cluster from Single Member

As a last resort, you can force a single member to form a new cluster.

```bash
# On the surviving member, stop etcd
sudo mv /etc/kubernetes/manifests/etcd.yaml /tmp/

# Edit the manifest to add --force-new-cluster flag
# Then start it
sudo mv /tmp/etcd.yaml /etc/kubernetes/manifests/

# Once running, remove the --force-new-cluster flag and restart
# Then add new members with etcdctl member add
```

## Verify

```bash
# All members are healthy
etcdctl endpoint health --cluster -w table

# Member list shows correct count
etcdctl member list -w table

# Kubernetes API server is functional
kubectl get nodes
kubectl get pods -n kube-system

# Data integrity check
kubectl get namespaces
kubectl get deployments -A
```

## Cleanup

If you modified etcd configuration for testing, restore original manifests and data directories. In a lab environment, the simplest cleanup is to rebuild the cluster using kubeadm.

## Reference

- [etcd Disaster Recovery](https://etcd.io/docs/v3.5/op-guide/recovery/)
- [Operating etcd for Kubernetes](https://kubernetes.io/docs/tasks/administer-cluster/configure-upgrade-etcd/)
- [etcd Clustering Guide](https://etcd.io/docs/v3.5/op-guide/clustering/)
