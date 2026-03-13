<!--
difficulty: intermediate
concepts: [applicationset, matrix-generator, merge-generator, cluster-generator, pull-request-generator, scm-provider-generator]
tools: [kubectl, argocd-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [19-gitops/05-argocd-app-of-apps]
-->

# 19.06 - ArgoCD ApplicationSets: Generator Types

## What You Will Build

Multiple ApplicationSets using different generator types: **matrix** (combine two generators), **merge** (override values from multiple generators), **cluster** (deploy to all registered clusters), and **pull request** (ephemeral environments per PR). You will see how each generator produces parameters that fill the ApplicationSet template.

## Step-by-Step Guide

### 1. Matrix Generator -- Cartesian Product

Deploy every app to every environment by combining a list of apps with a list of environments:

```yaml
# appset-matrix.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: matrix-apps
  namespace: argocd
spec:
  generators:
    - matrix:
        generators:
          - list:
              elements:
                - app: frontend
                  path: guestbook
                - app: backend
                  path: guestbook
          - list:
              elements:
                - env: dev
                  replicas: "1"
                - env: staging
                  replicas: "2"
                - env: prod
                  replicas: "3"
  template:
    metadata:
      name: "{{app}}-{{env}}"
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: "{{path}}"
      destination:
        server: https://kubernetes.default.svc
        namespace: "{{app}}-{{env}}"
      syncPolicy:
        syncOptions:
          - CreateNamespace=true
```

This produces 2 apps x 3 environments = 6 Applications.

### 2. Merge Generator -- Override Defaults

Use merge to define defaults and override specific values per environment:

```yaml
# appset-merge.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: merge-apps
  namespace: argocd
spec:
  generators:
    - merge:
        mergeKeys:
          - env
        generators:
          # Base generator: defaults for all environments
          - list:
              elements:
                - env: dev
                  replicas: "1"
                  region: us-east-1
                  syncPolicy: automated
                - env: staging
                  replicas: "2"
                  region: us-east-1
                  syncPolicy: automated
                - env: prod
                  replicas: "3"
                  region: us-west-2
                  syncPolicy: automated
          # Override generator: prod-specific values
          - list:
              elements:
                - env: prod
                  region: eu-west-1          # override region for prod
                  syncPolicy: manual         # override sync policy for prod
  template:
    metadata:
      name: "webapp-{{env}}"
      namespace: argocd
      labels:
        environment: "{{env}}"
        region: "{{region}}"
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: "webapp-{{env}}"
      syncPolicy:
        syncOptions:
          - CreateNamespace=true
```

### 3. Cluster Generator -- Deploy to All Clusters

The cluster generator iterates over all clusters registered in ArgoCD:

```yaml
# appset-cluster.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: cluster-addons
  namespace: argocd
spec:
  generators:
    - clusters:
        selector:
          matchLabels:
            env: production              # only target clusters labeled 'env: production'
  template:
    metadata:
      name: "monitoring-{{name}}"
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: guestbook
      destination:
        server: "{{server}}"             # each cluster's API server URL
        namespace: monitoring
      syncPolicy:
        automated:
          prune: true
        syncOptions:
          - CreateNamespace=true
```

For the in-cluster case (single cluster), add a label to the default cluster secret:

```bash
kubectl label secret -n argocd -l argocd.argoproj.io/secret-type=cluster env=production 2>/dev/null || \
  echo "No external cluster secrets found -- in-cluster only"
```

### 4. Pull Request Generator -- Ephemeral Environments

```yaml
# appset-pr.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: pr-preview
  namespace: argocd
spec:
  generators:
    - pullRequest:
        github:
          owner: argoproj
          repo: argocd-example-apps
          labels:
            - preview                    # only PRs with this label
        requeueAfterSeconds: 60
  template:
    metadata:
      name: "pr-{{number}}"
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: "https://github.com/argoproj/argocd-example-apps.git"
        targetRevision: "{{head_sha}}"
        path: guestbook
      destination:
        server: https://kubernetes.default.svc
        namespace: "pr-{{number}}"
      syncPolicy:
        automated:
          prune: true
        syncOptions:
          - CreateNamespace=true
```

When the PR is merged or closed, the ApplicationSet controller deletes the Application and its resources.

### 5. Apply and Observe

```bash
kubectl apply -f appset-matrix.yaml
kubectl apply -f appset-merge.yaml

# List generated Applications
kubectl get applications -n argocd --sort-by=.metadata.name
```

## Spot the Bug

This matrix generator is supposed to produce 6 Applications but produces 0:

```yaml
generators:
  - matrix:
      generators:
        - list:
            elements:
              - app: frontend
        - git:
            repoURL: https://github.com/argoproj/argocd-example-apps.git
            revision: HEAD
            directories:
              - path: "nonexistent-dir/*"
```

<details>
<summary>Answer</summary>

The git directory generator matches no directories because the path `nonexistent-dir/*` does not exist in the repository. In a matrix generator, if either generator produces zero elements, the cartesian product is empty -- zero Applications are created. Verify directory paths exist in the repository before using them in a git generator.

</details>

## Verify

```bash
# 1. Matrix generator created 6 Applications
kubectl get applications -n argocd -l '!environment' | grep -E "frontend|backend" | wc -l

# 2. Merge generator shows correct region for prod
kubectl get application webapp-prod -n argocd -o jsonpath='{.metadata.labels.region}' && echo ""

# 3. ApplicationSets are listed
kubectl get applicationset -n argocd

# 4. Describe a specific ApplicationSet
kubectl describe applicationset matrix-apps -n argocd
```

## Cleanup

```bash
kubectl delete applicationset matrix-apps merge-apps -n argocd
kubectl delete applicationset cluster-addons pr-preview -n argocd 2>/dev/null
sleep 10
kubectl get namespaces | grep -E "frontend-|backend-|webapp-" | awk '{print $1}' | xargs kubectl delete namespace --ignore-not-found
```

## What's Next

Next you will learn how Flux manages Helm releases using HelmRelease and HelmRepository resources: [19.07 - Flux HelmRelease and HelmRepository](../07-flux-helmrelease/07-flux-helmrelease.md).

## References

- [ApplicationSet Generators](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators/)
- [Matrix Generator](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Matrix/)
- [Merge Generator](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Merge/)
- [Pull Request Generator](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Pull-Request/)
