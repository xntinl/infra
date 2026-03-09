# Exercise 4: Linux Capabilities: Add and Drop

<!--
difficulty: intermediate
concepts: [linux-capabilities, cap-drop-all, cap-add, net-bind-service, net-raw, sys-ptrace]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security]
-->

## Introduction

Linux capabilities break the monolithic root privilege into fine-grained units. Instead of giving a process full root access, you grant only the specific capabilities it needs. Kubernetes lets you `drop` capabilities (remove them) and `add` capabilities (grant specific ones). The security best practice is to `drop: ALL` and then add back only what is strictly required.

Common capabilities:
- **NET_BIND_SERVICE** -- bind to ports below 1024
- **NET_RAW** -- use raw sockets (ping, tcpdump)
- **SYS_PTRACE** -- trace/debug other processes
- **CHOWN** -- change file ownership
- **DAC_OVERRIDE** -- bypass file permission checks

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: caps-lab
```

### 2. Pod with all capabilities dropped

```yaml
# pod-no-caps.yaml
apiVersion: v1
kind: Pod
metadata:
  name: no-caps
  namespace: caps-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]            # remove every capability
```

### 3. Pod that needs NET_BIND_SERVICE

```yaml
# pod-net-bind.yaml
apiVersion: v1
kind: Pod
metadata:
  name: net-bind
  namespace: caps-lab
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
        capabilities:
          drop: ["ALL"]
          add: ["NET_BIND_SERVICE"]   # only this one capability added back
```

### 4. Pod for debugging with NET_RAW and SYS_PTRACE

```yaml
# pod-debug.yaml
apiVersion: v1
kind: Pod
metadata:
  name: debug-pod
  namespace: caps-lab
  labels:
    purpose: debugging
spec:
  securityContext:
    runAsUser: 0            # debugging often requires root
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: debug
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
          add:
            - NET_RAW          # enables ping
            - SYS_PTRACE       # enables strace, debugging
```

### 5. Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f pod-no-caps.yaml
kubectl apply -f pod-net-bind.yaml
kubectl apply -f pod-debug.yaml
```

## Spot the Bug

This container is supposed to be able to ping other pods, but ping fails with "Operation not permitted". The security context looks correct. What is wrong?

```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
    add: ["NET_BIND_SERVICE"]
```

<details>
<summary>Answer</summary>

Ping requires `NET_RAW`, not `NET_BIND_SERVICE`. `NET_BIND_SERVICE` only allows binding to ports below 1024. Change `add: ["NET_BIND_SERVICE"]` to `add: ["NET_RAW"]`.

</details>

## Verify

```bash
# Check capabilities of the no-caps pod (should show empty set)
kubectl exec -n caps-lab no-caps -- cat /proc/1/status | grep -i cap

# Ping should fail in no-caps pod (no NET_RAW)
kubectl exec -n caps-lab no-caps -- ping -c 1 127.0.0.1 2>&1
# Expected: Operation not permitted

# Ping should work in debug pod (has NET_RAW)
kubectl exec -n caps-lab debug-pod -- ping -c 1 127.0.0.1
# Expected: 1 packets transmitted, 1 received

# Check what capabilities debug-pod has
kubectl exec -n caps-lab debug-pod -- cat /proc/1/status | grep -i cap

# Compare effective capabilities between pods
kubectl get pod no-caps -n caps-lab \
  -o jsonpath='{.spec.containers[0].securityContext.capabilities}'
kubectl get pod debug-pod -n caps-lab \
  -o jsonpath='{.spec.containers[0].securityContext.capabilities}'
```

## Cleanup

```bash
kubectl delete namespace caps-lab
```

## What's Next

The next exercise covers **Seccomp Profiles** -- filtering which system calls a container is allowed to make.
