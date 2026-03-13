<!--
difficulty: basic
concepts: [flux-v2, gitrepository, kustomization-controller, source-controller, reconciliation]
tools: [flux, kubectl]
estimated_time: 35m
bloom_level: understand
prerequisites: [deployments, services, 18-helm-kustomize-and-packaging/03-kustomize-basics]
-->

# 19.03 - Flux v2: GitRepository and Kustomization

## Why This Matters

**Flux v2** is the other major GitOps toolkit for Kubernetes, built as a set of composable controllers. Where ArgoCD provides a UI-centric experience, Flux follows a controller-per-concern design: the **source-controller** fetches Git repos (and Helm repos, OCI artifacts, S3 buckets), and the **kustomize-controller** applies Kustomize overlays from those sources. This separation makes Flux highly extensible and well-suited for teams that prefer a CLI-first, infrastructure-as-code workflow.

## What You Will Learn

- How to install Flux v2 using the `flux` CLI bootstrap
- How `GitRepository` resources tell the source-controller where to fetch manifests
- How `Kustomization` resources (Flux CRD, not to be confused with Kustomize's kustomization.yaml) tell the kustomize-controller what to apply and in what order
- How Flux's reconciliation loop continuously syncs Git state to the cluster

## Step-by-Step Guide

### 1. Install the Flux CLI

```bash
# macOS
brew install fluxcd/tap/flux

# Linux
curl -s https://fluxcd.io/install.sh | sudo bash
```

### 2. Check Cluster Compatibility

```bash
flux check --pre
```

### 3. Install Flux Controllers

```bash
# Bootstrap without a Git provider (for learning)
flux install
```

Wait for controllers to be ready:

```bash
kubectl wait --for=condition=Ready pods --all -n flux-system --timeout=120s
flux check
```

### 4. Create a GitRepository Source

```yaml
# git-repo.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  interval: 1m                             # poll Git every minute
  url: https://github.com/stefanprodan/podinfo.git
  ref:
    branch: master
  timeout: 60s
```

```bash
kubectl apply -f git-repo.yaml

# Check the source status
flux get sources git
kubectl get gitrepository -n flux-system
```

### 5. Create a Flux Kustomization

The Flux `Kustomization` CRD (different from Kustomize's `kustomization.yaml`) tells the kustomize-controller to apply manifests from a source.

```yaml
# flux-kustomization.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: podinfo
  namespace: flux-system
spec:
  interval: 5m                             # reconcile every 5 minutes
  sourceRef:
    kind: GitRepository
    name: podinfo                          # references the GitRepository above
  path: ./kustomize                        # directory in the repo to apply
  targetNamespace: podinfo                 # override namespace for all resources
  prune: true                              # garbage-collect removed resources
  wait: true                               # wait for resources to be ready
  timeout: 2m
  healthChecks:
    - apiVersion: apps/v1
      kind: Deployment
      name: podinfo
      namespace: podinfo
```

```bash
kubectl create namespace podinfo
kubectl apply -f flux-kustomization.yaml

# Watch reconciliation
flux get kustomizations --watch
```

### 6. Verify the Deployment

```bash
kubectl get all -n podinfo
```

### 7. Create a Second Kustomization with Dependencies

```yaml
# flux-kustomization-config.yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: podinfo-config
  namespace: flux-system
spec:
  dependsOn:
    - name: podinfo                        # wait for podinfo Kustomization to be ready
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: podinfo
  path: ./kustomize
  targetNamespace: podinfo-staging
  prune: true
  patches:                                 # inline patches in the Flux Kustomization
    - target:
        kind: Deployment
        name: podinfo
      patch: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: podinfo
        spec:
          replicas: 3
```

This shows how Flux Kustomizations can depend on each other and apply inline patches.

### 8. Force a Reconciliation

```bash
# Trigger immediate reconciliation without waiting for the interval
flux reconcile kustomization podinfo --with-source
```

### 9. Suspend and Resume

```bash
# Pause reconciliation (useful during maintenance)
flux suspend kustomization podinfo

# Resume
flux resume kustomization podinfo
```

## Common Mistakes

1. **Confusing Flux Kustomization with Kustomize kustomization.yaml** -- Flux's `Kustomization` CRD is a custom resource that drives the kustomize-controller. The `path` field points to a directory that may or may not contain a Kustomize `kustomization.yaml`. If no `kustomization.yaml` exists, Flux auto-generates one.
2. **Forgetting to create the target namespace** -- Unless you add `CreateNamespace=true` equivalent (not native in Flux -- you need a separate Kustomization that creates the namespace first, or use `targetNamespace` with a pre-existing namespace).
3. **Setting `interval` too low** -- Polling Git every 10 seconds generates excessive API calls. Use 1m minimum for learning, 5m+ for production.
4. **Not enabling `prune`** -- Without `prune: true`, resources removed from Git remain in the cluster as orphans.

## Verify

```bash
# 1. Flux controllers are healthy
flux check

# 2. GitRepository source is ready
flux get sources git
kubectl get gitrepository -n flux-system -o wide

# 3. Kustomization is applied and healthy
flux get kustomizations
kubectl get kustomization -n flux-system -o wide

# 4. Application pods are running
kubectl get pods -n podinfo

# 5. View events
flux events --for Kustomization/podinfo
```

## Cleanup

```bash
kubectl delete kustomization podinfo podinfo-config -n flux-system
kubectl delete gitrepository podinfo -n flux-system
kubectl delete namespace podinfo podinfo-staging --ignore-not-found
flux uninstall
```

## What's Next

In the next exercise you will learn ArgoCD application management patterns including health checks, resource hooks, and ignore differences: [19.04 - ArgoCD Application Management and Health Checks](../04-argocd-application-management/04-argocd-application-management.md).

## Summary

- Flux v2 is a composable GitOps toolkit with separate controllers for sources, Kustomize, Helm, notifications, and image automation
- `GitRepository` tells the source-controller where to fetch manifests and how often to poll
- Flux's `Kustomization` CRD applies manifests from a source, supports inline patches, health checks, and dependency ordering
- `prune: true` garbage-collects resources removed from Git
- `flux reconcile` forces an immediate sync without waiting for the polling interval
- `flux suspend/resume` pauses reconciliation for maintenance windows

## References

- [Flux Getting Started](https://fluxcd.io/flux/get-started/)
- [Flux GitRepository](https://fluxcd.io/flux/components/source/gitrepositories/)

## Additional Resources

- [Flux Kustomization](https://fluxcd.io/flux/components/kustomize/kustomizations/)
- [Flux Core Concepts](https://fluxcd.io/flux/concepts/)
- [Flux CLI Reference](https://fluxcd.io/flux/cmd/)
