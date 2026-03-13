# Exercise 4: Kyverno Mutate Policies: Defaults and Injection

<!--
difficulty: intermediate
concepts: [kyverno, mutate, strategic-merge-patch, json-patch, default-values, sidecar-injection]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [13-policy-engines/02-kyverno-basics]
-->

## Introduction

Kyverno mutate policies modify resources during admission, before they are stored in etcd. Common use cases include injecting default labels, adding sidecar containers, setting security defaults, and adding resource limits. Kyverno supports two mutation methods:

- **Strategic Merge Patch** (patchStrategicMerge) -- overlay-style patching, natural for Kubernetes resources
- **JSON Patch** (patchesJson6902) -- RFC 6902 operations (add, replace, remove) for precise modifications

## Step-by-Step

### 1. Mutate policy: add default labels

```yaml
# policy-add-labels.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: add-default-labels
spec:
  rules:
    - name: add-managed-by
      match:
        any:
          - resources:
              kinds:
                - Pod
      mutate:
        patchStrategicMerge:
          metadata:
            labels:
              +(app.kubernetes.io/managed-by): "kyverno"   # + means "add if not present"
              environment: "{{request.namespace}}"           # dynamic value from context
```

### 2. Mutate policy: set security defaults

```yaml
# policy-security-defaults.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: set-security-defaults
spec:
  rules:
    - name: set-security-context
      match:
        any:
          - resources:
              kinds:
                - Pod
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - kyverno
      mutate:
        patchStrategicMerge:
          spec:
            securityContext:
              +(runAsNonRoot): true              # add if not set
              +(seccompProfile):
                type: RuntimeDefault
            containers:
              - (name): "*"                      # match all containers
                securityContext:
                  +(allowPrivilegeEscalation): false
                  +(readOnlyRootFilesystem): true
```

### 3. Mutate policy: inject a sidecar container

```yaml
# policy-inject-sidecar.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: inject-logging-sidecar
spec:
  rules:
    - name: inject-sidecar
      match:
        any:
          - resources:
              kinds:
                - Pod
              selector:
                matchLabels:
                  logging: "enabled"             # only pods with this label
      mutate:
        patchesJson6902: |-
          - op: add
            path: /spec/containers/-
            value:
              name: log-collector
              image: busybox:1.37
              command: ["sh", "-c", "tail -f /var/log/app/*.log 2>/dev/null || sleep 3600"]
              volumeMounts:
                - name: shared-logs
                  mountPath: /var/log/app
              resources:
                limits:
                  cpu: "50m"
                  memory: "64Mi"
          - op: add
            path: /spec/volumes/-
            value:
              name: shared-logs
              emptyDir: {}
```

### 4. Mutate policy: add default resource limits using JSON Patch

```yaml
# policy-default-limits.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: add-default-limits
spec:
  rules:
    - name: add-limits
      match:
        any:
          - resources:
              kinds:
                - Pod
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - kyverno
      mutate:
        patchStrategicMerge:
          spec:
            containers:
              - (name): "*"
                resources:
                  +(limits):
                    cpu: "500m"
                    memory: "256Mi"
                  +(requests):
                    cpu: "100m"
                    memory: "128Mi"
```

### 5. Apply and test

```bash
kubectl apply -f policy-add-labels.yaml
kubectl apply -f policy-security-defaults.yaml
kubectl apply -f policy-default-limits.yaml
kubectl apply -f policy-inject-sidecar.yaml

# Create a plain pod and see what gets mutated
kubectl run test-mutate --image=nginx:1.27 \
  --labels=team=dev \
  --dry-run=server -o yaml
# Look for: added labels, security context defaults, resource limits

# Create a pod with the logging label to trigger sidecar injection
kubectl run test-sidecar --image=nginx:1.27 \
  --labels="team=dev,logging=enabled" \
  --dry-run=server -o yaml
# Look for: log-collector sidecar container and shared-logs volume
```

## Spot the Bug

This mutation is supposed to add a label only if it does not already exist, but it always overwrites the user's value. Why?

```yaml
mutate:
  patchStrategicMerge:
    metadata:
      labels:
        app.kubernetes.io/managed-by: "kyverno"
```

<details>
<summary>Answer</summary>

Missing the `+` prefix. Without `+(...)`, the patch always sets the value. Use `+(app.kubernetes.io/managed-by): "kyverno"` to add only if the key is absent.

</details>

## Verify

```bash
# Check that mutations are applied
kubectl run verify-mutate --image=nginx:1.27 --labels=team=dev
kubectl get pod verify-mutate -o yaml | grep -A5 labels
kubectl get pod verify-mutate -o yaml | grep -A10 securityContext
kubectl get pod verify-mutate -o yaml | grep -A5 limits

# Check policy status
kubectl get clusterpolicy

# Clean up test pod
kubectl delete pod verify-mutate
```

## Cleanup

```bash
kubectl delete clusterpolicy add-default-labels set-security-defaults \
  add-default-limits inject-logging-sidecar
```

## What's Next

The next exercise covers **Kyverno Generate Policies** -- automatically creating resources like NetworkPolicies and ResourceQuotas when a namespace is created.
