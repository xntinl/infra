# Exercise 1: Security Contexts and Pod Security Standards

<!--
difficulty: basic
concepts: [security-context, pod-security-context, runasnonroot, readonlyrootfilesystem, capabilities, pod-security-standards, pod-security-admission]
tools: [kubectl]
estimated_time: 30m
bloom_level: understand
prerequisites: []
-->

## Introduction

Every container in Kubernetes runs as a Linux process with specific privileges. **Security Contexts** let you control those privileges at the pod and container level. Kubernetes also defines three **Pod Security Standards** -- Privileged, Baseline, and Restricted -- that classify pods by their security posture. The **Pod Security Admission** controller enforces these standards at the namespace level.

Key concepts:

- **SecurityContext** (container-level) -- `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation`, `capabilities`
- **PodSecurityContext** (pod-level) -- `runAsUser`, `runAsGroup`, `fsGroup`, `seccompProfile`
- **Pod Security Standards** -- Privileged (unrestricted), Baseline (prevents known escalations), Restricted (hardened)
- **Pod Security Admission** -- built-in controller that enforces standards via namespace labels

## Why This Matters

A container running as root with a writable filesystem and all Linux capabilities is one exploit away from owning the node. Security contexts are the first line of defense: they restrict what a container can do even if the application code is compromised.

## Step-by-Step

### 1. Create a namespace with Restricted enforcement

```yaml
# namespace-restricted.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: psa-restricted
  labels:
    # enforce = reject non-compliant pods
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: latest
    # audit = log violations
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/audit-version: latest
    # warn = show warnings to the user
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/warn-version: latest
```

### 2. Create a Privileged namespace (for comparison)

```yaml
# namespace-privileged.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: psa-privileged
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/enforce-version: latest
```

### 3. Pod that passes the Restricted standard

```yaml
# pod-restricted.yaml
apiVersion: v1
kind: Pod
metadata:
  name: secure-app
  namespace: psa-restricted
  labels:
    app: secure-app
spec:
  securityContext:
    runAsNonRoot: true              # container must not run as UID 0
    runAsUser: 1000                 # explicit non-root UID
    runAsGroup: 1000                # explicit GID
    fsGroup: 1000                   # group ownership for mounted volumes
    seccompProfile:
      type: RuntimeDefault          # required by Restricted standard
  containers:
    - name: app
      image: nginx:1.27
      ports:
        - containerPort: 8080
      securityContext:
        allowPrivilegeEscalation: false   # prevents setuid binaries
        readOnlyRootFilesystem: true      # filesystem is immutable
        capabilities:
          drop:
            - ALL                         # remove all Linux capabilities
      volumeMounts:
        - name: tmp
          mountPath: /tmp                 # writable temp directory
        - name: cache
          mountPath: /var/cache/nginx     # nginx needs to write cache
        - name: run
          mountPath: /var/run             # nginx PID file location
  volumes:
    - name: tmp
      emptyDir: {}
    - name: cache
      emptyDir: {}
    - name: run
      emptyDir: {}
```

### 4. Privileged pod (will be rejected in Restricted namespace)

```yaml
# pod-privileged.yaml
apiVersion: v1
kind: Pod
metadata:
  name: privileged-app
  namespace: psa-restricted
  labels:
    app: privileged-app
spec:
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        privileged: true           # full host access -- extremely dangerous
        runAsUser: 0               # running as root
```

### 5. Pod that violates Restricted but passes Baseline

```yaml
# pod-baseline.yaml
apiVersion: v1
kind: Pod
metadata:
  name: baseline-app
  namespace: psa-restricted
  labels:
    app: baseline-app
spec:
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        allowPrivilegeEscalation: true   # violates Restricted
```

### 6. Same privileged pod in the Privileged namespace (will succeed)

```yaml
# pod-privileged-allowed.yaml
apiVersion: v1
kind: Pod
metadata:
  name: privileged-app
  namespace: psa-privileged
  labels:
    app: privileged-app
spec:
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        privileged: true
        runAsUser: 0
```

### 7. Apply and test

```bash
kubectl apply -f namespace-restricted.yaml
kubectl apply -f namespace-privileged.yaml
kubectl apply -f pod-restricted.yaml
```

## Common Mistakes

1. **Forgetting `seccompProfile: RuntimeDefault`** -- The Restricted standard requires it. Without it, your pod will be rejected even if everything else is correct.
2. **Not providing writable directories** -- With `readOnlyRootFilesystem: true`, applications that write temp files will crash. Use `emptyDir` volumes for `/tmp`, cache directories, and PID file locations.
3. **Setting `runAsNonRoot: true` without `runAsUser`** -- If the container image defaults to root and you do not set `runAsUser`, the pod will fail at runtime with a security context error.
4. **Dropping ALL capabilities but needing NET_BIND_SERVICE** -- If your app binds to a port below 1024, you must add back `NET_BIND_SERVICE` after dropping ALL.

## Verify

```bash
# Confirm namespace labels
kubectl get ns psa-restricted --show-labels
kubectl get ns psa-privileged --show-labels

# Restricted pod should be running
kubectl get pods -n psa-restricted

# Inspect the security context
kubectl get pod secure-app -n psa-restricted \
  -o jsonpath='{.spec.securityContext}' | python3 -m json.tool

# Inspect container-level security context
kubectl get pod secure-app -n psa-restricted \
  -o jsonpath='{.spec.containers[0].securityContext}' | python3 -m json.tool

# Privileged pod MUST be rejected in restricted namespace
kubectl apply -f pod-privileged.yaml
# Expected: Error -- violates PodSecurity "restricted"

# Baseline pod MUST also be rejected in restricted namespace
kubectl apply -f pod-baseline.yaml
# Expected: Error -- violates PodSecurity "restricted"

# Privileged pod succeeds in privileged namespace
kubectl apply -f pod-privileged-allowed.yaml
kubectl get pods -n psa-privileged

# Verify the user is not root inside the restricted pod
kubectl exec -n psa-restricted secure-app -- id
# Expected: uid=1000 gid=1000

# Verify root filesystem is read-only
kubectl exec -n psa-restricted secure-app -- touch /test-file 2>&1
# Expected: "Read-only file system"

# Verify /tmp is writable via emptyDir
kubectl exec -n psa-restricted secure-app -- touch /tmp/test-file
kubectl exec -n psa-restricted secure-app -- ls /tmp/test-file
```

## Cleanup

```bash
kubectl delete namespace psa-restricted psa-privileged
```

## What's Next

In the next exercise you will learn about the **Pod Security Admission Controller** in more detail -- configuring different enforcement modes per namespace and handling exemptions.

## Summary

- **SecurityContext** at the container level controls privileges: runAsNonRoot, readOnlyRootFilesystem, allowPrivilegeEscalation, capabilities.
- **PodSecurityContext** at the pod level applies to all containers: runAsUser, runAsGroup, fsGroup, seccompProfile.
- The **Restricted** Pod Security Standard is the most secure: requires non-root, read-only FS, dropped capabilities, and seccomp.
- **Pod Security Admission** enforces standards at the namespace level using labels.
- Use `emptyDir` volumes to provide writable directories when the root filesystem is read-only.
- Always `drop: ALL` capabilities and add back only what is strictly needed.

## Reference

- [Configure a Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)

## Additional Resources

- [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/)
- [Linux Capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html)
