# Exercise 3: Gatekeeper ConstraintTemplates and Constraints

<!--
difficulty: intermediate
concepts: [constrainttemplate, constraint, rego, parameterized-policy, audit-mode, enforcement-actions]
tools: [kubectl]
estimated_time: 35m
bloom_level: apply
prerequisites: [13-policy-engines/01-opa-gatekeeper-basics]
-->

## Introduction

This exercise goes deeper into writing custom ConstraintTemplates with Rego. You will create parameterized policies for image registry restrictions, container resource limits, and prohibited privilege escalation. You will also learn about enforcement actions (deny vs dryrun) and how to debug Rego policies.

## Step-by-Step

### 1. ConstraintTemplate: Allowed image registries

```yaml
# template-allowed-registries.yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sallowedregistries
spec:
  crd:
    spec:
      names:
        kind: K8sAllowedRegistries
      validation:
        openAPIV3Schema:
          type: object
          properties:
            registries:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sallowedregistries

        violation[{"msg": msg}] {
          container := input.review.object.spec.containers[_]
          not registry_allowed(container.image)
          msg := sprintf("Image '%v' is not from an allowed registry. Allowed: %v", [container.image, input.parameters.registries])
        }

        violation[{"msg": msg}] {
          container := input.review.object.spec.initContainers[_]
          not registry_allowed(container.image)
          msg := sprintf("Init container image '%v' is not from an allowed registry. Allowed: %v", [container.image, input.parameters.registries])
        }

        registry_allowed(image) {
          registry := input.parameters.registries[_]
          startswith(image, registry)
        }
```

### 2. Constraint: Only allow specific registries

```yaml
# constraint-allowed-registries.yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRegistries
metadata:
  name: allowed-registries
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  parameters:
    registries:
      - "docker.io/library/"
      - "gcr.io/myproject/"
      - "registry.k8s.io/"
```

### 3. ConstraintTemplate: Require resource limits

```yaml
# template-require-limits.yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequirelimits
spec:
  crd:
    spec:
      names:
        kind: K8sRequireLimits
      validation:
        openAPIV3Schema:
          type: object
          properties:
            requiredResources:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequirelimits

        violation[{"msg": msg}] {
          container := input.review.object.spec.containers[_]
          resource := input.parameters.requiredResources[_]
          not container.resources.limits[resource]
          msg := sprintf("Container '%v' must set limits for '%v'", [container.name, resource])
        }
```

### 4. Constraint: Require CPU and memory limits

```yaml
# constraint-require-limits.yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequireLimits
metadata:
  name: require-cpu-memory-limits
spec:
  enforcementAction: deny
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  parameters:
    requiredResources:
      - "cpu"
      - "memory"
```

### 5. ConstraintTemplate: Block privilege escalation

```yaml
# template-block-escalation.yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sblockprivilegeescalation
spec:
  crd:
    spec:
      names:
        kind: K8sBlockPrivilegeEscalation
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sblockprivilegeescalation

        violation[{"msg": msg}] {
          container := input.review.object.spec.containers[_]
          container.securityContext.allowPrivilegeEscalation == true
          msg := sprintf("Container '%v' must not allow privilege escalation", [container.name])
        }

        violation[{"msg": msg}] {
          container := input.review.object.spec.containers[_]
          container.securityContext.privileged == true
          msg := sprintf("Container '%v' must not run in privileged mode", [container.name])
        }
```

### 6. Apply and test

```bash
kubectl apply -f template-allowed-registries.yaml
kubectl apply -f template-require-limits.yaml
kubectl apply -f template-block-escalation.yaml
sleep 5
kubectl apply -f constraint-allowed-registries.yaml
kubectl apply -f constraint-require-limits.yaml

# Test: image from disallowed registry (should FAIL)
kubectl run bad-image --image=evil.io/backdoor:v1 --dry-run=server 2>&1

# Test: no resource limits (should FAIL)
kubectl run no-limits --image=nginx:1.27 --dry-run=server 2>&1

# Test: compliant pod (should SUCCEED)
kubectl run good-pod --image=nginx:1.27 \
  --requests='cpu=100m,memory=128Mi' \
  --limits='cpu=200m,memory=256Mi' \
  --dry-run=server
```

## TODO Exercise

The `k8sallowedregistries` template has a bug: it does not check `ephemeralContainers`. Add the missing Rego rule to cover ephemeral containers.

<details>
<summary>Solution</summary>

Add this rule to the Rego policy:

```rego
violation[{"msg": msg}] {
  container := input.review.object.spec.ephemeralContainers[_]
  not registry_allowed(container.image)
  msg := sprintf("Ephemeral container image '%v' is not from an allowed registry. Allowed: %v", [container.image, input.parameters.registries])
}
```

</details>

## Verify

```bash
# Check constraint status and violations
kubectl get k8sallowedregistries allowed-registries -o yaml | grep -A20 status
kubectl get k8srequirelimits require-cpu-memory-limits -o yaml | grep -A20 status

# Audit existing violations
kubectl get k8sallowedregistries -o json | \
  python3 -c "import sys,json; d=json.load(sys.stdin); [print(v['message']) for i in d['items'] for v in i.get('status',{}).get('violations',[])]"
```

## Cleanup

```bash
kubectl delete k8sallowedregistries allowed-registries
kubectl delete k8srequirelimits require-cpu-memory-limits
kubectl delete constrainttemplate k8sallowedregistries k8srequirelimits k8sblockprivilegeescalation
```

## What's Next

The next exercise covers **Kyverno Mutate Policies** -- automatically modifying resources as they are admitted to the cluster.
