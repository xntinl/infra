<!--
difficulty: basic
concepts: [go-templates, helpers-tpl, control-flow, template-functions, values-override, named-templates]
tools: [helm, kubectl]
estimated_time: 40m
bloom_level: understand
prerequisites: [18-helm-kustomize-and-packaging/01-helm-chart-basics]
-->

# 18.02 - Helm Values and Templates

## Why This Matters

A Helm chart becomes truly powerful when you use its template engine to produce different Kubernetes manifests from the same source. A single chart can serve dev (1 replica, debug logging) and prod (3 replicas, HPA, Ingress, TLS) by swapping a values file. Understanding Go templates, named helpers, and the values override hierarchy is essential for writing maintainable charts.

## What You Will Learn

- How Go template syntax works inside Helm (`{{ .Values.x }}`, `{{ .Release.Name }}`)
- How to write reusable named templates in `_helpers.tpl`
- How control flow (`if/else`, `range`, `with`) conditionally generates resources
- How template functions (`default`, `quote`, `toYaml`, `nindent`) transform output
- How values override precedence works: values.yaml < `-f custom.yaml` < `--set`

## Step-by-Step Guide

### 1. Create the Chart Structure

```bash
mkdir -p webapp/templates
```

### 2. Chart.yaml

```yaml
# webapp/Chart.yaml
apiVersion: v2
name: webapp
description: Chart for learning Helm templates and values
type: application
version: 0.2.0
appVersion: "2.0.0"
```

### 3. Default values.yaml

```yaml
# webapp/values.yaml
environment: dev

replicaCount: 1

image:
  repository: nginx
  tag: "1.27"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: false
  minReplicas: 1
  maxReplicas: 5
  targetCPUUtilizationPercentage: 80

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts: []
  tls: []

configMap:
  enabled: true
  data:
    APP_ENV: "development"
    LOG_LEVEL: "debug"

secrets:
  enabled: false
  data: {}

podAnnotations: {}
extraLabels: {}
tolerations: []
nodeSelector: {}
```

### 4. Named Templates in _helpers.tpl

```yaml
# webapp/templates/_helpers.tpl
{{/*
Fully qualified app name, truncated to 63 chars.
*/}}
{{- define "webapp.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name.
*/}}
{{- define "webapp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "webapp.labels" -}}
helm.sh/chart: {{ include "webapp.chart" . }}
{{ include "webapp.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
environment: {{ .Values.environment }}
{{- if .Values.extraLabels }}
{{ toYaml .Values.extraLabels }}
{{- end }}
{{- end }}

{{/*
Selector labels -- must be stable across upgrades.
*/}}
{{- define "webapp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "webapp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Chart label with version.
*/}}
{{- define "webapp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}
```

### 5. Deployment Template

```yaml
# webapp/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "webapp.fullname" . }}
  labels:
    {{- include "webapp.labels" . | nindent 4 }}
spec:
  {{- if not .Values.autoscaling.enabled }}
  replicas: {{ .Values.replicaCount }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "webapp.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      {{- with .Values.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      labels:
        {{- include "webapp.labels" . | nindent 8 }}
    spec:
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: 80
              protocol: TCP
          {{- if .Values.configMap.enabled }}
          envFrom:
            - configMapRef:
                name: {{ include "webapp.fullname" . }}
          {{- end }}
          {{- if .Values.secrets.enabled }}
            - secretRef:
                name: {{ include "webapp.fullname" . }}
          {{- end }}
          livenessProbe:
            httpGet:
              path: /
              port: http
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /
              port: http
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
```

### 6. Service Template

```yaml
# webapp/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "webapp.fullname" . }}
  labels:
    {{- include "webapp.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:
    {{- include "webapp.selectorLabels" . | nindent 4 }}
```

### 7. Conditional ConfigMap

```yaml
# webapp/templates/configmap.yaml
{{- if .Values.configMap.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "webapp.fullname" . }}
  labels:
    {{- include "webapp.labels" . | nindent 4 }}
data:
  {{- range $key, $value := .Values.configMap.data }}
  {{ $key }}: {{ $value | quote }}
  {{- end }}
{{- end }}
```

### 8. Conditional HPA

```yaml
# webapp/templates/hpa.yaml
{{- if .Values.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "webapp.fullname" . }}
  labels:
    {{- include "webapp.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "webapp.fullname" . }}
  minReplicas: {{ .Values.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.targetCPUUtilizationPercentage }}
{{- end }}
```

### 9. Per-Environment Values Files

```yaml
# webapp/values-dev.yaml
environment: dev
replicaCount: 1

resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 100m
    memory: 128Mi

autoscaling:
  enabled: false

configMap:
  enabled: true
  data:
    APP_ENV: "development"
    LOG_LEVEL: "debug"
    FEATURE_FLAGS: "experimental=true"
```

```yaml
# webapp/values-prod.yaml
environment: prod
replicaCount: 3

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 500m
    memory: 512Mi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

configMap:
  enabled: true
  data:
    APP_ENV: "production"
    LOG_LEVEL: "warn"
    FEATURE_FLAGS: "experimental=false"

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: webapp.example.com
      paths:
        - path: /
          pathType: Prefix

podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "80"

extraLabels:
  team: platform
  cost-center: engineering
```

### 10. Render and Compare

```bash
# Render for dev
helm template myapp ./webapp -f ./webapp/values-dev.yaml > /tmp/rendered-dev.yaml

# Render for prod
helm template myapp ./webapp -f ./webapp/values-prod.yaml > /tmp/rendered-prod.yaml

# See the differences
diff /tmp/rendered-dev.yaml /tmp/rendered-prod.yaml
```

## Common Mistakes

1. **Using `{{ .Values.x }}` without a default** -- If a value is missing, the template renders an empty string. Use `{{ .Values.x | default "fallback" }}` for required values.
2. **Wrong indentation with `nindent`** -- `nindent 4` adds a newline then 4 spaces. Using `indent` without the `n` prefix does not add a leading newline, which breaks YAML alignment.
3. **Selector labels that change on upgrade** -- Selector labels in a Deployment are immutable after creation. Never include mutable values (like `appVersion`) in `webapp.selectorLabels`.
4. **Forgetting the dash in `{{-`** -- Without the dash, Go templates leave whitespace. Use `{{-` and `-}}` to trim surrounding whitespace.

## Verify

```bash
# 1. Lint with both value files
helm lint ./webapp -f ./webapp/values-dev.yaml
helm lint ./webapp -f ./webapp/values-prod.yaml

# 2. Confirm conditionals work
echo "--- DEV: should NOT have HPA ---"
helm template myapp ./webapp -f ./webapp/values-dev.yaml | grep "kind: HorizontalPodAutoscaler" || echo "Correct: no HPA"

echo "--- PROD: should have HPA and Ingress ---"
helm template myapp ./webapp -f ./webapp/values-prod.yaml | grep -E "kind: (HorizontalPodAutoscaler|Ingress)"

# 3. Install with dev values
kubectl create namespace helm-templates-lab
helm install webapp-dev ./webapp --namespace helm-templates-lab -f ./webapp/values-dev.yaml

# 4. Check deployed resources
kubectl get all -n helm-templates-lab
helm get values webapp-dev -n helm-templates-lab --all
```

## Cleanup

```bash
helm uninstall webapp-dev --namespace helm-templates-lab
kubectl delete namespace helm-templates-lab
```

## What's Next

In the next exercise you will learn Kustomize -- a template-free approach to customizing Kubernetes manifests using base directories and overlays: [18.03 - Kustomize Basics: Base and Overlays](../03-kustomize-basics/03-kustomize-basics.md).

## Summary

- Go templates in Helm use `{{ .Values.x }}`, `{{ .Release.Name }}`, and `{{ .Chart.Name }}` to inject dynamic values
- `_helpers.tpl` holds named templates (`define`/`include`) for labels, names, and other reusable fragments
- Control flow (`if`, `range`, `with`) conditionally generates entire resources like HPA, Ingress, or Secrets
- Functions like `default`, `quote`, `toYaml`, and `nindent` transform and format output
- Multiple `-f` flags and `--set` overrides follow a last-wins precedence hierarchy
- Always render and diff templates for each environment before deploying

## References

- [Helm Chart Template Guide](https://helm.sh/docs/chart_template_guide/)
- [Helm Template Functions](https://helm.sh/docs/chart_template_guide/function_list/)

## Additional Resources

- [Named Templates](https://helm.sh/docs/chart_template_guide/named_templates/)
- [Flow Control](https://helm.sh/docs/chart_template_guide/control_structures/)
- [Helm Best Practices](https://helm.sh/docs/chart_best_practices/)
- [Go Template Documentation](https://pkg.go.dev/text/template)
