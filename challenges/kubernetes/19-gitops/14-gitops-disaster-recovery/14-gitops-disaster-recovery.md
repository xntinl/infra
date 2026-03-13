<!--
difficulty: insane
concepts: [gitops-disaster-recovery, drift-detection, state-backup, cluster-rebuild, reconciliation-validation, git-rollback]
tools: [argocd, flux, kubectl, velero, git]
estimated_time: 75m
bloom_level: create
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/03-flux-basics, 19-gitops/08-argocd-multi-cluster, 19-gitops/12-gitops-secrets-management]
-->

# 19.14 - GitOps Disaster Recovery and Drift Detection

## Scenario

Your company's production cluster experienced a catastrophic failure at 03:00 UTC. The nodes are gone, but your Git repository is intact. Your job is to prove that GitOps enables full cluster recovery from Git state alone, detect and remediate drift scenarios, and build operational runbooks for disaster recovery.

You will simulate three failure modes, recover from each using only Git as the source of truth, and build automated drift detection that alerts within 5 minutes of unauthorized changes.

## Constraints

1. **Failure Mode 1 -- Full Cluster Loss**: Destroy and recreate the cluster. Reinstall ArgoCD (or Flux). All applications must self-recover from Git within 10 minutes without any manual `kubectl apply` of application manifests
2. **Failure Mode 2 -- GitOps Controller Failure**: Delete the ArgoCD namespace (or flux-system). Demonstrate that after reinstalling the controller, it detects and reconciles all drift automatically
3. **Failure Mode 3 -- Accidental Git Commit**: Push a bad commit that removes a critical Deployment from Git. Demonstrate that ArgoCD prunes the resource, then recover by reverting the commit and confirming the Deployment returns
4. Build a **drift detection** mechanism that compares live cluster state against Git and produces a report of all differences. The report must include: resource kind, name, namespace, and a summary of what differs
5. Create an **ArgoCD Notification** (or Flux Alert) configuration that sends drift alerts to a webhook endpoint when any application goes OutOfSync
6. Write a **disaster recovery runbook** as a shell script that:
   - Bootstraps ArgoCD/Flux from scratch
   - Restores all Application/Kustomization CRDs from a Git backup
   - Waits for full reconciliation
   - Validates every application is Synced and Healthy
   - Outputs a recovery report with timestamps
7. The recovery script must handle secrets correctly -- demonstrate that SOPS-encrypted secrets or Sealed Secrets are restored without manual intervention
8. Measure and document **Recovery Time Objective (RTO)**: time from cluster creation to all apps healthy

## Success Criteria

1. After destroying and recreating the cluster, the DR script achieves full recovery with all applications Synced and Healthy
2. The RTO is documented and is under 15 minutes for a cluster with at least 5 applications
3. After deleting the ArgoCD namespace, reinstalling ArgoCD causes it to rediscover and sync all applications from the Application CRDs that were reapplied from Git
4. The bad-commit scenario shows the Deployment being pruned, then restored after `git revert`
5. The drift detection script produces a machine-readable report (JSON or CSV) showing all differences between Git and live state
6. The notification/alert configuration fires within 5 minutes of an application going OutOfSync
7. The DR runbook script exits 0 on successful recovery and non-zero with diagnostic output on failure

## Verification Commands

```bash
# Failure Mode 1: Full cluster rebuild
kind delete cluster --name prod
kind create cluster --name prod
./dr-runbook.sh --bootstrap --validate
# Should exit 0 with recovery report

# Failure Mode 2: Controller failure
kubectl delete namespace argocd --wait=true
./dr-runbook.sh --bootstrap-controller-only --validate
argocd app list | grep -c "Synced"

# Failure Mode 3: Bad commit recovery
git log --oneline -5
git revert HEAD --no-edit
git push
sleep 120
argocd app get critical-app | grep "Synced"

# Drift detection
./detect-drift.sh --output json > drift-report.json
cat drift-report.json | jq '.drifted_resources | length'

# Notification test
kubectl scale deployment webapp -n prod --replicas=99
sleep 300
# Check webhook endpoint for OutOfSync notification

# RTO measurement
./dr-runbook.sh --bootstrap --validate --measure-rto
# Output: "RTO: 8m32s -- 5/5 applications recovered"
```

## Cleanup

```bash
kind delete cluster --name prod
rm drift-report.json
```
