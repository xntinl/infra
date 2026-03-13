# CRD Structural Schemas and Validation

<!--
difficulty: basic
concepts: [crd-schema, openapi-v3, structural-schema, validation, default-values, enums, patterns]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [01-crd-basics]
-->

## Overview

Structural schemas define the exact shape of a custom resource's spec, status, and other fields using OpenAPI v3. They enforce type safety, set default values, validate input with patterns and enums, and ensure that custom resources conform to a well-defined contract. Without proper schemas, invalid data can be stored in etcd and cause runtime failures in controllers.

## Why This Matters

A schema is the contract between your custom resource and its controller. Without validation, users can create resources with missing fields, wrong types, or invalid values. The controller then crashes or silently ignores bad input. Strong schemas catch errors at creation time, before they cause problems.

## Step-by-Step Instructions

### Step 1 -- Define a CRD with Rich Validation

```yaml
# database-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: databases.data.example.com
spec:
  group: data.example.com
  names:
    kind: Database
    listKind: DatabaseList
    plural: databases
    singular: database
    shortNames: [db]
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          required: [spec]                          # spec is required at the top level
          properties:
            spec:
              type: object
              required: [engine, version, storage]  # required fields in spec
              properties:
                engine:
                  type: string
                  enum:                             # only these values are allowed
                    - postgres
                    - mysql
                    - redis
                  description: "Database engine type"
                version:
                  type: string
                  pattern: '^\d+\.\d+$'             # must match X.Y format
                  description: "Engine version (e.g., 16.4)"
                storage:
                  type: object
                  required: [size]
                  properties:
                    size:
                      type: string
                      pattern: '^\d+(Gi|Mi)$'       # must be like 10Gi or 512Mi
                      description: "Storage size (e.g., 10Gi)"
                    storageClass:
                      type: string
                      default: standard             # default value if not specified
                      description: "StorageClass name"
                replicas:
                  type: integer
                  minimum: 1                        # minimum value
                  maximum: 7                        # maximum value
                  default: 1                        # default value
                  description: "Number of replicas (odd numbers recommended)"
                backup:
                  type: object
                  properties:
                    enabled:
                      type: boolean
                      default: true
                    schedule:
                      type: string
                      default: "0 2 * * *"          # default: 2 AM daily
                      description: "Cron schedule for backups"
                    retentionDays:
                      type: integer
                      minimum: 1
                      maximum: 365
                      default: 30
                  default: {}                       # default to empty object (triggers nested defaults)
                tags:
                  type: object
                  additionalProperties:             # allows arbitrary key-value pairs
                    type: string
                  description: "Arbitrary key-value tags"
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: [Pending, Creating, Running, Failed, Deleting]
                readyReplicas:
                  type: integer
                endpoint:
                  type: string
                message:
                  type: string
      subresources:
        status: {}                                  # enable the /status subresource
```

```bash
kubectl apply -f database-crd.yaml
```

### Step 2 -- Test Validation

```yaml
# valid-database.yaml
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: app-db
spec:
  engine: postgres
  version: "16.4"
  storage:
    size: 10Gi
    storageClass: fast-ssd
  replicas: 3
  backup:
    enabled: true
    schedule: "0 3 * * *"
    retentionDays: 7
  tags:
    team: backend
    environment: production
```

```bash
# This should succeed
kubectl apply -f valid-database.yaml

# Verify defaults were applied
kubectl get database app-db -o yaml
```

Now test that invalid resources are rejected:

```bash
# Invalid engine (not in enum)
kubectl apply -f - <<'EOF'
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: bad-engine
spec:
  engine: mongodb
  version: "7.0"
  storage:
    size: 5Gi
EOF
# Expected: rejected -- mongodb is not in the enum

# Invalid version format
kubectl apply -f - <<'EOF'
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: bad-version
spec:
  engine: postgres
  version: "sixteen"
  storage:
    size: 5Gi
EOF
# Expected: rejected -- does not match pattern ^\d+\.\d+$

# Invalid storage size format
kubectl apply -f - <<'EOF'
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: bad-storage
spec:
  engine: redis
  version: "7.2"
  storage:
    size: "10 gigabytes"
EOF
# Expected: rejected -- does not match pattern ^\d+(Gi|Mi)$

# Replicas out of range
kubectl apply -f - <<'EOF'
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: bad-replicas
spec:
  engine: mysql
  version: "8.0"
  storage:
    size: 20Gi
  replicas: 15
EOF
# Expected: rejected -- maximum is 7
```

### Step 3 -- Verify Default Values

```bash
# Create a minimal resource -- defaults should fill in missing fields
kubectl apply -f - <<'EOF'
apiVersion: data.example.com/v1
kind: Database
metadata:
  name: minimal-db
spec:
  engine: redis
  version: "7.2"
  storage:
    size: 1Gi
EOF

kubectl get database minimal-db -o yaml
# Check that these defaults were applied:
# - spec.replicas: 1
# - spec.storage.storageClass: standard
# - spec.backup.enabled: true
# - spec.backup.schedule: "0 2 * * *"
# - spec.backup.retentionDays: 30
```

### Step 4 -- Status Subresource

The `/status` subresource separates spec updates from status updates. Regular users can update spec, while controllers update status.

```bash
# Try to set status via kubectl (simulating a controller)
kubectl proxy &
curl -X PUT http://localhost:8001/apis/data.example.com/v1/namespaces/default/databases/app-db/status \
  -H "Content-Type: application/json" \
  -d '{
    "apiVersion": "data.example.com/v1",
    "kind": "Database",
    "metadata": {"name": "app-db", "namespace": "default"},
    "status": {"phase": "Running", "readyReplicas": 3, "endpoint": "app-db.default:5432"}
  }'
kill %1

# Verify status
kubectl get database app-db -o jsonpath='{.status.phase}'
```

## Common Mistakes

1. **Not setting `required` fields** -- without the `required` array, all fields are optional and the controller must handle nil values for every field.
2. **Missing `default: {}` for nested objects** -- nested defaults only apply if the parent object exists. Use `default: {}` on the parent to ensure nested defaults are triggered.
3. **Using `x-kubernetes-preserve-unknown-fields` everywhere** -- this disables schema validation for the subtree. Only use it when you genuinely need to store arbitrary JSON.
4. **Forgetting the status subresource** -- without `subresources.status`, status updates overwrite the entire resource including spec.

## Verify

```bash
# Valid resource exists with defaults
kubectl get database minimal-db -o jsonpath='{.spec.replicas}'
# Expected: 1

kubectl get database minimal-db -o jsonpath='{.spec.backup.schedule}'
# Expected: 0 2 * * *

# Invalid resources were rejected (none of the bad-* resources should exist)
kubectl get database bad-engine 2>&1 | grep -q "NotFound" && echo "PASS: bad-engine rejected"
kubectl get database bad-version 2>&1 | grep -q "NotFound" && echo "PASS: bad-version rejected"
```

## Cleanup

```bash
kubectl delete databases --all
kubectl delete crd databases.data.example.com
```

## What's Next

- **Exercise 03** -- Add printer columns for better kubectl output
- **Exercise 04** -- Use CEL expressions for cross-field validation

## Summary

- Structural schemas define the exact shape and types of custom resource fields
- Enums restrict a field to a specific set of allowed values
- Patterns (regex) validate string format at creation time
- Default values fill in omitted fields automatically
- The `/status` subresource separates spec from status updates, enabling proper controller patterns
- Strong schemas catch errors at admission time rather than at runtime in the controller

## Reference

- [Structural Schemas](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#specifying-a-structural-schema)
- [OpenAPI v3 Validation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation)
- [Defaulting](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#defaulting)

## Additional Resources

- [OpenAPI Specification](https://swagger.io/specification/)
- [CRD Versioning](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/)
