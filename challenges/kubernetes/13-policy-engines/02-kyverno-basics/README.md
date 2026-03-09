# Exercise 2: Kyverno Installation and Validate Policies

<!--
difficulty: basic
concepts: [kyverno, clusterpolicy, validate, match, exclude, admission-controller]
tools: [kubectl, helm]
estimated_time: 30m
bloom_level: understand
prerequisites: []
-->

## Introduction

**Kyverno** is a Kubernetes-native policy engine that uses YAML (no separate policy language like Rego). Policies can:

- **Validate** -- accept or reject resources based on conditions
- **Mutate** -- modify resources on admission (add labels, set defaults)
- **Generate** -- create new resources when others are created (e.g., auto-create NetworkPolicies)
- **Verify Images** -- check container image signatures

Kyverno policies can be namespaced (`Policy`) or cluster-wide (`ClusterPolicy`).

## Why This Matters

Kyverno has a lower learning curve than OPA Gatekeeper because policies are written in familiar Kubernetes YAML. This makes it easier for platform teams to write, review, and maintain policies without learning Rego.

## Step-by-Step

### 1. Install Kyverno

```bash
# Using kubectl
kubectl create -f https://github.com/kyverno/kyverno/releases/download/v1.13.4/install.yaml

# Wait for Kyverno to be ready
kubectl wait --for=condition=ready pod -l app.kubernetes.io/component=admission-controller \
  -n kyverno --timeout=120s
```

### 2. Verify the installation

```bash
# Check pods
kubectl get pods -n kyverno

# Check CRDs
kubectl get crd | grep kyverno

# Check webhook
kubectl get validatingwebhookconfiguration | grep kyverno
kubectl get mutatingwebhookconfiguration | grep kyverno
```

### 3. Create a validate policy: require labels

```yaml
# policy-require-labels.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  validationFailureAction: Enforce    # Enforce = reject, Audit = allow but report
  background: true                     # audit existing resources
  rules:
    - name: check-team-label
      match:
        any:
          - resources:
              kinds:
                - Pod                  # apply to pods
      validate:
        message: "The label 'team' is required on all Pods."
        pattern:
          metadata:
            labels:
              team: "?*"              # must exist and not be empty
```

### 4. Create a validate policy: disallow latest tag

```yaml
# policy-disallow-latest.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: disallow-latest-tag
spec:
  validationFailureAction: Enforce
  background: true
  rules:
    - name: disallow-latest
      match:
        any:
          - resources:
              kinds:
                - Pod
      validate:
        message: "Using the ':latest' tag is not allowed. Specify a version tag."
        pattern:
          spec:
            containers:
              - image: "!*:latest"    # image must NOT end with :latest
```

### 5. Create a validate policy: require resource limits

```yaml
# policy-require-limits.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-resource-limits
spec:
  validationFailureAction: Enforce
  background: true
  rules:
    - name: check-limits
      match:
        any:
          - resources:
              kinds:
                - Pod
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system          # exempt system namespace
                - kyverno
      validate:
        message: "CPU and memory limits are required for all containers."
        pattern:
          spec:
            containers:
              - resources:
                  limits:
                    memory: "?*"       # must be set
                    cpu: "?*"          # must be set
```

### 6. Test the policies

```bash
kubectl apply -f policy-require-labels.yaml
kubectl apply -f policy-disallow-latest.yaml
kubectl apply -f policy-require-limits.yaml

# This should FAIL (no team label)
kubectl run test-no-label --image=nginx:1.27 --dry-run=server
# Expected: Error -- The label 'team' is required

# This should FAIL (latest tag)
kubectl run test-latest --image=nginx:latest \
  --labels=team=dev --dry-run=server
# Expected: Error -- Using the ':latest' tag is not allowed

# This should SUCCEED
kubectl run test-good --image=nginx:1.27 \
  --labels=team=dev \
  --requests='cpu=100m,memory=128Mi' \
  --limits='cpu=200m,memory=256Mi' \
  --dry-run=server
```

## Common Mistakes

1. **Confusing Enforce vs Audit** -- `Enforce` rejects violations. `Audit` allows them but reports violations in the policy status. Start with `Audit` in production.
2. **Forgetting to exclude system namespaces** -- Policies that match all Pods without excluding `kube-system` and `kyverno` can break the cluster.
3. **Pattern matching vs deny rules** -- Kyverno patterns describe what the resource SHOULD look like. Use `deny` rules with `conditions` for more complex logic.
4. **Not checking policy status** -- Use `kubectl get clusterpolicy -o yaml` to check if the policy was successfully compiled and is being enforced.

## Verify

```bash
# Check policy status
kubectl get clusterpolicy

# View policy details
kubectl describe clusterpolicy require-team-label

# Check for existing violations (background scan)
kubectl get policyreport -A

# Dry-run tests
kubectl run fail-test --image=nginx:latest --dry-run=server 2>&1
kubectl run pass-test --image=nginx:1.27 \
  --labels=team=platform \
  --requests='cpu=100m,memory=128Mi' \
  --limits='cpu=200m,memory=256Mi' \
  --dry-run=server
```

## Cleanup

```bash
kubectl delete clusterpolicy require-team-label disallow-latest-tag require-resource-limits
# Optionally remove Kyverno:
# kubectl delete -f https://github.com/kyverno/kyverno/releases/download/v1.13.4/install.yaml
```

## What's Next

In the next exercise you will learn about **Gatekeeper ConstraintTemplates and Constraints** -- writing custom Rego policies for more complex validations.

## Summary

- Kyverno policies are written in YAML, making them accessible to Kubernetes practitioners.
- `ClusterPolicy` applies cluster-wide; `Policy` applies to a single namespace.
- `validationFailureAction: Enforce` rejects violations; `Audit` reports them without blocking.
- Patterns describe the expected shape of a resource using `?*` (exists and not empty) and `!` (negation).
- Always exclude system namespaces from policies that match broad resource kinds.
- `background: true` enables auditing of existing resources against the policy.

## Reference

- [Kyverno Documentation](https://kyverno.io/docs/)
- [Kyverno Validate Rules](https://kyverno.io/docs/writing-policies/validate/)

## Additional Resources

- [Kyverno Policy Library](https://kyverno.io/policies/)
- [Kyverno Playground](https://playground.kyverno.io/)
