<!--
difficulty: intermediate
concepts: [helm-hooks, pre-install, post-install, pre-upgrade, hook-weight, hook-delete-policy]
tools: [helm, kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [18-helm-kustomize-and-packaging/02-helm-values-and-templates]
-->

# 18.06 - Helm Hooks: Pre/Post Install and Upgrade

## What You Will Build

A Helm chart that uses hooks to run a database migration Job before each upgrade and a notification Job after a successful install. You will control hook execution order with `hook-weight`, manage cleanup with `hook-delete-policy`, and observe the hook lifecycle using `helm install --debug`.

## Step-by-Step Guide

### 1. Scaffold the Chart

```bash
helm create hookdemo
rm -rf hookdemo/templates/tests hookdemo/templates/hpa.yaml hookdemo/templates/ingress.yaml
```

### 2. values.yaml

```yaml
# hookdemo/values.yaml
replicaCount: 2

image:
  repository: nginx
  tag: "1.27"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80

hooks:
  dbMigration:
    enabled: true
    image: busybox:1.37
  notification:
    enabled: true
    image: busybox:1.37
```

### 3. Pre-Upgrade Hook -- Database Migration Job

```yaml
# hookdemo/templates/hook-db-migrate.yaml
{{- if .Values.hooks.dbMigration.enabled }}
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hookdemo.fullname" . }}-db-migrate
  labels:
    {{- include "hookdemo.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade        # run before install and upgrade
    "helm.sh/hook-weight": "-5"                     # lower weight runs first
    "helm.sh/hook-delete-policy": before-hook-creation-delete   # delete previous Job before creating new one
spec:
  backoffLimit: 1
  template:
    metadata:
      labels:
        app: db-migrate
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: {{ .Values.hooks.dbMigration.image }}
          command:
            - /bin/sh
            - -c
            - |
              echo "Running database migrations..."
              echo "Migration 001: Create users table"
              echo "Migration 002: Add index on email"
              sleep 3
              echo "Migrations complete."
{{- end }}
```

### 4. Post-Install Hook -- Notification Job

```yaml
# hookdemo/templates/hook-notify.yaml
{{- if .Values.hooks.notification.enabled }}
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hookdemo.fullname" . }}-notify
  labels:
    {{- include "hookdemo.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": post-install,post-upgrade       # run after install/upgrade
    "helm.sh/hook-weight": "5"                       # runs after weight 0
    "helm.sh/hook-delete-policy": hook-succeeded      # auto-delete on success
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app: notify
    spec:
      restartPolicy: Never
      containers:
        - name: notify
          image: {{ .Values.hooks.notification.image }}
          command:
            - /bin/sh
            - -c
            - |
              echo "Sending deployment notification..."
              echo "Release: {{ .Release.Name }}"
              echo "Revision: {{ .Release.Revision }}"
              echo "Namespace: {{ .Release.Namespace }}"
              echo "Notification sent."
{{- end }}
```

### 5. Pre-Upgrade Hook -- Config Validation

```yaml
# hookdemo/templates/hook-validate.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hookdemo.fullname" . }}-validate
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-10"                     # runs before db-migrate (-5)
    "helm.sh/hook-delete-policy": before-hook-creation-delete
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: validate
          image: busybox:1.37
          command:
            - /bin/sh
            - -c
            - |
              echo "Validating configuration..."
              echo "Checking required env vars..."
              echo "Validation passed."
```

### 6. Install and Observe

```bash
kubectl create namespace hooks-lab

helm install hookdemo ./hookdemo \
  --namespace hooks-lab \
  --debug 2>&1 | grep -E "hook|Hook"

# Watch jobs execute
kubectl get jobs -n hooks-lab -w
```

### 7. Trigger an Upgrade to See Pre/Post-Upgrade Hooks

```bash
helm upgrade hookdemo ./hookdemo \
  --namespace hooks-lab \
  --set replicaCount=3

kubectl get jobs -n hooks-lab
kubectl logs job/hookdemo-db-migrate -n hooks-lab
```

## Hook Execution Order

| Weight | Hook | Phase |
|--------|------|-------|
| -10 | validate | pre-install / pre-upgrade |
| -5 | db-migrate | pre-install / pre-upgrade |
| 5 | notify | post-install / post-upgrade |

Hooks with the same weight execute in alphabetical order by resource name. Helm waits for each hook to complete (Job succeeds) before proceeding to the next weight.

## Verify

```bash
# 1. Confirm hooks ran in order
kubectl get jobs -n hooks-lab --sort-by=.metadata.creationTimestamp

# 2. Check migration logs
kubectl logs job/hookdemo-db-migrate -n hooks-lab

# 3. Confirm post-install hook was cleaned up (hook-succeeded policy)
kubectl get job hookdemo-notify -n hooks-lab 2>/dev/null || echo "Notify job was auto-deleted (hook-succeeded policy)"

# 4. Confirm the main deployment is running
kubectl get deployment -n hooks-lab
kubectl get pods -n hooks-lab -l app.kubernetes.io/instance=hookdemo
```

## Cleanup

```bash
helm uninstall hookdemo --namespace hooks-lab
kubectl delete namespace hooks-lab
```

## What's Next

Next you will explore Kustomize components and the replacements transformer for advanced reuse patterns: [18.07 - Kustomize Components and Replacements](../07-kustomize-components-and-replacements/07-kustomize-components-and-replacements.md).

## References

- [Helm Hooks](https://helm.sh/docs/topics/charts_hooks/)
- [Hook Delete Policies](https://helm.sh/docs/topics/charts_hooks/#hook-deletion-policies)
