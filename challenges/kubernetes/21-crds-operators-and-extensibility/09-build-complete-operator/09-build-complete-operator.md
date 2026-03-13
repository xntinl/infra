# Build a Complete Operator from Scratch

<!--
difficulty: insane
concepts: [operator, kubebuilder, controller-runtime, crd, reconciliation, webhooks, rbac, status, finalizers, e2e-testing]
tools: [kubebuilder, go, make, kubectl, docker, kind]
estimated_time: 120m
bloom_level: create
prerequisites: [06-kubebuilder-scaffold, 07-controller-runtime-reconciliation, 08-admission-webhooks]
-->

## Scenario

Build a production-quality **BackupPolicy** operator that automates PersistentVolumeClaim backups. The operator should:

- Accept a `BackupPolicy` custom resource that defines which PVCs to back up, the schedule, and retention
- Create CronJobs that snapshot PVCs on the defined schedule
- Track backup history in the status subresource
- Enforce validation rules via webhooks
- Clean up backup resources when the BackupPolicy is deleted

This simulates what production backup operators (Velero, Kasten) do at a simplified scale.

## Constraints

1. Use Kubebuilder to scaffold the project -- do not write CRD YAML by hand
2. The CRD must include: `spec.selector` (label selector for PVCs), `spec.schedule` (cron), `spec.retention.maxCount` (int), `spec.storage.bucket` (string)
3. The controller must create one CronJob per matched PVC, with owner references back to the BackupPolicy
4. Implement a mutating webhook that injects default retention (7 backups) and default schedule ("0 2 * * *")
5. Implement a validating webhook that rejects policies where `maxCount < 1` or `schedule` is not a valid 5-field cron expression
6. The status must track: `matchedPVCs` (count), `activeCronJobs` (count), `lastBackupTime`, `phase` (Active/Error/Suspended)
7. Implement a finalizer that deletes all CronJobs when the BackupPolicy is removed
8. Write at least 3 unit tests for the reconciler using envtest
9. The operator must be deployable to a kind cluster via `make deploy`

## Success Criteria

1. `kubebuilder create api` and `kubebuilder create webhook` produce a compilable project
2. `make test` passes with at least 3 reconciler tests
3. `make docker-build` produces a working container image
4. Deploying to a kind cluster and creating a BackupPolicy results in CronJobs being created for matching PVCs
5. Deleting the BackupPolicy removes all associated CronJobs
6. The mutating webhook injects correct defaults
7. The validating webhook rejects invalid policies (bad schedule, maxCount < 1)
8. The status subresource correctly reflects the number of matched PVCs and active CronJobs
9. Updating the BackupPolicy schedule updates all CronJob schedules

## Verification Commands

```bash
# CRD is installed
kubectl get crd backuppolicies.backup.example.com

# Operator is running
kubectl get pods -n backup-operator-system

# Create test PVCs
for i in 1 2 3; do
  kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data-vol-$i
  labels:
    backup: enabled
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
EOF
done

# Create BackupPolicy
kubectl apply -f - <<'EOF'
apiVersion: backup.example.com/v1
kind: BackupPolicy
metadata:
  name: nightly-backup
spec:
  selector:
    matchLabels:
      backup: enabled
  schedule: "0 2 * * *"
  retention:
    maxCount: 7
  storage:
    bucket: s3://my-backup-bucket
EOF

# Verify CronJobs were created (one per PVC)
kubectl get cronjobs
# Expected: 3 CronJobs

# Verify status
kubectl get backuppolicy nightly-backup -o yaml | grep -A 10 status
# Expected: matchedPVCs=3, activeCronJobs=3, phase=Active

# Update schedule
kubectl patch backuppolicy nightly-backup --type=merge -p '{"spec":{"schedule":"0 3 * * *"}}'
kubectl get cronjobs -o custom-columns='NAME:.metadata.name,SCHEDULE:.spec.schedule'
# All CronJobs should show "0 3 * * *"

# Test webhook: invalid schedule
kubectl apply -f - <<'EOF'
apiVersion: backup.example.com/v1
kind: BackupPolicy
metadata:
  name: bad-policy
spec:
  selector:
    matchLabels:
      backup: enabled
  schedule: "every day"
  retention:
    maxCount: 0
  storage:
    bucket: s3://bucket
EOF
# Expected: rejected by validating webhook

# Delete policy -- CronJobs should be cleaned up
kubectl delete backuppolicy nightly-backup
kubectl get cronjobs
# Expected: no CronJobs

# Run tests
make test
# Expected: PASS
```

## Cleanup

```bash
kubectl delete backuppolicies --all
kubectl delete pvc data-vol-1 data-vol-2 data-vol-3
make undeploy
make uninstall
kind delete cluster
```
