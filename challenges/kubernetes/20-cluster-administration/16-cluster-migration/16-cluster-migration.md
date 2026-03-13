# Cluster Migration: kubeadm to Managed Kubernetes

<!--
difficulty: insane
concepts: [cluster-migration, resource-export, stateful-migration, dns-cutover, managed-kubernetes, velero, persistent-volumes]
tools: [kubectl, velero, kubeadm, etcdctl, helm]
estimated_time: 90m
bloom_level: create
prerequisites: [05-etcd-backup-restore, 01-kubeadm-cluster-setup]
-->

## Scenario

Your organization is migrating from a self-managed kubeadm cluster to a managed Kubernetes service (EKS, GKE, or AKS). The cluster runs production workloads including:

- Stateless web applications (Deployments with Services)
- A StatefulSet-based PostgreSQL database with PersistentVolumes
- CronJobs for batch processing
- ConfigMaps and Secrets with application configuration
- RBAC policies (Roles, RoleBindings, ClusterRoles)
- NetworkPolicies
- Custom Resource Definitions with associated custom resources

You must migrate all workloads with minimal downtime and zero data loss.

## Constraints

1. Maximum 15 minutes of downtime for stateless workloads during cutover
2. Zero data loss for the PostgreSQL StatefulSet
3. All RBAC policies must be migrated (exclude system-generated resources)
4. CRDs and their instances must be migrated
5. DNS records must be updated to point to the new cluster's ingress
6. The old cluster must remain available as a fallback for 48 hours
7. Secrets must be re-encrypted in the target cluster (do not copy encryption keys)

## Success Criteria

1. All Deployments, StatefulSets, DaemonSets, and CronJobs are running in the target cluster
2. PostgreSQL data is intact and the application can read previously-written records
3. All Services are accessible via the new cluster's load balancer/ingress
4. RBAC policies are enforced identically in the target cluster
5. CRDs and custom resources are present and functional
6. DNS resolution points to the new cluster
7. The old cluster is in read-only standby mode
8. A rollback procedure document exists and has been tested

## Verification Commands

```bash
# Source cluster: verify backup completeness
velero backup describe full-migration --details

# Target cluster: all workloads running
kubectl get deployments -A
kubectl get statefulsets -A
kubectl get cronjobs -A
kubectl get daemonsets -A

# Target cluster: data integrity
kubectl exec -n database sts/postgres -- psql -U app -d appdb -c "SELECT count(*) FROM critical_table;"
# Compare count with source cluster

# Target cluster: RBAC
kubectl get clusterroles | grep -v system
kubectl get clusterrolebindings | grep -v system
kubectl auth can-i list pods --as=dev-user -n development
# Should match source cluster permissions

# Target cluster: CRDs
kubectl get crds
kubectl get <custom-resource> -A

# Target cluster: services accessible
kubectl get svc -A -o wide
curl -s http://<new-ingress-ip>/health

# DNS cutover verified
dig app.example.com +short
# Should resolve to new cluster's ingress IP

# Source cluster: read-only verification
kubectl --context=old-cluster cordon --all
kubectl --context=old-cluster get nodes
```

## Cleanup

```bash
# After 48-hour validation period, decommission the old cluster

# 1. Final data comparison between clusters
# 2. Remove the old cluster DNS records
# 3. Drain and delete old worker nodes
# 4. Take a final etcd backup of the old cluster for archival
ETCDCTL_API=3 etcdctl snapshot save /backup/final-archive.db \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key

# 5. Tear down the old cluster
sudo kubeadm reset -f
# 6. Decommission old VMs/hardware
```
