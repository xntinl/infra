# Exercise 8: Policy Testing in CI/CD Pipelines

<!--
difficulty: advanced
concepts: [policy-testing, kyverno-cli, gator, conftest, opa-test, cicd-integration, shift-left]
tools: [kubectl, kyverno-cli, gator, conftest]
estimated_time: 40m
bloom_level: analyze
prerequisites: [13-policy-engines/01-opa-gatekeeper-basics, 13-policy-engines/02-kyverno-basics]
-->

## Introduction

Policies should be tested before they are deployed to a cluster, just like application code. "Shift left" by validating both the policies themselves (do they work as intended?) and the resources against policies (will my manifests pass admission?) in CI/CD pipelines. This exercise covers testing tools for both Gatekeeper and Kyverno.

## Architecture

```
Developer pushes code
    |
    v
CI Pipeline
    |
    +-- Stage 1: Lint YAML manifests (kubeval, kubeconform)
    |
    +-- Stage 2: Test Kyverno policies
    |       kyverno apply policy.yaml --resource pod.yaml
    |
    +-- Stage 3: Test Gatekeeper policies
    |       gator test suite.yaml
    |
    +-- Stage 4: Test OPA policies with conftest
    |       conftest test deployment.yaml --policy policy/
    |
    +-- Stage 5: Deploy to cluster (if all tests pass)
    |
    v
Cluster (policies already in place)
```

## Suggested Steps

### Kyverno CLI Testing

1. **Install the Kyverno CLI:**

```bash
# macOS
brew install kyverno

# Or download from GitHub releases
curl -LO https://github.com/kyverno/kyverno/releases/download/v1.13.4/kyverno-cli_v1.13.4_darwin_arm64.tar.gz
```

2. **Test a policy against a resource:**

```bash
# Apply a policy to a resource file
kyverno apply policy-require-labels.yaml \
  --resource pod.yaml

# Test with multiple policies
kyverno apply policies/ \
  --resource manifests/
```

3. **Write Kyverno test cases:**

```yaml
# test/kyverno-test.yaml
apiVersion: cli.kyverno.io/v1alpha1
kind: Test
metadata:
  name: test-require-labels
policies:
  - ../policies/require-team-label.yaml
resources:
  - resources/pod-with-label.yaml
  - resources/pod-without-label.yaml
results:
  - policy: require-team-label
    rule: check-team-label
    resource: pod-with-label
    kind: Pod
    result: pass
  - policy: require-team-label
    rule: check-team-label
    resource: pod-without-label
    kind: Pod
    result: fail
```

```bash
kyverno test test/
```

### Gatekeeper Testing with gator

4. **Install gator:**

```bash
# Download from Gatekeeper releases
curl -LO https://github.com/open-policy-agent/gatekeeper/releases/download/v3.18.0/gator-v3.18.0-darwin-arm64.tar.gz
```

5. **Write a gator test suite:**

```yaml
# gator-suite.yaml
apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
tests:
  - name: require-labels
    template: templates/k8srequiredlabels.yaml
    constraint: constraints/require-team-label.yaml
    cases:
      - name: pod-with-label
        object: fixtures/pod-with-label.yaml
        assertions:
          - violations: "no"
      - name: pod-without-label
        object: fixtures/pod-without-label.yaml
        assertions:
          - violations: "yes"
            message: "Missing required labels"
```

```bash
gator verify gator-suite.yaml
```

### OPA/Conftest Testing

6. **Test with conftest** (for standalone OPA policies):

```bash
# Install conftest
brew install conftest

# Test resources against policies
conftest test deployment.yaml \
  --policy policy/ \
  --namespace k8s

# Output formats for CI
conftest test deployment.yaml \
  --policy policy/ \
  --output json
```

7. **Write OPA unit tests:**

```rego
# policy/k8s_test.rego
package k8s

test_missing_labels {
  deny["Missing required label: team"] with input as {
    "kind": "Pod",
    "metadata": {
      "labels": {}
    }
  }
}

test_has_labels {
  count(deny) == 0 with input as {
    "kind": "Pod",
    "metadata": {
      "labels": {"team": "platform"}
    }
  }
}
```

```bash
opa test policy/ -v
```

### CI/CD Integration

8. **GitHub Actions example:**

```yaml
# .github/workflows/policy-test.yaml
name: Policy Tests
on: [pull_request]
jobs:
  test-policies:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install Kyverno CLI
        run: |
          curl -LO https://github.com/kyverno/kyverno/releases/download/v1.13.4/kyverno-cli_v1.13.4_linux_amd64.tar.gz
          tar xzf kyverno-cli_*.tar.gz
          sudo mv kyverno /usr/local/bin/
      - name: Run Kyverno tests
        run: kyverno test test/
      - name: Validate manifests against policies
        run: kyverno apply policies/ --resource manifests/
```

## Verify

```bash
# Run Kyverno CLI tests
kyverno test test/
# Expected: all tests pass

# Run gator tests
gator verify gator-suite.yaml
# Expected: all tests pass

# Validate a known-bad manifest
kyverno apply policies/require-labels.yaml \
  --resource test/fixtures/pod-without-label.yaml
# Expected: policy violation reported
```

## Cleanup

No cluster resources to clean up (all testing is local/CI).

## What's Next

The next exercise is **Enterprise Policy Framework Design** -- an insane-level challenge where you design a comprehensive policy framework for a large organization.
