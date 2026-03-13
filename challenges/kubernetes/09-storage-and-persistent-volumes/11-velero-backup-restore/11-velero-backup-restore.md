# 11. Namespace Backup and Restore with Velero

<!--
difficulty: insane
concepts: [velero, backup-restore, volume-snapshots, disaster-recovery, namespace-migration]
tools: [kubectl, velero, minikube, helm]
estimated_time: 90m
bloom_level: create
prerequisites: [09-01, 09-02, 09-07, 09-08]
-->

## Prerequisites

- A running Kubernetes cluster with a CSI driver that supports snapshots
- `kubectl` installed and configured
- `velero` CLI installed
- `helm` installed
- Completion of exercises [01](../01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md), [02](../02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md), [07](../07-volume-snapshots/07-volume-snapshots.md), and [08](../08-csi-drivers/08-csi-drivers.md)

## The Scenario

Your company runs a multi-service application in the `production` namespace: a PostgreSQL StatefulSet with persistent data, an nginx Deployment serving static files from a shared PVC, and associated ConfigMaps and Secrets. A compliance requirement mandates daily backups with the ability to restore an entire namespace to a different cluster or to a new namespace within 15 minutes. You must design and implement a backup/restore workflow using Velero that handles both Kubernetes resources and persistent volume data.

## Constraints

1. Install Velero with a compatible storage provider (use MinIO for local development or a cloud provider for managed clusters).
2. Create the `production` namespace with a PostgreSQL StatefulSet (3 replicas, `postgres:16`, each with a 1Gi PVC), an nginx Deployment (2 replicas with a shared ConfigMap), and at least one Secret.
3. Populate PostgreSQL with test data (create a database, table, and insert rows) and write files to the nginx PVC.
4. Create a Velero backup of the entire `production` namespace, including volume snapshots.
5. Delete the `production` namespace entirely (simulating disaster).
6. Restore the namespace from the Velero backup.
7. Verify all resources are recreated: StatefulSet pods are running, PostgreSQL data is intact (query the rows), nginx is serving the correct content, ConfigMaps and Secrets are present.
8. Demonstrate restoring to a **different namespace** (`production-clone`) and verify independence from the original.

## Success Criteria

1. Velero backup completes with status `Completed` and includes volume snapshots for all PVCs.
2. After namespace deletion, `kubectl get all -n production` returns nothing.
3. After restore, all pods reach `Running` state within 5 minutes.
4. PostgreSQL query returns the exact rows inserted before backup.
5. Nginx serves the same content as before the disaster.
6. The `production-clone` namespace operates independently with its own PVCs and data.
7. `velero backup describe` shows correct item counts matching the original namespace.

## Hints

<details>
<summary>Hint 1: Installing Velero with MinIO</summary>

Deploy MinIO as a local S3-compatible backend, then install Velero with the AWS provider plugin pointing to MinIO's endpoint. Use `--use-volume-snapshots=true` and `--default-volumes-to-fs-backup=true` for volume data.

</details>

<details>
<summary>Hint 2: PostgreSQL StatefulSet</summary>

Use `volumeClaimTemplates` with 1Gi PVCs. Set `POSTGRES_PASSWORD` via a Secret. Connect with `kubectl exec postgresql-0 -- psql -U postgres` to create the database and table.

</details>

<details>
<summary>Hint 3: Cross-namespace restore</summary>

Use `velero restore create --from-backup production-backup --namespace-mappings production:production-clone` to restore into a different namespace.

</details>

## Verification Commands

```bash
velero backup describe production-backup --details
velero restore describe production-restore --details

kubectl get all -n production
kubectl get pvc -n production
kubectl get configmap,secret -n production

kubectl exec -n production postgresql-0 -- psql -U postgres -d testdb -c "SELECT * FROM employees;"

kubectl get all -n production-clone
kubectl exec -n production-clone postgresql-0 -- psql -U postgres -d testdb -c "SELECT * FROM employees;"

# Verify PVC data integrity
kubectl get pvc -n production -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,CAPACITY:.status.capacity.storage
```

## Cleanup

```bash
velero restore delete production-restore --confirm
velero restore delete production-clone-restore --confirm
velero backup delete production-backup --confirm
kubectl delete namespace production production-clone --ignore-not-found
velero uninstall --force
```
