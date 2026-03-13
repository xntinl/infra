# Exercise 5: Kyverno Generate Policies: Auto-Create Resources

<!--
difficulty: intermediate
concepts: [kyverno, generate, synchronize, trigger-resource, networkpolicy, resourcequota, limitrange]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [13-policy-engines/02-kyverno-basics]
-->

## Introduction

Kyverno generate policies automatically create Kubernetes resources in response to events. The most common pattern is creating default resources when a new namespace is created: NetworkPolicies, ResourceQuotas, LimitRanges, and RoleBindings. The `synchronize` option keeps generated resources in sync with the policy -- if the policy changes, all generated resources are updated.

## Step-by-Step

### 1. Generate a default deny NetworkPolicy for every new namespace

```yaml
# policy-generate-netpol.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: generate-default-deny-netpol
spec:
  rules:
    - name: default-deny-ingress
      match:
        any:
          - resources:
              kinds:
                - Namespace
      exclude:
        any:
          - resources:
              names:
                - kube-system
                - kube-public
                - kube-node-lease
                - kyverno
      generate:
        synchronize: true          # keep in sync with policy changes
        apiVersion: networking.k8s.io/v1
        kind: NetworkPolicy
        name: default-deny-ingress
        namespace: "{{request.object.metadata.name}}"
        data:
          spec:
            podSelector: {}        # matches all pods in namespace
            policyTypes:
              - Ingress            # deny all ingress by default
```

### 2. Generate a ResourceQuota for every new namespace

```yaml
# policy-generate-quota.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: generate-resource-quota
spec:
  rules:
    - name: default-quota
      match:
        any:
          - resources:
              kinds:
                - Namespace
      exclude:
        any:
          - resources:
              names:
                - kube-system
                - kube-public
                - kube-node-lease
                - kyverno
      generate:
        synchronize: true
        apiVersion: v1
        kind: ResourceQuota
        name: default-quota
        namespace: "{{request.object.metadata.name}}"
        data:
          spec:
            hard:
              pods: "20"
              services: "10"
              configmaps: "30"
              secrets: "20"
              requests.cpu: "4"
              requests.memory: "8Gi"
              limits.cpu: "8"
              limits.memory: "16Gi"
```

### 3. Generate a LimitRange for every new namespace

```yaml
# policy-generate-limitrange.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: generate-limit-range
spec:
  rules:
    - name: default-limits
      match:
        any:
          - resources:
              kinds:
                - Namespace
      exclude:
        any:
          - resources:
              names:
                - kube-system
                - kube-public
                - kube-node-lease
                - kyverno
      generate:
        synchronize: true
        apiVersion: v1
        kind: LimitRange
        name: default-limits
        namespace: "{{request.object.metadata.name}}"
        data:
          spec:
            limits:
              - type: Container
                default:
                  cpu: "500m"
                  memory: "256Mi"
                defaultRequest:
                  cpu: "100m"
                  memory: "128Mi"
                max:
                  cpu: "2"
                  memory: "2Gi"
```

### 4. Generate RBAC bindings based on namespace labels

```yaml
# policy-generate-rbac.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: generate-team-rbac
spec:
  rules:
    - name: team-admin-binding
      match:
        any:
          - resources:
              kinds:
                - Namespace
              selector:
                matchExpressions:
                  - key: team
                    operator: Exists
      generate:
        synchronize: true
        apiVersion: rbac.authorization.k8s.io/v1
        kind: RoleBinding
        name: team-admin
        namespace: "{{request.object.metadata.name}}"
        data:
          subjects:
            - kind: Group
              name: "team-{{request.object.metadata.labels.team}}"
              apiGroup: rbac.authorization.k8s.io
          roleRef:
            kind: ClusterRole
            name: admin
            apiGroup: rbac.authorization.k8s.io
```

### 5. Apply and test

```bash
kubectl apply -f policy-generate-netpol.yaml
kubectl apply -f policy-generate-quota.yaml
kubectl apply -f policy-generate-limitrange.yaml
kubectl apply -f policy-generate-rbac.yaml

# Create a new namespace
kubectl create namespace test-generated

# Create a namespace with team label
kubectl create namespace team-alpha
kubectl label namespace team-alpha team=alpha
```

## Verify

```bash
# Check generated resources in test-generated namespace
kubectl get networkpolicy -n test-generated
# Expected: default-deny-ingress

kubectl get resourcequota -n test-generated
# Expected: default-quota

kubectl get limitrange -n test-generated
# Expected: default-limits

# Check team-specific RBAC generation
kubectl get rolebinding -n team-alpha
# Expected: team-admin (binding to group team-alpha)

# Verify synchronize: delete the NetworkPolicy and it will be recreated
kubectl delete networkpolicy default-deny-ingress -n test-generated
sleep 5
kubectl get networkpolicy -n test-generated
# Expected: default-deny-ingress is recreated

# Check policy reports
kubectl get clusterpolicy generate-default-deny-netpol -o yaml | grep -A5 status
```

## Cleanup

```bash
kubectl delete namespace test-generated team-alpha
kubectl delete clusterpolicy generate-default-deny-netpol \
  generate-resource-quota generate-limit-range generate-team-rbac
```

## What's Next

The next exercise covers **Gatekeeper Mutations and Auto-Injection** -- Gatekeeper's mutation webhooks for modifying resources on admission.
