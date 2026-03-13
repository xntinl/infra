# Exercise 6: Gatekeeper Mutations and Auto-Injection

<!--
difficulty: advanced
concepts: [gatekeeper, assign, assignmetadata, modifyset, mutation-webhook, external-data]
tools: [kubectl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [13-policy-engines/01-opa-gatekeeper-basics, 13-policy-engines/03-gatekeeper-constraint-templates]
-->

## Introduction

Gatekeeper supports mutations through three CRDs: **Assign** (set or override field values), **AssignMetadata** (add labels and annotations), and **ModifySet** (add or remove items from lists). Unlike Gatekeeper's validation which uses Rego, mutations use a declarative YAML-based approach with path expressions and match conditions.

## Architecture

```
API Request (Pod creation)
    |
    v
kube-apiserver
    |
    +-- Mutating Admission (Gatekeeper mutation webhook)
    |       |
    |       +-- AssignMetadata: add/modify labels, annotations
    |       +-- Assign: set/override spec fields
    |       +-- ModifySet: add items to lists
    |       |
    |       v (modified object)
    |
    +-- Validating Admission (Gatekeeper validation webhook)
    |       |
    |       +-- ConstraintTemplates + Constraints
    |       v
    |
    v
etcd
```

## Suggested Steps

1. **Enable Gatekeeper mutations** (if not already enabled). Gatekeeper must be installed with mutation support:

```bash
# If using Helm:
helm upgrade gatekeeper gatekeeper/gatekeeper \
  --namespace gatekeeper-system \
  --set mutations.enable=true
```

2. **AssignMetadata: add default labels to all pods:**

```yaml
# assignmetadata-managed-by.yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: AssignMetadata
metadata:
  name: add-managed-by-label
spec:
  match:
    scope: Namespaced
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  location: "metadata.labels.app\\.kubernetes\\.io/managed-by"
  parameters:
    assign:
      value: "gatekeeper"
```

3. **Assign: set imagePullPolicy to Always for all containers:**

```yaml
# assign-pull-policy.yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: always-pull-images
spec:
  applyTo:
    - groups: [""]
      kinds: ["Pod"]
      versions: ["v1"]
  match:
    scope: Namespaced
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  location: "spec.containers[name:*].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
```

4. **Assign: set default securityContext fields:**

```yaml
# assign-security-context.yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: default-security-context
spec:
  applyTo:
    - groups: [""]
      kinds: ["Pod"]
      versions: ["v1"]
  match:
    scope: Namespaced
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  location: "spec.containers[name:*].securityContext.allowPrivilegeEscalation"
  parameters:
    assign:
      value: false
    pathTests:
      - subPath: "spec.containers[name:*].securityContext.allowPrivilegeEscalation"
        condition: MustNotExist    # only set if not already specified
```

5. **ModifySet: add a toleration to all pods in a specific namespace:**

```yaml
# modifyset-toleration.yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: ModifySet
metadata:
  name: add-spot-toleration
spec:
  applyTo:
    - groups: [""]
      kinds: ["Pod"]
      versions: ["v1"]
  match:
    scope: Namespaced
    namespaces:
      - batch-workloads        # only this namespace
  location: "spec.tolerations"
  parameters:
    operation: merge
    values:
      fromList:
        - key: "cloud.google.com/gke-spot"
          operator: "Equal"
          value: "true"
          effect: "NoSchedule"
```

6. **Test the mutations:**

```bash
kubectl apply -f assignmetadata-managed-by.yaml
kubectl apply -f assign-pull-policy.yaml
kubectl apply -f assign-security-context.yaml

# Create a pod and inspect the mutations
kubectl run test-mutation --image=nginx:1.27 --dry-run=server -o yaml

# Check for the added label
# Check imagePullPolicy is "Always"
# Check securityContext.allowPrivilegeEscalation is false
```

## Verify

```bash
# List all mutation policies
kubectl get assignmetadata,assign,modifyset

# Create a test pod and verify mutations
kubectl run mutation-test --image=nginx:1.27 -n default
kubectl get pod mutation-test -o yaml | grep -A3 "managed-by"
kubectl get pod mutation-test -o yaml | grep imagePullPolicy
kubectl get pod mutation-test -o yaml | grep allowPrivilegeEscalation

# Clean up test
kubectl delete pod mutation-test
```

## Cleanup

```bash
kubectl delete assignmetadata add-managed-by-label
kubectl delete assign always-pull-images default-security-context
kubectl delete modifyset add-spot-toleration 2>/dev/null
```

## What's Next

The next exercise covers **Kyverno Image Verification with cosign** -- enforcing that only signed container images run in your cluster.
