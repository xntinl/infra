# 12. Zero-Downtime StatefulSet Migration with Data Continuity

<!--
difficulty: insane
concepts: [statefulset-migration, pvc-retention, data-continuity, headless-service]
tools: [kubectl, minikube]
estimated_time: 90m
bloom_level: create
prerequisites: [03-01, 03-09, 03-10]
-->

## Prerequisites

- A running Kubernetes cluster with a default StorageClass (`kubectl get storageclass`)
- `kubectl` configured and cluster reachable
- Exercises 01, 09, and 10 in this category completed

## The Scenario

You operate a 3-replica StatefulSet (`datastore`) with per-pod PersistentVolumeClaims containing unique data. A breaking change requires modifying `volumeClaimTemplates` (which are immutable), switching to a different container image, and updating resource limits. A naive delete-and-recreate causes downtime. You need a migration plan that preserves data, maintains DNS resolution through the headless Service, and keeps the application accessible throughout.

## Constraints

1. Original StatefulSet: 3 replicas with a headless Service `datastore` providing DNS like `datastore-0.datastore.default.svc.cluster.local`. Each pod writes a unique identifier to its PVC at startup.
2. New StatefulSet must use a different container image AND different resource limits.
3. When original StatefulSet pods are deleted, their PVCs must NOT be deleted. New pods must bind to the existing PVCs. Understand `persistentVolumeClaimRetentionPolicy` and orphan-cascade delete.
4. The headless Service must resolve pod DNS names throughout migration. Clients should never get prolonged NXDOMAIN responses.
5. Migrate one pod at a time, highest ordinal first. Verify health and data integrity at each step.
6. Data written by original pods must be readable by new pods. Demonstrate explicitly.
7. If a new pod fails to become healthy, you must be able to restore the original pod without data loss.

## Success Criteria

- Before migration: all 3 original pods running, each with unique PVC data.
- During migration: at least 2 healthy endpoints at all times.
- After migration: all 3 new pods running with updated image and resources. Each PVC contains data written by the original pod at the same ordinal.
- `nslookup datastore-0.datastore.default.svc.cluster.local` resolves throughout (IP may change but should not return NXDOMAIN for extended periods).
- PVC creation timestamps predate the migration (PVCs were reused, not recreated).

## Verification Commands

```bash
kubectl get pvc -l app=datastore
for i in 0 1 2; do kubectl exec datastore-$i -- cat /data/identity; done
kubectl get pods -l app=datastore -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].image}{"\n"}{end}'
kubectl get pvc -l app=datastore -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.creationTimestamp}{"\n"}{end}'
kubectl run dns-test --rm -it --restart=Never --image=busybox:1.37 -- nslookup datastore-0.datastore.default.svc.cluster.local
```

## Cleanup

```bash
kubectl delete statefulset datastore --cascade=orphan
kubectl delete pvc -l app=datastore
kubectl delete service datastore
```
