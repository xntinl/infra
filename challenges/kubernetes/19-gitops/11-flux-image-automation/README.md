<!--
difficulty: advanced
concepts: [flux-image-automation, image-reflector, image-update-automation, semver-policy, git-commit-automation]
tools: [flux, kubectl]
estimated_time: 40m
bloom_level: analyze
prerequisites: [19-gitops/03-flux-basics, 19-gitops/07-flux-helmrelease]
-->

# 19.11 - Flux Image Automation: Auto-Update Images in Git

## Architecture

```
Flux Image Automation Pipeline
================================

  Container Registry
  ┌──────────────┐
  │ nginx:1.27.0 │
  │ nginx:1.27.1 │ ◄── CI pushes new tag
  │ nginx:1.27.2 │
  └──────┬───────┘
         │
         ▼
  Image Reflector Controller
  ┌──────────────────────┐
  │ ImageRepository      │  Scans registry for new tags
  │ ImagePolicy          │  Selects latest tag matching policy
  └──────────┬───────────┘
             │ "1.27.2 is newest"
             ▼
  Image Automation Controller
  ┌──────────────────────┐
  │ ImageUpdateAutomation│  Commits updated tag to Git
  │                      │  (patches YAML in-place)
  └──────────┬───────────┘
             │ git push
             ▼
  Git Repository
  ┌──────────────────────┐
  │ deployment.yaml      │
  │   image: nginx:1.27.2│ ◄── auto-committed
  └──────────┬───────────┘
             │
             ▼
  Kustomize Controller     ──► Applies to cluster
```

Flux's image automation controllers close the loop between CI (which builds and pushes images) and CD (which deploys them). The **image-reflector-controller** scans container registries for new tags. The **image-automation-controller** commits updated image tags back to Git, which Flux then deploys -- maintaining the GitOps principle that Git is always the source of truth.

## Suggested Steps

### 1. Install Image Automation Controllers

```bash
flux install --components-extra=image-reflector-controller,image-automation-controller
```

### 2. Create an ImageRepository

```yaml
# image-repo.yaml
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  image: ghcr.io/stefanprodan/podinfo
  interval: 5m                             # scan for new tags every 5 minutes
  exclusionList:
    - "^.*\\.sig$"                         # exclude cosign signature tags
```

### 3. Create an ImagePolicy

```yaml
# image-policy.yaml
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImagePolicy
metadata:
  name: podinfo
  namespace: flux-system
spec:
  imageRepositoryRef:
    name: podinfo
  policy:
    semver:
      range: ">=6.0.0 <7.0.0"             # accept any 6.x.x version
```

Alternative policies:

```yaml
# Alphabetical (for date-based tags like 20240101-abc123)
spec:
  policy:
    alphabetical:
      order: asc

# Numerical
spec:
  policy:
    numerical:
      order: asc
```

### 4. Mark Deployment for Auto-Update

Add a marker comment in the YAML that tells the image-automation-controller which field to update:

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: podinfo
  namespace: podinfo
spec:
  selector:
    matchLabels:
      app: podinfo
  template:
    spec:
      containers:
        - name: podinfo
          image: ghcr.io/stefanprodan/podinfo:6.0.0  # {"$imagepolicy": "flux-system:podinfo"}
          ports:
            - containerPort: 9898
```

The `# {"$imagepolicy": "flux-system:podinfo"}` marker tells the controller to replace the image tag on this line with the tag selected by the `podinfo` ImagePolicy.

### 5. Create an ImageUpdateAutomation

```yaml
# image-update-automation.yaml
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageUpdateAutomation
metadata:
  name: podinfo
  namespace: flux-system
spec:
  interval: 30m
  sourceRef:
    kind: GitRepository
    name: flux-system                      # the bootstrap GitRepository
  git:
    checkout:
      ref:
        branch: main
    commit:
      author:
        name: flux-bot
        email: flux-bot@example.com
      messageTemplate: |
        Automated image update

        Automation: {{ .AutomationObject }}

        Files changed:
        {{ range $filename, $_ := .Changed.FileChanges -}}
        - {{ $filename }}
        {{ end -}}

        Objects changed:
        {{ range $resource, $changes := .Changed.Objects -}}
        - {{ $resource.Kind }}/{{ $resource.Name }} ({{ $resource.Namespace }})
        {{ range $path, $change := $changes -}}
          - {{ $path }}: {{ $change.Old }} -> {{ $change.New }}
        {{ end -}}
        {{ end -}}
    push:
      branch: main
  update:
    path: ./clusters/production            # directory to scan for markers
    strategy: Setters                      # use marker-based replacement
```

### 6. Apply and Monitor

```bash
kubectl apply -f image-repo.yaml
kubectl apply -f image-policy.yaml
kubectl apply -f image-update-automation.yaml

# Check scanned tags
flux get image repository podinfo
kubectl get imagerepository podinfo -n flux-system -o yaml | grep -A5 lastScanResult

# Check selected tag
flux get image policy podinfo

# Check automation status
flux get image update podinfo
```

## Verify

```bash
# 1. ImageRepository found tags
flux get image repository podinfo

# 2. ImagePolicy selected the correct latest tag
flux get image policy podinfo

# 3. ImageUpdateAutomation committed to Git
flux get image update podinfo
kubectl describe imageupdateautomation podinfo -n flux-system

# 4. Verify the deployment image was updated
kubectl get deployment podinfo -n podinfo -o jsonpath='{.spec.template.spec.containers[0].image}' && echo ""

# 5. Force scan and reconciliation
flux reconcile image repository podinfo
flux reconcile image update podinfo
```

## Cleanup

```bash
kubectl delete imageupdateautomation podinfo -n flux-system
kubectl delete imagepolicy podinfo -n flux-system
kubectl delete imagerepository podinfo -n flux-system
kubectl delete namespace podinfo --ignore-not-found
```

## References

- [Flux Image Update Guide](https://fluxcd.io/flux/guides/image-update/)
- [Image Reflector Controller](https://fluxcd.io/flux/components/image/imagerepositories/)
- [Image Automation Controller](https://fluxcd.io/flux/components/image/imageupdateautomations/)
