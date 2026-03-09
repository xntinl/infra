# Custom Resource Definitions: Basics

<!--
difficulty: basic
concepts: [crd, custom-resources, api-extension, apiVersion, metadata, spec]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [kubectl-basics, yaml]
-->

## Overview

Custom Resource Definitions (CRDs) extend the Kubernetes API with your own resource types. Once a CRD is registered, you can create, read, update, and delete instances of that custom resource using kubectl, just like built-in resources (Pods, Services, Deployments). CRDs are the foundation for the operator pattern and for integrating domain-specific concepts into Kubernetes.

## Why This Matters

CRDs let you model your application's domain objects as Kubernetes resources. Instead of managing databases, message queues, or certificates through external tools, you can define them as Kubernetes resources and manage them with kubectl, GitOps pipelines, and RBAC -- the same tools you already use for everything else.

## Step-by-Step Instructions

### Step 1 -- Define a CRD

Create a CRD that represents a `Website` resource. A Website has a domain name, an image to serve, and a replica count.

```yaml
# website-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: websites.apps.example.com       # must match <plural>.<group>
spec:
  group: apps.example.com               # API group for this resource
  names:
    kind: Website                        # PascalCase resource kind
    listKind: WebsiteList                # kind for lists of this resource
    plural: websites                     # lowercase plural (used in URLs)
    singular: website                    # lowercase singular
    shortNames:                          # optional short names for kubectl
      - ws
  scope: Namespaced                      # Namespaced or Cluster
  versions:
    - name: v1alpha1                     # version name
      served: true                       # this version is served by the API
      storage: true                      # this version is used for storage
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required:                  # required fields in spec
                - domain
                - image
              properties:
                domain:
                  type: string
                  description: "The domain name for this website"
                image:
                  type: string
                  description: "Container image to serve the website"
                replicas:
                  type: integer
                  minimum: 1
                  maximum: 10
                  default: 1
                  description: "Number of replicas"
```

```bash
kubectl apply -f website-crd.yaml
```

### Step 2 -- Verify the CRD is Registered

```bash
# List all CRDs
kubectl get crds

# Get details about the Website CRD
kubectl get crd websites.apps.example.com

# The new resource type is now available in the API
kubectl api-resources | grep websites
```

### Step 3 -- Create Custom Resource Instances

```yaml
# my-website.yaml
apiVersion: apps.example.com/v1alpha1    # group/version from the CRD
kind: Website                            # kind from the CRD
metadata:
  name: company-site
  namespace: default
spec:
  domain: www.example.com
  image: nginx:1.27
  replicas: 3
---
apiVersion: apps.example.com/v1alpha1
kind: Website
metadata:
  name: blog
  namespace: default
spec:
  domain: blog.example.com
  image: nginx:1.27
  # replicas defaults to 1
```

```bash
kubectl apply -f my-website.yaml
```

### Step 4 -- Interact with Custom Resources

Custom resources support all standard kubectl operations.

```bash
# List websites (both full name and short name work)
kubectl get websites
kubectl get ws

# Describe a specific website
kubectl describe website company-site

# Get YAML output
kubectl get website company-site -o yaml

# Edit a website
kubectl edit website company-site

# Patch a website
kubectl patch website company-site --type=merge -p '{"spec":{"replicas":5}}'

# Delete a website
kubectl delete website blog
```

### Step 5 -- Understand What CRDs Do Not Do

A CRD only registers a resource type and stores instances in etcd. By itself, a CRD does NOT:

- Create Deployments, Pods, or Services based on the custom resource
- React to changes in custom resource instances
- Validate beyond the OpenAPI schema

To make custom resources actually do something, you need a controller (operator) that watches for changes and reconciles the desired state. That is covered in Exercises 05-09.

## Common Mistakes

1. **CRD name does not match `<plural>.<group>`** -- the CRD `metadata.name` must exactly equal `<spec.names.plural>.<spec.group>`. For example, `websites.apps.example.com`.
2. **Missing `openAPIV3Schema`** -- Kubernetes requires structural schemas for CRDs. A CRD without a schema is rejected.
3. **Using `Cluster` scope when `Namespaced` is intended** -- cluster-scoped resources exist globally; namespaced resources are isolated per namespace.
4. **Forgetting `served: true` and `storage: true`** -- exactly one version must have `storage: true`, and at least one must have `served: true`.

## Verify

```bash
# CRD exists
kubectl get crd websites.apps.example.com -o jsonpath='{.metadata.name}'
# Expected: websites.apps.example.com

# Custom resource exists with correct spec
kubectl get website company-site -o jsonpath='{.spec.domain}'
# Expected: www.example.com

kubectl get website company-site -o jsonpath='{.spec.replicas}'
# Expected: 5 (after the patch)

# Short name works
kubectl get ws
```

## Cleanup

```bash
# Delete custom resources first
kubectl delete websites --all

# Delete the CRD (this also deletes all remaining custom resource instances)
kubectl delete crd websites.apps.example.com
```

## What's Next

- **Exercise 02** -- Add structural schemas and validation to CRDs
- **Exercise 03** -- Add printer columns and short names for better kubectl output
- **Exercise 05** -- Learn the operator pattern that makes CRDs actionable

## Summary

- CRDs extend the Kubernetes API with custom resource types without modifying the API server
- The CRD name must follow the pattern `<plural>.<group>`
- Custom resources support all standard kubectl operations (get, describe, create, edit, delete)
- A structural OpenAPI v3 schema is required and defines the shape of the spec
- CRDs store data but do not act on it -- a controller (operator) is needed for reconciliation
- Short names provide convenient aliases for kubectl commands

## Reference

- [Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
- [CustomResourceDefinition](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)

## Additional Resources

- [API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Extend the Kubernetes API](https://kubernetes.io/docs/concepts/extend-kubernetes/)
