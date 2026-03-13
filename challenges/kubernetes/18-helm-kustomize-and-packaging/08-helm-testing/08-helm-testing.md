<!--
difficulty: advanced
concepts: [helm-test, chart-testing-ct, helm-unittest, lint-and-validate, ci-testing-pipeline]
tools: [helm, ct, helm-unittest, kubectl]
estimated_time: 40m
bloom_level: analyze
prerequisites: [18-helm-kustomize-and-packaging/02-helm-values-and-templates, 18-helm-kustomize-and-packaging/06-helm-hooks]
-->

# 18.08 - Helm Chart Testing with ct and helm test

## Architecture

```
Chart Testing Strategy
=======================

  Static Analysis          In-Cluster Tests         Unit Tests
  ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
  │  helm lint    │        │  helm test   │        │ helm-unittest│
  │  ct lint      │        │  (runs test  │        │ (template    │
  │  kubeval      │        │   pods in    │        │  assertions  │
  │  kubeconform  │        │   cluster)   │        │  no cluster) │
  └──────────────┘        └──────────────┘        └──────────────┘
         │                        │                       │
         ▼                        ▼                       ▼
   Fast feedback           Validates runtime        Validates template
   No cluster needed       behavior                 output correctness
```

Three layers of Helm chart testing: **static analysis** (`helm lint`, `ct lint`) catches syntax errors without a cluster; **unit tests** (`helm-unittest`) validate rendered template output against assertions; **in-cluster tests** (`helm test`) run test pods that verify the deployed release works end-to-end.

## Suggested Steps

### 1. Set Up a Testable Chart

Create a chart with a Service, Deployment, and a test pod:

```yaml
# testchart/templates/tests/test-connection.yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ include "testchart.fullname" . }}-test
  annotations:
    "helm.sh/hook": test                   # only runs on 'helm test'
spec:
  restartPolicy: Never
  containers:
    - name: wget
      image: busybox:1.37
      command:
        - wget
        - --spider
        - --timeout=5
        - "http://{{ include "testchart.fullname" . }}:{{ .Values.service.port }}"
```

### 2. Static Analysis

```bash
# Basic lint
helm lint ./testchart
helm lint ./testchart -f testchart/values-prod.yaml

# Validate rendered output against Kubernetes schemas
helm template testchart ./testchart | kubeconform -strict -kubernetes-version 1.30.0
```

### 3. Chart Testing Tool (ct)

`ct` (chart-testing) is the official CI tool for linting and installing changed charts in a Git repository.

```bash
# Install ct
brew install chart-testing   # or download from GitHub releases

# Lint
ct lint --charts ./testchart

# Lint and install (requires a running cluster)
ct lint-and-install --charts ./testchart --namespace ct-test
```

Create a `ct.yaml` configuration:

```yaml
# ct.yaml
chart-dirs:
  - charts
target-branch: main
helm-extra-args: --timeout=600s
validate-maintainers: false
```

### 4. Unit Tests with helm-unittest

```bash
# Install the plugin
helm plugin install https://github.com/helm-unittest/helm-unittest
```

```yaml
# testchart/tests/deployment_test.yaml
suite: Deployment tests
templates:
  - templates/deployment.yaml
tests:
  - it: should set replica count from values
    set:
      replicaCount: 5
    asserts:
      - equal:
          path: spec.replicas
          value: 5

  - it: should use the correct image
    set:
      image.repository: nginx
      image.tag: "1.27"
    asserts:
      - equal:
          path: spec.template.spec.containers[0].image
          value: "nginx:1.27"

  - it: should not set replicas when autoscaling is enabled
    set:
      autoscaling.enabled: true
    asserts:
      - isNull:
          path: spec.replicas
```

```yaml
# testchart/tests/service_test.yaml
suite: Service tests
templates:
  - templates/service.yaml
tests:
  - it: should default to ClusterIP
    asserts:
      - equal:
          path: spec.type
          value: ClusterIP

  - it: should use configured port
    set:
      service.port: 8080
    asserts:
      - equal:
          path: spec.ports[0].port
          value: 8080
```

Run the unit tests:

```bash
helm unittest ./testchart
```

### 5. In-Cluster Tests

```bash
kubectl create namespace helm-test-lab
helm install testchart ./testchart --namespace helm-test-lab --wait

# Run the test hook
helm test testchart --namespace helm-test-lab --logs
```

### 6. CI Pipeline Integration

A typical CI pipeline runs all three layers:

```bash
# Stage 1: Static analysis (no cluster)
helm lint ./testchart
helm template testchart ./testchart | kubeconform -strict

# Stage 2: Unit tests (no cluster)
helm unittest ./testchart

# Stage 3: Integration tests (requires cluster)
helm install testchart ./testchart -n test --wait
helm test testchart -n test --logs
helm uninstall testchart -n test
```

## Verify

```bash
# 1. Lint passes cleanly
helm lint ./testchart

# 2. Unit tests pass
helm unittest ./testchart

# 3. In-cluster test pod completes successfully
helm test testchart --namespace helm-test-lab --logs

# 4. Review test pod status
kubectl get pods -n helm-test-lab -l "helm.sh/hook=test"
```

## Cleanup

```bash
helm uninstall testchart --namespace helm-test-lab
kubectl delete namespace helm-test-lab
```

## References

- [Helm Chart Tests](https://helm.sh/docs/topics/chart_tests/)
- [Chart Testing Tool (ct)](https://github.com/helm/chart-testing)
- [helm-unittest Plugin](https://github.com/helm-unittest/helm-unittest)
- [kubeconform](https://github.com/yannh/kubeconform)
