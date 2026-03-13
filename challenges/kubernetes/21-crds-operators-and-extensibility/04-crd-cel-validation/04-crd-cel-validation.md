# CRD Validation with CEL Expressions

<!--
difficulty: intermediate
concepts: [cel, validation-rules, cross-field-validation, x-kubernetes-validations, common-expression-language]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-crd-basics, 02-crd-structural-schemas]
-->

## Overview

OpenAPI v3 schemas validate individual fields (type, enum, pattern), but they cannot express cross-field constraints like "if engine is redis, replicas must be odd" or "maxReplicas must be greater than minReplicas." Common Expression Language (CEL) validation rules fill this gap, enabling complex validation logic directly in the CRD without a webhook.

## Why This Matters

Before CEL, cross-field validation required deploying a validating admission webhook -- a separate service with its own deployment, certificates, and failure modes. CEL rules are evaluated by the API server itself, making validation simpler to implement and more reliable to operate.

## Step-by-Step Instructions

### Step 1 -- Define a CRD with CEL Validation Rules

```yaml
# autoscaler-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: autoscalers.scaling.example.com
spec:
  group: scaling.example.com
  names:
    kind: Autoscaler
    plural: autoscalers
    singular: autoscaler
    shortNames: [as]
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          required: [spec]
          properties:
            spec:
              type: object
              required: [targetRef, minReplicas, maxReplicas]
              x-kubernetes-validations:
                # Cross-field validation: max must be >= min
                - rule: "self.maxReplicas >= self.minReplicas"
                  message: "maxReplicas must be greater than or equal to minReplicas"
                # Conditional validation: if cooldownSeconds is set, it must be >= 30
                - rule: "!has(self.cooldownSeconds) || self.cooldownSeconds >= 30"
                  message: "cooldownSeconds must be at least 30 when specified"
              properties:
                targetRef:
                  type: object
                  required: [kind, name]
                  properties:
                    kind:
                      type: string
                      enum: [Deployment, StatefulSet]
                    name:
                      type: string
                      minLength: 1
                  x-kubernetes-validations:
                    - rule: "self.kind == 'Deployment' || self.kind == 'StatefulSet'"
                      message: "targetRef.kind must be Deployment or StatefulSet"
                minReplicas:
                  type: integer
                  minimum: 0
                maxReplicas:
                  type: integer
                  maximum: 100
                cooldownSeconds:
                  type: integer
                metrics:
                  type: array
                  items:
                    type: object
                    required: [type, target]
                    properties:
                      type:
                        type: string
                        enum: [cpu, memory, custom]
                      target:
                        type: integer
                        minimum: 1
                        maximum: 100
                      metricName:
                        type: string
                    x-kubernetes-validations:
                      # If type is "custom", metricName is required
                      - rule: "self.type != 'custom' || has(self.metricName)"
                        message: "metricName is required when type is 'custom'"
                  x-kubernetes-validations:
                    # At least one metric must be defined
                    - rule: "self.size() >= 1"
                      message: "at least one metric must be specified"
                schedule:
                  type: object
                  properties:
                    scaleUpTime:
                      type: string
                      pattern: '^\d{2}:\d{2}$'
                    scaleDownTime:
                      type: string
                      pattern: '^\d{2}:\d{2}$'
                  x-kubernetes-validations:
                    # If one schedule field is set, both must be set
                    - rule: "(has(self.scaleUpTime) && has(self.scaleDownTime)) || (!has(self.scaleUpTime) && !has(self.scaleDownTime))"
                      message: "both scaleUpTime and scaleDownTime must be set together"
            status:
              type: object
              properties:
                currentReplicas:
                  type: integer
                desiredReplicas:
                  type: integer
                lastScaleTime:
                  type: string
                  format: date-time
      subresources:
        status: {}
```

```bash
kubectl apply -f autoscaler-crd.yaml
```

### Step 2 -- Test Valid Resources

```yaml
# valid-autoscaler.yaml
apiVersion: scaling.example.com/v1
kind: Autoscaler
metadata:
  name: web-scaler
spec:
  targetRef:
    kind: Deployment
    name: web-frontend
  minReplicas: 2
  maxReplicas: 10
  cooldownSeconds: 60
  metrics:
    - type: cpu
      target: 70
    - type: memory
      target: 80
  schedule:
    scaleUpTime: "08:00"
    scaleDownTime: "20:00"
```

```bash
kubectl apply -f valid-autoscaler.yaml
kubectl get autoscaler web-scaler -o yaml
```

### Step 3 -- Test CEL Validation Rules

```bash
# FAIL: maxReplicas < minReplicas
kubectl apply -f - <<'EOF'
apiVersion: scaling.example.com/v1
kind: Autoscaler
metadata:
  name: bad-range
spec:
  targetRef:
    kind: Deployment
    name: test
  minReplicas: 10
  maxReplicas: 5
  metrics:
    - type: cpu
      target: 50
EOF
# Expected error: maxReplicas must be greater than or equal to minReplicas

# FAIL: cooldownSeconds too low
kubectl apply -f - <<'EOF'
apiVersion: scaling.example.com/v1
kind: Autoscaler
metadata:
  name: bad-cooldown
spec:
  targetRef:
    kind: Deployment
    name: test
  minReplicas: 1
  maxReplicas: 5
  cooldownSeconds: 10
  metrics:
    - type: cpu
      target: 50
EOF
# Expected error: cooldownSeconds must be at least 30 when specified

# FAIL: custom metric without metricName
kubectl apply -f - <<'EOF'
apiVersion: scaling.example.com/v1
kind: Autoscaler
metadata:
  name: bad-metric
spec:
  targetRef:
    kind: Deployment
    name: test
  minReplicas: 1
  maxReplicas: 5
  metrics:
    - type: custom
      target: 50
EOF
# Expected error: metricName is required when type is 'custom'

# FAIL: only one schedule field set
kubectl apply -f - <<'EOF'
apiVersion: scaling.example.com/v1
kind: Autoscaler
metadata:
  name: bad-schedule
spec:
  targetRef:
    kind: Deployment
    name: test
  minReplicas: 1
  maxReplicas: 5
  metrics:
    - type: cpu
      target: 50
  schedule:
    scaleUpTime: "08:00"
EOF
# Expected error: both scaleUpTime and scaleDownTime must be set together
```

### Step 4 -- CEL Expression Reference

Common CEL patterns for CRD validation:

| Pattern | CEL Expression |
|---------|---------------|
| Field A > Field B | `self.fieldA > self.fieldB` |
| Optional field present | `has(self.optionalField)` |
| Conditional requirement | `!has(self.triggerField) \|\| has(self.requiredField)` |
| Array minimum size | `self.items.size() >= 1` |
| String not empty | `self.name.size() > 0` |
| Regex match | `self.value.matches('^[a-z]+$')` |
| Immutable field (on update) | `self.field == oldSelf.field` |
| All-or-nothing fields | `(has(self.a) && has(self.b)) \|\| (!has(self.a) && !has(self.b))` |

## Verify

```bash
# Valid resource was created
kubectl get autoscaler web-scaler

# Invalid resources were all rejected (none should exist)
for name in bad-range bad-cooldown bad-metric bad-schedule; do
  kubectl get autoscaler $name 2>&1 | grep -q "NotFound" && echo "PASS: $name was rejected"
done
```

## Cleanup

```bash
kubectl delete autoscalers --all
kubectl delete crd autoscalers.scaling.example.com
```

## Reference

- [CRD Validation Rules](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules)
- [CEL in Kubernetes](https://kubernetes.io/docs/reference/using-api/cel/)
