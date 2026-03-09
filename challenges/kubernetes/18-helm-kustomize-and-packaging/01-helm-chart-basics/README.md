<!--
difficulty: basic
concepts: [helm-chart-structure, helm-install, helm-upgrade, helm-rollback, release-management, chart-repositories]
tools: [helm, kubectl]
estimated_time: 35m
bloom_level: understand
prerequisites: [deployments, services]
-->

# 18.01 - Helm Chart Basics

## Why This Matters

Deploying a single application to Kubernetes might involve a Deployment, Service, ConfigMap, ServiceAccount, and more. Maintaining these files by hand across multiple environments becomes error-prone fast. **Helm** is the package manager for Kubernetes -- it bundles related manifests into a single versioned unit called a **chart**, tracks each deployment as a **release**, and supports rollback when things go wrong.

## What You Will Learn

- How a Helm chart is structured (Chart.yaml, values.yaml, templates/)
- How to scaffold a chart with `helm create`
- How to install, upgrade, rollback, and uninstall releases
- How to work with chart repositories

## Step-by-Step Guide

### 1. Create a New Chart

```bash
helm create myapp
```

This generates the following structure:

```
myapp/
  Chart.yaml           # chart metadata (name, version, description)
  values.yaml          # default configuration values
  charts/              # dependency charts go here
  templates/           # Go template manifests
    deployment.yaml
    service.yaml
    serviceaccount.yaml
    hpa.yaml
    ingress.yaml
    NOTES.txt          # post-install instructions shown to user
    _helpers.tpl        # reusable named templates
    tests/
      test-connection.yaml
```

### 2. Examine and Customize Chart.yaml

```yaml
# myapp/Chart.yaml
apiVersion: v2                            # Helm 3 uses apiVersion v2
name: myapp
description: A sample application to learn Helm basics
type: application                         # application (default) or library
version: 0.1.0                            # chart version (SemVer)
appVersion: "1.0.0"                       # version of the app being deployed
maintainers:
  - name: devteam
    email: dev@example.com
```

### 3. Customize values.yaml

```yaml
# myapp/values.yaml
replicaCount: 2

image:
  repository: nginx
  pullPolicy: IfNotPresent
  tag: "1.27"                             # pin to a specific image version

service:
  type: ClusterIP
  port: 80

resources:
  limits:
    cpu: 100m
    memory: 128Mi
  requests:
    cpu: 50m
    memory: 64Mi

autoscaling:
  enabled: false

ingress:
  enabled: false
```

### 4. Validate Before Installing

```bash
# Render templates locally without contacting the cluster
helm template myapp ./myapp

# Validate against the cluster API (dry-run)
helm install myapp-test ./myapp --dry-run --debug
```

### 5. Install the Chart

```bash
kubectl create namespace helm-lab

helm install myapp ./myapp \
  --namespace helm-lab \
  --set replicaCount=3 \
  --set service.type=ClusterIP
```

### 6. Upgrade the Release

```bash
helm upgrade myapp ./myapp \
  --namespace helm-lab \
  --set replicaCount=4 \
  --set image.tag="1.27"
```

### 7. View Revision History and Rollback

```bash
# See all revisions
helm history myapp --namespace helm-lab

# Roll back to revision 1
helm rollback myapp 1 --namespace helm-lab
```

### 8. Work with Chart Repositories

```bash
# Add a repository
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

# Search for charts
helm search repo bitnami/nginx

# Inspect a chart's configurable values
helm show values bitnami/nginx

# Install a chart from a repository
helm install my-nginx bitnami/nginx \
  --namespace helm-lab \
  --set service.type=ClusterIP
```

### 9. Package and Lint

```bash
helm lint ./myapp          # check for issues
helm package ./myapp       # creates myapp-0.1.0.tgz
```

## Common Mistakes

1. **Forgetting `--namespace` on upgrade** -- Helm treats the same chart name in different namespaces as separate releases. Always pass `--namespace` consistently.
2. **Not pinning image tags** -- Using `latest` in values.yaml makes rollbacks unreliable because the image content changes without a chart version bump.
3. **Confusing `version` and `appVersion`** -- `version` is the chart package version; `appVersion` is the version of the software inside the container. They are independent.
4. **Skipping `helm lint`** -- Template syntax errors only surface at install time if you skip linting. Always lint before packaging.

## Verify

```bash
# 1. Lint the chart
helm lint ./myapp

# 2. List releases
helm list --namespace helm-lab

# 3. Check release status
helm status myapp --namespace helm-lab

# 4. Verify deployed resources
kubectl get all -n helm-lab -l app.kubernetes.io/instance=myapp

# 5. Inspect applied values
helm get values myapp --namespace helm-lab
helm get values myapp --namespace helm-lab --all

# 6. View rendered manifests stored in the release
helm get manifest myapp --namespace helm-lab
```

## Cleanup

```bash
helm uninstall myapp --namespace helm-lab
helm uninstall my-nginx --namespace helm-lab
kubectl delete namespace helm-lab
```

## What's Next

In the next exercise you will dive into Helm's Go template language -- named templates, control flow, functions, and per-environment value overrides: [18.02 - Helm Values and Templates](../02-helm-values-and-templates/).

## Summary

- A Helm chart is a directory containing `Chart.yaml`, `values.yaml`, and a `templates/` folder with Go-templated Kubernetes manifests
- `helm create` scaffolds a production-ready chart structure
- Each `helm install` creates a named release; `helm upgrade` creates a new revision of that release
- `helm rollback` restores a previous revision, giving you a reliable undo mechanism
- Chart repositories let you share and discover community-maintained charts
- Always lint and dry-run before installing to catch errors early

## References

- [Helm Charts](https://helm.sh/docs/topics/charts/)
- [Using Helm](https://helm.sh/docs/intro/using_helm/)

## Additional Resources

- [Helm Getting Started Guide](https://helm.sh/docs/chart_template_guide/getting_started/)
- [Helm Commands Reference](https://helm.sh/docs/helm/helm/)
- [Artifact Hub](https://artifacthub.io/)
