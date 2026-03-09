# 8. Deployment Revision History and Management

<!--
difficulty: intermediate
concepts: [revision-history, revisionHistoryLimit, change-cause, rollback-to-revision, annotation-tracking]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [02-05, 02-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/) and [exercise 07 (ReplicaSets and Ownership)](../07-replicasets-and-ownership/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** annotations to track change causes in Deployment revision history
- **Analyze** specific revisions using `kubectl rollout history --revision`
- **Apply** rollback to a specific revision number (not just the previous one)

## Why Revision History Management?

In production, you might deploy ten times in a week. When a regression appears on Friday, you need to know exactly what changed in each version and roll back to the specific revision that last worked. Kubernetes tracks every Pod template change as a numbered revision. Without annotations, these revisions show `<none>` for their change cause, making it impossible to identify which revision corresponds to which change.

## Step 1: Deploy with Change Tracking

```yaml
# tracked-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tracked-app
  annotations:
    kubernetes.io/change-cause: "Initial deployment with nginx 1.24"
spec:
  replicas: 3
  revisionHistoryLimit: 5
  selector:
    matchLabels:
      app: tracked-app
  template:
    metadata:
      labels:
        app: tracked-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.24
```

```bash
kubectl apply -f tracked-deploy.yaml
kubectl rollout status deployment/tracked-app
```

## Step 2: Create Multiple Revisions

```bash
kubectl set image deployment/tracked-app nginx=nginx:1.25
kubectl annotate deployment/tracked-app kubernetes.io/change-cause="Upgrade nginx to 1.25 for security patch" --overwrite
kubectl rollout status deployment/tracked-app

kubectl set image deployment/tracked-app nginx=nginx:1.26
kubectl annotate deployment/tracked-app kubernetes.io/change-cause="Upgrade nginx to 1.26 for performance fix" --overwrite
kubectl rollout status deployment/tracked-app

kubectl set image deployment/tracked-app nginx=nginx:1.27
kubectl annotate deployment/tracked-app kubernetes.io/change-cause="Upgrade nginx to 1.27 for HTTP/3 support" --overwrite
kubectl rollout status deployment/tracked-app
```

View the full history:

```bash
kubectl rollout history deployment/tracked-app
```

Expected output:

```
deployment.apps/tracked-app
REVISION  CHANGE-CAUSE
1         Initial deployment with nginx 1.24
2         Upgrade nginx to 1.25 for security patch
3         Upgrade nginx to 1.26 for performance fix
4         Upgrade nginx to 1.27 for HTTP/3 support
```

## Step 3: Inspect Specific Revisions

View the details of revision 2:

```bash
kubectl rollout history deployment/tracked-app --revision=2
```

This shows the full Pod template for that revision, including the image, labels, and any other template fields.

Compare the images across revisions:

```bash
for rev in 1 2 3 4; do
  img=$(kubectl rollout history deployment/tracked-app --revision=$rev | grep Image | awk '{print $2}')
  echo "Revision $rev: $img"
done
```

## Step 4: Roll Back to a Specific Revision

Suppose revision 3 (nginx:1.26) was the last known good version:

```bash
kubectl rollout undo deployment/tracked-app --to-revision=3
kubectl rollout status deployment/tracked-app
```

Verify the image:

```bash
kubectl get deployment tracked-app -o jsonpath='{.spec.template.spec.containers[0].image}'
# Expected: nginx:1.26
```

Check the history:

```bash
kubectl rollout history deployment/tracked-app
```

Revision 3 no longer exists -- it became revision 5 (the rollback). This is Kubernetes' deduplication behavior.

## Step 5: Control revisionHistoryLimit

The `revisionHistoryLimit` field controls how many old ReplicaSets are kept:

```bash
kubectl get deployment tracked-app -o jsonpath='{.spec.revisionHistoryLimit}'
# Expected: 5
```

Count the current ReplicaSets:

```bash
kubectl get rs -l app=tracked-app
```

Setting `revisionHistoryLimit: 0` would delete all old ReplicaSets, disabling rollback entirely. Use this only if disk space is critical and you have other rollback mechanisms (like GitOps).

## Spot the Bug

A developer uses `kubectl apply` to update the image but forgets to annotate. Then they need to find which revision had the security patch:

```bash
kubectl rollout history deployment/tracked-app
```

```
REVISION  CHANGE-CAUSE
5         Upgrade nginx to 1.26 for performance fix
6         <none>
```

<details>
<summary>Explanation</summary>

Revision 6 has no change-cause because the annotation was not updated. Always annotate after every change, or use a CI/CD pipeline that automatically sets the annotation. You can still inspect the revision details with `--revision=6` to see the image, but the human-readable context is lost.

</details>

## Verify What You Learned

```bash
kubectl rollout history deployment/tracked-app
# Expected: multiple revisions with descriptive change-causes

kubectl get deployment tracked-app -o jsonpath='{.spec.template.spec.containers[0].image}'
# Expected: nginx:1.26 (from the rollback to revision 3)

kubectl get rs -l app=tracked-app | wc -l
# Expected: header + multiple ReplicaSets (up to revisionHistoryLimit)
```

## Cleanup

```bash
kubectl delete deployment tracked-app
```

## What's Next

You now understand how to manage Deployment revision history for production traceability. The next exercises move into advanced territory with canary deployments using label selectors. Continue to [exercise 09 (Canary Deployments with Label Selectors)](../09-canary-deployments/).

## Summary

- Use `kubernetes.io/change-cause` annotations to document **why** each revision was created
- `kubectl rollout history --revision=N` shows the full Pod template for any revision
- `kubectl rollout undo --to-revision=N` rolls back to a specific revision (not just the previous one)
- `revisionHistoryLimit` controls how many old ReplicaSets are kept; default is 10
- Rolled-back revisions are renumbered -- the original revision number disappears

## Reference

- [Deployment Revision History](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#revision-history-limit) — revisionHistoryLimit
- [Rolling Back a Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#rolling-back-a-deployment) — undo and to-revision
- [kubectl rollout](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_rollout/) — CLI reference

## Additional Resources

- [Kubernetes API Reference: Deployment v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/)
- [GitOps and Deployment History](https://fluxcd.io/flux/concepts/) — alternative revision tracking
- [Annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) — metadata best practices
