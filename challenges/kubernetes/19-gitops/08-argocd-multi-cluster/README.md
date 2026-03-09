<!--
difficulty: advanced
concepts: [argocd-multi-cluster, cluster-registration, applicationset-cluster-generator, hub-spoke, cluster-secrets]
tools: [kubectl, argocd-cli, kind]
estimated_time: 45m
bloom_level: analyze
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/06-argocd-applicationsets]
-->

# 19.08 - ArgoCD Multi-Cluster Deployment

## Architecture

```
Hub-and-Spoke Multi-Cluster
=============================

  Hub Cluster (ArgoCD)
  ┌───────────────────────────┐
  │  ArgoCD Server            │
  │  Application Controller   │──────► Cluster Secrets
  │  ApplicationSet Controller│        (stored in argocd namespace)
  └───────────┬───────────────┘
              │
    ┌─────────┼──────────┐
    │         │          │
    ▼         ▼          ▼
  Spoke 1   Spoke 2    Spoke 3
  (dev)     (staging)  (prod)

  Each spoke is registered via 'argocd cluster add'
  which creates a Secret in the hub's argocd namespace.
```

ArgoCD runs on a single **hub** cluster and manages applications across multiple **spoke** clusters. Each spoke is registered by creating a cluster Secret that contains the API server URL and credentials. ApplicationSets with the cluster generator can then deploy to all registered clusters automatically.

## Suggested Steps

### 1. Create Multiple Clusters with kind

```bash
# Hub cluster (ArgoCD lives here)
kind create cluster --name hub --kubeconfig /tmp/hub.kubeconfig

# Spoke clusters
kind create cluster --name spoke-dev --kubeconfig /tmp/spoke-dev.kubeconfig
kind create cluster --name spoke-prod --kubeconfig /tmp/spoke-prod.kubeconfig
```

### 2. Install ArgoCD on the Hub

```bash
kubectl --kubeconfig /tmp/hub.kubeconfig create namespace argocd
kubectl --kubeconfig /tmp/hub.kubeconfig apply -n argocd \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl --kubeconfig /tmp/hub.kubeconfig wait --for=condition=Ready pods --all -n argocd --timeout=300s
```

### 3. Register Spoke Clusters

```bash
# Get admin password
ARGOCD_PASS=$(kubectl --kubeconfig /tmp/hub.kubeconfig -n argocd \
  get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)

# Port-forward and login
kubectl --kubeconfig /tmp/hub.kubeconfig port-forward svc/argocd-server -n argocd 8080:443 &
argocd login localhost:8080 --insecure --username admin --password "$ARGOCD_PASS"

# Register spoke clusters
argocd cluster add kind-spoke-dev --name spoke-dev --label env=dev
argocd cluster add kind-spoke-prod --name spoke-prod --label env=prod
```

### 4. Verify Cluster Registration

```bash
argocd cluster list

# Cluster secrets in the hub
kubectl --kubeconfig /tmp/hub.kubeconfig get secrets -n argocd \
  -l argocd.argoproj.io/secret-type=cluster
```

### 5. Deploy to a Specific Cluster

```yaml
# app-spoke-dev.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook-dev
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    name: spoke-dev                        # cluster name from registration
    namespace: guestbook
  syncPolicy:
    automated:
      prune: true
    syncOptions:
      - CreateNamespace=true
```

### 6. ApplicationSet with Cluster Generator

```yaml
# appset-all-clusters.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: cluster-monitoring
  namespace: argocd
spec:
  generators:
    - clusters:
        selector:
          matchExpressions:
            - key: env
              operator: In
              values:
                - dev
                - prod
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
        server: "{{server}}"
        namespace: monitoring
      syncPolicy:
        automated:
          prune: true
        syncOptions:
          - CreateNamespace=true
```

### 7. Matrix Generator: Apps x Clusters

```yaml
# appset-matrix-clusters.yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: platform-apps
  namespace: argocd
spec:
  generators:
    - matrix:
        generators:
          - clusters:
              selector:
                matchLabels:
                  env: prod
          - list:
              elements:
                - app: nginx-ingress
                  path: guestbook
                  namespace: ingress-system
                - app: cert-manager
                  path: guestbook
                  namespace: cert-manager
  template:
    metadata:
      name: "{{app}}-{{name}}"
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/argoproj/argocd-example-apps.git
        targetRevision: HEAD
        path: "{{path}}"
      destination:
        server: "{{server}}"
        namespace: "{{namespace}}"
      syncPolicy:
        automated:
          prune: true
        syncOptions:
          - CreateNamespace=true
```

## Verify

```bash
# 1. Clusters are registered
argocd cluster list

# 2. Applications deployed to correct clusters
argocd app list
argocd app get guestbook-dev

# 3. Resources exist on spoke-dev
kubectl --kubeconfig /tmp/spoke-dev.kubeconfig get all -n guestbook

# 4. Monitoring deployed to all clusters
kubectl get applications -n argocd -l '!environment' | grep monitoring

# 5. Check cluster health
argocd cluster get spoke-dev
argocd cluster get spoke-prod
```

## Cleanup

```bash
kubectl delete applicationset cluster-monitoring platform-apps -n argocd
kubectl delete application guestbook-dev -n argocd
sleep 15
kind delete cluster --name hub
kind delete cluster --name spoke-dev
kind delete cluster --name spoke-prod
rm /tmp/hub.kubeconfig /tmp/spoke-dev.kubeconfig /tmp/spoke-prod.kubeconfig
```

## References

- [ArgoCD Cluster Management](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/#clusters)
- [Cluster Generator](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Cluster/)
- [ArgoCD Multi-Cluster](https://argo-cd.readthedocs.io/en/stable/getting_started/#5-register-a-cluster-to-deploy-apps-to-optional)
