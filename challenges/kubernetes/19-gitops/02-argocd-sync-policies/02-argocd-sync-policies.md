<!--
difficulty: basic
concepts: [argocd-sync-policies, auto-sync, sync-waves, sync-windows, manual-sync, prune-propagation]
tools: [kubectl, argocd-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [19-gitops/01-argocd-gitops-deployment]
-->

# 19.02 - ArgoCD Sync Policies and Auto-Sync

## Why This Matters

ArgoCD can sync automatically on every Git push, or it can wait for manual approval -- the right choice depends on the environment. A dev cluster benefits from auto-sync for fast feedback, while production may require manual sync with approval gates. **Sync waves** control the order in which resources are applied (Namespace before Deployment, Deployment before Service mesh config), and **sync windows** restrict when syncs can happen (block production deployments during business hours).

## What You Will Learn

- The difference between manual sync, automated sync, and automated sync with self-heal
- How sync waves (`argocd.argoproj.io/sync-wave`) order resource creation
- How sync windows restrict sync operations to specific time periods
- How prune propagation deletes resources removed from Git

## Step-by-Step Guide

### 1. Prerequisites

ArgoCD must be installed (see [19.01](../01-argocd-gitops-deployment/01-argocd-gitops-deployment.md)).

### 2. Manual Sync Application

```yaml
# app-manual.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: manual-sync-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: manual-app
  syncPolicy:                              # no 'automated' key = manual sync
    syncOptions:
      - CreateNamespace=true
```

```bash
kubectl apply -f app-manual.yaml

# Check status -- should be OutOfSync
kubectl get application manual-sync-app -n argocd \
  -o jsonpath='{.status.sync.status}' && echo ""

# Trigger manual sync
argocd app sync manual-sync-app
```

### 3. Auto-Sync Application with Self-Heal

```yaml
# app-auto.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: auto-sync-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: auto-app
  syncPolicy:
    automated:
      prune: true                          # remove resources deleted from Git
      selfHeal: true                       # revert manual changes
      allowEmpty: false                    # never sync if source produces zero resources
    syncOptions:
      - CreateNamespace=true
      - PrunePropagationPolicy=foreground  # wait for dependents to be deleted
      - PruneLast=true                     # prune after all other resources are synced
```

### 4. Sync Waves -- Ordered Resource Creation

```yaml
# app-waves.yaml -- points to local manifests directory
# Create these manifests in a Git repo or apply them directly

# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: waves-demo
  annotations:
    argocd.argoproj.io/sync-wave: "-1"     # created first (lowest wave)

---
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: waves-demo
  annotations:
    argocd.argoproj.io/sync-wave: "0"      # created second
data:
  APP_ENV: "production"

---
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
  namespace: waves-demo
  annotations:
    argocd.argoproj.io/sync-wave: "1"      # created third
spec:
  replicas: 2
  selector:
    matchLabels:
      app: webapp
  template:
    metadata:
      labels:
        app: webapp
    spec:
      containers:
        - name: webapp
          image: nginx:1.27
          envFrom:
            - configMapRef:
                name: app-config
          resources:
            requests:
              cpu: 50m
              memory: 64Mi

---
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: webapp
  namespace: waves-demo
  annotations:
    argocd.argoproj.io/sync-wave: "2"      # created last
spec:
  selector:
    app: webapp
  ports:
    - port: 80
      targetPort: 80
```

Wave execution order: `-1` (Namespace) -> `0` (ConfigMap) -> `1` (Deployment) -> `2` (Service). ArgoCD waits for each wave's resources to be healthy before proceeding.

### 5. Sync Windows -- Time-Based Restrictions

```yaml
# project-with-windows.yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: production
  namespace: argocd
spec:
  description: Production project with sync windows
  sourceRepos:
    - "*"
  destinations:
    - namespace: "prod-*"
      server: https://kubernetes.default.svc
  syncWindows:
    - kind: allow                          # only allow syncs in this window
      schedule: "0 6 * * 1-5"             # weekdays at 06:00 UTC
      duration: 2h                         # for 2 hours
      applications:
        - "*"
    - kind: deny                           # block syncs in this window
      schedule: "0 0 * * 0"               # Sundays at midnight
      duration: 24h                        # all day
      applications:
        - "*"
      manualSync: true                     # also block manual syncs
```

## Common Mistakes

1. **Setting `prune: true` without testing** -- Prune deletes resources that are no longer in Git. If you accidentally remove a manifest file, prune will delete the live resource. Start with `prune: false` and enable it after validating.
2. **Sync wave deadlocks** -- If a resource in wave 0 never becomes healthy, all higher waves are blocked forever. Always set health checks that can succeed or fail within a reasonable timeout.
3. **Forgetting that sync windows affect the CLI** -- When `manualSync: true` is set on a deny window, even `argocd app sync` is blocked during that period.

## Verify

```bash
# 1. Manual sync app is OutOfSync until explicitly synced
kubectl get application manual-sync-app -n argocd -o jsonpath='{.status.sync.status}'

# 2. Auto sync app becomes Synced automatically
kubectl get application auto-sync-app -n argocd -o jsonpath='{.status.sync.status}'

# 3. Test self-heal: manually delete a resource and watch it return
kubectl delete svc guestbook-ui -n auto-app
sleep 20
kubectl get svc -n auto-app

# 4. Check sync waves executed in order
kubectl get events -n waves-demo --sort-by=.lastTimestamp

# 5. View sync windows on project
argocd proj windows list production
```

## Cleanup

```bash
kubectl delete application manual-sync-app auto-sync-app -n argocd
kubectl delete appproject production -n argocd
kubectl delete namespace manual-app auto-app waves-demo --ignore-not-found
```

## What's Next

In the next exercise you will install Flux v2 and learn its fundamental building blocks -- GitRepository and Kustomization controllers: [19.03 - Flux v2: GitRepository and Kustomization](../03-flux-basics/03-flux-basics.md).

## Summary

- Manual sync requires explicit `argocd app sync` commands; automated sync triggers on every detected Git change
- `selfHeal: true` reverts any cluster state that drifts from Git within the reconciliation interval
- Sync waves order resource creation: lower wave numbers are applied and must become healthy before higher waves begin
- Sync windows use cron schedules to allow or deny sync operations during specific time periods
- `PruneLast` ensures deletions happen only after all new resources are healthy
- `PrunePropagationPolicy: foreground` ensures dependent resources are fully deleted before the parent

## References

- [ArgoCD Auto Sync](https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/)
- [Sync Waves and Hooks](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-waves/)

## Additional Resources

- [ArgoCD Sync Options](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/)
- [ArgoCD Sync Windows](https://argo-cd.readthedocs.io/en/stable/user-guide/sync_windows/)
- [ArgoCD Projects](https://argo-cd.readthedocs.io/en/stable/user-guide/projects/)
