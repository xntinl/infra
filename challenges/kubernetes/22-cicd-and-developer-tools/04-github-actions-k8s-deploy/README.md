# GitHub Actions for Kubernetes Deployment

<!--
difficulty: intermediate
concepts: [github-actions, ci-cd, kubernetes-deployment, container-registry, kubeconfig, kustomize, environments]
tools: [github-actions, kubectl, docker, kustomize]
estimated_time: 30m
bloom_level: apply
prerequisites: [kubectl-basics, docker-basics, deployments]
-->

## Overview

GitHub Actions is a popular CI/CD platform integrated directly into GitHub repositories. This exercise covers building a workflow that builds a container image, pushes it to a registry, and deploys it to a Kubernetes cluster. You will handle secrets securely, use environments for staging/production separation, and implement deployment strategies.

## Why This Matters

GitHub Actions is the most common CI/CD tool for teams using GitHub. Knowing how to wire it to Kubernetes deployments lets you automate the full path from code commit to production deployment, with proper approvals and rollback capabilities.

## Step-by-Step Instructions

### Step 1 -- Basic Build and Push Workflow

```yaml
# .github/workflows/build-and-deploy.yaml
name: Build and Deploy to Kubernetes

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write        # needed for pushing to GHCR

    outputs:
      image-tag: ${{ steps.meta.outputs.tags }}
      image-digest: ${{ steps.build.outputs.digest }}

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Container Registry
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata for Docker
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=sha,prefix=
            type=ref,event=branch

      - name: Build and push Docker image
        id: build
        uses: docker/build-push-action@v5
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

### Step 2 -- Deploy to Staging

```yaml
  deploy-staging:
    needs: build
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    environment:
      name: staging                    # GitHub environment with protection rules
      url: https://staging.example.com

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up kubectl
        uses: azure/setup-kubectl@v3
        with:
          version: 'v1.30.0'

      - name: Configure kubeconfig
        run: |
          mkdir -p $HOME/.kube
          echo "${{ secrets.KUBE_CONFIG_STAGING }}" | base64 -d > $HOME/.kube/config

      - name: Deploy with Kustomize
        run: |
          cd k8s/overlays/staging
          kustomize edit set image app-image=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${{ needs.build.outputs.image-digest }}
          kustomize build . | kubectl apply -f -

      - name: Verify deployment
        run: |
          kubectl rollout status deployment/myapp -n staging --timeout=300s

      - name: Run smoke tests
        run: |
          kubectl run smoke-test --image=busybox:1.37 --restart=Never \
            --command -- wget -qO- --timeout=10 http://myapp.staging.svc/health
          kubectl logs smoke-test
          kubectl delete pod smoke-test
```

### Step 3 -- Deploy to Production with Approval

```yaml
  deploy-production:
    needs: [build, deploy-staging]
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    environment:
      name: production                 # requires manual approval in GitHub settings
      url: https://app.example.com

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up kubectl
        uses: azure/setup-kubectl@v3
        with:
          version: 'v1.30.0'

      - name: Configure kubeconfig
        run: |
          mkdir -p $HOME/.kube
          echo "${{ secrets.KUBE_CONFIG_PRODUCTION }}" | base64 -d > $HOME/.kube/config

      - name: Deploy with Kustomize
        run: |
          cd k8s/overlays/production
          kustomize edit set image app-image=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${{ needs.build.outputs.image-digest }}
          kustomize build . | kubectl apply -f -

      - name: Verify deployment
        run: |
          kubectl rollout status deployment/myapp -n production --timeout=300s

      - name: Rollback on failure
        if: failure()
        run: |
          echo "Deployment failed, rolling back..."
          kubectl rollout undo deployment/myapp -n production
          kubectl rollout status deployment/myapp -n production --timeout=120s
```

### Step 4 -- Kustomize Directory Structure

```
k8s/
├── base/
│   ├── kustomization.yaml
│   ├── deployment.yaml
│   ├── service.yaml
│   └── hpa.yaml
├── overlays/
│   ├── staging/
│   │   ├── kustomization.yaml     # patches for staging
│   │   └── replicas-patch.yaml
│   └── production/
│       ├── kustomization.yaml     # patches for production
│       └── replicas-patch.yaml
```

```yaml
# k8s/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
  - service.yaml

# k8s/overlays/staging/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: staging
resources:
  - ../../base
images:
  - name: app-image
    newName: ghcr.io/myorg/myapp
    newTag: latest
patches:
  - path: replicas-patch.yaml
```

### Step 5 -- Configure GitHub Secrets

In your GitHub repository settings, configure these secrets:

| Secret | Description |
|--------|-------------|
| `KUBE_CONFIG_STAGING` | Base64-encoded kubeconfig for staging cluster |
| `KUBE_CONFIG_PRODUCTION` | Base64-encoded kubeconfig for production cluster |

```bash
# Generate the base64-encoded kubeconfig
cat ~/.kube/config | base64 -w 0
# Add this as a GitHub Actions secret
```

Configure environment protection rules:
- **staging**: No restrictions (deploys automatically)
- **production**: Require manual approval from designated reviewers

## Spot the Bug

This workflow has a security issue. Can you find it?

```yaml
- name: Deploy
  run: |
    echo "${{ secrets.KUBE_CONFIG }}" > kubeconfig.yaml
    kubectl --kubeconfig=kubeconfig.yaml apply -f k8s/
```

<details>
<summary>Answer</summary>

The kubeconfig is written to a plaintext file that could be cached or leaked via `set -x` debugging. Always decode secrets to a protected location and clean up afterward, or better, use the `$HOME/.kube/config` approach with base64 decoding, which is not logged and lives in the runner's home directory.
</details>

## Verify

```bash
# In GitHub, check the Actions tab for workflow run status

# Verify the deployment in the cluster
kubectl get deployment myapp -n staging
kubectl get deployment myapp -n production

# Check the image tag matches the commit SHA
kubectl get deployment myapp -n staging \
  -o jsonpath='{.spec.template.spec.containers[0].image}'
```

## Cleanup

```bash
kubectl delete deployment myapp -n staging --ignore-not-found
kubectl delete deployment myapp -n production --ignore-not-found
```

## Reference

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [docker/build-push-action](https://github.com/docker/build-push-action)
- [GitHub Environments](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment)
