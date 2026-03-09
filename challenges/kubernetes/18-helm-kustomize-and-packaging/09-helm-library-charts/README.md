<!--
difficulty: advanced
concepts: [helm-library-chart, shared-templates, chart-type-library, template-reuse, organizational-standards]
tools: [helm, kubectl]
estimated_time: 40m
bloom_level: analyze
prerequisites: [18-helm-kustomize-and-packaging/02-helm-values-and-templates, 18-helm-kustomize-and-packaging/04-helm-subcharts-and-dependencies]
-->

# 18.09 - Helm Library Charts for Shared Templates

## Architecture

```
Library Chart Pattern
======================

  library-common/                    consumer-api/
  ├── Chart.yaml (type: library)     ├── Chart.yaml
  └── templates/                     │   dependencies:
      └── _helpers.tpl               │     - name: library-common
          ├── common.labels           ├── templates/
          ├── common.deployment       │   └── deployment.yaml
          ├── common.service          │       {{ include "library-common.deployment" . }}
          └── common.configmap        └── values.yaml

  consumer-worker/
  ├── Chart.yaml
  │   dependencies:
  │     - name: library-common
  ├── templates/
  │   └── deployment.yaml
  │       {{ include "library-common.deployment" . }}
  └── values.yaml
```

A **library chart** (`type: library`) contains only named templates -- it never renders resources on its own. Consumer charts declare it as a dependency and call `{{ include "library-common.deployment" . }}` to generate standardized resources. This ensures consistent labels, security contexts, and annotations across all microservices in an organization.

## Suggested Steps

### 1. Create the Library Chart

```yaml
# library-common/Chart.yaml
apiVersion: v2
name: library-common
description: Shared templates for all microservices
type: library                              # cannot be installed directly
version: 0.1.0
```

### 2. Define Shared Template Helpers

```yaml
# library-common/templates/_helpers.tpl
{{/*
Standard labels applied to all resources.
*/}}
{{- define "library-common.labels" -}}
app.kubernetes.io/name: {{ .Values.appName | default .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- if .Values.team }}
team: {{ .Values.team }}
{{- end }}
{{- end }}

{{/*
Selector labels -- must be immutable.
*/}}
{{- define "library-common.selectorLabels" -}}
app.kubernetes.io/name: {{ .Values.appName | default .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Standard Deployment template. Consumer charts call:
  {{ include "library-common.deployment" . }}
*/}}
{{- define "library-common.deployment" -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}-{{ .Values.appName | default .Chart.Name }}
  labels:
    {{- include "library-common.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount | default 1 }}
  selector:
    matchLabels:
      {{- include "library-common.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "library-common.selectorLabels" . | nindent 8 }}
    spec:
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: {{ .Values.appName | default .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy | default "IfNotPresent" }}
          ports:
            {{- range .Values.ports }}
            - containerPort: {{ .containerPort }}
              name: {{ .name }}
              protocol: {{ .protocol | default "TCP" }}
            {{- end }}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
{{- end }}

{{/*
Standard Service template.
*/}}
{{- define "library-common.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}-{{ .Values.appName | default .Chart.Name }}
  labels:
    {{- include "library-common.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type | default "ClusterIP" }}
  selector:
    {{- include "library-common.selectorLabels" . | nindent 4 }}
  ports:
    {{- range .Values.ports }}
    - port: {{ .servicePort | default .containerPort }}
      targetPort: {{ .containerPort }}
      name: {{ .name }}
    {{- end }}
{{- end }}
```

### 3. Create a Consumer Chart

```yaml
# consumer-api/Chart.yaml
apiVersion: v2
name: consumer-api
description: API service using library-common templates
type: application
version: 0.1.0
appVersion: "1.0.0"

dependencies:
  - name: library-common
    version: "0.1.0"
    repository: "file://../library-common"   # local path for development
```

```yaml
# consumer-api/values.yaml
appName: api
team: platform

image:
  repository: nginx
  tag: "1.27"
  pullPolicy: IfNotPresent

replicaCount: 2

ports:
  - name: http
    containerPort: 80
    servicePort: 80

service:
  type: ClusterIP

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 200m
    memory: 256Mi
```

### 4. Consumer Templates That Call the Library

```yaml
# consumer-api/templates/deployment.yaml
{{ include "library-common.deployment" . }}
```

```yaml
# consumer-api/templates/service.yaml
{{ include "library-common.service" . }}
```

### 5. Build and Validate

```bash
# Update dependencies (downloads library-common)
cd consumer-api && helm dependency update && cd ..

# Render to verify output
helm template myapi ./consumer-api

# Lint
helm lint ./consumer-api
```

### 6. Create a Second Consumer to Prove Reuse

```yaml
# consumer-worker/Chart.yaml
apiVersion: v2
name: consumer-worker
description: Background worker using library-common templates
type: application
version: 0.1.0
appVersion: "1.0.0"

dependencies:
  - name: library-common
    version: "0.1.0"
    repository: "file://../library-common"
```

```yaml
# consumer-worker/values.yaml
appName: worker
team: data

image:
  repository: busybox
  tag: "1.37"

replicaCount: 1

ports:
  - name: metrics
    containerPort: 9090

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 100m
    memory: 128Mi
```

Both consumer charts produce compliant resources with identical label structure, security contexts, and resource definitions.

## Verify

```bash
# 1. Render both consumers and confirm consistent labels
helm template myapi ./consumer-api | grep "team:"
helm template myworker ./consumer-worker | grep "team:"

# 2. Confirm security context is present in both
helm template myapi ./consumer-api | grep "runAsNonRoot"
helm template myworker ./consumer-worker | grep "runAsNonRoot"

# 3. Install both to verify runtime behavior
kubectl create namespace library-lab
helm install myapi ./consumer-api -n library-lab
helm install myworker ./consumer-worker -n library-lab
kubectl get all -n library-lab
```

## Cleanup

```bash
helm uninstall myapi -n library-lab
helm uninstall myworker -n library-lab
kubectl delete namespace library-lab
```

## References

- [Library Charts](https://helm.sh/docs/topics/library_charts/)
- [Named Templates](https://helm.sh/docs/chart_template_guide/named_templates/)
- [Chart Dependencies](https://helm.sh/docs/helm/helm_dependency/)
