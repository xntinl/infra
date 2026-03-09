<!--
difficulty: advanced
concepts: [kustomize-helm-integration, helm-chart-inflation-generator, post-rendering, kustomize-helmcharts, hybrid-approach]
tools: [kubectl, kustomize, helm]
estimated_time: 35m
bloom_level: analyze
prerequisites: [18-helm-kustomize-and-packaging/03-kustomize-basics, 18-helm-kustomize-and-packaging/02-helm-values-and-templates]
-->

# 18.10 - Kustomize with Helm: HelmChartInflationGenerator

## Architecture

```
Kustomize + Helm Integration
==============================

  Option A: helmCharts in kustomization.yaml
  ┌─────────────────────────────────┐
  │  kustomization.yaml             │
  │    helmCharts:                  │
  │      - name: nginx              │──► kustomize build --enable-helm
  │        repo: https://...        │      │
  │        valuesInline:            │      ▼
  │          replicaCount: 3        │    Rendered YAML
  │    patches:                     │      │
  │      - target: Deployment       │      ▼
  │        patch: ...               │    Patched + transformed output
  └─────────────────────────────────┘

  Option B: helm template | kustomize (post-rendering)
  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
  │  helm        │    │  pipe to     │    │  kustomize   │
  │  template    │───►│  kustomize   │───►│  patches +   │
  │  chart       │    │  as resource │    │  transforms  │
  └──────────────┘    └──────────────┘    └──────────────┘
```

Kustomize can inflate Helm charts as a built-in generator, letting you combine Helm's templating with Kustomize's patching. You get the community chart ecosystem plus fine-grained post-rendering customization -- without maintaining a fork.

## Suggested Steps

### 1. Option A: helmCharts in kustomization.yaml

```yaml
# helm-kustomize/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: helm-kustomize-lab

helmCharts:
  - name: nginx
    repo: https://charts.bitnami.com/bitnami
    version: "15.14.0"
    releaseName: my-nginx
    namespace: helm-kustomize-lab
    valuesInline:
      replicaCount: 2
      service:
        type: ClusterIP
      resources:
        requests:
          cpu: 100m
          memory: 128Mi

# Apply Kustomize patches on top of Helm output
patches:
  - target:
      kind: Deployment
      name: my-nginx
    patch: |
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: my-nginx
        annotations:
          custom-annotation: "added-by-kustomize"

commonLabels:
  managed-by: kustomize-helm
```

Build with Helm support enabled:

```bash
kubectl kustomize helm-kustomize/ --enable-helm
```

### 2. Option B: Helm Post-Rendering with Kustomize

Create a post-render script:

```bash
#!/bin/bash
# post-render.sh
# Helm pipes rendered YAML to stdin; we apply kustomize patches
cat > /tmp/helm-output/all.yaml
kubectl kustomize /tmp/helm-output/
```

```yaml
# post-render-overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - all.yaml                              # the Helm-rendered output

commonAnnotations:
  deploy-method: "helm-post-render"

patches:
  - target:
      kind: Deployment
    patch: |
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: any
      spec:
        template:
          metadata:
            annotations:
              sidecar.istio.io/inject: "true"
```

Use post-rendering during helm install:

```bash
helm install my-nginx bitnami/nginx \
  --namespace helm-kustomize-lab \
  --post-renderer ./post-render.sh
```

### 3. Practical Example: Community Chart with Organization Patches

```yaml
# org-nginx/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: web-team

helmCharts:
  - name: nginx
    repo: https://charts.bitnami.com/bitnami
    version: "15.14.0"
    releaseName: web-nginx
    valuesInline:
      replicaCount: 3
      service:
        type: ClusterIP

# Organization-standard patches
patches:
  # Add PodDisruptionBudget
  - target:
      kind: Deployment
      name: web-nginx
    patch: |
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: web-nginx
      spec:
        template:
          spec:
            securityContext:
              runAsNonRoot: true
              seccompProfile:
                type: RuntimeDefault

# Additional resources not in the Helm chart
resources:
  - pdb.yaml
```

```yaml
# org-nginx/pdb.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web-nginx-pdb
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: nginx
      app.kubernetes.io/instance: web-nginx
```

### 4. Build and Compare

```bash
# Pure Helm output
helm template web-nginx bitnami/nginx --set replicaCount=3 > /tmp/pure-helm.yaml

# Kustomize + Helm output
kubectl kustomize org-nginx/ --enable-helm > /tmp/kustomize-helm.yaml

# See what Kustomize added
diff /tmp/pure-helm.yaml /tmp/kustomize-helm.yaml
```

## Verify

```bash
# 1. Render and check custom annotation
kubectl kustomize helm-kustomize/ --enable-helm | grep "custom-annotation"

# 2. Confirm common labels were applied
kubectl kustomize helm-kustomize/ --enable-helm | grep "managed-by: kustomize-helm"

# 3. Render org overlay and confirm PDB exists
kubectl kustomize org-nginx/ --enable-helm | grep "PodDisruptionBudget"

# 4. Confirm security context was injected
kubectl kustomize org-nginx/ --enable-helm | grep "runAsNonRoot"

# 5. Apply to cluster
kubectl create namespace helm-kustomize-lab
kubectl kustomize helm-kustomize/ --enable-helm | kubectl apply -f -
kubectl get all -n helm-kustomize-lab
```

## Cleanup

```bash
kubectl delete namespace helm-kustomize-lab
rm -rf helm-kustomize org-nginx post-render-overlay
```

## References

- [Kustomize helmCharts](https://kubectl.docs.kubernetes.io/references/kustomize/builtins/#_helmchartinflationgenerator_)
- [Helm Post-Rendering](https://helm.sh/docs/topics/advanced/#post-rendering)
- [Kustomize Built-in Generators](https://kubectl.docs.kubernetes.io/references/kustomize/builtins/)
