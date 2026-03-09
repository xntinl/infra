<!--
difficulty: intermediate
concepts: [flux-helmrelease, helmrepository, oci-repository, values-override, dependency-ordering, drift-detection]
tools: [flux, kubectl, helm]
estimated_time: 35m
bloom_level: apply
prerequisites: [19-gitops/03-flux-basics, 18-helm-kustomize-and-packaging/01-helm-chart-basics]
-->

# 19.07 - Flux HelmRelease and HelmRepository

## What You Will Build

A Flux-managed Helm deployment where HelmRepository sources define chart registries, HelmRelease resources declare chart installations with values overrides, and dependency ordering ensures infrastructure charts are installed before application charts.

## Step-by-Step Guide

### 1. Prerequisites

Flux must be installed (see [19.03](../03-flux-basics/)).

```bash
flux check
```

### 2. Create a HelmRepository Source

```yaml
# helm-repo-bitnami.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: bitnami
  namespace: flux-system
spec:
  interval: 30m                            # refresh chart index every 30 minutes
  url: https://charts.bitnami.com/bitnami
  timeout: 3m
```

```yaml
# helm-repo-podinfo.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  interval: 10m
  url: https://stefanprodan.github.io/podinfo
```

```bash
kubectl apply -f helm-repo-bitnami.yaml
kubectl apply -f helm-repo-podinfo.yaml

flux get sources helm
```

### 3. Create an OCI HelmRepository

```yaml
# helm-repo-oci.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo-oci
  namespace: flux-system
spec:
  type: oci                                # OCI registry instead of HTTP index
  interval: 10m
  url: oci://ghcr.io/stefanprodan/charts
```

### 4. Create a HelmRelease for Redis

```yaml
# helmrelease-redis.yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: redis
  namespace: flux-system
spec:
  interval: 15m
  chart:
    spec:
      chart: redis
      version: "18.x"                     # semver constraint
      sourceRef:
        kind: HelmRepository
        name: bitnami
      interval: 10m
  targetNamespace: data-layer              # install into this namespace
  install:
    createNamespace: true
    remediation:
      retries: 3
  upgrade:
    remediation:
      retries: 3
      remediateLastFailure: true
  values:
    architecture: standalone
    auth:
      enabled: false
    master:
      persistence:
        enabled: false
      resources:
        requests:
          cpu: 50m
          memory: 64Mi
        limits:
          cpu: 100m
          memory: 128Mi
```

### 5. Create a HelmRelease for Podinfo (Depends on Redis)

```yaml
# helmrelease-podinfo.yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: flux-system
spec:
  dependsOn:
    - name: redis                          # wait for Redis to be ready first
  interval: 15m
  chart:
    spec:
      chart: podinfo
      version: ">=6.0.0"
      sourceRef:
        kind: HelmRepository
        name: podinfo
  targetNamespace: app-layer
  install:
    createNamespace: true
  values:
    replicaCount: 2
    cache: "tcp://redis-master.data-layer:6379"
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
      limits:
        cpu: 100m
        memory: 128Mi
  # Values from ConfigMap/Secret
  valuesFrom:
    - kind: ConfigMap
      name: podinfo-values
      valuesKey: extra-values.yaml
      optional: true                       # do not fail if ConfigMap does not exist
```

### 6. Drift Detection

```yaml
# helmrelease-drift.yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo-strict
  namespace: flux-system
spec:
  interval: 5m
  chart:
    spec:
      chart: podinfo
      version: ">=6.0.0"
      sourceRef:
        kind: HelmRepository
        name: podinfo
  targetNamespace: drift-demo
  install:
    createNamespace: true
  driftDetection:
    mode: enabled                          # warn or enabled
    ignore:
      - paths:
          - /spec/replicas                 # ignore replica changes from HPA
        target:
          kind: Deployment
```

### 7. Apply All Resources

```bash
kubectl apply -f helm-repo-bitnami.yaml
kubectl apply -f helm-repo-podinfo.yaml
kubectl apply -f helmrelease-redis.yaml
kubectl apply -f helmrelease-podinfo.yaml

# Watch installation progress
flux get helmreleases --watch
```

## Verify

```bash
# 1. Helm repositories are ready
flux get sources helm

# 2. HelmReleases are installed
flux get helmreleases

# 3. Redis is running
kubectl get pods -n data-layer

# 4. Podinfo is running (installed after Redis)
kubectl get pods -n app-layer

# 5. Check Helm release metadata
helm list -n data-layer
helm list -n app-layer

# 6. Force reconciliation
flux reconcile helmrelease podinfo --with-source

# 7. View events
flux events --for HelmRelease/redis
flux events --for HelmRelease/podinfo
```

## Cleanup

```bash
kubectl delete helmrelease redis podinfo podinfo-strict -n flux-system
kubectl delete helmrepository bitnami podinfo podinfo-oci -n flux-system
kubectl delete namespace data-layer app-layer drift-demo --ignore-not-found
```

## What's Next

Next you will learn ArgoCD multi-cluster deployment with remote cluster registration: [19.08 - ArgoCD Multi-Cluster Deployment](../08-argocd-multi-cluster/).

## References

- [Flux HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/)
- [Flux HelmRepository](https://fluxcd.io/flux/components/source/helmrepositories/)
- [Flux Helm Controller](https://fluxcd.io/flux/components/helm/)
