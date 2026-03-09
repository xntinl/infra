<!--
difficulty: intermediate
concepts: [kustomize-components, replacements, vars-replacement, cross-resource-references, reusable-kustomize-modules]
tools: [kubectl, kustomize]
estimated_time: 35m
bloom_level: apply
prerequisites: [18-helm-kustomize-and-packaging/05-kustomize-overlays]
-->

# 18.07 - Kustomize Components and Replacements

## What You Will Build

A Kustomize project that uses **components** for opt-in features (monitoring sidecar, external secret injection) and **replacements** to propagate values across resources -- for example, injecting a ConfigMap-defined hostname into an Ingress host field and a Deployment env var from the same source.

## Step-by-Step Guide

### 1. Directory Structure

```bash
mkdir -p kustomize-advanced/base
mkdir -p kustomize-advanced/components/{monitoring,external-secrets}
mkdir -p kustomize-advanced/overlays/{dev,prod}
```

### 2. Base Resources

```yaml
# kustomize-advanced/base/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
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
          env:
            - name: APP_HOST
              value: PLACEHOLDER          # will be replaced
```

```yaml
# kustomize-advanced/base/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: webapp
spec:
  selector:
    app: webapp
  ports:
    - port: 80
      targetPort: 80
```

```yaml
# kustomize-advanced/base/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: webapp
spec:
  rules:
    - host: PLACEHOLDER                   # will be replaced
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: webapp
                port:
                  number: 80
```

```yaml
# kustomize-advanced/base/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: webapp-config
data:
  APP_HOST: "webapp.dev.example.com"
```

```yaml
# kustomize-advanced/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml
  - ingress.yaml
  - configmap.yaml

# Replacements propagate values across resources
replacements:
  - source:
      kind: ConfigMap
      name: webapp-config
      fieldPath: data.APP_HOST
    targets:
      - select:
          kind: Ingress
          name: webapp
        fieldPaths:
          - spec.rules.0.host
      - select:
          kind: Deployment
          name: webapp
        fieldPaths:
          - spec.template.spec.containers.[name=webapp].env.[name=APP_HOST].value
```

### 3. Monitoring Component (Opt-In Sidecar)

```yaml
# kustomize-advanced/components/monitoring/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1alpha1   # components use alpha API
kind: Component

patches:
  - target:
      kind: Deployment
      name: webapp
    patch: |
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: webapp
      spec:
        template:
          metadata:
            annotations:
              prometheus.io/scrape: "true"
              prometheus.io/port: "9113"
          spec:
            containers:
              - name: nginx-exporter
                image: nginx/nginx-prometheus-exporter:1.1
                args:
                  - "-nginx.scrape-uri=http://localhost/stub_status"
                ports:
                  - containerPort: 9113
                    name: metrics
                resources:
                  requests:
                    cpu: 10m
                    memory: 16Mi
                  limits:
                    cpu: 50m
                    memory: 32Mi
```

### 4. External Secrets Component

```yaml
# kustomize-advanced/components/external-secrets/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1alpha1
kind: Component

resources:
  - external-secret.yaml
```

```yaml
# kustomize-advanced/components/external-secrets/external-secret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: webapp-secrets
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: webapp-secrets
    creationPolicy: Owner
  data:
    - secretKey: DB_PASSWORD
      remoteRef:
        key: /webapp/db-password
```

### 5. Dev Overlay -- Uses Monitoring Component

```yaml
# kustomize-advanced/overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: dev

resources:
  - ../../base

components:
  - ../../components/monitoring            # opt-in to prometheus sidecar

patches:
  - target:
      kind: ConfigMap
      name: webapp-config
    patch: |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: webapp-config
      data:
        APP_HOST: "webapp.dev.example.com"
```

### 6. Prod Overlay -- Uses Both Components

```yaml
# kustomize-advanced/overlays/prod/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: prod

resources:
  - ../../base

components:
  - ../../components/monitoring
  - ../../components/external-secrets      # prod also pulls secrets from external store

patches:
  - target:
      kind: ConfigMap
      name: webapp-config
    patch: |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: webapp-config
      data:
        APP_HOST: "webapp.prod.example.com"

replicas:
  - name: webapp
    count: 3
```

### 7. Build and Validate Replacements

```bash
# Dev -- should show dev hostname in Ingress and Deployment env
kubectl kustomize kustomize-advanced/overlays/dev

# Prod -- should show prod hostname, monitoring sidecar, and ExternalSecret
kubectl kustomize kustomize-advanced/overlays/prod
```

## Verify

```bash
# 1. Confirm replacements propagated the hostname to Ingress
kubectl kustomize kustomize-advanced/overlays/dev | grep "host: webapp.dev"

# 2. Confirm replacements propagated the hostname to Deployment env
kubectl kustomize kustomize-advanced/overlays/dev | grep "webapp.dev.example.com"

# 3. Confirm monitoring sidecar is present in both overlays
kubectl kustomize kustomize-advanced/overlays/dev | grep "nginx-exporter"
kubectl kustomize kustomize-advanced/overlays/prod | grep "nginx-exporter"

# 4. Confirm ExternalSecret only in prod
kubectl kustomize kustomize-advanced/overlays/dev | grep "ExternalSecret" || echo "Not in dev -- correct"
kubectl kustomize kustomize-advanced/overlays/prod | grep "ExternalSecret"

# 5. Confirm prod hostname is different from dev
kubectl kustomize kustomize-advanced/overlays/prod | grep "host: webapp.prod"
```

## Cleanup

```bash
# No cluster resources were created -- just remove local files
rm -rf kustomize-advanced
```

## What's Next

Next you will learn how to write and run automated tests for Helm charts: [18.08 - Helm Chart Testing with ct and helm test](../08-helm-testing/).

## References

- [Kustomize Components](https://kubectl.docs.kubernetes.io/guides/config_management/components/)
- [Kustomize Replacements](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/replacements/)
- [Kustomize Vars to Replacements Migration](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/replacements/)
