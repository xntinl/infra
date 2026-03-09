# Exercise 2: Pod Security Admission Controller

<!--
difficulty: basic
concepts: [pod-security-admission, enforce, audit, warn, exemptions, namespace-labels]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security]
-->

## Introduction

The Pod Security Admission (PSA) controller is built into Kubernetes and replaces the deprecated PodSecurityPolicy. It enforces Pod Security Standards at the namespace level using three modes:

- **enforce** -- rejects pods that violate the standard
- **audit** -- allows the pod but logs the violation in the API server audit log
- **warn** -- allows the pod but returns a warning to the user

You can mix different standards across modes. For example, enforce `baseline` while warning on `restricted` to prepare for a stricter policy rollout.

## Why This Matters

PSA is the built-in, zero-dependency way to enforce pod security across your cluster. Unlike external tools (OPA, Kyverno), it requires no installation -- just namespace labels. Every cluster operator should know how to configure it.

## Step-by-Step

### 1. Create namespaces with different PSA configurations

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: psa-enforce-baseline
  labels:
    pod-security.kubernetes.io/enforce: baseline     # reject non-baseline pods
    pod-security.kubernetes.io/enforce-version: v1.31
    pod-security.kubernetes.io/warn: restricted       # warn if not restricted
    pod-security.kubernetes.io/warn-version: v1.31
---
apiVersion: v1
kind: Namespace
metadata:
  name: psa-audit-only
  labels:
    pod-security.kubernetes.io/enforce: privileged    # allow everything
    pod-security.kubernetes.io/audit: restricted      # log all violations
    pod-security.kubernetes.io/audit-version: v1.31
    pod-security.kubernetes.io/warn: restricted       # warn user
    pod-security.kubernetes.io/warn-version: v1.31
---
apiVersion: v1
kind: Namespace
metadata:
  name: psa-full-restricted
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: v1.31
```

### 2. Test pod -- passes Baseline but not Restricted

```yaml
# pod-baseline-only.yaml
apiVersion: v1
kind: Pod
metadata:
  name: baseline-pod
  labels:
    app: baseline
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      securityContext:
        allowPrivilegeEscalation: false   # passes Baseline
        # Missing: runAsNonRoot, capabilities drop, seccomp -- fails Restricted
```

### 3. Test pod -- passes Restricted

```yaml
# pod-restricted.yaml
apiVersion: v1
kind: Pod
metadata:
  name: restricted-pod
  labels:
    app: restricted
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: nginx
      image: nginx:1.27
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: tmp
          mountPath: /tmp
  volumes:
    - name: tmp
      emptyDir: {}
```

### 4. Test pod -- violates Baseline (privileged)

```yaml
# pod-privileged.yaml
apiVersion: v1
kind: Pod
metadata:
  name: privileged-pod
  labels:
    app: privileged
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      securityContext:
        privileged: true
```

### 5. Apply and test across namespaces

```bash
kubectl apply -f namespaces.yaml

# In enforce-baseline: baseline pod succeeds (with restricted warning)
kubectl apply -f pod-baseline-only.yaml -n psa-enforce-baseline
# Expected: created, but with warnings about restricted violations

# In enforce-baseline: privileged pod is REJECTED
kubectl apply -f pod-privileged.yaml -n psa-enforce-baseline
# Expected: Error

# In audit-only: everything is allowed, but warnings appear
kubectl apply -f pod-privileged.yaml -n psa-audit-only
# Expected: created, with warnings

# In full-restricted: only restricted pod succeeds
kubectl apply -f pod-restricted.yaml -n psa-full-restricted
# Expected: created

kubectl apply -f pod-baseline-only.yaml -n psa-full-restricted
# Expected: Error
```

## Common Mistakes

1. **Using `latest` version in production** -- Pin to a specific version like `v1.31` so behavior does not change on cluster upgrade.
2. **Forgetting that PSA applies to all pods in the namespace** -- System pods, operators, and monitoring agents must also comply. Plan exemptions or use a separate namespace.
3. **Applying enforce=restricted to kube-system** -- Many system components need elevated privileges. Never apply restrictive enforcement to system namespaces without exemptions.
4. **Not testing with warn/audit first** -- Always start with `warn` or `audit` mode before switching to `enforce` to discover violations without breaking workloads.

## Verify

```bash
# Check namespace labels
kubectl get ns psa-enforce-baseline --show-labels
kubectl get ns psa-audit-only --show-labels
kubectl get ns psa-full-restricted --show-labels

# Verify baseline pod is running in enforce-baseline
kubectl get pods -n psa-enforce-baseline

# Verify privileged pod is running in audit-only (allowed despite violations)
kubectl get pods -n psa-audit-only

# Verify restricted pod is running in full-restricted
kubectl get pods -n psa-full-restricted

# Dry-run to test without creating
kubectl apply -f pod-privileged.yaml -n psa-full-restricted --dry-run=server
# Expected: Error
```

## Cleanup

```bash
kubectl delete namespace psa-enforce-baseline psa-audit-only psa-full-restricted
```

## What's Next

In the next exercise you will dive deeper into **runAsNonRoot and Read-Only Root Filesystem** -- practical patterns for hardening real applications.

## Summary

- Pod Security Admission uses namespace labels to enforce Pod Security Standards.
- Three modes: `enforce` (reject), `audit` (log), `warn` (warning to user).
- Pin versions like `v1.31` instead of `latest` for predictable behavior.
- Start with `warn`/`audit` before moving to `enforce` in production.
- Different namespaces can have different security levels.
- PSA is built-in and requires no additional installation.

## Reference

- [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/)
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)

## Additional Resources

- [Migrate from PodSecurityPolicy to PSA](https://kubernetes.io/docs/tasks/configure-pod-container/migrate-from-psp/)
- [Enforce Pod Security Standards with Namespace Labels](https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-namespace-labels/)
