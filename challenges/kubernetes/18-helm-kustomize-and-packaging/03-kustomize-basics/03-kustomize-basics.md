<!--
difficulty: basic
concepts: [kustomize, kustomization-yaml, base-and-overlays, strategic-merge-patch, kubectl-apply-k]
tools: [kubectl, kustomize]
estimated_time: 30m
bloom_level: understand
prerequisites: [deployments, services, configmaps]
-->

# 18.03 - Kustomize Basics: Base and Overlays

## Why This Matters

Not every team wants a template engine. **Kustomize** is built into `kubectl` and lets you customize plain YAML manifests without introducing template syntax. You write standard Kubernetes YAML as your **base**, then layer environment-specific changes on top using **overlays** -- no curly braces, no `values.yaml`, just declarative patches on real manifests.

## What You Will Learn

- How Kustomize organizes resources into base and overlay directories
- How `kustomization.yaml` declares resources, patches, and transformations
- How `namePrefix`, `namespace`, and `commonLabels` transform all resources at once
- How strategic merge patches modify specific fields without rewriting entire manifests
- How to build and apply with `kubectl kustomize` and `kubectl apply -k`

## Step-by-Step Guide

### 1. Create the Directory Structure

```bash
mkdir -p kustomize-demo/base
mkdir -p kustomize-demo/overlays/dev
mkdir -p kustomize-demo/overlays/prod
```

### 2. Base Deployment

```yaml
# kustomize-demo/base/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
  labels:
    app: webapp
spec:
  replicas: 1
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
          image: nginx:1.27                # base image version
          ports:
            - containerPort: 80
              name: http
          envFrom:
            - configMapRef:
                name: webapp-config
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
          livenessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 10
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
```

### 3. Base Service

```yaml
# kustomize-demo/base/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: webapp
  labels:
    app: webapp
spec:
  selector:
    app: webapp
  ports:
    - name: http
      port: 80
      targetPort: 80
  type: ClusterIP
```

### 4. Base ConfigMap

```yaml
# kustomize-demo/base/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: webapp-config
  labels:
    app: webapp
data:
  APP_NAME: "webapp"
  LOG_FORMAT: "json"
```

### 5. Base kustomization.yaml

```yaml
# kustomize-demo/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml

commonLabels:
  managed-by: kustomize
```

### 6. Dev Overlay

```yaml
# kustomize-demo/overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: dev                            # set namespace on all resources
namePrefix: dev-                          # prefix all resource names

resources:
  - ../../base                            # reference the base directory

commonLabels:
  environment: dev

patches:
  - path: patch-deployment.yaml
    target:
      kind: Deployment
      name: webapp

configMapGenerator:                       # merge additional keys into the base ConfigMap
  - name: webapp-config
    behavior: merge
    literals:
      - APP_ENV=development
      - LOG_LEVEL=debug
```

```yaml
# kustomize-demo/overlays/dev/patch-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: webapp
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
```

### 7. Prod Overlay

```yaml
# kustomize-demo/overlays/prod/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: prod
namePrefix: prod-

resources:
  - ../../base

commonLabels:
  environment: prod

commonAnnotations:
  team: platform

patches:
  # Strategic merge patch for Deployment
  - path: patch-deployment.yaml
    target:
      kind: Deployment
      name: webapp
  # Inline JSON Patch for Service
  - target:
      kind: Service
      name: webapp
    patch: |
      - op: replace
        path: /spec/type
        value: LoadBalancer

configMapGenerator:
  - name: webapp-config
    behavior: merge
    literals:
      - APP_ENV=production
      - LOG_LEVEL=warn

replicas:
  - name: webapp
    count: 3
```

```yaml
# kustomize-demo/overlays/prod/patch-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
spec:
  template:
    spec:
      containers:
        - name: webapp
          resources:
            requests:
              cpu: 200m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
```

### 8. Build and Compare

```bash
# Render dev overlay
kubectl kustomize kustomize-demo/overlays/dev > /tmp/kustomize-dev.yaml

# Render prod overlay
kubectl kustomize kustomize-demo/overlays/prod > /tmp/kustomize-prod.yaml

# Compare the two
diff /tmp/kustomize-dev.yaml /tmp/kustomize-prod.yaml
```

## Common Mistakes

1. **Forgetting to list resources in kustomization.yaml** -- Kustomize only processes files explicitly listed under `resources`. Dropping a YAML file into the directory is not enough.
2. **Using `commonLabels` with immutable selectors** -- `commonLabels` injects labels into `spec.selector.matchLabels`. If you add labels after initial deployment, the selector becomes immutable and the update fails.
3. **Wrong relative path in `resources`** -- Overlay references like `../../base` are relative to the kustomization.yaml location, not your working directory.
4. **Mixing `configMapGenerator` names with raw ConfigMaps** -- When using `configMapGenerator`, Kustomize appends a hash suffix to the name. References in Deployments are updated automatically, but only if the ConfigMap was also declared through Kustomize.

## Verify

```bash
# 1. Confirm name prefixes are applied
kubectl kustomize kustomize-demo/overlays/dev | grep "name:"
kubectl kustomize kustomize-demo/overlays/prod | grep "name:"

# 2. Confirm labels propagate
kubectl kustomize kustomize-demo/overlays/prod | grep "environment:"

# 3. Apply dev overlay
kubectl create namespace dev
kubectl apply -k kustomize-demo/overlays/dev
kubectl get all -n dev

# 4. Apply prod overlay
kubectl create namespace prod
kubectl apply -k kustomize-demo/overlays/prod
kubectl get all -n prod

# 5. Compare replica counts
kubectl get deployment -n dev -o jsonpath='{.items[0].spec.replicas}' && echo ""
kubectl get deployment -n prod -o jsonpath='{.items[0].spec.replicas}' && echo ""
```

## Cleanup

```bash
kubectl delete -k kustomize-demo/overlays/dev
kubectl delete -k kustomize-demo/overlays/prod
kubectl delete namespace dev prod
```

## What's Next

In the next exercise you will learn how Helm manages complex dependency trees using subcharts: [18.04 - Helm Subcharts and Dependencies](../04-helm-subcharts-and-dependencies/04-helm-subcharts-and-dependencies.md).

## Summary

- Kustomize customizes plain YAML without template syntax -- it is built into `kubectl`
- A **base** directory holds shared resources; **overlays** layer environment-specific changes on top
- `kustomization.yaml` declares which resources to include and which patches to apply
- `namePrefix`, `namespace`, and `commonLabels` apply transformations across all resources at once
- Strategic merge patches modify specific fields; JSON patches use explicit add/remove/replace operations
- `kubectl apply -k <dir>` renders and applies in one step

## References

- [Managing Kubernetes Objects with Kustomize](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/)
- [Kustomize Reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/)

## Additional Resources

- [Kustomize GitHub](https://github.com/kubernetes-sigs/kustomize)
- [Kustomize Built-in Transformers](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/)
- [Kustomize Patches](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/)
