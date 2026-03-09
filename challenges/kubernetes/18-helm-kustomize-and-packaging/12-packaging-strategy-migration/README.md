<!--
difficulty: insane
concepts: [helm-to-kustomize-migration, kustomize-to-helm-migration, packaging-strategy, hybrid-approach, migration-validation]
tools: [helm, kubectl, kustomize, diff]
estimated_time: 60m
bloom_level: create
prerequisites: [18-helm-kustomize-and-packaging/02-helm-values-and-templates, 18-helm-kustomize-and-packaging/05-kustomize-overlays, 18-helm-kustomize-and-packaging/10-kustomize-helm-integration]
-->

# 18.12 - Migration from Helm to Kustomize (or Vice Versa)

## Scenario

Your team inherited a production Helm chart for a three-tier application (frontend, backend API, worker) that runs across dev, staging, and prod environments. The chart has grown to 1200 lines of Go templates, has nested conditionals five levels deep, and new engineers cannot safely modify it. Management has decided to evaluate whether migrating to Kustomize would reduce complexity, or whether a hybrid approach (Kustomize wrapping Helm) is more practical.

Your task is to perform a zero-drift migration: produce identical rendered manifests from the new packaging approach as from the old one, then demonstrate the benefits of each approach.

## Constraints

1. Start with a Helm chart containing a Deployment, Service, ConfigMap, Secret, Ingress, HPA, and PDB for each tier (frontend, backend, worker) -- at least 7 resource types across 3 tiers
2. Render the Helm chart for all three environments (dev, staging, prod) and store the output as the **baseline**
3. Create a Kustomize base+overlay structure that produces byte-identical YAML for all three environments (ignoring label ordering and whitespace)
4. The Kustomize structure must use at least: `configMapGenerator`, `secretGenerator`, `replicas`, `images`, `patches`, and `namePrefix`
5. Create a validation script that renders both Helm and Kustomize for each environment and reports any semantic differences (using `kubectl diff --server-side` dry-run or a YAML-aware diff tool like `dyff`)
6. Document the trade-offs: file count, line count, learning curve, and extensibility for each approach
7. As a bonus, create a hybrid approach using Kustomize's `helmCharts` generator wrapping the original Helm chart, and compare its output to the other two

## Success Criteria

1. `helm template` for dev/staging/prod produces the baseline YAML (21+ resources across 3 environments)
2. `kubectl kustomize overlays/dev` produces semantically identical output to the Helm dev baseline
3. `kubectl kustomize overlays/staging` and `overlays/prod` likewise match their Helm baselines
4. The validation script exits 0 when all environments match, non-zero when any differ
5. A written comparison table shows file count, total lines, and complexity assessment for Helm vs Kustomize vs hybrid
6. The hybrid approach using `helmCharts` generator also matches the baseline

## Verification Commands

```bash
# Generate baselines
helm template app ./helm-chart -f values-dev.yaml > /tmp/baseline-dev.yaml
helm template app ./helm-chart -f values-staging.yaml > /tmp/baseline-staging.yaml
helm template app ./helm-chart -f values-prod.yaml > /tmp/baseline-prod.yaml

# Generate Kustomize output
kubectl kustomize kustomize/overlays/dev > /tmp/kustomize-dev.yaml
kubectl kustomize kustomize/overlays/staging > /tmp/kustomize-staging.yaml
kubectl kustomize kustomize/overlays/prod > /tmp/kustomize-prod.yaml

# Semantic diff (install dyff: brew install homeport/tap/dyff)
dyff between /tmp/baseline-dev.yaml /tmp/kustomize-dev.yaml
dyff between /tmp/baseline-staging.yaml /tmp/kustomize-staging.yaml
dyff between /tmp/baseline-prod.yaml /tmp/kustomize-prod.yaml

# Validate all at once
./validate-migration.sh && echo "MIGRATION VERIFIED" || echo "DRIFT DETECTED"

# Hybrid approach
kubectl kustomize hybrid/ --enable-helm > /tmp/hybrid-dev.yaml
dyff between /tmp/baseline-dev.yaml /tmp/hybrid-dev.yaml

# Compare complexity
wc -l helm-chart/templates/*.yaml
find kustomize/ -name "*.yaml" | xargs wc -l
```

## Cleanup

```bash
rm -rf helm-chart kustomize hybrid /tmp/baseline-*.yaml /tmp/kustomize-*.yaml
```
