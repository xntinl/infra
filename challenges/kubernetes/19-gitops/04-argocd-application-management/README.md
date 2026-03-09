<!--
difficulty: intermediate
concepts: [argocd-health-checks, resource-tracking, ignore-differences, custom-health-lua, sync-options]
tools: [kubectl, argocd-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [19-gitops/01-argocd-gitops-deployment, 19-gitops/02-argocd-sync-policies]
-->

# 19.04 - ArgoCD Application Management and Health Checks

## What You Will Build

An ArgoCD setup that demonstrates advanced application management: custom health checks using Lua scripts, `ignoreDifferences` to avoid false OutOfSync on fields managed by controllers (like HPA modifying replica counts), resource tracking methods, and fine-grained sync options per resource.

## Step-by-Step Guide

### 1. Application with ignoreDifferences

When an HPA manages replicas, ArgoCD shows the Deployment as OutOfSync because `spec.replicas` differs from Git. Use `ignoreDifferences` to solve this.

```yaml
# app-with-hpa.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-with-hpa
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: hpa-demo
  ignoreDifferences:
    - group: apps
      kind: Deployment
      jsonPointers:
        - /spec/replicas              # ignore replica count managed by HPA
    - group: ""
      kind: Service
      jqPathExpressions:
        - .spec.clusterIP             # ignore cluster-assigned IP
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - RespectIgnoreDifferences=true  # self-heal respects ignoreDifferences
```

### 2. Per-Resource Sync Options

```yaml
# app-resource-options.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: resource-options-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: resource-opts
  syncPolicy:
    automated:
      prune: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true           # use server-side apply for all resources
      - ApplyOutOfSyncOnly=true        # only apply resources that are out of sync
```

### 3. Custom Health Check with Lua

ArgoCD uses Lua scripts to assess health. You can add custom health checks in the argocd-cm ConfigMap.

```yaml
# argocd-cm-health.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  # Custom health check for a CronJob -- consider healthy if last schedule succeeded
  resource.customizations.health.batch_CronJob: |
    hs = {}
    if obj.status ~= nil then
      if obj.status.lastScheduleTime ~= nil then
        hs.status = "Healthy"
        hs.message = "Last scheduled: " .. obj.status.lastScheduleTime
      else
        hs.status = "Progressing"
        hs.message = "Waiting for first schedule"
      end
    else
      hs.status = "Progressing"
      hs.message = "No status available"
    end
    return hs

  # Ignore differences globally for all MutatingWebhookConfigurations
  resource.customizations.ignoreDifferences.admissionregistration.k8s.io_MutatingWebhookConfiguration: |
    jqPathExpressions:
      - '.webhooks[]?.clientConfig.caBundle'
```

### 4. Resource Tracking Method

```yaml
# argocd-cm-tracking.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  application.resourceTrackingMethod: annotation   # annotation (default), label, or annotation+label
```

Options:
- `label` -- adds `app.kubernetes.io/instance` label (legacy, can conflict with Helm)
- `annotation` -- uses `argocd.argoproj.io/tracking-id` annotation (recommended)
- `annotation+label` -- uses both

### 5. Application with Detailed Health Checks

```yaml
# app-health-demo.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: health-demo
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: health-demo
  syncPolicy:
    automated:
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
  # Override resource actions
  info:
    - name: Documentation
      value: "https://wiki.example.com/guestbook"
    - name: Owner
      value: "platform-team"
```

### 6. Inspect Health and Sync Detail

```bash
# Detailed application status
argocd app get health-demo --show-operation

# Resource-level health
argocd app resources health-demo

# Diff what would change on sync
argocd app diff health-demo

# View application logs
argocd app logs health-demo --container guestbook-ui
```

## Spot the Bug

This Application is always showing OutOfSync even though the manifests match Git:

```yaml
spec:
  syncPolicy:
    automated:
      selfHeal: true
  ignoreDifferences:
    - group: apps
      kind: Deployment
      jsonPointers:
        - /spec/replicas
```

<details>
<summary>Answer</summary>

The `ignoreDifferences` is correctly configured, but `RespectIgnoreDifferences=true` is missing from `syncOptions`. Without it, self-heal will try to sync the ignored field, causing a perpetual sync loop.

Add:
```yaml
syncOptions:
  - RespectIgnoreDifferences=true
```

</details>

## Verify

```bash
# 1. Check that ignoreDifferences prevents OutOfSync
kubectl apply -f app-with-hpa.yaml
sleep 30
argocd app get app-with-hpa | grep "Sync Status"

# 2. Manually change replicas and confirm ArgoCD ignores it
kubectl scale deployment guestbook-ui -n hpa-demo --replicas=5
sleep 20
argocd app get app-with-hpa | grep "Sync Status"  # should still be Synced

# 3. Check resource health per resource
argocd app resources health-demo

# 4. Verify custom health check is loaded
kubectl get cm argocd-cm -n argocd -o yaml | grep "batch_CronJob"
```

## Cleanup

```bash
kubectl delete application app-with-hpa resource-options-app health-demo -n argocd
kubectl delete namespace hpa-demo resource-opts health-demo --ignore-not-found
```

## What's Next

Next you will implement the App of Apps pattern for managing multiple applications from a single root: [19.05 - ArgoCD: App of Apps Pattern](../05-argocd-app-of-apps/).

## References

- [ArgoCD Sync Options](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/)
- [ArgoCD Resource Health](https://argo-cd.readthedocs.io/en/stable/operator-manual/health/)
- [ArgoCD Diffing Customization](https://argo-cd.readthedocs.io/en/stable/user-guide/diffing/)
