<!--
difficulty: insane
concepts: [helm-chart-repository, oci-registry, chartmuseum, github-pages, chart-versioning, ci-release-pipeline]
tools: [helm, docker, gh, chartmuseum]
estimated_time: 60m
bloom_level: create
prerequisites: [18-helm-kustomize-and-packaging/08-helm-testing, 18-helm-kustomize-and-packaging/09-helm-library-charts]
-->

# 18.11 - Helm Chart Repository and Release Platform

## Scenario

Your organization has 15 microservices, each packaged as a Helm chart. Today, charts are copied between repositories and versioned inconsistently. Your task is to build a centralized chart repository platform that supports:

- An OCI-based registry for chart storage
- A traditional Helm repository served from static files
- Automated chart packaging, linting, and publishing via CI
- Semantic versioning enforced on every release
- A library chart consumed by all service charts

## Constraints

1. Deploy ChartMuseum in-cluster as the primary HTTP-based chart repository
2. Configure Helm to also push/pull charts via OCI to a local Docker registry
3. Create at least three charts: a library chart, a frontend chart, and a backend chart -- where frontend and backend depend on the library
4. Write a CI script (shell) that lints, tests, packages, and publishes every changed chart
5. Charts must use SemVer; the CI script must reject non-SemVer versions in `Chart.yaml`
6. The library chart version bump must trigger a dependency update in all consumer charts
7. Implement chart signing with `helm package --sign` using a GPG key (or document the commands)

## Success Criteria

1. `helm repo add org http://chartmuseum:8080` works from within the cluster
2. `helm pull oci://localhost:5000/charts/frontend --version 0.2.0` retrieves the OCI-stored chart
3. `helm install frontend org/frontend -n prod` installs the frontend with its library dependency resolved
4. The CI script exits non-zero when `helm lint` fails or `Chart.yaml` version is not valid SemVer
5. Running the CI script on a clean repo produces `.tgz` packages and pushes them to both ChartMuseum and OCI registry
6. `helm verify frontend-0.2.0.tgz` passes when signing is enabled

## Verification Commands

```bash
# ChartMuseum is running
kubectl get pods -n chart-infra -l app=chartmuseum
curl http://chartmuseum.chart-infra.svc:8080/api/charts | jq 'keys'

# OCI registry is running
docker pull localhost:5000/charts/frontend:0.2.0 2>/dev/null && echo "OCI OK"

# Charts are published
helm search repo org/ --versions
helm pull org/frontend --version 0.2.0 --untar

# Library dependency resolved
helm dependency list ./frontend | grep library-common

# CI script validates SemVer
echo "version: not-semver" > /tmp/bad-chart/Chart.yaml
./ci/publish-charts.sh /tmp/bad-chart 2>&1 | grep -i "invalid version"

# Signed chart verification
helm verify frontend-0.2.0.tgz --keyring pubring.gpg
```

## Cleanup

```bash
helm uninstall chartmuseum -n chart-infra
kubectl delete namespace chart-infra
docker rm -f registry
rm -rf charts-repo
```
