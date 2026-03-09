# The Operator Pattern: Concepts and Architecture

<!--
difficulty: intermediate
concepts: [operator-pattern, reconciliation-loop, controller, informers, work-queue, level-triggered, desired-state]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-crd-basics, 02-crd-structural-schemas]
-->

## Overview

The operator pattern combines a CRD (defining what the user wants) with a controller (making it happen). A controller watches for changes to custom resources and reconciles the actual state of the system with the desired state declared in the resource spec. This exercise explains the pattern conceptually and walks through a simple operator implemented as a shell script to demystify the reconciliation loop.

## Why This Matters

Operators encode operational knowledge into software. Instead of documenting runbooks for deploying databases, managing certificates, or configuring monitoring, an operator automates these tasks. Understanding the pattern is prerequisite to building operators with frameworks like Kubebuilder or Operator SDK.

## Step-by-Step Instructions

### Step 1 -- Understand the Reconciliation Loop

```
┌──────────────────────────────────────────────────────────────┐
│                   Operator Architecture                       │
│                                                                │
│   User creates/updates       API Server stores                │
│   Custom Resource ──────────▶ in etcd                         │
│                                    │                           │
│                                    ▼                           │
│                              Informer/Watch                   │
│                                    │                           │
│                                    ▼                           │
│                            ┌──────────────┐                   │
│                            │  Work Queue   │                   │
│                            └──────┬───────┘                   │
│                                   │                            │
│                                   ▼                            │
│                     ┌─────────────────────────┐               │
│                     │   Reconcile Function     │               │
│                     │                           │               │
│                     │  1. Get current state     │               │
│                     │  2. Compare with desired  │               │
│                     │  3. Take corrective action│               │
│                     │  4. Update status         │               │
│                     └─────────────────────────┘               │
│                                   │                            │
│                            Creates/Updates                    │
│                          Deployments, Services,               │
│                          ConfigMaps, etc.                      │
└──────────────────────────────────────────────────────────────┘
```

Key principles:
- **Level-triggered, not edge-triggered** -- the reconciler looks at the current state, not the change that triggered it. This makes it idempotent and self-healing.
- **Desired state vs actual state** -- the spec declares what should exist; the controller makes it so.
- **Status reflects reality** -- the status subresource reports what actually exists, not what was requested.
- **Idempotent** -- running reconcile multiple times with the same input produces the same result.

### Step 2 -- Create the CRD

```yaml
# simpleapp-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: simpleapps.demo.example.com
spec:
  group: demo.example.com
  names:
    kind: SimpleApp
    plural: simpleapps
    singular: simpleapp
    shortNames: [sa]
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
        - name: Image
          type: string
          jsonPath: .spec.image
        - name: Replicas
          type: integer
          jsonPath: .spec.replicas
        - name: Status
          type: string
          jsonPath: .status.phase
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
                  maximum: 10
                  default: 1
                port:
                  type: integer
                  default: 80
            status:
              type: object
              properties:
                phase:
                  type: string
                availableReplicas:
                  type: integer
      subresources:
        status: {}
```

```bash
kubectl apply -f simpleapp-crd.yaml
```

### Step 3 -- Build a Shell-Based Controller

This simplified controller demonstrates the reconciliation loop. Real operators use Go with controller-runtime, but the logic is the same.

```bash
# simpleapp-controller.sh
#!/bin/bash
# A minimal operator in bash -- for learning purposes only

NAMESPACE="default"
API="apis/demo.example.com/v1/namespaces/$NAMESPACE/simpleapps"

echo "Starting SimpleApp controller..."

while true; do
  # List all SimpleApp resources
  APPS=$(kubectl get simpleapps -n $NAMESPACE -o json 2>/dev/null)

  # For each SimpleApp, reconcile
  echo "$APPS" | jq -c '.items[]' 2>/dev/null | while read -r app; do
    NAME=$(echo "$app" | jq -r '.metadata.name')
    IMAGE=$(echo "$app" | jq -r '.spec.image')
    REPLICAS=$(echo "$app" | jq -r '.spec.replicas')
    PORT=$(echo "$app" | jq -r '.spec.port')

    echo "Reconciling SimpleApp: $NAME (image=$IMAGE, replicas=$REPLICAS)"

    # Check if deployment exists
    if kubectl get deployment "$NAME" -n $NAMESPACE &>/dev/null; then
      # Update if spec changed
      CURRENT_IMAGE=$(kubectl get deployment "$NAME" -n $NAMESPACE -o jsonpath='{.spec.template.spec.containers[0].image}')
      CURRENT_REPLICAS=$(kubectl get deployment "$NAME" -n $NAMESPACE -o jsonpath='{.spec.replicas}')

      if [ "$CURRENT_IMAGE" != "$IMAGE" ] || [ "$CURRENT_REPLICAS" != "$REPLICAS" ]; then
        echo "  Updating deployment $NAME"
        kubectl set image deployment/$NAME app=$IMAGE -n $NAMESPACE
        kubectl scale deployment/$NAME --replicas=$REPLICAS -n $NAMESPACE
      fi
    else
      # Create deployment
      echo "  Creating deployment $NAME"
      kubectl create deployment "$NAME" --image="$IMAGE" --replicas="$REPLICAS" -n $NAMESPACE
    fi

    # Ensure service exists
    if ! kubectl get service "$NAME" -n $NAMESPACE &>/dev/null; then
      echo "  Creating service $NAME"
      kubectl expose deployment "$NAME" --port=$PORT --target-port=$PORT -n $NAMESPACE
    fi

    # Update status
    AVAILABLE=$(kubectl get deployment "$NAME" -n $NAMESPACE -o jsonpath='{.status.availableReplicas}' 2>/dev/null)
    AVAILABLE=${AVAILABLE:-0}

    if [ "$AVAILABLE" -eq "$REPLICAS" ]; then
      PHASE="Running"
    else
      PHASE="Progressing"
    fi

    kubectl proxy --port=8099 &>/dev/null &
    PROXY_PID=$!
    sleep 1
    curl -s -X PUT "http://localhost:8099/$API/$NAME/status" \
      -H "Content-Type: application/json" \
      -d "$(echo "$app" | jq ".status = {\"phase\": \"$PHASE\", \"availableReplicas\": $AVAILABLE}")" > /dev/null
    kill $PROXY_PID 2>/dev/null
  done

  sleep 10  # Poll interval
done
```

### Step 4 -- Test the Controller

```bash
# In one terminal, run the controller
chmod +x simpleapp-controller.sh
./simpleapp-controller.sh

# In another terminal, create a SimpleApp
kubectl apply -f - <<'EOF'
apiVersion: demo.example.com/v1
kind: SimpleApp
metadata:
  name: my-web
spec:
  image: nginx:1.27
  replicas: 3
  port: 80
EOF

# Wait for the controller to reconcile
sleep 15

# Verify the controller created the deployment and service
kubectl get deployment my-web
kubectl get service my-web
kubectl get simpleapp my-web

# Update the SimpleApp
kubectl patch simpleapp my-web --type=merge -p '{"spec":{"replicas":5}}'

# Wait for reconciliation
sleep 15
kubectl get deployment my-web  # should show 5 replicas
```

### Step 5 -- Observe Controller Behavior

```bash
# Delete the deployment (simulate drift) -- the controller should recreate it
kubectl delete deployment my-web

# Wait for the next reconciliation cycle
sleep 15
kubectl get deployment my-web  # recreated by the controller
```

## Verify

```bash
# SimpleApp has correct status
kubectl get simpleapp my-web
# Should show Image, Replicas, and Status columns

# Deployment and service exist
kubectl get deployment my-web
kubectl get service my-web
```

## Cleanup

```bash
# Stop the controller (Ctrl+C in its terminal)
kubectl delete simpleapp my-web
kubectl delete deployment my-web
kubectl delete service my-web
kubectl delete crd simpleapps.demo.example.com
```

## Reference

- [Operator Pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
- [Custom Controllers](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#custom-controllers)
- [Controller Runtime](https://github.com/kubernetes-sigs/controller-runtime)
