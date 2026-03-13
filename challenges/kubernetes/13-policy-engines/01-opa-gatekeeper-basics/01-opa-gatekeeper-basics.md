# Exercise 1: OPA Gatekeeper Installation and Basics

<!--
difficulty: basic
concepts: [opa, gatekeeper, constraint-template, constraint, admission-controller, rego]
tools: [kubectl, helm]
estimated_time: 30m
bloom_level: understand
prerequisites: []
-->

## Introduction

**OPA Gatekeeper** is an admission controller that enforces policies written in Rego (a declarative policy language). It uses two custom resources:

- **ConstraintTemplate** -- defines the policy logic in Rego and the parameters it accepts
- **Constraint** -- instantiates a template with specific parameters to enforce a policy

When a user creates or updates a resource, the API server sends it to Gatekeeper's webhook, which evaluates it against all matching Constraints.

## Why This Matters

Pod Security Admission handles basic pod security standards, but real-world clusters need policies beyond security: naming conventions, required labels, resource limits, image registry restrictions, and more. Gatekeeper provides a flexible, extensible framework for all of these.

## Step-by-Step

### 1. Install Gatekeeper

```bash
# Using kubectl
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.18.0/deploy/gatekeeper.yaml

# Wait for all components
kubectl wait --for=condition=ready pod -l control-plane=controller-manager \
  -n gatekeeper-system --timeout=120s
kubectl wait --for=condition=ready pod -l control-plane=audit-controller \
  -n gatekeeper-system --timeout=120s
```

### 2. Verify the installation

```bash
# Check CRDs
kubectl get crd | grep gatekeeper

# Check pods
kubectl get pods -n gatekeeper-system

# Check webhook
kubectl get validatingwebhookconfiguration | grep gatekeeper
```

### 3. Create a ConstraintTemplate that requires labels

```yaml
# template-required-labels.yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels         # the Constraint kind this template creates
      validation:
        openAPIV3Schema:
          type: object
          properties:
            labels:
              type: array
              items:
                type: string            # list of required label keys
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels

        # Violation if any required label is missing
        violation[{"msg": msg}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          msg := sprintf("Missing required labels: %v", [missing])
        }
```

### 4. Create a Constraint

```yaml
# constraint-require-team-label.yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]           # apply to Namespaces
  parameters:
    labels:
      - "team"                         # every Namespace must have a "team" label
```

### 5. Test the policy

```yaml
# namespace-missing-label.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: test-no-label
  # No "team" label -- should be rejected
```

```yaml
# namespace-with-label.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: test-with-label
  labels:
    team: platform                     # has the required label -- should be accepted
```

### 6. Apply and test

```bash
kubectl apply -f template-required-labels.yaml
kubectl apply -f constraint-require-team-label.yaml

# Wait for the constraint to be enforced
sleep 5

# This should FAIL
kubectl apply -f namespace-missing-label.yaml
# Expected: Error -- Missing required labels: {"team"}

# This should SUCCEED
kubectl apply -f namespace-with-label.yaml
```

## Common Mistakes

1. **Applying the Constraint before the ConstraintTemplate** -- The Constraint CRD does not exist until the template is created. Apply templates first, then constraints.
2. **Forgetting to wait for template sync** -- Gatekeeper needs a few seconds to compile the Rego and register the webhook. Applying the Constraint immediately may result in "no matching resource" errors.
3. **Matching too broadly** -- A Constraint that matches all resources (`kinds: ["*"]`) can break your cluster by blocking system resources. Always scope matches carefully.
4. **Not testing with dry-run first** -- Use `--dry-run=server` to test policies without actually creating resources.

## Verify

```bash
# Check ConstraintTemplate status
kubectl get constrainttemplate k8srequiredlabels -o yaml | \
  grep -A5 status

# Check Constraint status (look for totalViolations)
kubectl get k8srequiredlabels require-team-label -o yaml | \
  grep -A10 status

# Audit existing resources for violations
kubectl get k8srequiredlabels require-team-label \
  -o jsonpath='{.status.violations}' | python3 -m json.tool

# Dry-run test
kubectl create namespace test-dryrun --dry-run=server
# Expected: Error (missing team label)
```

## Cleanup

```bash
kubectl delete k8srequiredlabels require-team-label
kubectl delete constrainttemplate k8srequiredlabels
kubectl delete namespace test-with-label 2>/dev/null
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.18.0/deploy/gatekeeper.yaml --prune -l gatekeeper.sh/system=yes 2>/dev/null
# Or: kubectl delete -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.18.0/deploy/gatekeeper.yaml
```

## What's Next

In the next exercise you will learn about **Kyverno Installation and Validate Policies** -- a Kubernetes-native policy engine that uses YAML instead of Rego.

## Summary

- OPA Gatekeeper is a validating admission controller that enforces custom policies.
- Policies are defined in two parts: ConstraintTemplate (Rego logic) and Constraint (parameters + scope).
- Gatekeeper audits both new and existing resources against Constraints.
- Always apply ConstraintTemplates before Constraints.
- Use `match.kinds` to scope which resources a Constraint evaluates.
- The `status.violations` field shows existing resources that violate the policy.

## Reference

- [OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/website/docs/)
- [Rego Language Reference](https://www.openpolicyagent.org/docs/latest/policy-language/)

## Additional Resources

- [Gatekeeper Library](https://open-policy-agent.github.io/gatekeeper-library/website/)
- [OPA Playground](https://play.openpolicyagent.org/)
