# 14. Multi-Cluster Deployment Orchestration

<!--
difficulty: insane
concepts: [multi-cluster, deployment-orchestration, kube-contexts, phased-rollout, health-validation, rollback-coordination]
tools: [kubectl, kind, minikube]
estimated_time: 90m
bloom_level: create
prerequisites: [02-05, 02-08, 02-09, 02-13]
-->

## Prerequisites

- Two separate Kubernetes clusters (use `kind` to create them: `kind create cluster --name=cluster-east` and `kind create cluster --name=cluster-west`)
- `kubectl` installed with both contexts configured
- Completion of exercises [05](../05-rolling-updates-and-rollbacks/05-rolling-updates-and-rollbacks.md), [08](../08-deployment-revision-history/08-deployment-revision-history.md), [09](../09-canary-deployments/09-canary-deployments.md), and [13](../13-blue-green-deployments/13-blue-green-deployments.md)

Verify both clusters are reachable:

```bash
kubectl --context kind-cluster-east cluster-info
kubectl --context kind-cluster-west cluster-info
```

## The Scenario

Your organization runs a critical API service across two Kubernetes clusters in different regions (east and west). Currently, deployments are done manually and ad-hoc. Last month, a bad deployment was pushed to both clusters simultaneously, causing a global outage. Management wants a deployment process that:

1. Deploys to the east cluster first (canary region)
2. Validates health in the east cluster before proceeding
3. Deploys to the west cluster only if east is healthy
4. Supports instant rollback of both clusters independently
5. Uses only native Kubernetes resources and shell scripting (no Argo CD, Flux, or other GitOps tools)

Build a deployment orchestration system using shell scripts and native Kubernetes primitives.

## Constraints

1. Both clusters must run a Deployment called `api-service` with 3 replicas, a ClusterIP Service, and health-check endpoints.
2. The deployment script must accept a target image version as a parameter and perform a **phased rollout**: east first, then west.
3. After updating east, the script must wait for `kubectl rollout status` to succeed AND verify the health endpoint returns 200 for at least 30 seconds before proceeding to west.
4. If the health check fails on east, the script must **automatically roll back** the east cluster and **skip** the west cluster entirely, printing a failure report.
5. The script must record the deployment in an annotation on each Deployment: `deploy.io/last-deploy` with the format `<image>|<timestamp>|<status>`.
6. Both clusters must maintain at least 3 revisions in their rollout history with descriptive change-cause annotations.
7. A separate **rollback script** must accept a cluster name and revision number and roll back that cluster while leaving the other cluster untouched.
8. All scripts must be idempotent -- running them twice with the same parameters produces the same result without errors.

## Success Criteria

1. Running `./deploy.sh nginx:1.27` updates east first, validates, then updates west.
2. Running `./deploy.sh nginx:9.99` (invalid image) updates east, detects the failure (ImagePullBackOff or failed health check), rolls back east, and never touches west.
3. `kubectl --context kind-cluster-east rollout history deployment/api-service` shows descriptive change-cause annotations for each revision.
4. `kubectl --context kind-cluster-west rollout history deployment/api-service` shows its own independent history.
5. Running `./rollback.sh cluster-east 2` rolls back only the east cluster to revision 2.
6. After a successful deployment, the annotation `deploy.io/last-deploy` on both clusters contains the image, timestamp, and `success` status.
7. After a failed deployment, the annotation on the east cluster contains the image, timestamp, and `rolled-back` status. The west cluster annotation is unchanged.

## Verification Commands

```bash
# Check both clusters
for ctx in kind-cluster-east kind-cluster-west; do
  echo "=== $ctx ==="
  kubectl --context "$ctx" get deployment api-service
  kubectl --context "$ctx" rollout history deployment/api-service
  kubectl --context "$ctx" get deployment api-service -o jsonpath='{.metadata.annotations.deploy\.io/last-deploy}'
  echo
done
```

```bash
# Health check (adapt the port-forward to each context)
kubectl --context kind-cluster-east port-forward svc/api-service 8080:80 &
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080
kill %1

kubectl --context kind-cluster-west port-forward svc/api-service 8081:80 &
curl -s -o /dev/null -w "%{http_code}" http://localhost:8081
kill %1
```

## Cleanup

```bash
for ctx in kind-cluster-east kind-cluster-west; do
  kubectl --context "$ctx" delete deployment api-service
  kubectl --context "$ctx" delete svc api-service
done
kind delete cluster --name=cluster-east
kind delete cluster --name=cluster-west
```
