<!--
difficulty: insane
concepts: [multi-cluster-gitops, multi-environment, progressive-delivery, secrets-management, repo-structure, platform-engineering]
tools: [argocd, flux, kubectl, helm, kustomize, sops]
estimated_time: 90m
bloom_level: create
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/05-argocd-app-of-apps, 19-gitops/08-argocd-multi-cluster, 19-gitops/12-gitops-secrets-management]
-->

# 19.13 - Full GitOps Platform: Multi-Env, Multi-Cluster

## Scenario

You are building the GitOps platform for a company that operates three environments (dev, staging, prod) across two clusters (us-east, eu-west). The platform must manage ten microservices, each with its own Helm chart, deployed through a promotion pipeline (dev -> staging -> prod). Secrets are encrypted with SOPS, infrastructure add-ons (ingress controller, cert-manager, monitoring) are deployed via App of Apps, and canary rollouts gate production deployments.

## Constraints

1. Use a **monorepo** structure with clear separation:
   - `infrastructure/` -- cluster add-ons (ingress, cert-manager, monitoring stack)
   - `apps/base/` -- base Kustomize manifests for each microservice
   - `apps/overlays/{dev,staging,prod}/` -- per-environment overlays
   - `clusters/{us-east,eu-west}/` -- per-cluster ArgoCD Application definitions
2. Deploy ArgoCD on a **management** cluster that manages all target clusters
3. Use **ApplicationSets with matrix generator** (clusters x apps) for infrastructure add-ons
4. Use **App of Apps** pattern for application deployments, with separate root apps per environment
5. Implement **promotion** by changing image tags in overlay kustomization files (not by re-tagging images)
6. All secrets in Git must be encrypted with **SOPS** using age keys; each environment has its own encryption key
7. Production microservices must use **Argo Rollouts canary strategy** with analysis templates that check HTTP success rate
8. Configure **sync windows** to block production deployments outside business hours (Mon-Fri 09:00-17:00 UTC)
9. Implement **drift detection** alerts -- any manual `kubectl` change in prod must generate a notification (use ArgoCD notifications or Flux alerts)
10. Document the full directory structure and the promotion workflow

## Success Criteria

1. Three kind clusters are running: management (ArgoCD), us-east (target), eu-west (target)
2. `argocd cluster list` shows both target clusters registered
3. Infrastructure add-ons are deployed to both target clusters via a single ApplicationSet
4. At least three microservices are deployed to dev, staging, and prod via Kustomize overlays
5. SOPS-encrypted secrets in each overlay can be decrypted only with the correct environment key
6. Promoting an image tag from dev to staging requires only a single file change in `apps/overlays/staging/kustomization.yaml`
7. Production deployments use Argo Rollouts canary with at least a 3-step weight progression (20% -> 50% -> 100%)
8. Sync windows block prod deployments outside the defined schedule
9. A manual `kubectl scale` in prod triggers an automatic revert (selfHeal) within 60 seconds
10. A README.md in the repo root documents the full architecture, directory layout, and promotion process

## Verification Commands

```bash
# Cluster registration
argocd cluster list

# Infrastructure deployed to both clusters
kubectl --context kind-us-east get pods -n ingress-system
kubectl --context kind-eu-west get pods -n ingress-system

# Applications across environments
argocd app list | grep -E "dev|staging|prod"

# SOPS encryption per environment
sops --decrypt apps/overlays/dev/secrets.enc.yaml 2>/dev/null && echo "dev: OK"
sops --decrypt apps/overlays/prod/secrets.enc.yaml 2>/dev/null && echo "prod: OK"

# Canary rollout in prod
kubectl --context kind-us-east argo rollouts get rollout api-server -n prod --watch

# Sync windows
argocd proj windows list production

# Self-heal test
kubectl --context kind-us-east scale deployment api-server -n prod --replicas=10
sleep 60
kubectl --context kind-us-east get deployment api-server -n prod -o jsonpath='{.spec.replicas}'

# Directory structure
find . -name "*.yaml" | head -50
tree -L 3 .
```

## Cleanup

```bash
argocd app delete-all --yes
kind delete cluster --name management
kind delete cluster --name us-east
kind delete cluster --name eu-west
```
