<!--
difficulty: basic
concepts: [argocd, gitops, application-crd, sync-status, health-status, self-heal]
tools: [kubectl, argocd-cli, helm]
estimated_time: 40m
bloom_level: understand
prerequisites: [deployments, services, namespaces]
-->

# 19.01 - ArgoCD: GitOps Deployment

## Why This Matters

In traditional CI/CD, a pipeline pushes changes to the cluster -- if the pipeline breaks or someone runs `kubectl edit` directly, the cluster state drifts from what is in version control. **GitOps** flips this model: Git is the single source of truth, and a controller running inside the cluster continuously reconciles the actual state to match what Git declares. **ArgoCD** is the most widely adopted GitOps controller for Kubernetes. It watches your repository, detects drift, and can automatically sync -- or alert you -- when the cluster diverges.

## What You Will Learn

- How to install ArgoCD and access the UI
- How the Application CRD links a Git repository to a cluster destination
- How sync status (Synced/OutOfSync) and health status (Healthy/Degraded) work
- How automated sync with `selfHeal` reverts manual cluster changes
- How to deploy applications sourced from plain manifests, Kustomize, and Helm

## Step-by-Step Guide

### 1. Install ArgoCD

```bash
kubectl create namespace argocd

# Option A: plain manifests
kubectl apply -n argocd \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# Option B: Helm
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update
helm install argocd argo/argo-cd \
  --namespace argocd \
  --create-namespace \
  --set server.service.type=ClusterIP \
  --wait
```

### 2. Wait for All Pods

```bash
kubectl wait --for=condition=Ready pods --all -n argocd --timeout=300s
```

### 3. Retrieve the Admin Password

```bash
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 -d; echo
```

### 4. Access the UI

```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443 &
echo "Open https://localhost:8080  (user: admin)"
```

### 5. Install the ArgoCD CLI (Optional)

```bash
# macOS
brew install argocd

# Linux
curl -sSL -o argocd \
  https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64
chmod +x argocd && sudo mv argocd /usr/local/bin/
```

```bash
argocd login localhost:8080 --insecure --username admin \
  --password "$(kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 -d)"
```

### 6. Create an Application (Plain Manifests)

```yaml
# argocd-app-guestbook.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io   # cascade-delete managed resources
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook                            # directory containing manifests
  destination:
    server: https://kubernetes.default.svc     # in-cluster
    namespace: guestbook
  syncPolicy:
    automated:
      prune: true                              # delete resources removed from Git
      selfHeal: true                           # revert manual cluster edits
    syncOptions:
      - CreateNamespace=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
```

### 7. Create an Application (Kustomize Source)

```yaml
# argocd-app-kustomize.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kustomize-guestbook
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: kustomize-guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: kustomize-guestbook
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### 8. Create an Application (Helm Source)

```yaml
# argocd-app-helm.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: helm-guestbook
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: helm-guestbook
    helm:
      valueFiles:
        - values.yaml
      parameters:
        - name: replicaCount
          value: "2"
  destination:
    server: https://kubernetes.default.svc
    namespace: helm-guestbook
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### 9. Apply All Applications

```bash
kubectl apply -f argocd-app-guestbook.yaml
kubectl apply -f argocd-app-kustomize.yaml
kubectl apply -f argocd-app-helm.yaml
```

### 10. Test Self-Heal

```bash
# Manually scale the guestbook deployment
kubectl scale deployment guestbook-ui -n guestbook --replicas=5

# Wait for ArgoCD to detect and revert
echo "Waiting 30s for ArgoCD to self-heal..."
sleep 30
kubectl get deployment guestbook-ui -n guestbook -o jsonpath='{.spec.replicas}' && echo ""
# Should be back to the Git-declared value
```

## Common Mistakes

1. **Forgetting the `resources-finalizer`** -- Without the finalizer, deleting an Application leaves orphaned resources in the destination namespace.
2. **Not enabling `CreateNamespace=true`** -- ArgoCD does not create the destination namespace by default. Without this sync option, the first sync fails with "namespace not found."
3. **Using `selfHeal` without understanding it** -- Self-heal reverts ALL manual changes, including emergency hotfixes applied via `kubectl`. Disable it during incident response or use `argocd app sync --prune=false`.
4. **Pointing multiple Applications at the same namespace** -- Resource name collisions cause sync failures. Use distinct namespaces or careful naming.

## Verify

```bash
# 1. ArgoCD pods are running
kubectl get pods -n argocd

# 2. Applications exist
kubectl get applications -n argocd

# 3. Sync and health status
kubectl get applications -n argocd -o custom-columns=\
"NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status"

# 4. Resources in destination namespaces
kubectl get all -n guestbook
kubectl get all -n kustomize-guestbook
kubectl get all -n helm-guestbook

# 5. CLI status
argocd app list
argocd app get guestbook

# 6. Sync history
argocd app history guestbook
```

## Cleanup

```bash
kubectl delete application guestbook kustomize-guestbook helm-guestbook -n argocd
sleep 15    # wait for finalizers to clean up managed resources
kubectl delete namespace argocd
```

## What's Next

In the next exercise you will explore ArgoCD sync policies in depth -- manual vs automated sync, sync windows, and sync waves: [19.02 - ArgoCD Sync Policies and Auto-Sync](../02-argocd-sync-policies/02-argocd-sync-policies.md).

## Summary

- GitOps uses Git as the single source of truth; a controller in the cluster reconciles actual state to match
- ArgoCD's Application CRD links a Git source (repo + path + revision) to a cluster destination (server + namespace)
- Sync status tracks whether the cluster matches Git; health status tracks whether resources are functioning
- `selfHeal: true` automatically reverts manual changes made outside Git
- `prune: true` deletes resources that were removed from the Git repository
- ArgoCD supports plain manifests, Kustomize, Helm, and Jsonnet as source types

## References

- [ArgoCD Getting Started](https://argo-cd.readthedocs.io/en/stable/getting_started/)
- [ArgoCD Declarative Setup](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)

## Additional Resources

- [ArgoCD Auto Sync](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)
- [ArgoCD Example Apps](https://github.com/argoproj/argocd-example-apps)
- [Application Health](https://argo-cd.readthedocs.io/en/stable/operator-manual/health/)
