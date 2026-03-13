<!--
difficulty: intermediate
concepts: [app-of-apps-pattern, applicationset, generators, appproject, cluster-bootstrapping]
tools: [kubectl, argocd-cli]
estimated_time: 40m
bloom_level: apply
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/02-argocd-sync-policies]
-->

# 19.05 - ArgoCD: App of Apps Pattern

## What You Will Build

A cluster bootstrapping setup using the App of Apps pattern: a single root Application that manages child Applications as its resources. When ArgoCD syncs the root, it creates all child Applications, which in turn deploy their own resources. You will also create an AppProject for access isolation and an ApplicationSet with a list generator for multi-environment deployment.

## Step-by-Step Guide

### 1. Prerequisites

ArgoCD must be installed (see [19.01](../01-argocd-gitops-deployment/01-argocd-gitops-deployment.md)).

### 2. Create an AppProject for Isolation

```yaml
# team-platform-project.yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: team-platform
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  description: "Project for the platform team"
  sourceRepos:
    - "https://github.com/argoproj/argocd-example-apps.git"
    - "*"
  destinations:
    - namespace: "frontend-*"
      server: https://kubernetes.default.svc
    - namespace: "backend-*"
      server: https://kubernetes.default.svc
    - namespace: "argocd"
      server: https://kubernetes.default.svc
  clusterResourceWhitelist:
    - group: ""
      kind: Namespace
  namespaceResourceWhitelist:
    - group: "*"
      kind: "*"
  orphanedResources:
    warn: true
```

### 3. Create Child Applications

```yaml
# child-apps/frontend.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: frontend
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
  labels:
    app-group: platform
    component: frontend
spec:
  project: team-platform
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: frontend-app
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

```yaml
# child-apps/backend.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: backend
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
  labels:
    app-group: platform
    component: backend
spec:
  project: team-platform
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: backend-app
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

```yaml
# child-apps/cache.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cache
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
  labels:
    app-group: platform
    component: cache
spec:
  project: team-platform
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: backend-cache
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

### 4. Create the Root Application

In a real Git workflow, the root Application points at the directory containing the child Application manifests. For this exercise, apply the children directly to simulate what the root would do:

```yaml
# root-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: root-app
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: apps                             # directory containing child Application manifests
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

Apply the project and child apps:

```bash
kubectl apply -f team-platform-project.yaml
kubectl apply -f child-apps/frontend.yaml
kubectl apply -f child-apps/backend.yaml
kubectl apply -f child-apps/cache.yaml
```

### 5. ApplicationSet with List Generator

```yaml
# appset-multi-env.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: multi-env-guestbook
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - env: dev
            namespace: guestbook-dev
            replicas: "1"
            targetRevision: HEAD
          - env: staging
            namespace: guestbook-staging
            replicas: "2"
            targetRevision: HEAD
          - env: prod
            namespace: guestbook-prod
            replicas: "3"
            targetRevision: HEAD
  template:
    metadata:
      name: "guestbook-{{env}}"
      namespace: argocd
      labels:
        environment: "{{env}}"
        managed-by: applicationset
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: "{{targetRevision}}"
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: "{{namespace}}"
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
        syncOptions:
          - CreateNamespace=true
```

### 6. ApplicationSet with Git Directory Generator

```yaml
# appset-git-dirs.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: git-directory-apps
  namespace: argocd
spec:
  generators:
    - git:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        revision: HEAD
        directories:
          - path: "*"
          - path: plugins
            exclude: true
  template:
    metadata:
      name: "{{path.basename}}"
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: "{{path}}"
      destination:
        server: https://kubernetes.default.svc
        namespace: "{{path.basename}}"
      syncPolicy:
        syncOptions:
          - CreateNamespace=true
```

```bash
kubectl apply -f appset-multi-env.yaml
kubectl apply -f appset-git-dirs.yaml
```

## Verify

```bash
# 1. AppProject exists
kubectl get appproject -n argocd
kubectl describe appproject team-platform -n argocd

# 2. Child Applications are running
kubectl get applications -n argocd

# 3. Sync and health status
kubectl get applications -n argocd -o custom-columns=\
"NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,NAMESPACE:.spec.destination.namespace"

# 4. Resources deployed by child apps
kubectl get all -n frontend-app
kubectl get all -n backend-app
kubectl get all -n backend-cache

# 5. ApplicationSet generated Applications
kubectl get applicationset -n argocd
kubectl get applications -n argocd -l managed-by=applicationset

# 6. Applications per environment
kubectl get applications -n argocd -l environment

# 7. Created namespaces
kubectl get namespaces | grep -E "guestbook|frontend|backend"
```

## Cleanup

```bash
kubectl delete applicationset multi-env-guestbook git-directory-apps -n argocd
kubectl delete application frontend backend cache -n argocd
sleep 20
kubectl delete appproject team-platform -n argocd
kubectl delete namespace frontend-app backend-app backend-cache \
  guestbook-dev guestbook-staging guestbook-prod --ignore-not-found
```

## What's Next

Next you will dive deeper into ApplicationSet generators -- matrix, merge, cluster, and pull request generators: [19.06 - ArgoCD ApplicationSets: Generator Types](../06-argocd-applicationsets/06-argocd-applicationsets.md).

## References

- [ArgoCD Cluster Bootstrapping](https://argo-cd.readthedocs.io/en/stable/operator-manual/cluster-bootstrapping/)
- [ArgoCD ApplicationSet](https://argo-cd.readthedocs.io/en/stable/user-guide/application-set/)
- [ApplicationSet Generators](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators/)
- [ArgoCD Projects](https://argo-cd.readthedocs.io/en/stable/user-guide/projects/)
