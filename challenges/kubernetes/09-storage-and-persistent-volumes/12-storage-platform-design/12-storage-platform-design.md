# 12. Cross-Cloud Storage Platform Design

<!--
difficulty: insane
concepts: [storage-abstraction, multi-cloud, storage-tiering, data-lifecycle, csi-integration, backup-strategy]
tools: [kubectl, helm, minikube]
estimated_time: 120m
bloom_level: create
prerequisites: [09-01, 09-02, 09-07, 09-08, 09-10, 09-11]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` and `helm` installed and configured
- Completion of all previous exercises in this category

## The Scenario

You are the storage platform engineer for a company running Kubernetes across three environments: AWS (EKS), GCP (GKE), and on-premises (bare metal with Rook-Ceph). Development teams should not need to know which cloud they are running on. They request storage by tier (fast, standard, archive) and access pattern (single-writer, shared-read, shared-write). You must design a storage platform that provides a consistent interface across all environments, handles automatic tiering, implements backup policies, and enforces quotas.

## Constraints

1. Define three storage tiers as StorageClasses: `tier-fast` (SSD, no replication delay), `tier-standard` (general purpose, balanced), and `tier-archive` (HDD, cheap, high latency acceptable). Each tier must work in all three environments by mapping to the appropriate backend (e.g., `tier-fast` maps to `gp3` on AWS, `pd-ssd` on GCP, `ceph-rbd-ssd` on-prem).
2. Create a storage request CRD-like pattern using ConfigMaps that teams fill out: team name, tier, access mode, capacity, backup frequency, and retention. Write a validation script or Job that checks these requests against namespace quotas.
3. Implement a data lifecycle policy: PVCs in `tier-fast` older than 30 days without pod attachment should be flagged for migration to `tier-standard`. Demonstrate this with a CronJob that identifies orphaned PVCs.
4. Design a backup matrix: `tier-fast` gets hourly snapshots (retain 24), `tier-standard` gets daily snapshots (retain 7), `tier-archive` gets weekly snapshots (retain 4). Implement at least the daily snapshot policy using a CronJob.
5. Create ResourceQuotas per namespace that limit total storage by tier: `tier-fast` max 50Gi per namespace, `tier-standard` max 200Gi, `tier-archive` max 1Ti.
6. Write a storage dashboard Job that reports: total capacity by tier, used capacity, number of PVCs, orphaned PVCs, and backup compliance status.
7. Demonstrate the complete workflow: a team requests 10Gi of `tier-fast` storage, the system validates the request, provisions the PVC, takes a snapshot, and the dashboard reflects the new state.

## Success Criteria

1. Three StorageClasses exist with appropriate parameters for the simulated environment.
2. The storage request validation Job correctly accepts valid requests and rejects requests that would exceed quotas.
3. The orphaned PVC detector CronJob identifies PVCs not mounted by any pod.
4. The snapshot CronJob successfully creates VolumeSnapshots and cleans up old ones beyond retention.
5. ResourceQuotas prevent provisioning beyond tier limits.
6. The dashboard Job produces a readable summary of cluster storage state.
7. End-to-end workflow completes: request, validate, provision, snapshot, report.

## Hints

<details>
<summary>Hint 1: Storage tier mapping</summary>

Use StorageClass parameters to simulate different backends. For local development, all three tiers can use the same provisioner but with different `reclaimPolicy` and `volumeBindingMode` settings to demonstrate the concept.

</details>

<details>
<summary>Hint 2: Orphaned PVC detection</summary>

A PVC is "orphaned" if no pod references it via `spec.volumes[].persistentVolumeClaim.claimName`. Use `kubectl get pods` and `kubectl get pvc` in a shell script to cross-reference.

</details>

<details>
<summary>Hint 3: Snapshot retention CronJob</summary>

Use `kubectl get volumesnapshot --sort-by=.metadata.creationTimestamp` and delete snapshots older than the retention window. Parse timestamps with `date` commands.

</details>

## Verification Commands

```bash
kubectl get storageclass | grep tier-
kubectl get resourcequota -A | grep storage
kubectl get pvc -A -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name,SC:.spec.storageClassName,SIZE:.spec.resources.requests.storage
kubectl get volumesnapshot -A
kubectl logs job/storage-dashboard
kubectl logs job/pvc-validator

# Verify tier quotas
for ns in $(kubectl get ns -l managed-by=storage-platform -o name); do
  echo "=== $ns ==="
  kubectl describe resourcequota -n $(basename $ns) | grep -E "tier-|Used|Hard"
done
```

## Cleanup

```bash
kubectl delete cronjob orphan-detector snapshot-manager --ignore-not-found
kubectl delete job storage-dashboard pvc-validator --ignore-not-found
kubectl delete pvc -l managed-by=storage-platform --ignore-not-found
kubectl delete storageclass tier-fast tier-standard tier-archive --ignore-not-found
kubectl delete resourcequota -l managed-by=storage-platform -A --ignore-not-found
kubectl delete namespace storage-platform-demo --ignore-not-found
```
