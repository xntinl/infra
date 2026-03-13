<!--
difficulty: insane
concepts: [multi-environment, kustomize-overlays, config-hierarchy, secret-rotation, environment-promotion, gitops-config]
tools: [kubectl, kustomize, helm, sops]
estimated_time: 120m
bloom_level: create
prerequisites: [configmap-volume-updates, immutable-configmaps-and-secrets, external-secrets-operator, sealed-secrets]
-->

# 8.14 Multi-Environment Configuration Management Platform

## The Scenario

Your organization runs the same application across four environments: `dev`, `staging`, `production-us`, and `production-eu`. Each environment has different configuration values, secrets, resource limits, and feature flags. You must design a configuration management system that:

- Eliminates configuration drift between environments
- Makes it impossible to accidentally deploy production secrets to dev
- Supports promoting configuration changes from dev through staging to production
- Handles region-specific overrides (database endpoints, CDN URLs)
- Encrypts all secrets in Git

The application stack consists of:
- A web frontend (needs API_URL, CDN_URL, feature flags)
- A backend API (needs database credentials, Redis URL, rate limits, log level)
- A worker service (needs queue URL, batch size, retry config)

## Constraints

1. Use Kustomize overlays with a clear base/overlay hierarchy
2. All secrets must be encrypted in Git using SOPS or Sealed Secrets
3. Environment-specific values must be defined in exactly one place -- no duplication
4. A configuration change must be promotable: dev -> staging -> production with a single PR per promotion
5. Production-us and production-eu share a base production config but override region-specific values
6. Resource limits (CPU, memory) must scale per environment (dev: small, staging: medium, production: large)
7. Feature flags must be independently toggleable per environment
8. Database credentials must be sourced from an ExternalSecret (AWS Secrets Manager or Vault) in production, but from a local Secret in dev/staging
9. The system must generate immutable ConfigMaps (hash-suffixed) so that any config change triggers a rolling update
10. A validation step must prevent applying a config that is missing required keys

## Success Criteria

1. Directory structure follows: `base/`, `overlays/dev/`, `overlays/staging/`, `overlays/production-us/`, `overlays/production-eu/`
2. `kustomize build overlays/dev` produces valid manifests with dev-specific values
3. `kustomize build overlays/production-us` produces manifests with production values and US-region overrides
4. ConfigMap names include a content hash suffix (e.g., `app-config-8g2h5k`)
5. Secrets in `overlays/production-*` are encrypted with SOPS; `overlays/dev` uses plain Secrets
6. A diff between `kustomize build overlays/staging` and `kustomize build overlays/production-us` shows only the expected environment-specific differences
7. Adding a new configuration key requires touching at most 2 files (base + environment override)
8. Adding a new environment requires creating one overlay directory with only the environment-specific values

## Verification Commands

```bash
# Build and diff environments
kustomize build overlays/dev > /tmp/dev.yaml
kustomize build overlays/staging > /tmp/staging.yaml
kustomize build overlays/production-us > /tmp/prod-us.yaml
kustomize build overlays/production-eu > /tmp/prod-eu.yaml

# Verify ConfigMap hash suffixes
grep "name: app-config-" /tmp/dev.yaml
grep "name: app-config-" /tmp/prod-us.yaml
# Names should differ (different values = different hash)

# Verify environment-specific values
grep "APP_ENV" /tmp/dev.yaml        # should show "development"
grep "APP_ENV" /tmp/prod-us.yaml    # should show "production"

# Verify region-specific overrides
grep "DB_HOST" /tmp/prod-us.yaml    # should show us-east-1 endpoint
grep "DB_HOST" /tmp/prod-eu.yaml    # should show eu-west-1 endpoint

# Verify secrets are encrypted in production overlays
cat overlays/production-us/secrets.enc.yaml | grep "ENC\["

# Verify secrets are plain in dev
cat overlays/dev/secrets.yaml | grep -v "ENC\["

# Apply to dev and verify
kustomize build overlays/dev | kubectl apply -f -
kubectl get configmap -l app=myapp
kubectl get secret -l app=myapp

# Verify resource limits differ
kubectl get deploy -o jsonpath='{.items[0].spec.template.spec.containers[0].resources}' 2>/dev/null

# Diff staging vs production
diff <(kustomize build overlays/staging) <(kustomize build overlays/production-us)
```

## Cleanup

```bash
kustomize build overlays/dev | kubectl delete -f -
```
