# Namespace Management and Lifecycle

<!--
difficulty: basic
concepts: [namespaces, resource-quotas, limit-ranges, labels, annotations, finalizers]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [kubectl-basics]
-->

## Overview

Namespaces provide logical isolation within a Kubernetes cluster. They scope resource names, enable access control boundaries, and allow teams to set quotas and limits independently. This exercise covers namespace creation, resource quotas, limit ranges, and lifecycle management.

## Why This Matters

In multi-team or multi-environment clusters, namespaces are the primary mechanism for dividing cluster resources. Properly configured namespaces with quotas prevent a single team from consuming all cluster capacity, and limit ranges ensure every pod gets sensible resource defaults.

## Step-by-Step Instructions

### Step 1 -- Create Namespaces

```yaml
# team-namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-alpha
  labels:
    team: alpha            # label for policy and network targeting
    environment: dev       # environment classification
  annotations:
    description: "Team Alpha development namespace"
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-beta
  labels:
    team: beta
    environment: dev
  annotations:
    description: "Team Beta development namespace"
```

```bash
kubectl apply -f team-namespaces.yaml

# Verify namespaces exist
kubectl get namespaces --show-labels
```

### Step 2 -- Set Resource Quotas

Resource quotas limit the total resources a namespace can consume.

```yaml
# quota-alpha.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: compute-quota
  namespace: team-alpha
spec:
  hard:
    requests.cpu: "2"          # total CPU requests across all pods
    requests.memory: 4Gi       # total memory requests
    limits.cpu: "4"            # total CPU limits
    limits.memory: 8Gi         # total memory limits
    pods: "20"                 # maximum number of pods
    services: "10"             # maximum number of services
    configmaps: "20"           # maximum number of ConfigMaps
    persistentvolumeclaims: "5"
```

```bash
kubectl apply -f quota-alpha.yaml
kubectl describe resourcequota compute-quota -n team-alpha
```

### Step 3 -- Set Limit Ranges

Limit ranges set default resource requests/limits for containers that do not specify them.

```yaml
# limitrange-alpha.yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: default-limits
  namespace: team-alpha
spec:
  limits:
    - type: Container
      default:               # default limits applied when not specified
        cpu: 200m
        memory: 256Mi
      defaultRequest:         # default requests applied when not specified
        cpu: 50m
        memory: 64Mi
      min:                    # minimum allowed values
        cpu: 10m
        memory: 16Mi
      max:                    # maximum allowed values
        cpu: "1"
        memory: 1Gi
    - type: Pod
      max:
        cpu: "2"
        memory: 2Gi
```

```bash
kubectl apply -f limitrange-alpha.yaml
kubectl describe limitrange default-limits -n team-alpha
```

### Step 4 -- Test Quota Enforcement

```bash
# This pod should succeed (within limits)
kubectl run test-pod --image=nginx:1.27 -n team-alpha \
  --requests='cpu=50m,memory=64Mi' --limits='cpu=100m,memory=128Mi'

# Verify it received the quota allocation
kubectl describe resourcequota compute-quota -n team-alpha

# Try to exceed the quota by creating many pods
kubectl create deployment quota-test --image=nginx:1.27 \
  --replicas=25 -n team-alpha
# Some replicas will fail to schedule -- check events
kubectl get events -n team-alpha --field-selector reason=FailedCreate
```

### Step 5 -- Test LimitRange Defaults

```bash
# Create a pod without specifying resources
kubectl run no-resources --image=busybox:1.37 -n team-alpha \
  --command -- sleep 3600

# Check that default requests and limits were injected
kubectl get pod no-resources -n team-alpha -o yaml | grep -A 6 resources
```

### Step 6 -- Namespace Lifecycle and Deletion

```bash
# Deleting a namespace removes ALL resources inside it
kubectl delete namespace team-beta

# Verify everything inside was deleted
kubectl get all -n team-beta  # should return "No resources found"
```

If a namespace is stuck in `Terminating`, it is usually because a finalizer is blocking deletion.

```bash
# Check for finalizers on a stuck namespace
kubectl get namespace <stuck-ns> -o jsonpath='{.spec.finalizers}'

# As a last resort, remove the finalizer (use with caution)
kubectl get namespace <stuck-ns> -o json \
  | jq '.spec.finalizers = []' \
  | kubectl replace --raw "/api/v1/namespaces/<stuck-ns>/finalize" -f -
```

## Common Mistakes

1. **Creating pods without resource requests when a ResourceQuota exists** -- pods are rejected unless they specify requests/limits (unless a LimitRange provides defaults).
2. **Deleting a namespace by accident** -- namespace deletion is cascading and irreversible. Always double-check before running `kubectl delete namespace`.
3. **Forgetting that ResourceQuota counts include terminating pods** -- pods in `Terminating` state still consume quota until fully removed.
4. **Not labeling namespaces** -- unlabeled namespaces cannot be targeted by NetworkPolicies or admission rules that use namespace selectors.

## Verify

```bash
# Namespace exists with correct labels
kubectl get namespace team-alpha --show-labels

# ResourceQuota is enforced
kubectl describe resourcequota compute-quota -n team-alpha

# LimitRange defaults are applied
kubectl get pod no-resources -n team-alpha \
  -o jsonpath='{.spec.containers[0].resources.requests.cpu}'
# Expected: 50m

# Pod count is tracked
kubectl get resourcequota compute-quota -n team-alpha \
  -o jsonpath='{.status.used.pods}'
```

## Cleanup

```bash
kubectl delete namespace team-alpha
```

## What's Next

- **Exercise 07** -- Use node maintenance workflows with cordon and drain
- **Exercise 08** -- Configure API server audit logging to track namespace access

## Summary

- Namespaces provide logical isolation for names, RBAC, quotas, and network policies
- ResourceQuotas cap the total resources (CPU, memory, object counts) a namespace can consume
- LimitRanges inject default resource requests and limits into containers that omit them
- A ResourceQuota requires pods to specify resource requests and limits (or a LimitRange to provide defaults)
- Namespace deletion is cascading -- all resources inside are removed
- Labels on namespaces enable targeting by NetworkPolicies and admission webhooks

## Reference

- [Namespaces](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/)
- [Resource Quotas](https://kubernetes.io/docs/concepts/policy/resource-quotas/)
- [Limit Ranges](https://kubernetes.io/docs/concepts/policy/limit-range/)

## Additional Resources

- [Namespaces Walkthrough](https://kubernetes.io/docs/tasks/administer-cluster/namespaces-walkthrough/)
- [Configure Default CPU Requests and Limits](https://kubernetes.io/docs/tasks/administer-cluster/manage-resources/cpu-default-namespace/)
