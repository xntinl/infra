<!--
difficulty: intermediate
concepts: [kustomize-overlays, configmap-generator, secret-generator, image-transformer, strategic-merge-patch, json-patch]
tools: [kubectl, kustomize]
estimated_time: 35m
bloom_level: apply
prerequisites: [18-helm-kustomize-and-packaging/03-kustomize-basics]
-->

# 18.05 - Kustomize Overlays for Multi-Environment

## What You Will Build

A complete Kustomize structure with a base application and three overlays (dev, staging, prod) that demonstrate advanced overlay techniques: `configMapGenerator` and `secretGenerator` with hash suffixes, `images` transformers, `replicas` overrides, both strategic merge patches and JSON patches, and `commonAnnotations`.

## Step-by-Step Guide

### 1. Create the Directory Structure

```bash
mkdir -p kustomize-multi/base
mkdir -p kustomize-multi/overlays/{dev,staging,prod}
```

### 2. Base Resources

```yaml
# kustomize-multi/base/deployment.yaml
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
          image: nginx:1.27
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
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 5
```

```yaml
# kustomize-multi/base/service.yaml
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
      protocol: TCP
  type: ClusterIP
```

```yaml
# kustomize-multi/base/configmap.yaml
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

```yaml
# kustomize-multi/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml

commonLabels:
  managed-by: kustomize
```

### 3. Dev Overlay

```yaml
# kustomize-multi/overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: dev
namePrefix: dev-

resources:
  - ../../base

commonLabels:
  environment: dev

patches:
  - path: patch-deployment.yaml
    target:
      kind: Deployment
      name: webapp

configMapGenerator:
  - name: webapp-config
    behavior: merge
    literals:
      - APP_ENV=development
      - LOG_LEVEL=debug
      - DEBUG_MODE=true
```

```yaml
# kustomize-multi/overlays/dev/patch-deployment.yaml
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

### 4. Staging Overlay -- Uses Image Transformer

```yaml
# kustomize-multi/overlays/staging/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: staging
namePrefix: staging-

resources:
  - ../../base

commonLabels:
  environment: staging

images:
  - name: nginx                           # match by original image name
    newTag: "1.27"                         # pin to specific tag

replicas:
  - name: webapp
    count: 2

configMapGenerator:
  - name: webapp-config
    behavior: merge
    literals:
      - APP_ENV=staging
      - LOG_LEVEL=info

secretGenerator:
  - name: webapp-secrets
    literals:
      - API_KEY=staging-api-key-value
    type: Opaque
```

### 5. Prod Overlay -- Full Configuration

```yaml
# kustomize-multi/overlays/prod/kustomization.yaml
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
  cost-center: engineering

patches:
  # Strategic merge patch for Deployment
  - path: patch-deployment.yaml
    target:
      kind: Deployment
      name: webapp
  # JSON Patch to change Service type
  - target:
      kind: Service
      name: webapp
    patch: |
      - op: add
        path: /metadata/annotations
        value:
          service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
      - op: replace
        path: /spec/type
        value: LoadBalancer

configMapGenerator:
  - name: webapp-config
    behavior: merge
    literals:
      - APP_ENV=production
      - LOG_LEVEL=warn
      - DEBUG_MODE=false

replicas:
  - name: webapp
    count: 3
```

```yaml
# kustomize-multi/overlays/prod/patch-deployment.yaml
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
          env:
            - name: GOMAXPROCS
              value: "2"
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: webapp
```

### 6. Build and Compare All Three

```bash
kubectl kustomize kustomize-multi/overlays/dev > /tmp/k-dev.yaml
kubectl kustomize kustomize-multi/overlays/staging > /tmp/k-staging.yaml
kubectl kustomize kustomize-multi/overlays/prod > /tmp/k-prod.yaml

diff /tmp/k-dev.yaml /tmp/k-staging.yaml
diff /tmp/k-staging.yaml /tmp/k-prod.yaml
```

## TODO

The staging overlay is missing a topologySpreadConstraint that distributes pods across zones (not hostnames). Add a `patch-deployment.yaml` to the staging overlay that adds a `topologySpreadConstraint` using `topology.kubernetes.io/zone` as the topology key.

<details>
<summary>Solution</summary>

```yaml
# kustomize-multi/overlays/staging/patch-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
spec:
  template:
    spec:
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: webapp
```

Then add the patch to the staging kustomization.yaml:

```yaml
patches:
  - path: patch-deployment.yaml
    target:
      kind: Deployment
      name: webapp
```

</details>

## Verify

```bash
# 1. Confirm namePrefix and namespace on each overlay
for env in dev staging prod; do
  echo "--- $env ---"
  kubectl kustomize kustomize-multi/overlays/$env | grep -E "^  name:|^  namespace:"
done

# 2. Confirm ConfigMap hash suffix
kubectl kustomize kustomize-multi/overlays/dev | grep "webapp-config"

# 3. Confirm prod Service type is LoadBalancer
kubectl kustomize kustomize-multi/overlays/prod | grep "type: LoadBalancer"

# 4. Apply dev and prod
kubectl create namespace dev
kubectl create namespace prod
kubectl apply -k kustomize-multi/overlays/dev
kubectl apply -k kustomize-multi/overlays/prod
kubectl get all -n dev
kubectl get all -n prod
```

## Cleanup

```bash
kubectl delete -k kustomize-multi/overlays/dev
kubectl delete -k kustomize-multi/overlays/prod
kubectl delete namespace dev prod staging
```

## What's Next

Next you will learn Helm hooks for running jobs at specific points in the release lifecycle: [18.06 - Helm Hooks: Pre/Post Install and Upgrade](../06-helm-hooks/).

## References

- [Kustomize Reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/)
- [Kustomize Patches](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/)
- [Kustomize configMapGenerator](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/configmapgenerator/)
