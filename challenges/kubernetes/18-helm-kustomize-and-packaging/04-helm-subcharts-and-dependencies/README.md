<!--
difficulty: intermediate
concepts: [helm-subcharts, chart-dependencies, condition-tags, global-values, dependency-resolution]
tools: [helm, kubectl]
estimated_time: 35m
bloom_level: apply
prerequisites: [18-helm-kustomize-and-packaging/01-helm-chart-basics, 18-helm-kustomize-and-packaging/02-helm-values-and-templates]
-->

# 18.04 - Helm Subcharts and Dependencies

## What You Will Build

A parent chart for a web application that depends on two subcharts: a Redis cache and a PostgreSQL database. You will declare dependencies in `Chart.yaml`, control which subcharts are enabled through `condition` and `tags`, pass configuration to subcharts via `values.yaml`, and use `global` values shared across all charts.

## Step-by-Step Guide

### 1. Scaffold the Parent Chart

```bash
helm create webapp-stack
rm -rf webapp-stack/templates/tests
```

### 2. Declare Dependencies

```yaml
# webapp-stack/Chart.yaml
apiVersion: v2
name: webapp-stack
description: Web application with Redis and PostgreSQL dependencies
type: application
version: 0.1.0
appVersion: "1.0.0"

dependencies:
  - name: redis
    version: "18.x.x"                     # match any 18.x patch
    repository: "https://charts.bitnami.com/bitnami"
    condition: redis.enabled               # toggle via values
    tags:
      - backend-cache
  - name: postgresql
    version: "14.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: postgresql.enabled
    tags:
      - backend-database
```

### 3. Configure Subchart Values from the Parent

```yaml
# webapp-stack/values.yaml
replicaCount: 2

image:
  repository: nginx
  tag: "1.27"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80

# Global values -- accessible to all subcharts via .Values.global
global:
  storageClass: "standard"
  environment: "dev"

# Redis subchart values -- key matches dependency name
redis:
  enabled: true
  architecture: standalone                 # standalone or replication
  auth:
    enabled: false
  master:
    persistence:
      enabled: false                       # disable persistence for dev

# PostgreSQL subchart values
postgresql:
  enabled: true
  auth:
    postgresPassword: "devpassword"
    database: "webapp"
  primary:
    persistence:
      enabled: false
```

### 4. Download Dependencies

```bash
cd webapp-stack
helm dependency update
ls charts/    # should show redis-*.tgz and postgresql-*.tgz
```

### 5. Reference Subchart Services in the Parent Deployment

```yaml
# webapp-stack/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "webapp-stack.fullname" . }}
  labels:
    {{- include "webapp-stack.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "webapp-stack.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "webapp-stack.selectorLabels" . | nindent 8 }}
    spec:
      containers:
        - name: webapp
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          ports:
            - containerPort: 80
          env:
            {{- if .Values.redis.enabled }}
            - name: REDIS_HOST
              value: "{{ .Release.Name }}-redis-master"
            - name: REDIS_PORT
              value: "6379"
            {{- end }}
            {{- if .Values.postgresql.enabled }}
            - name: DB_HOST
              value: "{{ .Release.Name }}-postgresql"
            - name: DB_PORT
              value: "5432"
            - name: DB_NAME
              value: {{ .Values.postgresql.auth.database | quote }}
            {{- end }}
            - name: ENVIRONMENT
              value: {{ .Values.global.environment | quote }}
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 200m
              memory: 256Mi
```

### 6. Create a Production Override that Disables PostgreSQL

```yaml
# webapp-stack/values-prod.yaml
global:
  environment: "production"

redis:
  enabled: true
  architecture: replication
  auth:
    enabled: true
    password: "prod-redis-secret"
  master:
    persistence:
      enabled: true
      size: 8Gi

postgresql:
  enabled: false       # use an external managed database in prod
```

### 7. Install and Test

```bash
kubectl create namespace subchart-lab

# Install with dev values (both subcharts enabled)
helm install webapp-stack ./webapp-stack --namespace subchart-lab

# Check what was deployed
kubectl get pods -n subchart-lab
kubectl get svc -n subchart-lab
```

## Spot the Bug

The following `Chart.yaml` has a subtle issue that will cause `helm dependency update` to fail. Can you spot it?

```yaml
dependencies:
  - name: redis
    version: "18.0.0"
    repository: "bitnami"
    condition: redis.enabled
```

<details>
<summary>Answer</summary>

The `repository` field must be a full URL (`https://charts.bitnami.com/bitnami`) or a locally added repo alias prefixed with `@` (e.g., `@bitnami`). The plain string `"bitnami"` is neither a valid URL nor a valid alias.

</details>

## Verify

```bash
# 1. Confirm dependency chart archives exist
ls webapp-stack/charts/

# 2. List all releases
helm list -n subchart-lab

# 3. Check that subchart resources are running
kubectl get pods -n subchart-lab -l app.kubernetes.io/name=redis
kubectl get pods -n subchart-lab -l app.kubernetes.io/name=postgresql

# 4. Verify env vars in the parent deployment
kubectl exec -n subchart-lab deploy/webapp-stack -- env | grep -E "REDIS_|DB_"

# 5. Render with prod values and confirm postgresql is absent
helm template webapp-stack ./webapp-stack -f webapp-stack/values-prod.yaml | grep "kind: " | sort | uniq
```

## Cleanup

```bash
helm uninstall webapp-stack --namespace subchart-lab
kubectl delete namespace subchart-lab
```

## What's Next

Next you will revisit Kustomize with deeper multi-environment overlay patterns including configMapGenerator, secretGenerator, and image transformers: [18.05 - Kustomize Overlays for Multi-Environment](../05-kustomize-overlays/).

## References

- [Helm Chart Dependencies](https://helm.sh/docs/helm/helm_dependency/)
- [Subcharts and Global Values](https://helm.sh/docs/chart_template_guide/subcharts_and_globals/)
- [Helm Dependency Update](https://helm.sh/docs/helm/helm_dependency_update/)
