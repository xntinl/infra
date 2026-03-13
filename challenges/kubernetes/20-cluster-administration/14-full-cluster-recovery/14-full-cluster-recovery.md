# Full Cluster Recovery from etcd Snapshot

<!--
difficulty: insane
concepts: [etcd-restore, cluster-recovery, pki, static-pods, kubeadm, disaster-recovery, data-integrity]
tools: [etcdctl, kubeadm, kubectl, openssl, systemctl, crictl]
estimated_time: 60m
bloom_level: create
prerequisites: [05-etcd-backup-restore, 09-etcd-disaster-recovery, 06-certificate-expiry-rotation, 12-static-pods-and-manifests]
-->

## Scenario

You are the on-call engineer for a production Kubernetes cluster. A catastrophic storage failure has destroyed the control plane node's root filesystem. You have:

- A fresh Ubuntu 22.04 machine with the same IP address as the original control plane
- An etcd snapshot taken 1 hour before the failure (`/backup/etcd-snapshot.db`)
- A backup of the PKI directory (`/backup/pki/`)
- The original kubeadm configuration file (`/backup/kubeadm-config.yaml`)
- Two worker nodes that are still running but reporting the control plane as unreachable

Your goal is to fully restore the cluster from these backups so that all worker nodes rejoin and existing workloads resume.

## Constraints

1. You must use the same Kubernetes version as the original cluster (v1.30.4)
2. The restored cluster must use the original PKI certificates (do not generate new ones)
3. Worker nodes must rejoin without being re-provisioned (no `kubeadm reset` on workers)
4. All resources from the etcd snapshot must be present after recovery
5. The restored cluster must pass a health check within 15 minutes
6. You must not modify any configuration on the worker nodes

## Success Criteria

1. `kubectl get nodes` shows the control plane and both workers as Ready
2. All namespaces, deployments, services, and secrets from the snapshot are present
3. Pods that were running before the failure are rescheduled and Running
4. CoreDNS is functional (DNS resolution works from pods)
5. The cluster can create new workloads (deploy a test nginx pod)
6. `etcdctl endpoint health` reports healthy
7. All control plane components (API server, scheduler, controller manager) are Running

## Verification Commands

```bash
# Node status
kubectl get nodes -o wide

# Control plane health
kubectl get pods -n kube-system -l tier=control-plane
kubectl get cs  # component status (deprecated but useful)

# etcd health
ETCDCTL_API=3 etcdctl endpoint health \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key

# Data integrity -- verify resources exist
kubectl get namespaces
kubectl get deployments -A
kubectl get services -A
kubectl get secrets -A --field-selector type!=kubernetes.io/service-account-token | head -20

# DNS resolution
kubectl run dns-verify --image=busybox:1.37 --restart=Never \
  --command -- nslookup kubernetes.default.svc.cluster.local
kubectl logs dns-verify
kubectl delete pod dns-verify

# New workload creation
kubectl create deployment recovery-test --image=nginx:1.27
kubectl rollout status deployment/recovery-test --timeout=120s
kubectl delete deployment recovery-test

# Certificate validity
sudo kubeadm certs check-expiration
```

## Cleanup

```bash
# If this was a lab exercise, tear down the cluster
sudo kubeadm reset -f
sudo rm -rf /etc/kubernetes /var/lib/etcd $HOME/.kube

# On worker nodes
sudo kubeadm reset -f
```
