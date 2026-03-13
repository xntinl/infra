# CRD Printer Columns and Short Names

<!--
difficulty: intermediate
concepts: [printer-columns, additional-printer-columns, short-names, categories, kubectl-output]
tools: [kubectl]
estimated_time: 20m
bloom_level: apply
prerequisites: [01-crd-basics, 02-crd-structural-schemas]
-->

## Overview

By default, `kubectl get <custom-resource>` shows only NAME and AGE. Printer columns let you surface important fields (like status, replicas, or version) directly in the kubectl table output, making custom resources feel like first-class Kubernetes citizens. Short names and categories further improve the user experience.

## Why This Matters

Operators and platform teams create CRDs that other engineers interact with daily. Good printer columns mean users can assess resource health at a glance without running `kubectl describe` or `-o yaml` for every resource.

## Step-by-Step Instructions

### Step 1 -- Define a CRD with Printer Columns

```yaml
# application-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: applications.platform.example.com
spec:
  group: platform.example.com
  names:
    kind: Application
    listKind: ApplicationList
    plural: applications
    singular: application
    shortNames:
      - app                               # kubectl get app
      - apps                              # kubectl get apps
    categories:
      - all                               # included in kubectl get all
      - platform                          # kubectl get platform (shows all resources in this category)
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
        - name: Image                      # column header
          type: string
          jsonPath: .spec.image            # JSONPath to the value
          description: "Container image"
        - name: Replicas
          type: integer
          jsonPath: .spec.replicas
          description: "Desired replica count"
        - name: Ready
          type: string
          jsonPath: .status.readyReplicas
          description: "Ready replicas"
        - name: Status
          type: string
          jsonPath: .status.phase
          description: "Current phase"
        - name: Endpoint
          type: string
          jsonPath: .status.endpoint
          priority: 1                      # priority 1 = shown only with -o wide
          description: "Service endpoint"
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required: [image]
              properties:
                image:
                  type: string
                replicas:
                  type: integer
                  minimum: 1
                  default: 1
                port:
                  type: integer
                  default: 80
                environment:
                  type: string
                  enum: [dev, staging, production]
                  default: dev
            status:
              type: object
              properties:
                phase:
                  type: string
                readyReplicas:
                  type: string
                endpoint:
                  type: string
      subresources:
        status: {}
```

```bash
kubectl apply -f application-crd.yaml
```

### Step 2 -- Create Resources and View Output

```yaml
# sample-apps.yaml
apiVersion: platform.example.com/v1
kind: Application
metadata:
  name: web-frontend
spec:
  image: nginx:1.27
  replicas: 3
  port: 80
  environment: production
---
apiVersion: platform.example.com/v1
kind: Application
metadata:
  name: api-service
spec:
  image: busybox:1.37
  replicas: 2
  port: 8080
  environment: staging
```

```bash
kubectl apply -f sample-apps.yaml

# Default output shows printer columns
kubectl get applications
# NAME            IMAGE          REPLICAS   READY   STATUS   AGE
# web-frontend    nginx:1.27     3                           10s
# api-service     busybox:1.37   2                           10s

# Wide output includes priority=1 columns
kubectl get applications -o wide
# NAME            IMAGE          REPLICAS   READY   STATUS   ENDPOINT   AGE

# Short names work
kubectl get app
kubectl get apps
```

### Step 3 -- Simulate Status Updates

```bash
# Use kubectl proxy to update status (simulating what a controller would do)
kubectl proxy &
PID=$!

curl -s -X PUT http://localhost:8001/apis/platform.example.com/v1/namespaces/default/applications/web-frontend/status \
  -H "Content-Type: application/json" \
  -d "$(kubectl get application web-frontend -o json | jq '.status = {"phase": "Running", "readyReplicas": "3/3", "endpoint": "web-frontend.default:80"}')"

curl -s -X PUT http://localhost:8001/apis/platform.example.com/v1/namespaces/default/applications/api-service/status \
  -H "Content-Type: application/json" \
  -d "$(kubectl get application api-service -o json | jq '.status = {"phase": "Running", "readyReplicas": "2/2", "endpoint": "api-service.default:8080"}')"

kill $PID

# Now the output shows status
kubectl get app
# NAME            IMAGE          REPLICAS   READY   STATUS    AGE
# web-frontend    nginx:1.27     3          3/3     Running   1m
# api-service     busybox:1.37   2          2/2     Running   1m

kubectl get app -o wide
# Shows Endpoint column too
```

### Step 4 -- Categories

```bash
# Applications are in the "all" category, so they appear in kubectl get all
kubectl get all
# Shows pods, services, deployments, AND applications

# Custom category grouping
kubectl get platform
# Shows all resources in the "platform" category
```

## Verify

```bash
# Printer columns display correctly
kubectl get app web-frontend \
  -o custom-columns='NAME:.metadata.name,IMAGE:.spec.image,REPLICAS:.spec.replicas'

# Short name works
kubectl get app -o name | head -2

# Category includes applications
kubectl api-resources --categories=all | grep applications
kubectl api-resources --categories=platform | grep applications
```

## Cleanup

```bash
kubectl delete applications --all
kubectl delete crd applications.platform.example.com
```

## Reference

- [Additional Printer Columns](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#additional-printer-columns)
- [Short Names and Categories](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#short-names-and-categories)
