# Exercise 3: runAsNonRoot and Read-Only Root Filesystem

<!--
difficulty: intermediate
concepts: [runasnonroot, runasuser, readonlyrootfilesystem, emptydir, tmpfs, writable-paths]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security]
-->

## Introduction

Running containers as non-root and with a read-only root filesystem are two of the most impactful security hardening steps. This exercise covers practical patterns for making real applications work under these constraints, including handling applications that expect to write to specific paths.

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: nonroot-lab
```

### 2. Nginx configured for non-root with read-only filesystem

```yaml
# nginx-hardened.yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx-hardened
  namespace: nonroot-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 101         # nginx user in the official image
    runAsGroup: 101
    fsGroup: 101
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
      ports:
        - containerPort: 8080
      volumeMounts:
        - name: tmp
          mountPath: /tmp
        - name: cache
          mountPath: /var/cache/nginx
        - name: run
          mountPath: /var/run
        - name: nginx-conf
          mountPath: /etc/nginx/conf.d
      env:
        - name: NGINX_PORT
          value: "8080"      # non-privileged port (no NET_BIND_SERVICE needed)
  volumes:
    - name: tmp
      emptyDir: {}
    - name: cache
      emptyDir: {}
    - name: run
      emptyDir: {}
    - name: nginx-conf
      configMap:
        name: nginx-nonroot-conf
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-nonroot-conf
  namespace: nonroot-lab
data:
  default.conf: |
    server {
        listen 8080;
        location / {
            root /usr/share/nginx/html;
            index index.html;
        }
    }
```

### 3. Redis configured for non-root

```yaml
# redis-hardened.yaml
apiVersion: v1
kind: Pod
metadata:
  name: redis-hardened
  namespace: nonroot-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 999          # redis user
    runAsGroup: 999
    fsGroup: 999
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: redis
      image: redis:7
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: data
          mountPath: /data
        - name: tmp
          mountPath: /tmp
  volumes:
    - name: data
      emptyDir: {}
    - name: tmp
      emptyDir:
        medium: Memory        # tmpfs -- faster for temp files, limited to node memory
        sizeLimit: 64Mi
```

### 4. Pod that demonstrates common failure patterns

```yaml
# pod-broken.yaml
apiVersion: v1
kind: Pod
metadata:
  name: broken-pod
  namespace: nonroot-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      # Missing volumeMounts for /var/cache/nginx, /var/run, /tmp
      # This pod will CrashLoopBackOff because nginx cannot write
```

### 5. Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f nginx-hardened.yaml
kubectl apply -f redis-hardened.yaml
kubectl apply -f pod-broken.yaml
```

## Spot the Bug

This pod is supposed to run as non-root but keeps failing. Why?

```yaml
spec:
  securityContext:
    runAsNonRoot: true
  containers:
    - name: app
      image: nginx:1.27
      securityContext:
        readOnlyRootFilesystem: true
```

<details>
<summary>Answer</summary>

The official `nginx:1.27` image has `USER root` in its Dockerfile. Setting `runAsNonRoot: true` without specifying `runAsUser` causes a runtime error because the container process would start as root, which violates the constraint. Fix: add `runAsUser: 101` (the nginx user).

</details>

## Verify

```bash
# Hardened nginx should be running
kubectl get pod nginx-hardened -n nonroot-lab
kubectl exec -n nonroot-lab nginx-hardened -- id
# Expected: uid=101 gid=101

# Root filesystem is read-only
kubectl exec -n nonroot-lab nginx-hardened -- touch /etc/test 2>&1
# Expected: Read-only file system

# Writable directories work
kubectl exec -n nonroot-lab nginx-hardened -- touch /tmp/test
kubectl exec -n nonroot-lab nginx-hardened -- touch /var/cache/nginx/test

# Redis is running as non-root
kubectl exec -n nonroot-lab redis-hardened -- id
# Expected: uid=999 gid=999

# Broken pod should be in CrashLoopBackOff
kubectl get pod broken-pod -n nonroot-lab
kubectl logs broken-pod -n nonroot-lab
```

## Cleanup

```bash
kubectl delete namespace nonroot-lab
```

## What's Next

The next exercise covers **Linux Capabilities: Add and Drop** -- fine-grained control over what privileged operations a container can perform.
